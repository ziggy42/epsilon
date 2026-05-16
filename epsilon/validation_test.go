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
	"strings"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

func getModule(wat string) (*moduleDefinition, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return nil, err
	}
	return newParser(bytes.NewReader(wasm)).parse()
}

func TestInvalidDataUnknownGlobal(t *testing.T) {
	wat := `(module 
		(memory 1)
		(global i32 (i32.const 0))
		(data (global.get 0) "z")
	)`
	module, err := getModule(wat)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}
	validator := newValidator(DefaultConfig())

	err = validator.validateModule(module)

	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

func TestInvalidDataUnknownMemory(t *testing.T) {
	wat := `(module (data (i32.const 0) ""))`
	module, err := getModule(wat)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}
	validator := newValidator(DefaultConfig())

	err = validator.validateModule(module)

	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

func TestValidDataMemory(t *testing.T) {
	wat := `(module (memory 1) (data (i32.const 0) ""))`
	module, err := getModule(wat)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}
	validator := newValidator(DefaultConfig())

	err = validator.validateModule(module)

	if err != nil {
		t.Fatalf("expected validation success, got error: %v", err)
	}
}

func TestVuln06(t *testing.T) {
	// We use raw bytes because standard assemblers like wat2wasm often
	// catch or "fix" OOB indices before they reach the engine.
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // 1 type definition
		0x03, 0x02, 0x01, 0x00, // 1 function, type index 0
		0x0a, 0x07, 0x01, 0x05, 0x00, // code size 7, function body size 5
		0x02, 0x01, // block, type index 1 (OOB)
		0x0b, 0x0b, // end of block, end of function
	}

	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}
	validator := newValidator(DefaultConfig())

	err = validator.validateModule(module)
	if err == nil {
		t.Errorf("expected validation error for OOB block type index, got nil")
	}
}

func TestNonCanonicalBlockType(t *testing.T) {
	// Block type is decoded as s33 SLEB128. The valid negative encodings are
	// exactly the valtype codes (-1..-5, -16, -17) and -64 (empty). The bytes
	// 0xff 0x7e are a canonical 2-byte SLEB128 of -129, which is not a legal
	// block type and must be rejected; an earlier implementation masked it with
	// 0x7f and silently treated it as i32.
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type () -> ()
		0x03, 0x02, 0x01, 0x00, // 1 function of type 0
		0x0a, 0x0b, 0x01,
		0x09,             // body size
		0x00,             // local count
		0x02, 0xff, 0x7e, // block, blocktype = SLEB128(-129)
		0x41, 0x00, // i32.const 0
		0x0b, // end of block
		0x1a, // drop
		0x0b, // end of function
	}

	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	err = newValidator(DefaultConfig()).validateModule(module)
	if err == nil {
		t.Fatalf("expected validation error for non-canonical block type, got nil")
	}
}

func TestMemoryIndexValidation(t *testing.T) {
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // header
		0x01, 0x05, 0x01, 0x60, 0x00, 0x00, // type section
		0x03, 0x02, 0x01, 0x00, // function section
		0x05, 0x03, 0x01, 0x00, 0x01, // memory section
		0x0a, 0x0a, 0x01, 0x08, // code section
		0x00,       // local count
		0x41, 0x00, // i32.const 0
		0x28, 0x40, 0x01, 0x00, // i32.load align=0x40, memIndex=1, offset=0
		0x0b, // end
	}

	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	config := DefaultConfig()
	config.ExperimentalMultipleMemories = true
	validator := newValidator(config)
	err = validator.validateModule(module)
	if err == nil {
		t.Errorf("expected validation error for OOB memory index, got nil")
	}
}

func TestMaxTableElementsRejectsOversizedTable(t *testing.T) {
	module, err := getModule(`(module (table 1001 funcref))`)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	err = newValidator(Config{MaxTableElements: 1000}).validateModule(module)
	if !errors.Is(err, errInvalidLimits) {
		t.Fatalf("expected errInvalidLimits, got %v", err)
	}

	err = newValidator(Config{MaxTableElements: 2000}).validateModule(module)
	if err != nil {
		t.Fatalf("expected success with raised limit, got %v", err)
	}
}

func TestMaxTableElementsRejectsOversizedTableMax(t *testing.T) {
	module, err := getModule(`(module (table 1 2000 funcref))`)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	err = newValidator(Config{MaxTableElements: 1000}).validateModule(module)
	if !errors.Is(err, errInvalidLimits) {
		t.Fatalf("expected errInvalidLimits, got %v", err)
	}

	err = newValidator(Config{MaxTableElements: 5000}).validateModule(module)
	if err != nil {
		t.Fatalf("expected success with raised limit, got %v", err)
	}
}

func TestMaxMemoryPagesRejectsOversizedMemory(t *testing.T) {
	module, err := getModule(`(module (memory 101))`)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	err = newValidator(Config{MaxMemoryPages: 100}).validateModule(module)
	if !errors.Is(err, errInvalidLimits) {
		t.Fatalf("expected errInvalidLimits, got %v", err)
	}

	err = newValidator(Config{MaxMemoryPages: 200}).validateModule(module)
	if err != nil {
		t.Fatalf("expected success with raised limit, got %v", err)
	}
}

func TestMaxMemoryPagesRejectsOversizedMemoryMax(t *testing.T) {
	module, err := getModule(`(module (memory 1 200))`)
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}

	err = newValidator(Config{MaxMemoryPages: 100}).validateModule(module)
	if !errors.Is(err, errInvalidLimits) {
		t.Fatalf("expected errInvalidLimits, got %v", err)
	}

	err = newValidator(Config{MaxMemoryPages: 300}).validateModule(module)
	if err != nil {
		t.Fatalf("expected success with raised limit, got %v", err)
	}
}

func TestMaxLocalsPerFunctionRejectsOversizedFunction(t *testing.T) {
	var watBuilder strings.Builder
	watBuilder.WriteString("(module (func (local")
	const localCount = 101
	for i := 0; i < localCount; i++ {
		watBuilder.WriteString(" i32")
	}
	watBuilder.WriteString(")))")

	wasm, err := wabt.Wat2Wasm(watBuilder.String())
	if err != nil {
		t.Fatalf("failed to assemble wat: %v", err)
	}

	_, err = newParserWithConfig(
		bytes.NewReader(wasm),
		Config{MaxLocalsPerFunction: 100},
	).parse()
	if err == nil || !strings.Contains(err.Error(), "too many locals") {
		t.Fatalf("expected too-many-locals error, got %v", err)
	}
	if !strings.Contains(err.Error(), "101") ||
		!strings.Contains(err.Error(), "100") {
		t.Fatalf("expected error to mention actual count and configured limit, got %v", err)
	}

	_, err = newParserWithConfig(
		bytes.NewReader(wasm),
		Config{MaxLocalsPerFunction: 200},
	).parse()
	if err != nil {
		t.Fatalf("expected success with raised limit, got %v", err)
	}
}
