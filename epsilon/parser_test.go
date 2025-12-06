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
	"epsilon/internal/wabt"
	"reflect"
	"testing"
)

func parseModule(wat string) (Module, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return Module{}, err
	}
	module, err := NewParser(bytes.NewReader(wasm)).Parse()
	if err != nil {
		return Module{}, err
	}
	return *module, nil
}

func TestParseEmptyModule(t *testing.T) {
	_, err := parseModule("(module)")
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
}

func TestParseExportedFunction(t *testing.T) {
	wat := `(module
  (func (export "sum") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)
  )`
	expectedModule := Module{
		Types: []FunctionType{
			{
				ParamTypes:  []ValueType{I32, I32},
				ResultTypes: []ValueType{I32},
			},
		},
		Funcs: []Function{
			{
				TypeIndex: 0,
				Locals:    []ValueType{},
				Body:      []byte{byte(LocalGet), 0, byte(LocalGet), 1, byte(I32Add)},
			},
		},
		Exports: []Export{
			{
				Name:      "sum",
				IndexType: FunctionExportKind,
				Index:     0,
			},
		},
		StartIndex: nil,
	}

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if !reflect.DeepEqual(expectedModule, module) {
		t.Errorf(
			"parseModule() result mismatch:\n\nwant: %+v\n\ngot:  %+v",
			expectedModule,
			module,
		)
	}
}

func TestParseLocals(t *testing.T) {
	wat := `(module
		(func (export "mul") (param $a i32) (param $b i32) (result i32)
			(local $sum i32)
			(local.set $sum (i32.const 0))
			(block
				(loop
					(if (local.get $b) (i32.const 0) (i32.eq)
						(then (br 2)))
					(local.set $sum (i32.add (local.get $sum) (local.get $a)))
					(local.set $b (i32.sub (local.get $b) (i32.const 1)))
					(br 0)
				)
			)
			(local.get $sum))
	)`

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	expectedLocals := []ValueType{I32}
	if !reflect.DeepEqual(expectedLocals, module.Funcs[0].Locals) {
		t.Errorf(
			"parseModule() result mismatch:\n\nwant: %+v\n\ngot:  %+v",
			expectedLocals,
			module.Funcs[0].Locals,
		)
	}
}

func TestParseActiveElement(t *testing.T) {
	wat := `(module
		(table $t 2 funcref)

		(func $add (param $a i32) (param $b i32) (result i32)
			local.get $a
			local.get $b
			i32.add)

		(func $sub (param $a i32) (param $b i32) (result i32)
			local.get $a
			local.get $b
			i32.sub)

		(elem (table $t) (i32.const 0) func $add $sub)
  )`
	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if len(module.ElementSegments) != 1 {
		t.Fatalf("expected 1 element, got %d", len(module.ElementSegments))
	}

	element := module.ElementSegments[0]
	if element.Mode != ActiveElementMode {
		t.Fatalf("expected active element, got mode %d", element.Mode)
	}

	if element.TableIndex != 0 {
		t.Fatalf("expected table index 0, got %d", element.TableIndex)
	}

	expectedOffsetExpression := []byte{byte(I32Const), 0x0}
	if !bytes.Equal(element.OffsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset %v, got %v",
			expectedOffsetExpression,
			element.OffsetExpression,
		)
	}
	if element.Kind != FuncRefType {
		t.Fatalf("expected FuncRefType, got %d", element.Kind)
	}

	if len(element.FuncIndexes) != 2 {
		t.Fatalf("expected 2 func indexes, got %d", len(element.FuncIndexes))
	}

	if element.FuncIndexes[0] != 0 {
		t.Fatalf("expected func index 0, got %d", element.FuncIndexes[0])
	}

	if element.FuncIndexes[1] != 1 {
		t.Fatalf("expected func index 1, got %d", element.FuncIndexes[1])
	}

	if len(element.FuncIndexesExpressions) != 0 {
		t.Fatalf(
			"expected 0 func indexes expressions, got %d",
			len(element.FuncIndexesExpressions),
		)
	}
}

func TestParseGlobalVariable(t *testing.T) {
	wat := "(module (global $g (mut i32) (i32.const 42)))"

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if len(module.GlobalVariables) != 1 {
		t.Fatalf("expected 1 global variable, got %d", len(module.GlobalVariables))
	}

	globalVar := module.GlobalVariables[0]
	if !globalVar.GlobalType.IsMutable {
		t.Error("expected global variable to be mutable")
	}

	if globalVar.GlobalType.ValueType != I32 {
		t.Errorf(
			"expected value type %d, got %d",
			I32,
			globalVar.GlobalType.ValueType,
		)
	}

	expectedInitExpression := []byte{byte(I32Const), 42}
	if !bytes.Equal(globalVar.InitExpression, expectedInitExpression) {
		t.Errorf(
			"expected init expression %v, got %v",
			expectedInitExpression,
			globalVar.InitExpression,
		)
	}
}

func TestParseImmutableGlobalVariable(t *testing.T) {
	wat := "(module (global $g i32 (i32.const 63)))"

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if len(module.GlobalVariables) != 1 {
		t.Fatalf("expected 1 global variable, got %d", len(module.GlobalVariables))
	}

	globalVar := module.GlobalVariables[0]
	if globalVar.GlobalType.IsMutable {
		t.Error("expected global variable to be immutable")
	}

	if globalVar.GlobalType.ValueType != I32 {
		t.Errorf(
			"expected value type %d, got %d",
			I32,
			globalVar.GlobalType.ValueType,
		)
	}

	expectedInitExpression := []byte{byte(I32Const), 63}
	if !bytes.Equal(globalVar.InitExpression, expectedInitExpression) {
		t.Errorf(
			"expected init expression %v, got %v",
			expectedInitExpression,
			globalVar.InitExpression,
		)
	}
}

func TestParseImportFunction(t *testing.T) {
	wat := `(module (import "console" "log" (func $log (param i32))))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.Imports))
	}
	imp := module.Imports[0]
	if imp.ModuleName != "console" {
		t.Errorf("expected module name \"console\", got %s", imp.ModuleName)
	}
	if imp.Name != "log" {
		t.Errorf("expected name \"log\", got %s", imp.Name)
	}
	if imp.Type != FunctionTypeIndex(0) {
		t.Errorf("expected import type FunctionTypeIndex(0), got %v", imp.Type)
	}
}

func TestParseImportTable(t *testing.T) {
	wat := `(module (import "module" "table" (table 1 funcref)))`

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.Imports))
	}
	imp := module.Imports[0]
	if imp.ModuleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.ModuleName)
	}
	if imp.Name != "table" {
		t.Errorf("expected name \"table\", got %s", imp.Name)
	}
	tableType, ok := imp.Type.(TableType)
	if !ok {
		t.Fatalf("expected import type TableType, got %T", imp.Type)
	}
	if tableType.Limits.Min != 1 {
		t.Errorf("expected limits min 1, got %d", tableType.Limits.Min)
	}
	if tableType.Limits.Max != nil {
		t.Errorf("expected limits max nil, got %d", *tableType.Limits.Max)
	}
	if tableType.ReferenceType != FuncRefType {
		t.Errorf(
			"expected reference type FuncRefType, got %v",
			tableType.ReferenceType,
		)
	}
}

func TestParseImportMemory(t *testing.T) {
	wat := `(module (import "module" "memory" (memory 1)))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.Imports))
	}
	imp := module.Imports[0]
	if imp.ModuleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.ModuleName)
	}
	if imp.Name != "memory" {
		t.Errorf("expected name \"memory\", got %s", imp.Name)
	}
	memoryType, ok := imp.Type.(MemoryType)
	if !ok {
		t.Fatalf("expected import type MemoryType, got %T", imp.Type)
	}
	if memoryType.Limits.Min != 1 {
		t.Errorf("expected limits min 1, got %d", memoryType.Limits.Min)
	}
	if memoryType.Limits.Max != nil {
		t.Errorf("expected limits max nil, got %d", *memoryType.Limits.Max)
	}
}

func TestParseImportGlobal(t *testing.T) {
	wat := `(module (import "module" "global" (global i32)))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.Imports))
	}
	imp := module.Imports[0]
	if imp.ModuleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.ModuleName)
	}
	if imp.Name != "global" {
		t.Errorf("expected name \"global\", got %s", imp.Name)
	}
	globalType, ok := imp.Type.(GlobalType)
	if !ok {
		t.Fatalf("expected import type GlobalType, got %T", imp.Type)
	}
	if globalType.IsMutable {
		t.Error("expected global to be immutable")
	}
	if globalType.ValueType != I32 {
		t.Errorf("expected value type I32, got %v", globalType.ValueType)
	}
}

func TestParseActiveDataSegment(t *testing.T) {
	wat := `(module (memory 1) (data (i32.const 0) "\01\02"))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.DataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.DataSegments))
	}
	data := module.DataSegments[0]
	if data.Mode != ActiveDataMode {
		t.Fatalf("expected active data segment, got mode %d", data.Mode)
	}
	if data.MemoryIndex != 0 {
		t.Fatalf("expected memory index 0, got %d", data.MemoryIndex)
	}
	expectedOffsetExpression := []byte{byte(I32Const), 0x0}
	if !bytes.Equal(data.OffsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset expression %v, got %v",
			expectedOffsetExpression,
			data.OffsetExpression,
		)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.Content, expectedContent) {
		t.Fatalf("expected content %v, got %v", expectedContent, data.Content)
	}
}

func TestParseActiveDataSegmentWithMemoryIndex(t *testing.T) {
	wat := `(module (memory 1) (data (memory 0) (i32.const 0) "\01\02"))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.DataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.DataSegments))
	}
	data := module.DataSegments[0]
	if data.Mode != ActiveDataMode {
		t.Fatalf("expected active data segment, got mode %d", data.Mode)
	}
	if data.MemoryIndex != 0 {
		t.Fatalf("expected memory index 0, got %d", data.MemoryIndex)
	}
	expectedOffsetExpression := []byte{byte(I32Const), 0x0}
	if !bytes.Equal(data.OffsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset expression %v, got %v",
			expectedOffsetExpression,
			data.OffsetExpression,
		)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.Content, expectedContent) {
		t.Fatalf("expected content %v, got %v", expectedContent, data.Content)
	}
}

func TestParsePassiveDataSegment(t *testing.T) {
	wat := `(module (memory 1) (data "\01\02"))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.DataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.DataSegments))
	}
	data := module.DataSegments[0]
	if data.Mode != PassiveDataMode {
		t.Fatalf("expected passive data segment, got mode %d", data.Mode)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.Content, expectedContent) {
		t.Fatalf(
			"expected content %v, got %v",
			expectedContent,
			data.Content,
		)
	}
}
