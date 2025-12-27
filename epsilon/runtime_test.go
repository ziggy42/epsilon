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

	imports := NewModuleImportBuilder("env").
		AddHostFunc("multiply", func(m *ModuleInstance, args ...any) []any {
			a := args[0].(int32)
			b := args[1].(int32)
			return []any{a * b}
		}).
		Build()

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

	memory := NewMemory(MemoryType{Limits: Limits{Min: 1}})
	testData := binary.LittleEndian.AppendUint32(nil, 42)
	err := memory.Set(0, 100, testData)
	if err != nil {
		t.Fatalf("failed to set memory: %v", err)
	}

	imports := NewModuleImportBuilder("env").
		AddMemory("memory", memory).
		Build()

	instance, err := NewRuntime().
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

	imports := NewModuleImportBuilder("env").
		AddGlobal("offset", int32(100), false, I32).
		Build()

	instance, err := NewRuntime().
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

	imports := NewModuleImportBuilder("env").
		AddHostFunc("host_sub", func(m *ModuleInstance, args ...any) []any {
			x := args[0].(int32)
			return []any{x - 1}
		}).
		AddTable("table", NewTable(TableType{
			ReferenceType: FuncRefType,
			Limits:        Limits{Min: 2},
		})).
		Build()

	instance, err := NewRuntime().
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
		NewModuleImportBuilder("math").AddModuleExports(module1).Build(),
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
