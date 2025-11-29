package epsilon

import (
	"bytes"
	"epsilon/wabt"
	"testing"
)

func getModule(wat string) (*Module, error) {
	wasm, err := wabt.Wat2Wasm(wat)
	if err != nil {
		return nil, err
	}
	return NewParser(bytes.NewReader(wasm)).Parse()
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
	validator := NewValidator()

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
	validator := NewValidator()

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
	validator := NewValidator()

	err = validator.validateModule(module)

	if err != nil {
		t.Fatalf("expected validation success, got error: %v", err)
	}
}
