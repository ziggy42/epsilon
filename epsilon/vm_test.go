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

package epsilon

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

func initVM(
	wat string,
	imports map[string]map[string]any,
	features *ExperimentalFeatures,
) (*VM, *ModuleInstance, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return nil, nil, err
	}
	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		return nil, nil, err
	}

	var vm *VM
	if features != nil {
		vm = NewVMWithFeatures(*features)
	} else {
		vm = NewVM()
	}

	moduleInstance, err := vm.Instantiate(module, imports)
	if err != nil {
		return nil, nil, err
	}
	return vm, moduleInstance, nil
}

func TestExecuteExportedFunctionSum(t *testing.T) {
	wat := `(module
		(func (export "sum") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "sum", int32(1), int32(1))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(2)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteExportedFunctionDiff(t *testing.T) {
	wat := `(module
		(func (export "diff") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.sub)
  )`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "diff", int32(5), int32(2))

	if err != nil {
		t.Fatalf("failed to execute diff: %v", err)
	}
	expected := int32(3)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteCall(t *testing.T) {
	wat := `(module
		(func $swap (param i32 i32) (result i32 i32)
			local.get 1
			local.get 0)

		(func (export "reverseSub") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			call $swap
			i32.sub)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "reverseSub", int32(5), int32(3))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(-2)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteIf(t *testing.T) {
	wat := `(module
		(func $min (param $a i32) (param $b i32) (result i32)
			local.get $a
			local.get $b
			i32.lt_s
			if (result i32)
				local.get $a
			else
				local.get $b
			end
		)
		(export "min" (func $min))
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "min", int32(7), int32(2))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(2)
	if result[0] != int32(2) {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "min", int32(3), int32(5))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(3)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteNestedIf(t *testing.T) {
	wat := `(module
		(func $nested_if (param $p1 i32) (param $p2 i32) (result i32)
			local.get $p1
			i32.const 10
			i32.gt_s
			if (result i32)
				local.get $p2
				i32.const 5
				i32.eq
				if (result i32)
					i32.const 1
				else
					i32.const 2
				end
			else
				i32.const 3
			end
		)
		(export "nested_if" (func $nested_if))
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "nested_if", int32(11), int32(2))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(2)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteRecursive(t *testing.T) {
	wat := `(module
		(func $fac (export "fac") (param i32) (result i32)
			local.get 0
			i32.const 1
			i32.lt_s
			if (result i32)
				i32.const 1
			else
				local.get 0
				local.get 0
				i32.const 1
				i32.sub
				call $fac
				i32.mul
			end)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "fac", int32(5))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(120)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrFromIf(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			i32.const 1
			if (result i32)
				i32.const 100
				br 0
				i32.const 1
				i32.add
			else
				i32.const 200
			end)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(100)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrFromNestedIf(t *testing.T) {
	wat := `(module
	(func (export "test") (result i32)
		i32.const 1
		if (result i32)
			i32.const 1
			if (result i32)
				i32.const 100
				br 1
				i32.const 1
				i32.add
			else
				i32.const 200
			end
			i32.const 1
			i32.add
		else
			i32.const 300
		end)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(100)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBlock(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			block (result i32)
				i32.const 10
				i32.const 20
				i32.add
			end
			i32.const 5
			i32.add)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(35)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrBlock(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			block (result i32)
				i32.const 10
				br 0
				i32.const 20
				i32.add
			end
			i32.const 5
			i32.add)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(15)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrFromNestedBlock(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			block (result i32)
				i32.const 10
				block (result i32)
					i32.const 20
					br 1
				end
				i32.add
			end)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(20)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteLoop(t *testing.T) {
	wat := `(module
		(func (export "mul") (param $a i32) (param $b i32) (result i32)
			(local $sum i32)
			i32.const 0
			local.set $sum

			block
				loop
					local.get $b
					i32.eqz
					if
						br 2
					end

					local.get $sum
					local.get $a
					i32.add
					local.set $sum

					local.get $b
					i32.const 1
					i32.sub
					local.set $b

					br 0
				end
			end

			local.get $sum)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "mul", int32(3), int32(5))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(15)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrIf(t *testing.T) {
	wat := `(module
		(func (export "test") (param i32) (result i32)
			i32.const 1
			local.get 0
			br_if 0
			drop
			i32.const 0)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(10))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(1)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "test", int32(0))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(0)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrIfPreservesStack(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			i32.const 100
			i32.const 0
			br_if 0
			i32.const 1
			i32.add)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(101)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteBrTable(t *testing.T) {
	wat := `(module
		(func (export "test") (param i32) (result i32)
			block (result i32)
				block (result i32)
					block (result i32)
						i32.const 99
						local.get 0
						br_table 0 1 2
					end
					i32.const 10
					i32.add
				end
				i32.const 20
				i32.add
			end)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(0))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(129)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "test", int32(1))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(119)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "test", int32(2))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(99)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "test", int32(3))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(99)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteReturn(t *testing.T) {
	wat := `(module
		(func (export "test") (result i32)
			i32.const 10
			return
			i32.const 20)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(10))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(10)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteCallIndirect(t *testing.T) {
	wat := `(module
		(type $t0 (func (param i32) (result i32)))

		(func $add_one (type $t0)
			local.get 0
			i32.const 1
			i32.add)

		(func $sub_one (type $t0)
			local.get 0
			i32.const 1
			i32.sub)

		(table 2 funcref)
		(elem (i32.const 0) $add_one $sub_one)

		(func $dispatch (param $idx i32) (param $val i32) (result i32)
			local.get $val
			local.get $idx
			call_indirect (type $t0))

		(export "dispatch" (func $dispatch))
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "dispatch", int32(0), int32(10))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(11)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "dispatch", int32(1), int32(10))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(9)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteSelect(t *testing.T) {
	wat := `(module
		(func (export "select") (param $condition i32) (result i32)
			i32.const 1
			i32.const 2
			local.get $condition
			select
		)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "select", int32(1))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(1)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "select", int32(0))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(2)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteLocalTee(t *testing.T) {
	wat := `(module
		(func (export "test") (param $a i32) (result i32)
			local.get $a
			i32.const 10
			i32.add
			local.tee $a
			i32.const 5
			i32.mul
		)
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(1))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(55)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}

	result, err = vm.Invoke(moduleInstance, "test", int32(2))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected = int32(60)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteLoadStore(t *testing.T) {
	wat := `(module
		(memory 1)

  	(func $test (param $address i32) (param $value i32) (result i32)
			local.get $address
			local.get $value
			i32.store
			local.get $address
			i32.load
		)

		(export "test" (func $test))
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(2), int32(8))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(8)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestExecuteLoadOutOfBoundsTraps(t *testing.T) {
	wat := `(module
		(memory 1)
		(func $test (param $address i32) (result i32)
			local.get $address
			i32.load offset=1)
		(export "test" (func $test))
	)`
	vm, moduleInstance, err := initVM(wat, nil, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	_, err = vm.Invoke(moduleInstance, "test", int32(65532))

	if err == nil {
		t.Fatalf("expected trap")
	}
	expectedTrap := "out of bounds memory access"
	if err.Error() != expectedTrap {
		t.Fatalf("expected trap '%s', got '%s'", expectedTrap, err.Error())
	}
}

func TestFunctionImport(t *testing.T) {
	wat := `(module
		(import "module" "sum" (func $sum (param i32) (param i32) (result i32)))
		(func (export "native_sum") (param i32) (param i32) (result i32)
			local.get 0
			local.get 1
			call $sum)
	)`
	imports := map[string]map[string]any{
		"module": {
			"sum": func(args ...any) []any {
				val := args[0].(int32) + args[1].(int32)
				return []any{val}
			},
		},
	}
	vm, moduleInstance, err := initVM(wat, imports, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "native_sum", int32(2), int32(3))

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(5)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestGlobalGet(t *testing.T) {
	wat := `(module
		(import "module" "global" (global $g i32))

		(func (export "test") (result i32)
			global.get $g
		)
	)`
	imports := map[string]map[string]any{
		"module": {
			"global": int32(42),
		},
	}
	vm, moduleInstance, err := initVM(wat, imports, nil)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")

	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}
	expected := int32(42)
	if result[0] != expected {
		t.Fatalf("expected %d, got %d", expected, result[0])
	}
}

func TestInstantiateMultipleMemories(t *testing.T) {
	wat := `(module
		(memory $mem0 (export "mem0") 1)
		(memory $mem1 (export "mem1") 1)

		(data (memory $mem0) (i32.const 0) "hello")
		(data (memory $mem1) (i32.const 0) "world")
	)`

	_, moduleInstance, err := initVM(wat, nil, &ExperimentalFeatures{MultipleMemories: true})
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	checkMemory := func(exportName, expectedValue string) {
		t.Helper()

		mem, err := getExportedMemory(moduleInstance, exportName)
		if err != nil {
			t.Fatalf("failed to get %s: %v", exportName, err)
		}

		data, err := mem.Get(0, 0, uint32(len(expectedValue)))
		if err != nil {
			t.Fatalf("failed to read %s: %v", exportName, err)
		}

		if got := string(data); got != expectedValue {
			t.Errorf("%s: expected %q, got %q", exportName, expectedValue, got)
		}
	}

	checkMemory("mem0", "hello")
	checkMemory("mem1", "world")
}

func TestStoreLoadMultipleMemories(t *testing.T) {
	wat := `(module
		(memory $mem0 (export "mem0") 1)
		(memory $mem1 (export "mem1") 1)

		(data (memory $mem0) (i32.const 0) "hello")

		;; Loads from mem0 the value at index $idx, stores it in mem1 at index 0,
		;; loads the value back from mem1.
		(func (export "test") (param $idx i32) (result i32)
			i32.const 0
			local.get $idx
			i32.load8_u $mem0
			i32.store8 $mem1
			i32.const 0
			i32.load8_u $mem1
		)
	)`
	vm, moduleInstance, err := initVM(
		wat,
		nil,
		&ExperimentalFeatures{MultipleMemories: true},
	)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(2))
	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}

	if got := result[0]; got != int32('l') {
		t.Fatalf("expected %d, got %d", int32('l'), got)
	}
}

func TestV128StoreLoadMultipleMemories(t *testing.T) {
	wat := `(module
		(memory $mem0 (export "mem0") 1)
		(memory $mem1 (export "mem1") 1)

		(data (memory $mem0) (i32.const 0) "hello")

		;; Loads from mem0 using v128.load8_lane, stores to mem1 using 
		;; v128.store8_lane, then loads back using v128.load8_lane to return the 
		;; result.
		(func (export "test") (param $idx i32) (result i32)
			i32.const 0
			local.get $idx
			v128.const i64x2 0 0
			v128.load8_lane $mem0 0
			v128.store8_lane $mem1 0
			i32.const 0
			v128.const i64x2 0 0
			v128.load8_lane $mem1 0
			i32x4.extract_lane 0
		)
	)`

	vm, moduleInstance, err := initVM(
		wat,
		nil,
		&ExperimentalFeatures{MultipleMemories: true},
	)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test", int32(2))
	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}

	if got := result[0]; got != int32('l') {
		t.Fatalf("expected %d, got %d", int32('l'), got)
	}
}

func TestMemoryInitCopyMultipleMemories(t *testing.T) {
	wat := `(module
		(memory $mem0 1)
		(memory $mem1 1)
		(memory $mem2 1)
		(data $d0 "\2a\00\00\00")
		(func (export "test") (result i32)
			i32.const 0
			i32.const 0
			i32.const 4
			memory.init $mem1 $d0
			i32.const 0
			i32.const 0
			i32.const 4
			memory.copy $mem2 $mem1
			i32.const 0
			i32.load $mem2
		)
	)`
	vm, moduleInstance, err := initVM(
		wat,
		nil,
		&ExperimentalFeatures{MultipleMemories: true},
	)
	if err != nil {
		t.Fatalf("failed to create vm: %v", err)
	}

	result, err := vm.Invoke(moduleInstance, "test")
	if err != nil {
		t.Fatalf("failed to execute function: %v", err)
	}

	if got := result[0]; got != int32(0x2a) {
		t.Fatalf("expected %d, got %d", 0x2a, got)
	}
}

func getExportedMemory(module *ModuleInstance, name string) (*Memory, error) {
	for _, export := range module.Exports {
		if export.Name == name {
			return export.Value.(*Memory), nil
		}
	}
	return nil, fmt.Errorf("Memory %s not found", name)
}
