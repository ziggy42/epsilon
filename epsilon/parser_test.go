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
	"errors"
	"io"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

func parseModule(wat string) (moduleDefinition, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return moduleDefinition{}, err
	}
	module, err := newParser(bytes.NewReader(wasm), DefaultConfig()).parse()
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
				body: []uint64{
					uint64(localGet), 0,
					uint64(localGet), 1,
					uint64(i32Add),
					uint64(end),
				},
				jumpCache:     nil,
				jumpElseCache: nil,
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

	expectedOffsetExpression := []uint64{uint64(i32Const), 0x0, uint64(end)}
	if !slices.Equal(element.offsetExpression, expectedOffsetExpression) {
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

	expectedInitExpression := []uint64{uint64(i32Const), 42, uint64(end)}
	if !slices.Equal(globalVar.initExpression, expectedInitExpression) {
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

	expectedInitExpression := []uint64{uint64(i32Const), 63, uint64(end)}
	if !slices.Equal(globalVar.initExpression, expectedInitExpression) {
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

func TestParseTableTypeRejectsInvalidReferenceType(t *testing.T) {
	_, err := newParser(
		bytes.NewReader([]byte{0x00}),
		DefaultConfig(),
	).parseTableType()
	if err != errInvalidReferenceType {
		t.Fatalf("expected %v, got %v", errInvalidReferenceType, err)
	}
}

func TestParseRejectsInvalidUtf8Names(t *testing.T) {
	// 0xff never appears in well-formed UTF-8.
	tests := []struct {
		name    string
		encoded []byte
		parse   func(*parser) error
	}{
		{
			name:    "import module name",
			encoded: []byte{0x01, 0xff, 0x01, 0x66, 0x00, 0x00},
			parse: func(p *parser) error {
				_, err := p.parseImport()
				return err
			},
		},
		{
			name:    "import name",
			encoded: []byte{0x01, 0x66, 0x01, 0xff, 0x00, 0x00},
			parse: func(p *parser) error {
				_, err := p.parseImport()
				return err
			},
		},
		{
			name:    "export name",
			encoded: []byte{0x01, 0xff, 0x00, 0x00},
			parse: func(p *parser) error {
				_, err := p.parseExport()
				return err
			},
		},
		{
			name:    "custom section name",
			encoded: []byte{0x01, 0xff},
			parse:   (*parser).parseCustomSection,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := newParser(bytes.NewReader(test.encoded), DefaultConfig())
			if err := test.parse(parser); !errors.Is(err, errInvalidUTF8) {
				t.Fatalf("expected %v, got %v", errInvalidUTF8, err)
			}
		})
	}
}

func TestParseElementRejectsInvalidReferenceType(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
	}{
		{
			name:    "passive",
			encoded: []byte{0x05, 0x00},
		},
		{
			name: "active",
			encoded: []byte{
				0x06, 0x00, byte(i32Const), 0x00, byte(end), 0x00,
			},
		},
		{
			name:    "declarative",
			encoded: []byte{0x07, 0x00},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newParser(
				bytes.NewReader(test.encoded),
				DefaultConfig(),
			).parseElementSegment()
			if err != errInvalidReferenceType {
				t.Fatalf("expected %v, got %v", errInvalidReferenceType, err)
			}
		})
	}
}

func TestParseElementRejectsOversizedIndexes(t *testing.T) {
	tooLarge := []byte{0x80, 0x80, 0x80, 0x80, 0x10}
	tests := []struct {
		name    string
		encoded []byte
	}{
		{
			name: "function index",
			encoded: append(
				[]byte{0x00, byte(i32Const), 0x00, byte(end), 0x01},
				tooLarge...,
			),
		},
		{
			name:    "table index with function indexes",
			encoded: append([]byte{0x02}, tooLarge...),
		},
		{
			name:    "table index with expressions",
			encoded: append([]byte{0x06}, tooLarge...),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newParser(
				bytes.NewReader(test.encoded),
				DefaultConfig(),
			).parseElementSegment()
			if err != errIntegerTooLarge {
				t.Fatalf("expected %v, got %v", errIntegerTooLarge, err)
			}
		})
	}
}

func TestReadUleb128(t *testing.T) {
	tests := []struct {
		name     string
		bitWidth uint
		encoded  []byte
		want     uint64
		wantErr  error
	}{
		{
			name:     "u32 maximum",
			bitWidth: 32,
			encoded:  []byte{0xff, 0xff, 0xff, 0xff, 0x0f},
			want:     math.MaxUint32,
		},
		{
			name:     "u32 padded zero",
			bitWidth: 32,
			encoded:  []byte{0x80, 0x80, 0x80, 0x80, 0x00},
		},
		{
			name:     "u32 unused bits",
			bitWidth: 32,
			encoded:  []byte{0x80, 0x80, 0x80, 0x80, 0x10},
			wantErr:  errIntegerTooLarge,
		},
		{
			name:     "u32 continuation after last byte",
			bitWidth: 32,
			encoded:  []byte{0x80, 0x80, 0x80, 0x80, 0x80},
			wantErr:  errIntRepresentationTooLong,
		},
		{
			name:     "u64 maximum",
			bitWidth: 64,
			encoded: []byte{
				0xff, 0xff, 0xff, 0xff, 0xff,
				0xff, 0xff, 0xff, 0xff, 0x01,
			},
			want: math.MaxUint64,
		},
		{
			name:     "u64 padded two",
			bitWidth: 64,
			encoded: []byte{
				0x82, 0x80, 0x80, 0x80, 0x80,
				0x80, 0x80, 0x80, 0x80, 0x00,
			},
			want: 2,
		},
		{
			name:     "u64 unused low bits",
			bitWidth: 64,
			encoded: []byte{
				0x82, 0x80, 0x80, 0x80, 0x80,
				0x80, 0x80, 0x80, 0x80, 0x10,
			},
			wantErr: errIntegerTooLarge,
		},
		{
			name:     "u64 unused high bits",
			bitWidth: 64,
			encoded: []byte{
				0x82, 0x80, 0x80, 0x80, 0x80,
				0x80, 0x80, 0x80, 0x80, 0x40,
			},
			wantErr: errIntegerTooLarge,
		},
		{
			name:     "u64 continuation after last byte",
			bitWidth: 64,
			encoded: []byte{
				0x80, 0x80, 0x80, 0x80, 0x80,
				0x80, 0x80, 0x80, 0x80, 0x80,
			},
			wantErr: errIntRepresentationTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := newParser(bytes.NewReader(test.encoded), DefaultConfig())
			got, err := parser.readUleb128(test.bitWidth)
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
			if got != test.want {
				t.Fatalf("expected value %d, got %d", test.want, got)
			}
		})
	}
}

func TestReadCodeDecodesLaneIndexesAsBytes(t *testing.T) {
	shuffleEncoded := []byte{0xfd, 0x0d}
	shuffleEncoded = append(
		shuffleEncoded,
		bytes.Repeat([]byte{0x80}, 16)...,
	)
	shuffleEncoded = append(shuffleEncoded, byte(end))
	shuffleBytecode := []uint64{uint64(i8x16Shuffle)}
	for range 16 {
		shuffleBytecode = append(shuffleBytecode, 0x80)
	}
	shuffleBytecode = append(shuffleBytecode, uint64(end))

	tests := []struct {
		name     string
		encoded  []byte
		bytecode []uint64
	}{
		{
			name: "extract lane",
			encoded: []byte{
				0xfd, 0x15, 0x80, byte(unreachable), byte(end),
			},
			bytecode: []uint64{
				uint64(i8x16ExtractLaneS),
				0x80,
				uint64(unreachable),
				uint64(end),
			},
		},
		{
			name: "load lane",
			encoded: []byte{
				0xfd, 0x54,
				0x00,
				0x00,
				0x80,
				byte(unreachable),
				byte(end),
			},
			bytecode: []uint64{
				uint64(v128Load8Lane),
				0,
				0,
				0,
				0x80,
				uint64(unreachable),
				uint64(end),
			},
		},
		{
			name:     "shuffle",
			encoded:  shuffleEncoded,
			bytecode: shuffleBytecode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := newParser(bytes.NewReader(test.encoded), DefaultConfig())
			result, err := parser.readCode(uint32(len(test.encoded)), nil)
			if err != nil {
				t.Fatalf("readCode failed: %v", err)
			}
			if !slices.Equal(result.bytecode, test.bytecode) {
				t.Fatalf(
					"expected bytecode %v, got %v",
					test.bytecode,
					result.bytecode,
				)
			}
		})
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
	expectedOffsetExpression := []uint64{uint64(i32Const), 0x0, uint64(end)}
	if !slices.Equal(data.offsetExpression, expectedOffsetExpression) {
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
	expectedOffsetExpression := []uint64{uint64(i32Const), 0x0, uint64(end)}
	if !slices.Equal(data.offsetExpression, expectedOffsetExpression) {
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

func TestParseDataCountAsUint32(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
		wantErr error
	}{
		{
			name:    "five-byte padded zero",
			encoded: []byte{0x80, 0x80, 0x80, 0x80, 0x00},
		},
		{
			name:    "six-byte zero",
			encoded: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00},
			wantErr: errIntRepresentationTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wasm := []byte{
				0x00, 0x61, 0x73, 0x6d,
				0x01, 0x00, 0x00, 0x00,
				byte(dataCountSectionId),
				byte(len(test.encoded)),
			}
			wasm = append(wasm, test.encoded...)

			_, err := newParser(
				bytes.NewReader(wasm),
				DefaultConfig(),
			).parse()
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
		})
	}
}

func TestParseLocalCountAsUint32(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
		wantErr error
	}{
		{
			name: "five-byte padded zero",
			encoded: []byte{
				0x80, 0x80, 0x80, 0x80, 0x00,
				0x7f,
			},
		},
		{
			name: "six-byte zero",
			encoded: []byte{
				0x80, 0x80, 0x80, 0x80, 0x80, 0x00,
				0x7f,
			},
			wantErr: errIntRepresentationTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newParser(
				bytes.NewReader(test.encoded),
				DefaultConfig(),
			).parseLocalVariables()
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
		})
	}
}

func TestParseTruncatedFunctionBody(t *testing.T) {
	// Truncated function body where the last byte is 0x0B (end), but it's
	// actually an immediate for i32.const.
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // header
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section
		0x03, 0x02, 0x01, 0x00, // function section
		0x0a, 0x05, 0x01, 0x03, 0x00, 0x41, 0x0b, // i32.const 0x0B (truncated)
	}

	p := newParser(bytes.NewReader(wasm), DefaultConfig())
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error for truncated function body, got nil")
	}
	if err != errMissingEndOpcode {
		t.Errorf("expected errMissingEndOpcode, got %v", err)
	}
}

func TestParseSectionPayloadBounds(t *testing.T) {
	header := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	}
	tests := []struct {
		name    string
		section []byte
		wantErr error
	}{
		{
			name:    "content outside payload",
			section: []byte{byte(typeSectionId), 0x00, 0x00},
			wantErr: io.EOF,
		},
		{
			name:    "payload shorter than declared",
			section: []byte{byte(typeSectionId), 0x02, 0x00},
			wantErr: errSectionSizeMismatch,
		},
		{
			name:    "trailing payload content",
			section: []byte{byte(typeSectionId), 0x02, 0x00, 0x00},
			wantErr: errSectionSizeMismatch,
		},
		{
			name: "function body outside payload",
			section: []byte{
				byte(codeSectionId), 0x03, 0x01, 0x02, 0x00,
			},
			wantErr: io.ErrUnexpectedEOF,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wasm := append(slices.Clone(header), test.section...)
			_, err := newParser(
				bytes.NewReader(wasm),
				DefaultConfig(),
			).parse()
			if err != test.wantErr {
				t.Fatalf("expected %v, got %v", test.wantErr, err)
			}
		})
	}
}

func TestParseCustomSectionDoesNotReadPastPayload(t *testing.T) {
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		byte(customSectionId), 0x01, 0x01,
		byte(typeSectionId), 0x01, 0x00,
	}
	reader := bytes.NewReader(wasm)
	_, err := newParser(reader, DefaultConfig()).parse()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
	if reader.Len() != 3 {
		t.Fatalf(
			"expected next section to remain unread, got %d bytes",
			reader.Len(),
		)
	}
}

func TestReadOpcodeRejectsNamespaceAliasing(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
		want    opcode
		wantErr error
	}{
		{
			name:    "FC cannot alias FD",
			encoded: []byte{0xFC, 0x80, 0x02},
			wantErr: errPrefixedOpcodeOutOfRange,
		},
		{
			name: "FD cannot wrap to a single-byte opcode",
			encoded: []byte{
				0xFD, 0x80, 0x86, 0xFC, 0xFF, 0x0F,
			},
			wantErr: errPrefixedOpcodeOutOfRange,
		},
		{
			name:    "largest representable subopcode",
			encoded: []byte{0xFD, 0xFF, 0x01},
			want:    f64x2ConvertLowI32x4U,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := newParser(
				bytes.NewReader(test.encoded),
				DefaultConfig(),
			).readOpcode()
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
			if got != test.want {
				t.Fatalf("expected opcode %#x, got %#x", test.want, got)
			}
		})
	}
}
