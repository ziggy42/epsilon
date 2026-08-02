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
	runtime            *epsilon.Runtime
	moduleInstanceMap  map[string]*epsilon.ModuleInstance
	lastModuleInstance *epsilon.ModuleInstance
	spectestImports    *epsilon.ModuleImports
}

func newSpecRunner(t *testing.T, wasmDict map[string][]byte) *specTestRunner {
	importMemoryLimitMax := uint32(2)
	tableLimitMax := uint32(20)

	// Spec tests intentionally declare resources at the WebAssembly spec
	// maxima (e.g. tables with Max=2^32-1) to exercise edge cases. Set the
	// configured ceilings to the spec maxima so the runner reflects what
	// the engine validator must accept by spec, not what hosts ship by
	// default.
	runtime := epsilon.NewRuntimeWithConfig(epsilon.Config{
		MaxCallStackDepth:          epsilon.DefaultMaxCallStackDepth,
		CallStackPreallocationSize: epsilon.DefaultCallStackPreallocationSize,
		MaxTableElements:           math.MaxUint32,
		MaxMemoryPages:             uint32(1) << 16,
		MaxLocalsPerFunction:       epsilon.DefaultMaxLocalsPerFunction,
	})
	spectestImports := epsilon.NewModuleImports("spectest").
		AddGlobal("global_i32", runtime.NewGlobalI32(666, false)).
		AddGlobal("global_i64", runtime.NewGlobalI64(666, false)).
		AddGlobal("global_f32", runtime.NewGlobalF32(666.6, false)).
		AddGlobal("global_f64", runtime.NewGlobalF64(666.6, false)).
		AddTable("table", runtime.NewTable(epsilon.TableType{
			Limits:        epsilon.Limits{Min: 10, Max: &tableLimitMax},
			ReferenceType: epsilon.FuncRefType,
		})).
		AddMemory("memory", runtime.NewMemory(
			epsilon.MemoryType{
				Limits: epsilon.Limits{Min: 1, Max: &importMemoryLimitMax},
			},
		)).
		AddHostFunc(
			"print_i32",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%d", args[0].(int32))
				return nil
			},
		).
		AddHostFunc(
			"print_i64",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%d", args[0].(int64))
				return nil
			},
		).
		AddHostFunc(
			"print_f32",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%f", args[0].(float32))
				return nil
			},
		).
		AddHostFunc(
			"print_f64",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%f", args[0].(float64))
				return nil
			},
		).
		AddHostFunc(
			"print_i32_f32",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%d %f", args[0].(int32), args[1].(float32))
				return nil
			},
		).
		AddHostFunc(
			"print_i64_f64",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%d %f", args[0].(int64), args[1].(float64))
				return nil
			},
		).
		AddHostFunc(
			"print_f64_f64",
			func(m *epsilon.ModuleInstance, args ...any) []any {
				fmt.Printf("%f %f", args[0].(float64), args[1].(float64))
				return nil
			},
		).
		AddHostFunc("print", func(m *epsilon.ModuleInstance, args ...any) []any {
			fmt.Printf("Print called!")
			return nil
		})

	return &specTestRunner{
		t:                 t,
		wasmDict:          wasmDict,
		runtime:           runtime,
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

func (r *specTestRunner) buildImports() []*epsilon.ModuleImports {
	imports := []*epsilon.ModuleImports{r.spectestImports}
	for regName, moduleInstance := range r.moduleInstanceMap {
		moduleImport := epsilon.NewModuleImports(regName).
			AddModuleExports(moduleInstance)
		imports = append(imports, moduleImport)
	}
	return imports
}

func (r *specTestRunner) handleModule(cmd wabt.Command) {
	wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
	instance, err := r.runtime.
		InstantiateModuleWithImports(wasm, r.buildImports()...)
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
	var err error
	if cmd.Filename != "" {
		// This is asserting that instantiating a module will trap.
		wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
		_, err = r.runtime.InstantiateModuleWithImports(wasm, r.buildImports()...)
	} else {
		// This is asserting that a function call will trap.
		_, err = r.handleAction(cmd.Action)
	}

	if err == nil {
		r.fatalf(cmd.Line, "expected trap %q, but got no error", cmd.Text)
	}
	if !trapMatches(cmd.Text, err.Error()) {
		r.fatalf(cmd.Line, "expected trap %q, but got: %v", cmd.Text, err)
	}
}

// trapMatches reports whether a trap message satisfies the failure reason an
// assertion expects. Epsilon appends context to some trap messages (the
// offending element index, for instance), so the expected reason may stop
// short of the full message, but only at a word boundary.
func trapMatches(expected, actual string) bool {
	rest, ok := strings.CutPrefix(actual, expected)
	return ok && (rest == "" || rest[0] == ' ')
}

func (r *specTestRunner) handleAssertInvalid(cmd wabt.Command) {
	wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.InstantiateModuleWithImports(wasm, r.buildImports()...)
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

	wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.InstantiateModuleWithImports(wasm, r.buildImports()...)
	if err == nil {
		r.fatalf(cmd.Line, "expected validation error, but got no error")
	}
}

func (r *specTestRunner) handleAssertUninstantiable(cmd wabt.Command) {
	wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.InstantiateModuleWithImports(wasm, r.buildImports()...)
	if err == nil {
		r.fatalf(cmd.Line, "expected uninstantiable module, it wasn't")
	}
}

func (r *specTestRunner) handleAssertUnlinkable(cmd wabt.Command) {
	wasm := bytes.NewReader(r.wasmDict[cmd.Filename])
	_, err := r.runtime.InstantiateModuleWithImports(wasm, r.buildImports()...)
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
		return moduleInstance.Invoke(action.Field, args...)
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
	matches, err := valueMatches(expectedVal, actual)
	if err != nil {
		r.fatalf(line, "failed to compare expected value: %v", err)
	}

	if !matches {
		r.fatalf(
			line,
			"mismatch: expected %v, got %v (%T)",
			expectedVal.Value,
			actual,
			actual,
		)
	}
}

// valueMatches reports whether an action result satisfies an `assert_return`
// expectation. Floats are compared bit for bit so that the sign of zero is
// significant, except for the `nan:canonical` and `nan:arithmetic` patterns,
// which stand for the sets of NaNs the spec allows an operation to produce.
func valueMatches(expected wabt.Value, actual any) (bool, error) {
	if expected.Type == "v128" {
		lanes, ok := expected.Value.([]any)
		if !ok {
			return false, fmt.Errorf(
				"v128 value is not an array: %T",
				expected.Value,
			)
		}
		act, ok := actual.(epsilon.V128Value)
		if !ok {
			return false, nil
		}
		return v128Matches(lanes, expected.LaneType, act)
	}

	raw, ok := expected.Value.(string)
	if !ok {
		return false, fmt.Errorf(
			"val for type %s not a string: %T",
			expected.Type,
			expected.Value,
		)
	}

	return scalarMatches(raw, expected.Type, actual)
}

func v128Matches(
	lanes []any,
	laneType string,
	actual epsilon.V128Value,
) (bool, error) {
	for i, lane := range lanes {
		raw, ok := lane.(string)
		if !ok {
			return false, fmt.Errorf("v128 lane is not a string: %T", lane)
		}
		matches, err := scalarMatches(raw, laneType, extractLane(
			actual,
			laneType,
			uint32(i),
		))
		if err != nil || !matches {
			return false, err
		}
	}
	return true, nil
}

func scalarMatches(raw, valueType string, actual any) (bool, error) {
	switch valueType {
	case "f32":
		act, ok := actual.(float32)
		if !ok {
			return false, nil
		}
		return f32Matches(raw, act)
	case "f64":
		act, ok := actual.(float64)
		if !ok {
			return false, nil
		}
		return f64Matches(raw, act)
	default:
		expected, err := parseScalar(raw, valueType)
		if err != nil {
			return false, err
		}
		return expected == actual, nil
	}
}

const (
	// canonicalNaN32 is the f32 canonical NaN with the sign bit cleared; a
	// NaN is arithmetic when its payload is at least the canonical one, i.e.
	// when the most significant payload bit is set.
	canonicalNaN32 = uint32(0x7fc00000)
	canonicalNaN64 = uint64(0x7ff8000000000000)
)

func f32Matches(raw string, actual float32) (bool, error) {
	bits := math.Float32bits(actual)
	switch raw {
	case "nan:canonical":
		return bits&^uint32(1<<31) == canonicalNaN32, nil
	case "nan:arithmetic":
		return bits&canonicalNaN32 == canonicalNaN32, nil
	}
	expected, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return false, err
	}
	return uint32(expected) == bits, nil
}

func f64Matches(raw string, actual float64) (bool, error) {
	bits := math.Float64bits(actual)
	switch raw {
	case "nan:canonical":
		return bits&^uint64(1<<63) == canonicalNaN64, nil
	case "nan:arithmetic":
		return bits&canonicalNaN64 == canonicalNaN64, nil
	}
	expected, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return false, err
	}
	return expected == bits, nil
}

func extractLane(v epsilon.V128Value, laneType string, laneIndex uint32) any {
	switch laneType {
	case "f32":
		return simdF32x4ExtractLane(v, laneIndex)
	case "f64":
		return simdF64x2ExtractLane(v, laneIndex)
	case "i8":
		return int8(laneBits(v, laneIndex, 8))
	case "i16":
		return int16(laneBits(v, laneIndex, 16))
	case "i32":
		return int32(laneBits(v, laneIndex, 32))
	default:
		return int64(laneBits(v, laneIndex, 64))
	}
}

func laneBits(v epsilon.V128Value, laneIndex, laneWidth uint32) uint64 {
	source := v.Low
	lanesPerHalf := 64 / laneWidth
	if laneIndex >= lanesPerHalf {
		source = v.High
	}
	shift := (laneIndex % lanesPerHalf) * laneWidth
	mask := uint64(1)<<laneWidth - 1
	if laneWidth == 64 {
		mask = ^uint64(0)
	}
	return (source >> shift) & mask
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
			return epsilon.NullReference, nil
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
			return math.Float32frombits(canonicalNaN32), nil
		case "arithmetic":
			return math.Float32frombits(canonicalNaN32 | 1), nil
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
			return math.Float64frombits(canonicalNaN64), nil
		case "arithmetic":
			return math.Float64frombits(canonicalNaN64 | 1), nil
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
