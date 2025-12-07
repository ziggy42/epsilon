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
	validator := newValidator(ExperimentalFeatures{})

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
	validator := newValidator(ExperimentalFeatures{})

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
	validator := newValidator(ExperimentalFeatures{})

	err = validator.validateModule(module)

	if err != nil {
		t.Fatalf("expected validation success, got error: %v", err)
	}
}
