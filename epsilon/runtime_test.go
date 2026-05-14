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
	"encoding/binary"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

func TestRuntimeTrivialFunction(t *testing.T) {
	wasm, _ := wabt.Wat2Wasm(`(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add)
	)`)

	instance, err := NewRuntime().InstantiateModule(bytes.NewReader(wasm))
	if err != nil {
		t.Fatalf("failed to instantiate module: %v", err)
	}

	results, err := instance.Invoke("add", int32(5), int32(3))
	if err != nil {
		t.Fatalf("failed to invoke function: %v", err)
	}

	expected := int32(8)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}
}

func TestRuntimeImportedFunction(t *testing.T) {
	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "multiply" (func $multiply (param i32 i32) (result i32)))
		(func (export "computeArea") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			call $multiply)
	)`)

	imports := NewModuleImports("env").
		AddHostFunc("multiply", func(module *ModuleInstance, args ...any) []any {
			a := args[0].(int32)
			b := args[1].(int32)
			return []any{a * b}
		})

	instance, err := NewRuntime().
		InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err != nil {
		t.Fatalf("failed to instantiate module: %v", err)
	}

	results, err := instance.Invoke("computeArea", int32(7), int32(6))
	if err != nil {
		t.Fatalf("failed to invoke function: %v", err)
	}

	expected := int32(42)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}
}

func TestRuntimeImportedMemory(t *testing.T) {
	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "memory" (memory 1))
		(export "memory" (memory 0))
		(func (export "readAt") (param i32) (result i32)
			local.get 0
			i32.load)
	)`)

	runtime := NewRuntime()
	memory := runtime.NewMemory(MemoryType{Limits: Limits{Min: 1}})
	testData := binary.LittleEndian.AppendUint32(nil, 42)
	err := memory.Set(0, 100, testData)
	if err != nil {
		t.Fatalf("failed to set memory: %v", err)
	}

	imports := NewModuleImports("env").AddMemory("memory", memory)

	instance, err := runtime.
		InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err != nil {
		t.Fatalf("failed to instantiate module: %v", err)
	}

	results, err := instance.Invoke("readAt", int32(100))
	if err != nil {
		t.Fatalf("failed to invoke function: %v", err)
	}

	expected := int32(42)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}

	exportedMemory, err := instance.GetMemory("memory")
	if err != nil {
		t.Fatalf("failed to get memory: %v", err)
	}

	data, err := exportedMemory.Get(0, 100, 4)
	if err != nil {
		t.Fatalf("failed to read from memory: %v", err)
	}

	if !bytes.Equal(data, testData) {
		t.Fatalf("expected %v, got %v", testData, data)
	}
}

func TestRuntimeImportedGlobal(t *testing.T) {
	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "offset" (global $offset i32))
		(func (export "addOffset") (param i32) (result i32)
			local.get 0
			global.get $offset
			i32.add)
	)`)

	runtime := NewRuntime()
	imports := NewModuleImports("env").
		AddGlobal("offset", runtime.NewGlobal(int32(100), false, I32))

	instance, err := runtime.
		InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err != nil {
		t.Fatalf("failed to instantiate module: %v", err)
	}

	results, err := instance.Invoke("addOffset", int32(23))
	if err != nil {
		t.Fatalf("failed to invoke function: %v", err)
	}

	expected := int32(123)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}
}

func TestRuntimeImportedFunctionsInTable(t *testing.T) {
	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "table" (table 2 funcref))
		(import "env" "host_sub" (func $host_sub (param i32) (result i32)))
		(type $op (func (param i32) (result i32)))
		
		(func $wasm_add (param i32) (result i32)
			local.get 0
			i32.const 1
			i32.add)
		
		(elem (i32.const 0) $host_sub $wasm_add)
		
		(func (export "applyOp") (param i32 i32) (result i32)
			local.get 1
			local.get 0
			call_indirect (type $op))
	)`)

	runtime := NewRuntime()
	imports := NewModuleImports("env").
		AddHostFunc("host_sub", func(module *ModuleInstance, args ...any) []any {
			x := args[0].(int32)
			return []any{x - 1}
		}).
		AddTable("table", runtime.NewTable(TableType{
			ReferenceType: FuncRefType,
			Limits:        Limits{Min: 2},
		}))

	instance, err := runtime.
		InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err != nil {
		t.Fatalf("failed to instantiate module: %v", err)
	}

	results, err := instance.Invoke("applyOp", int32(0), int32(10))
	if err != nil {
		t.Fatalf("failed to invoke applyOp with host function: %v", err)
	}
	if results[0].(int32) != int32(9) {
		t.Fatalf("expected 9, got %d", results[0])
	}

	results, err = instance.Invoke("applyOp", int32(1), int32(10))
	if err != nil {
		t.Fatalf("failed to invoke applyOp with WASM function: %v", err)
	}
	if results[0].(int32) != int32(11) {
		t.Fatalf("expected 11, got %d", results[0])
	}
}

func TestRuntimeModuleToModuleImport(t *testing.T) {
	module1Wasm, _ := wabt.Wat2Wasm(`(module
		(func (export "multiply") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.mul)
		(func (export "square") (param i32) (result i32)
			local.get 0
			local.get 0
			i32.mul)
	)`)

	runtime := NewRuntime()
	module1, err := runtime.InstantiateModule(bytes.NewReader(module1Wasm))
	if err != nil {
		t.Fatalf("failed to instantiate module1: %v", err)
	}

	module2Wasm, _ := wabt.Wat2Wasm(`(module
		(import "math" "multiply" (func $multiply (param i32 i32) (result i32)))
		(func (export "multiplyAndAdd") (param i32 i32 i32) (result i32)
			local.get 0
			local.get 1
			call $multiply
			local.get 2
			i32.add)
	)`)

	module2, err := runtime.InstantiateModuleWithImports(
		bytes.NewReader(module2Wasm),
		NewModuleImports("math").AddModuleExports(module1),
	)
	if err != nil {
		t.Fatalf("failed to instantiate module2: %v", err)
	}

	results, err := module2.Invoke("multiplyAndAdd", int32(3), int32(4), int32(5))
	if err != nil {
		t.Fatalf("failed to invoke multiplyAndAdd: %v", err)
	}

	expected := int32(17)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}

	results, err = module1.Invoke("square", int32(5))
	if err != nil {
		t.Fatalf("failed to invoke square: %v", err)
	}

	expected = int32(25)
	if results[0].(int32) != expected {
		t.Fatalf("expected %d, got %d", expected, results[0])
	}
}

func TestInvokeFuncrefInjectionIsRejected(t *testing.T) {
	// Module A has a private function $secret that is NOT exported.
	wasmA, _ := wabt.Wat2Wasm(`(module
		(func $secret (result i32) i32.const 42)
		(func (export "nop"))
	)`)

	// Module B accepts a funcref, puts it in a table, and calls it.
	wasmB, _ := wabt.Wat2Wasm(`(module
		(table $t 1 funcref)
		(type $void_to_i32 (func (result i32)))
		(func (export "exploit") (param $f funcref) (result i32)
			(table.set $t (i32.const 0) (local.get $f))
			(call_indirect $t (type $void_to_i32) (i32.const 0)))
	)`)

	runtime := NewRuntime()
	_, err := runtime.InstantiateModule(bytes.NewReader(wasmA))
	if err != nil {
		t.Fatalf("failed to instantiate Module A: %v", err)
	}

	moduleB, err := runtime.InstantiateModule(bytes.NewReader(wasmB))
	if err != nil {
		t.Fatalf("failed to instantiate Module B: %v", err)
	}

	// Attempt to inject store index 0 (Module A's private $secret) as a
	// funcref into Module B. This should be rejected.
	_, err = moduleB.Invoke("exploit", int32(0))
	if err == nil {
		t.Fatal("expected error injecting funcref to inaccessible function")
	}

	// Null references should still be allowed (they'll trap at call_indirect,
	// not at the validation boundary).
	_, err = moduleB.Invoke("exploit", NullReference)
	if err == nil {
		t.Fatal("expected trap from call_indirect with null reference")
	}
}

func TestRuntimeConfusion(t *testing.T) {
	// 1. Create two completely isolated Runtimes
	r1 := NewRuntime()
	r2 := NewRuntime()

	// 2. Instantiate Module A in Runtime 1
	wasmA, _ := wabt.Wat2Wasm(`(module
		(func $secret (result i32) i32.const 42)
		(func (export "wrapper") (result i32) call $secret)
	)`)
	modA, err := r1.InstantiateModule(bytes.NewReader(wasmA))
	if err != nil {
		t.Fatalf("failed to instantiate Module A: %v", err)
	}

	// 3. Instantiate Module B in Runtime 2
	wasmB, _ := wabt.Wat2Wasm(`(module
		(func $secret2 (result i32) i32.const 1337)
		(export "unused" (func $secret2))
	)`)
	_, err = r2.InstantiateModule(bytes.NewReader(wasmB))
	if err != nil {
		t.Fatalf("failed to instantiate Module B: %v", err)
	}

	// 4. Create Module C in Runtime 2, but import the "wrapper" from Runtime 1
	wasmC, _ := wabt.Wat2Wasm(`(module
		(import "env" "wrapper" (func $wrapper (result i32)))
		(func (export "exploit") (result i32) call $wrapper)
	)`)

	imports := NewModuleImports("env").
		AddModuleExports(modA)

	_, err = r2.InstantiateModuleWithImports(bytes.NewReader(wasmC), imports)
	if err == nil {
		t.Fatal("expected error when instantiating module with " +
			"cross-runtime function import")
	}

	expectedErrMsg := "cross-runtime import of env.wrapper is forbidden"
	if err.Error() != expectedErrMsg {
		t.Fatalf("expected error %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestCrossRuntimeMemorySharingIsForbidden(t *testing.T) {
	r1 := NewRuntime()
	r2 := NewRuntime()

	mem := r1.NewMemory(MemoryType{Limits: Limits{Min: 1}})

	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "memory" (memory 1))
	)`)

	imports := NewModuleImports("env").
		AddMemory("memory", mem)

	_, err := r2.InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err == nil {
		t.Fatal("expected error when sharing memory across runtimes")
	}
}

func TestCrossRuntimeTableSharingIsForbidden(t *testing.T) {
	r1 := NewRuntime()
	r2 := NewRuntime()

	table := r1.NewTable(TableType{
		ReferenceType: FuncRefType,
		Limits:        Limits{Min: 1},
	})

	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "table" (table 1 funcref))
	)`)

	imports := NewModuleImports("env").
		AddTable("table", table)

	_, err := r2.InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err == nil {
		t.Fatal("expected error when sharing table across runtimes")
	}
}

func TestCrossRuntimeGlobalSharingIsForbidden(t *testing.T) {
	r1 := NewRuntime()
	r2 := NewRuntime()

	global := r1.NewGlobalI32(42, false)

	wasm, _ := wabt.Wat2Wasm(`(module
		(import "env" "global" (global i32))
	)`)

	imports := NewModuleImports("env").
		AddGlobal("global", global)

	_, err := r2.InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err == nil {
		t.Fatal("expected error when sharing global across runtimes")
	}
}
