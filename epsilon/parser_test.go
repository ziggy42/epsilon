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
	"reflect"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

func parseModule(wat string) (moduleDefinition, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return moduleDefinition{}, err
	}
	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		return moduleDefinition{}, err
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
	expectedModule := moduleDefinition{
		types: []FunctionType{
			{
				ParamTypes:  []ValueType{I32, I32},
				ResultTypes: []ValueType{I32},
			},
		},
		funcs: []function{
			{
				typeIndex: 0,
				locals:    []ValueType{},
				body:      []byte{byte(localGet), 0, byte(localGet), 1, byte(i32Add)},
			},
		},
		exports: []export{
			{
				name:      "sum",
				indexType: functionExportKind,
				index:     0,
			},
		},
		startIndex: nil,
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
	if !reflect.DeepEqual(expectedLocals, module.funcs[0].locals) {
		t.Errorf(
			"parseModule() result mismatch:\n\nwant: %+v\n\ngot:  %+v",
			expectedLocals,
			module.funcs[0].locals,
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

	if len(module.elementSegments) != 1 {
		t.Fatalf("expected 1 element, got %d", len(module.elementSegments))
	}

	element := module.elementSegments[0]
	if element.mode != activeElementMode {
		t.Fatalf("expected active element, got mode %d", element.mode)
	}

	if element.tableIndex != 0 {
		t.Fatalf("expected table index 0, got %d", element.tableIndex)
	}

	expectedOffsetExpression := []byte{byte(i32Const), 0x0}
	if !bytes.Equal(element.offsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset %v, got %v",
			expectedOffsetExpression,
			element.offsetExpression,
		)
	}
	if element.kind != FuncRefType {
		t.Fatalf("expected FuncRefType, got %d", element.kind)
	}

	if len(element.functionIndexes) != 2 {
		t.Fatalf("expected 2 func indexes, got %d", len(element.functionIndexes))
	}

	if element.functionIndexes[0] != 0 {
		t.Fatalf("expected func index 0, got %d", element.functionIndexes[0])
	}

	if element.functionIndexes[1] != 1 {
		t.Fatalf("expected func index 1, got %d", element.functionIndexes[1])
	}

	if len(element.functionIndexesExpressions) != 0 {
		t.Fatalf(
			"expected 0 func indexes expressions, got %d",
			len(element.functionIndexesExpressions),
		)
	}
}

func TestParseGlobalVariable(t *testing.T) {
	wat := "(module (global $g (mut i32) (i32.const 42)))"

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if len(module.globalVariables) != 1 {
		t.Fatalf("expected 1 global variable, got %d", len(module.globalVariables))
	}

	globalVar := module.globalVariables[0]
	if !globalVar.globalType.IsMutable {
		t.Error("expected global variable to be mutable")
	}

	if globalVar.globalType.ValueType != I32 {
		t.Errorf(
			"expected value type %d, got %d",
			I32,
			globalVar.globalType.ValueType,
		)
	}

	expectedInitExpression := []byte{byte(i32Const), 42}
	if !bytes.Equal(globalVar.initExpression, expectedInitExpression) {
		t.Errorf(
			"expected init expression %v, got %v",
			expectedInitExpression,
			globalVar.initExpression,
		)
	}
}

func TestParseImmutableGlobalVariable(t *testing.T) {
	wat := "(module (global $g i32 (i32.const 63)))"

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}

	if len(module.globalVariables) != 1 {
		t.Fatalf("expected 1 global variable, got %d", len(module.globalVariables))
	}

	globalVar := module.globalVariables[0]
	if globalVar.globalType.IsMutable {
		t.Error("expected global variable to be immutable")
	}

	if globalVar.globalType.ValueType != I32 {
		t.Errorf(
			"expected value type %d, got %d",
			I32,
			globalVar.globalType.ValueType,
		)
	}

	expectedInitExpression := []byte{byte(i32Const), 63}
	if !bytes.Equal(globalVar.initExpression, expectedInitExpression) {
		t.Errorf(
			"expected init expression %v, got %v",
			expectedInitExpression,
			globalVar.initExpression,
		)
	}
}

func TestParseImportFunction(t *testing.T) {
	wat := `(module (import "console" "log" (func $log (param i32))))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.imports))
	}
	imp := module.imports[0]
	if imp.moduleName != "console" {
		t.Errorf("expected module name \"console\", got %s", imp.moduleName)
	}
	if imp.name != "log" {
		t.Errorf("expected name \"log\", got %s", imp.name)
	}
	if _, ok := imp.importType.(functionTypeIndex); !ok {
		t.Errorf("expected import type FunctionTypeIndex, got %T", imp.importType)
	}
}

func TestParseImportTable(t *testing.T) {
	wat := `(module (import "module" "table" (table 1 funcref)))`

	module, err := parseModule(wat)
	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.imports))
	}
	imp := module.imports[0]
	if imp.moduleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.moduleName)
	}
	if imp.name != "table" {
		t.Errorf("expected name \"table\", got %s", imp.name)
	}
	tableType, ok := imp.importType.(TableType)
	if !ok {
		t.Fatalf("expected import type TableType, got %T", imp.importType)
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
	if len(module.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.imports))
	}
	imp := module.imports[0]
	if imp.moduleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.moduleName)
	}
	if imp.name != "memory" {
		t.Errorf("expected name \"memory\", got %s", imp.name)
	}
	memoryType, ok := imp.importType.(MemoryType)
	if !ok {
		t.Fatalf("expected import type MemoryType, got %T", imp.importType)
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
	if len(module.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(module.imports))
	}
	imp := module.imports[0]
	if imp.moduleName != "module" {
		t.Errorf("expected module name \"module\", got %s", imp.moduleName)
	}
	if imp.name != "global" {
		t.Errorf("expected name \"global\", got %s", imp.name)
	}
	globalType, ok := imp.importType.(GlobalType)
	if !ok {
		t.Fatalf("expected import type GlobalType, got %T", imp.importType)
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
	if len(module.dataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.dataSegments))
	}
	data := module.dataSegments[0]
	if data.mode != activeDataMode {
		t.Fatalf("expected active data segment, got mode %d", data.mode)
	}
	if data.memoryIndex != 0 {
		t.Fatalf("expected memory index 0, got %d", data.memoryIndex)
	}
	expectedOffsetExpression := []byte{byte(i32Const), 0x0}
	if !bytes.Equal(data.offsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset expression %v, got %v",
			expectedOffsetExpression,
			data.offsetExpression,
		)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.content, expectedContent) {
		t.Fatalf("expected content %v, got %v", expectedContent, data.content)
	}
}

func TestParseActiveDataSegmentWithMemoryIndex(t *testing.T) {
	wat := `(module (memory 1) (data (memory 0) (i32.const 0) "\01\02"))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.dataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.dataSegments))
	}
	data := module.dataSegments[0]
	if data.mode != activeDataMode {
		t.Fatalf("expected active data segment, got mode %d", data.mode)
	}
	if data.memoryIndex != 0 {
		t.Fatalf("expected memory index 0, got %d", data.memoryIndex)
	}
	expectedOffsetExpression := []byte{byte(i32Const), 0x0}
	if !bytes.Equal(data.offsetExpression, expectedOffsetExpression) {
		t.Fatalf(
			"expected offset expression %v, got %v",
			expectedOffsetExpression,
			data.offsetExpression,
		)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.content, expectedContent) {
		t.Fatalf("expected content %v, got %v", expectedContent, data.content)
	}
}

func TestParsePassiveDataSegment(t *testing.T) {
	wat := `(module (memory 1) (data "\01\02"))`

	module, err := parseModule(wat)

	if err != nil {
		t.Fatalf("parsing module failed: %v", err)
	}
	if len(module.dataSegments) != 1 {
		t.Fatalf("expected 1 data segment, got %d", len(module.dataSegments))
	}
	data := module.dataSegments[0]
	if data.mode != passiveDataMode {
		t.Fatalf("expected passive data segment, got mode %d", data.mode)
	}
	expectedContent := []byte{0x01, 0x02}
	if !bytes.Equal(data.content, expectedContent) {
		t.Fatalf(
			"expected content %v, got %v",
			expectedContent,
			data.content,
		)
	}
}
