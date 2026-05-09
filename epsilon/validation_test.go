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
	validator := newValidator(Config{})

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
	validator := newValidator(Config{})

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
	validator := newValidator(Config{})

	err = validator.validateModule(module)

	if err != nil {
		t.Fatalf("expected validation success, got error: %v", err)
	}
}

func TestVuln06(t *testing.T) {
	// Raw WASM bytes from VULN-06 report.
	// This module has 1 type definition (index 0) but tries to use index 1 in a block.
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // 1 type definition
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x08, 0x01, 0x06, 0x00,
		0x02, 0x01, // block (type index 1) — OOB
		0x0b, 0x0b,
	}

	module, err := newParser(bytes.NewReader(wasm)).parse()
	if err != nil {
		t.Fatalf("failed to parse module: %v", err)
	}
	validator := newValidator(Config{})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Recovered from panic: %v", r)
		}
	}()

	err = validator.validateModule(module)
	if err == nil {
		t.Errorf("expected validation error, got nil")
	}
}
