// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spec_tests

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/ziggy42/epsilon/epsilon"
	"github.com/ziggy42/epsilon/internal/wabt"
)

// specTestRunner manages the state and execution of a single spec test file.
type specTestRunner struct {
	t                  *testing.T
	wasmDict           map[string][]byte
	vm                 *epsilon.VM
	moduleInstanceMap  map[string]*epsilon.ModuleInstance
	lastModuleInstance *epsilon.ModuleInstance
	spectestImports    map[string]any
}

func newSpecRunner(t *testing.T, wasmDict map[string][]byte) *specTestRunner {
	importMemoryLimitMax := uint32(2)
	limits := epsilon.Limits{Min: 1, Max: &importMemoryLimitMax}
	memory := epsilon.NewMemory(epsilon.MemoryType{Limits: limits})
	tableLimitMax := uint32(20)
	table := epsilon.NewTable(epsilon.TableType{
		Limits:        epsilon.Limits{Min: 10, Max: &tableLimitMax},
		ReferenceType: epsilon.FuncRefType,
	})
	spectestImports := map[string]any{
		"global_i32": int32(666),
		"global_i64": int64(666),
		"global_f32": float32(666.6),
		"global_f64": float64(666.6),
		"table":      table,
		"memory":     memory,
		"print_i32": func(args ...any) []any {
			fmt.Printf("%d", args[0].(int32))
			return nil
		},
		"print_i64": func(args ...any) []any {
			fmt.Printf("%d", args[0].(int64))
			return nil
		},
		"print_f32": func(args ...any) []any {
			fmt.Printf("%f", args[0].(float32))
			return nil
		},
		"print_f64": func(args ...any) []any {
			fmt.Printf("%f", args[0].(float64))
			return nil
		},
		"print_i32_f32": func(args ...any) []any {
			fmt.Printf("%d %f", args[0].(int32), args[1].(float32))
			return nil
		},
		"print_i64_f64": func(args ...any) []any {
			fmt.Printf("%d %f", args[0].(int64), args[1].(float64))
			return nil
		},
		"print_f64_f64": func(args ...any) []any {
			fmt.Printf("%f %f", args[0].(float64), args[1].(float64))
			return nil
		},
		"print": func(args ...any) []any {
			fmt.Printf("Print called!")
			return nil
		},
	}

	return &specTestRunner{
		t:                 t,
		wasmDict:          wasmDict,
		vm:                epsilon.NewVM(),
		moduleInstanceMap: make(map[string]*epsilon.ModuleInstance),
		spectestImports:   spectestImports,
	}
}

func (r *specTestRunner) run(commands []wabt.Command) {
	for _, cmd := range commands {
		r.t.Logf("Line %d: executing command type: %s", cmd.Line, cmd.Type)
		switch cmd.Type {
		case "module":
			r.handleModule(cmd)
		case "assert_return":
			r.handleAssertReturn(cmd)
		case "assert_trap":
			r.handleAssertTrap(cmd)
		case "assert_uninstantiable":
			r.handleAssertUninstantiable(cmd)
		case "action":
			r.handleAction(cmd.Action)
		case "register":
			r.handleRegister(cmd)
		case "assert_exhaustion":
			r.handleAssertExhaustion(cmd)
		case "assert_invalid":
			r.handleAssertInvalid(cmd)
		case "assert_malformed":
			r.handleAssertMalformed(cmd)
		case "assert_unlinkable":
			r.handleAssertUnlinkable(cmd)
		default:
			r.fatalf(cmd.Line, "unknown command type: %s", cmd.Type)
		}
	}
}

func (r *specTestRunner) handleAssertExhaustion(cmd wabt.Command) {
	_, err := r.handleAction(cmd.Action)
	if err == nil {
		r.fatalf(cmd.Line, "expected call stack exhaustion, but got no error")
	}

	if err.Error() != "call stack exhausted" {
		r.fatalf(cmd.Line, "expected call stack exhaustion, but got: %v", err)
	}
}

func (r *specTestRunner) handleRegister(cmd wabt.Command) {
	if r.lastModuleInstance == nil {
		r.fatalf(cmd.Line, "no module to register")
	}
	r.moduleInstanceMap[cmd.As] = r.lastModuleInstance
}

func (r *specTestRunner) buildImports() map[string]map[string]any {
	imports := map[string]map[string]any{
		"spectest": r.spectestImports,
	}

	for regName, moduleInstance := range r.moduleInstanceMap {
		exports := make(map[string]any)
		for _, export := range moduleInstance.Exports {
			exports[export.Name] = export.Value
		}
		imports[regName] = exports
	}

	return imports
}

func (r *specTestRunner) handleModule(cmd wabt.Command) {
	wasmBytes := r.wasmDict[cmd.Filename]
	module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
	if err != nil {
		r.fatalf(cmd.Line, "failed to parse module %s: %v", cmd.Filename, err)
	}

	instance, err := r.vm.Instantiate(module, r.buildImports())
	if err != nil {
		r.fatalf(cmd.Line, "failed to instantiate module %s: %v", cmd.Filename, err)
	}

	r.lastModuleInstance = instance
	if cmd.Name != "" {
		r.moduleInstanceMap[cmd.Name] = instance
	}
}

func (r *specTestRunner) handleAssertReturn(cmd wabt.Command) {
	actual, err := r.handleAction(cmd.Action)
	if err != nil {
		r.fatalf(cmd.Line, "action failed unexpectedly: %v", err)
	}

	if len(actual) != len(cmd.Expected) {
		r.fatalf(
			cmd.Line,
			"expected %d results, got %d",
			len(cmd.Expected),
			len(actual),
		)
	}

	for i := range actual {
		r.assertValuesEqual(cmd.Line, cmd.Expected[i], actual[i])
	}
}

func (r *specTestRunner) handleAssertTrap(cmd wabt.Command) {
	if cmd.Filename != "" {
		// This is asserting that instantiating a module will trap.
		wasmBytes := r.wasmDict[cmd.Filename]
		module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
		if err != nil {
			r.fatalf(cmd.Line, "failed to parse module %s: %v", cmd.Filename, err)
		}

		_, err = r.vm.Instantiate(module, r.buildImports())
		if err == nil {
			r.fatalf(cmd.Line, "expected trap during instantiation, but got no error")
		}
	} else {
		// This is asserting that a function call will trap.
		_, err := r.handleAction(cmd.Action)
		if err == nil {
			r.fatalf(cmd.Line, "expected trap, but got no error")
		}
	}
}

func (r *specTestRunner) handleAssertInvalid(cmd wabt.Command) {
	wasmBytes := r.wasmDict[cmd.Filename]
	module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
	if err != nil {
		// We accept also parsing errors as valid form of invalid module.
		return
	}

	_, err = r.vm.Instantiate(module, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected validation error, but got no error")
	}
}

func (r *specTestRunner) handleAssertMalformed(cmd wabt.Command) {
	if strings.HasSuffix(cmd.Filename, ".wat") {
		// "assert_malformed" in text format cannot even be compiled to wasm,
		// therefore there is no point in trying to run this test.
		return
	}

	wasmBytes := r.wasmDict[cmd.Filename]
	module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
	if err != nil {
		return
	}

	// Checks on the instructions are not done in the parser, but later during
	// validation. Therefore we also need to try to instantiate the module.
	_, err = r.vm.Instantiate(module, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected validation error, but got no error")
	}
}

func (r *specTestRunner) handleAssertUninstantiable(cmd wabt.Command) {
	wasmBytes := r.wasmDict[cmd.Filename]
	module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
	if err != nil {
		// A parsing error is a valid form of being uninstantiable.
		return
	}

	_, err = r.vm.Instantiate(module, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected uninstantiable module, it wasn't")
	}
}

func (r *specTestRunner) handleAssertUnlinkable(cmd wabt.Command) {
	wasmBytes := r.wasmDict[cmd.Filename]
	module, err := epsilon.NewParser(bytes.NewReader(wasmBytes)).Parse()
	if err != nil {
		r.fatalf(cmd.Line, "failed to parse module %s: %v", cmd.Filename, err)
	}

	_, err = r.vm.Instantiate(module, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected unlinkable module, it wasn't")
	}
}

func (r *specTestRunner) handleAction(action *wabt.Action) ([]any, error) {
	moduleInstance := r.getModuleInstance(action.Module)
	switch action.Type {
	case "invoke":
		args := make([]any, len(action.Args))
		for i, arg := range action.Args {
			val, err := valueToGolang(arg)
			if err != nil {
				return nil, fmt.Errorf("could not convert arg %d: %w", i, err)
			}
			args[i] = val
		}
		return r.vm.Invoke(moduleInstance, action.Field, args...)
	case "get":
		res, err := r.vm.Get(moduleInstance, action.Field)
		return []any{res}, err
	default:
		return nil, fmt.Errorf("unknown action type %s", action.Type)
	}
}

func (r *specTestRunner) assertValuesEqual(
	line int,
	expectedVal wabt.Value,
	actual any,
) {
	expected, err := valueToGolang(expectedVal)
	if err != nil {
		r.fatalf(line, "failed to convert expected value: %v", err)
	}

	if !areEqual(expected, actual, expectedVal.LaneType) {
		r.fatalf(
			line,
			"mismatch: expected %v (%T), got %v (%T)",
			expected,
			expected,
			actual,
			actual,
		)
	}
}

func areEqual(expected, actual any, laneType string) bool {
	switch exp := expected.(type) {
	case float32:
		return floatsEqual(exp, actual.(float32))
	case float64:
		return floatsEqual(exp, actual.(float64))
	case epsilon.V128Value:
		act, ok := actual.(epsilon.V128Value)
		if !ok {
			return false
		}
		return v128Equal(exp, act, laneType)
	default:
		return expected == actual
	}
}

func v128Equal(expected, actual epsilon.V128Value, laneType string) bool {
	switch laneType {
	case "f32":
		for i := range uint32(4) {
			expLane := simdF32x4ExtractLane(expected, i)
			actLane := simdF32x4ExtractLane(actual, i)
			if !floatsEqual(expLane, actLane) {
				return false
			}
		}
		return true
	case "f64":
		for i := range uint32(2) {
			expLane := simdF64x2ExtractLane(expected, i)
			actLane := simdF64x2ExtractLane(actual, i)
			if !floatsEqual(expLane, actLane) {
				return false
			}
		}
		return true
	default:
		return expected == actual
	}
}

func floatsEqual[T float32 | float64](expected, actual T) bool {
	if math.IsNaN(float64(expected)) {
		return math.IsNaN(float64(actual))
	}
	return expected == actual
}

func (r *specTestRunner) getModuleInstance(
	module string,
) *epsilon.ModuleInstance {
	if module == "" {
		if r.lastModuleInstance == nil {
			r.t.Fatal("no module instance available for action")
		}
		return r.lastModuleInstance
	}
	instance, ok := r.moduleInstanceMap[module]
	if !ok {
		r.t.Fatalf("Module instance with name '%s' not found", module)
	}
	return instance
}

func (r *specTestRunner) fatalf(line int, format string, args ...any) {
	r.t.Helper()
	r.t.Fatalf("line %d: %s", line, fmt.Sprintf(format, args...))
}

func valueToGolang(v wabt.Value) (any, error) {
	if v.Type == "v128" {
		return parseV128(v)
	}

	s, ok := v.Value.(string)
	if !ok {
		return nil, fmt.Errorf("val for type %s not a string: %T", v.Type, v.Value)
	}

	return parseScalar(s, v.Type)
}

func parseV128(v wabt.Value) (any, error) {
	lanes, ok := v.Value.([]any)
	if !ok {
		return nil, fmt.Errorf("v128 value is not an array: %T", v.Value)
	}

	buf := new(bytes.Buffer)
	for _, lane := range lanes {
		lane, err := parseScalar(lane.(string), v.LaneType)
		if err != nil {
			return nil, err
		}
		binary.Write(buf, binary.LittleEndian, lane)
	}

	return epsilon.V128Value{
		Low:  binary.LittleEndian.Uint64(buf.Bytes()[0:8]),
		High: binary.LittleEndian.Uint64(buf.Bytes()[8:16]),
	}, nil
}

func parseScalar(value string, valueType string) (any, error) {
	switch valueType {
	case "i8":
		val, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return nil, err
		}
		return int8(val), nil
	case "i16":
		val, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return nil, err
		}
		return int16(val), nil
	case "i32":
		val, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(val), nil
	case "i64":
		val, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return int64(val), nil
	case "f32":
		return parseF32(value)
	case "f64":
		return parseF64(value)
	case "externref", "funcref":
		if value == "null" {
			return epsilon.NullVal, nil
		}
		val, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(val), nil
	default:
		return nil, fmt.Errorf("unsupported value type: %s", valueType)
	}
}

func parseF32(s string) (float32, error) {
	if pattern, ok := strings.CutPrefix(s, "nan:"); ok {
		switch pattern {
		case "canonical":
			return math.Float32frombits(0x7ff80000), nil
		case "arithmetic":
			return math.Float32frombits(0x7ff80001), nil
		default:
			return 0, fmt.Errorf("unknown NaN pattern: %s", s)
		}
	}
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(val)), nil
}

func parseF64(s string) (float64, error) {
	if pattern, ok := strings.CutPrefix(s, "nan:"); ok {
		switch pattern {
		case "canonical":
			return math.Float64frombits(0x7ff8000000000000), nil
		case "arithmetic":
			return math.Float64frombits(0x7ff8000000000001), nil
		default:
			return 0, fmt.Errorf("unknown NaN pattern: %s", s)
		}
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(val), nil
}

func simdF32x4ExtractLane(v epsilon.V128Value, laneIndex uint32) float32 {
	source := v.Low
	if laneIndex >= 2 {
		source = v.High
	}

	shift := (laneIndex & 1) * 32
	return math.Float32frombits(uint32(source >> shift))
}

func simdF64x2ExtractLane(v epsilon.V128Value, laneIndex uint32) float64 {
	bits := v.Low
	if laneIndex == 1 {
		bits = v.High
	}
	return math.Float64frombits(bits)
}
