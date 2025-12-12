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
	"context"
	"encoding/binary"
	"fmt"
	"maps"
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
	runtime            *epsilon.Runtime
	moduleInstanceMap  map[string]*epsilon.ModuleInstance
	lastModuleInstance *epsilon.ModuleInstance
	spectestImports    map[string]map[string]any
}

func newSpecRunner(t *testing.T, wasmDict map[string][]byte) *specTestRunner {
	importMemoryLimitMax := uint32(2)
	tableLimitMax := uint32(20)

	spectestImports := epsilon.NewImportBuilder().
		AddGlobal("spectest", "global_i32", int32(666), false, epsilon.I32).
		AddGlobal("spectest", "global_i64", int64(666), false, epsilon.I64).
		AddGlobal("spectest", "global_f32", float32(666.6), false, epsilon.F32).
		AddGlobal("spectest", "global_f64", float64(666.6), false, epsilon.F64).
		AddTable("spectest", "table", epsilon.NewTable(epsilon.TableType{
			Limits:        epsilon.Limits{Min: 10, Max: &tableLimitMax},
			ReferenceType: epsilon.FuncRefType,
		})).
		AddMemory(
			"spectest",
			"memory",
			epsilon.NewMemory(
				epsilon.MemoryType{
					Limits: epsilon.Limits{Min: 1, Max: &importMemoryLimitMax},
				},
			),
		).
		AddHostFunc("spectest", "print_i32", func(args ...any) []any {
			fmt.Printf("%d", args[0].(int32))
			return nil
		}).
		AddHostFunc("spectest", "print_i64", func(args ...any) []any {
			fmt.Printf("%d", args[0].(int64))
			return nil
		}).
		AddHostFunc("spectest", "print_f32", func(args ...any) []any {
			fmt.Printf("%f", args[0].(float32))
			return nil
		}).
		AddHostFunc("spectest", "print_f64", func(args ...any) []any {
			fmt.Printf("%f", args[0].(float64))
			return nil
		}).
		AddHostFunc("spectest", "print_i32_f32", func(args ...any) []any {
			fmt.Printf("%d %f", args[0].(int32), args[1].(float32))
			return nil
		}).
		AddHostFunc("spectest", "print_i64_f64", func(args ...any) []any {
			fmt.Printf("%d %f", args[0].(int64), args[1].(float64))
			return nil
		}).
		AddHostFunc("spectest", "print_f64_f64", func(args ...any) []any {
			fmt.Printf("%f %f", args[0].(float64), args[1].(float64))
			return nil
		}).
		AddHostFunc("spectest", "print", func(args ...any) []any {
			fmt.Printf("Print called!")
			return nil
		}).
		Build()

	return &specTestRunner{
		t:                 t,
		wasmDict:          wasmDict,
		runtime:           epsilon.NewRuntime(),
		moduleInstanceMap: make(map[string]*epsilon.ModuleInstance),
		spectestImports:   spectestImports,
	}
}

func (r *specTestRunner) run(commands []wabt.Command) {
	ctx := context.Background()
	for _, cmd := range commands {
		r.t.Logf("Line %d: executing command type: %s", cmd.Line, cmd.Type)
		switch cmd.Type {
		case "module":
			r.handleModule(ctx, cmd)
		case "assert_return":
			r.handleAssertReturn(ctx, cmd)
		case "assert_trap":
			r.handleAssertTrap(ctx, cmd)
		case "assert_uninstantiable":
			r.handleAssertUninstantiable(ctx, cmd)
		case "action":
			r.handleAction(ctx, cmd.Action)
		case "register":
			r.handleRegister(cmd)
		case "assert_exhaustion":
			r.handleAssertExhaustion(ctx, cmd)
		case "assert_invalid":
			r.handleAssertInvalid(ctx, cmd)
		case "assert_malformed":
			r.handleAssertMalformed(ctx, cmd)
		case "assert_unlinkable":
			r.handleAssertUnlinkable(ctx, cmd)
		default:
			r.fatalf(cmd.Line, "unknown command type: %s", cmd.Type)
		}
	}
}

func (r *specTestRunner) handleAssertExhaustion(
	ctx context.Context,
	cmd wabt.Command,
) {
	_, err := r.handleAction(ctx, cmd.Action)
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
	builder := epsilon.NewImportBuilder()
	for regName, moduleInstance := range r.moduleInstanceMap {
		builder.AddModuleExports(regName, moduleInstance)
	}
	imports := builder.Build()
	maps.Copy(imports, r.spectestImports)
	return imports
}

func (r *specTestRunner) handleModule(ctx context.Context, cmd wabt.Command) {
	wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
	instance, err := r.runtime.
		InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
	if err != nil {
		r.fatalf(cmd.Line, "failed to instantiate module %s: %v", cmd.Filename, err)
	}

	r.lastModuleInstance = instance
	if cmd.Name != "" {
		r.moduleInstanceMap[cmd.Name] = instance
	}
}

func (r *specTestRunner) handleAssertReturn(
	ctx context.Context,
	cmd wabt.Command,
) {
	actual, err := r.handleAction(ctx, cmd.Action)
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

func (r *specTestRunner) handleAssertTrap(
	ctx context.Context,
	cmd wabt.Command,
) {
	if cmd.Filename != "" {
		// This is asserting that instantiating a module will trap.
		wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
		_, err := r.runtime.
			InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
		if err == nil {
			r.fatalf(cmd.Line, "expected trap during instantiation, but got no error")
		}
	} else {
		// This is asserting that a function call will trap.
		_, err := r.handleAction(ctx, cmd.Action)
		if err == nil {
			r.fatalf(cmd.Line, "expected trap, but got no error")
		}
	}
}

func (r *specTestRunner) handleAssertInvalid(
	ctx context.Context,
	cmd wabt.Command,
) {
	wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.
		InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected validation error, but got no error")
	}
}

func (r *specTestRunner) handleAssertMalformed(
	ctx context.Context,
	cmd wabt.Command,
) {
	if strings.HasSuffix(cmd.Filename, ".wat") {
		// "assert_malformed" in text format cannot even be compiled to wasm,
		// therefore there is no point in trying to run this test.
		return
	}

	wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.
		InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected validation error, but got no error")
	}
}

func (r *specTestRunner) handleAssertUninstantiable(
	ctx context.Context,
	cmd wabt.Command,
) {
	wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.
		InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected uninstantiable module, it wasn't")
	}
}

func (r *specTestRunner) handleAssertUnlinkable(
	ctx context.Context,
	cmd wabt.Command,
) {
	wasmReader := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.
		InstantiateModuleWithImports(ctx, wasmReader, r.buildImports())
	if err == nil {
		r.fatalf(cmd.Line, "expected unlinkable module, it wasn't")
	}
}

func (r *specTestRunner) handleAction(
	ctx context.Context,
	action *wabt.Action,
) ([]any, error) {
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
		return moduleInstance.Invoke(ctx, action.Field, args...)
	case "get":
		res, err := moduleInstance.GetGlobal(action.Field)
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
