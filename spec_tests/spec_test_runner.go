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
	"testing"

	"epsilon/epsilon"
	"epsilon/wabt"
)

// SpecTestRunner manages the state and execution of a single spec test file.
type SpecTestRunner struct {
	t                  *testing.T
	wasmDict           map[string][]byte
	vm                 *epsilon.VM
	moduleInstanceMap  map[string]*epsilon.ModuleInstance
	lastModuleInstance *epsilon.ModuleInstance
	spectestImports    map[string]any
}

func newSpecRunner(t *testing.T, wasmDict map[string][]byte) *SpecTestRunner {
	importMemoryLimitMax := uint64(2)
	limits := epsilon.Limits{Min: 1, Max: &importMemoryLimitMax}
	memory := epsilon.NewMemory(epsilon.MemoryType{Limits: limits})
	table := epsilon.NewTable(epsilon.TableType{Limits: epsilon.Limits{Min: 10}})
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

	return &SpecTestRunner{
		t:                 t,
		wasmDict:          wasmDict,
		vm:                epsilon.NewVM(),
		moduleInstanceMap: make(map[string]*epsilon.ModuleInstance),
		spectestImports:   spectestImports,
	}
}

func (r *SpecTestRunner) run(commands []wabt.Command) {
	for _, cmd := range commands {
		// r.t.Logf("Executing '%v'", cmd)
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
		case "assert_invalid", "assert_malformed":
			// TODO(pivetta): We have to actual handle these.
			r.t.Logf("Line %d: skipping command type: %s", cmd.Line, cmd.Type)
		}
	}
}

func (r *SpecTestRunner) handleAssertExhaustion(cmd wabt.Command) {
	_, err := r.handleAction(cmd.Action)
	if err == nil {
		r.fatalf(cmd.Line, "expected call stack exhaustion, but got no error")
	}

	if err.Error() != "call stack exhausted" {
		r.fatalf(cmd.Line, "expected call stack exhaustion, but got: %v", err)
	}
}

func (r *SpecTestRunner) handleRegister(cmd wabt.Command) {
	if r.lastModuleInstance == nil {
		r.fatalf(cmd.Line, "no module to register")
	}
	r.moduleInstanceMap[cmd.As] = r.lastModuleInstance
}

func (r *SpecTestRunner) buildImports() map[string]map[string]any {
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

func (r *SpecTestRunner) handleModule(cmd wabt.Command) {
	wasmBytes, ok := r.wasmDict[cmd.Filename]
	if !ok {
		r.fatalf(cmd.Line, "Wasm file %s not found in wasmDict", cmd.Filename)
	}
	parser := epsilon.NewParser(bytes.NewReader(wasmBytes))
	module, err := parser.Parse()
	if err != nil {
		r.fatalf(cmd.Line, "failed to parse module %s: %v", cmd.Filename, err)
	}

	imports := r.buildImports()
	instance, err := r.vm.Instantiate(module, imports)
	if err != nil {
		r.fatalf(cmd.Line, "failed to instantiate module %s: %v", cmd.Filename, err)
	}

	r.lastModuleInstance = instance
	if cmd.Name != "" {
		r.moduleInstanceMap[cmd.Name] = instance
	}
}

func (r *SpecTestRunner) handleAssertReturn(cmd wabt.Command) {
	actual, err := r.handleAction(cmd.Action)
	if err != nil {
		r.fatalf(cmd.Line, "action failed unexpectedly: %v", err)
	}

	expected := make([]any, len(cmd.Expected))
	for i, v := range cmd.Expected {
		expected[i], err = valueToGolang(v)
		if err != nil {
			r.fatalf(cmd.Line, "failed to convert expected value: %v", err)
		}
	}

	if len(actual) != len(expected) {
		r.fatalf(
			cmd.Line,
			"expected %d results, got %d",
			len(expected),
			len(actual),
		)
	}

	for i := range actual {
		r.assertValuesEqual(cmd.Line, expected[i], actual[i])
	}
}

func (r *SpecTestRunner) handleAssertTrap(cmd wabt.Command) {
	_, err := r.handleAction(cmd.Action)
	if err == nil {
		r.fatalf(cmd.Line, "expected trap, but got no error")
	}
}

func (r *SpecTestRunner) handleAssertUninstantiable(cmd wabt.Command) {
	wasmBytes, ok := r.wasmDict[cmd.Filename]
	if !ok {
		r.fatalf(cmd.Line, "wasm file %s not found", cmd.Filename)
	}

	parser := epsilon.NewParser(bytes.NewReader(wasmBytes))
	module, err := parser.Parse()
	if err != nil {
		// A parsing error is a valid form of being uninstantiable.
		return
	}

	imports := r.buildImports()
	_, err = r.vm.Instantiate(module, imports)
	if err == nil {
		r.fatalf(cmd.Line, "expected uninstantiable module, it wasn't")
	}
}

func (r *SpecTestRunner) handleAction(action *wabt.Action) ([]any, error) {
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

func (r *SpecTestRunner) assertValuesEqual(line int, expected, actual any) {
	r.t.Helper()
	// Special case for NaN, as NaN != NaN
	if f32, ok := expected.(float32); ok && math.IsNaN(float64(f32)) {
		if !math.IsNaN(float64(actual.(float32))) {
			r.fatalf(line, "expected NaN, got %v", actual)
		}
		return
	}
	if f64, ok := expected.(float64); ok && math.IsNaN(f64) {
		if !math.IsNaN(actual.(float64)) {
			r.fatalf(line, "expected NaN, got %v", actual)
		}
		return
	}

	if v128, ok := expected.(epsilon.V128Value); ok {
		actualV128, ok := actual.(epsilon.V128Value)
		if !ok {
			r.fatalf(line, "mismatch: expected V128, got %T", actual)
		}
		if v128 != actualV128 {
			r.fatalf(line, "mismatch: expected %v, got %v", v128, actualV128)
		}
		return
	}

	if expected != actual {
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

func (r *SpecTestRunner) getModuleInstance(
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

func (r *SpecTestRunner) fatalf(line int, format string, args ...any) {
	r.t.Helper()
	message := fmt.Sprintf(format, args...)
	r.t.Fatalf("line %d: %s", line, message)
}

func v128ToGolang(v wabt.Value) (any, error) {
	arr, ok := v.Value.([]any)
	if !ok {
		return nil, fmt.Errorf("v128 value is not an array: %T", v.Value)
	}

	lanes := arr
	buf := new(bytes.Buffer)

	switch v.LaneType {
	case "i8":
		if len(lanes) != 16 {
			return nil, fmt.Errorf("i8x16 requires 16 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			u, err := strconv.ParseUint(s, 10, 8)
			if err != nil {
				return nil, err
			}
			buf.WriteByte(byte(u))
		}
	case "i16":
		if len(lanes) != 8 {
			return nil, fmt.Errorf("i16x8 requires 8 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			u, err := strconv.ParseUint(s, 10, 16)
			if err != nil {
				return nil, err
			}
			binary.Write(buf, binary.LittleEndian, uint16(u))
		}
	case "i32":
		if len(lanes) != 4 {
			return nil, fmt.Errorf("i32x4 requires 4 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			u, err := strconv.ParseUint(s, 10, 32)
			if err != nil {
				return nil, err
			}
			binary.Write(buf, binary.LittleEndian, uint32(u))
		}
	case "i64":
		if len(lanes) != 2 {
			return nil, fmt.Errorf("i64x2 requires 2 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			u, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return nil, err
			}
			binary.Write(buf, binary.LittleEndian, u)
		}
	case "f32":
		if len(lanes) != 4 {
			return nil, fmt.Errorf("f32x4 requires 4 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			val, err := parseF32(s)
			if err != nil {
				return nil, err
			}
			binary.Write(buf, binary.LittleEndian, val)
		}
	case "f64":
		if len(lanes) != 2 {
			return nil, fmt.Errorf("f64x2 requires 2 lanes, got %d", len(lanes))
		}
		for _, lane := range lanes {
			s, _ := lane.(string)
			val, err := parseF64(s)
			if err != nil {
				return nil, err
			}
			binary.Write(buf, binary.LittleEndian, val)
		}
	default:
		return nil, fmt.Errorf("unsupported v128 lane type: %s", v.LaneType)
	}

	var v128 epsilon.V128Value
	v128.Low = binary.LittleEndian.Uint64(buf.Bytes()[0:8])
	v128.High = binary.LittleEndian.Uint64(buf.Bytes()[8:16])
	return v128, nil
}

func parseF32(s string) (float32, error) {
	if s == "nan:canonical" {
		return float32(math.NaN()), nil
	}
	if s == "nan:arithmetic" {
		return float32(math.NaN()), nil
	}
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(val)), nil
}

func parseF64(s string) (float64, error) {
	if s == "nan:canonical" {
		return math.NaN(), nil
	}
	if s == "nan:arithmetic" {
		return math.NaN(), nil
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(val), nil
}

func valueToGolang(v wabt.Value) (any, error) {
	if v.Type == "v128" {
		return v128ToGolang(v)
	}

	s, ok := v.Value.(string)
	if !ok {
		return nil, fmt.Errorf("value for type %s is not a string: %T", v.Type, v.Value)
	}

	switch v.Type {
	case "i32":
		val, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(val), nil
	case "i64":
		val, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return int64(val), nil
	case "f32":
		return parseF32(s)
	case "f64":
		return parseF64(s)
	case "externref", "funcref":
		if s == "null" {
			return epsilon.NullVal, nil
		}
		val, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(val), nil
	default:
		return nil, fmt.Errorf("unsupported value type: %s", v.Type)
	}
}
