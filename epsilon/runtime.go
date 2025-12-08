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
	"io"
)

// Runtime provides the main API for instantiating and interacting with WASM
// modules.
type Runtime struct {
	vm *vm
}

// NewRuntime creates a new Runtime with default settings.
func NewRuntime() *Runtime {
	return &Runtime{vm: newVm()}
}

func (r *Runtime) WithFeatures(features ExperimentalFeatures) *Runtime {
	r.vm.features = features
	return r
}

// InstantiateModule parses and instantiates a WASM module from an io.Reader.
func (r *Runtime) InstantiateModule(wasm io.Reader) (*ModuleInstance, error) {
	return r.InstantiateModuleWithImports(wasm, nil)
}

// InstantiateModuleWithImports parses and instantiates a WASM module with
// imports.
func (r *Runtime) InstantiateModuleWithImports(
	wasm io.Reader,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	module, err := newParser(wasm).parse()
	if err != nil {
		return nil, err
	}

	return r.vm.instantiate(module, imports)
}

// InstantiateModuleFromBytes is a convenience method to instantiate a WASM
// module from a byte slice.
func (r *Runtime) InstantiateModuleFromBytes(
	data []byte,
) (*ModuleInstance, error) {
	return r.InstantiateModule(bytes.NewReader(data))
}

// ImportBuilder provides a fluent, type-safe API for building import objects
// for WASM module instantiation.
//
// Example:
//
//	imports := epsilon.NewImportBuilder().
//	    AddHostFunc("env", "log", func(x int32) {
//	        fmt.Println("WASM says:", x)
//	    }).
//	    AddMemory("env", "memory", epsilon.NewMemory(epsilon.MemoryType{
//	        Limits: epsilon.Limits{Min: 1},
//	    })).
//	    AddGlobal("env", "offset", int32(1024), false, epsilon.I32).
//	    Build()
//
//	instance, err := runtime.InstantiateModuleWithImports(wasmReader, imports)
type ImportBuilder struct {
	imports map[string]map[string]any
}

// NewImportBuilder creates a new ImportBuilder.
func NewImportBuilder() *ImportBuilder {
	return &ImportBuilder{
		imports: make(map[string]map[string]any),
	}
}

func (b *ImportBuilder) AddHostFunc(
	module, name string,
	fn func(...any) []any,
) *ImportBuilder {
	b.ensureModule(module)
	b.imports[module][name] = fn
	return b
}

func (b *ImportBuilder) AddMemory(
	module, name string,
	memory *Memory,
) *ImportBuilder {
	b.ensureModule(module)
	b.imports[module][name] = memory
	return b
}

func (b *ImportBuilder) AddTable(
	module, name string,
	table *Table,
) *ImportBuilder {
	b.ensureModule(module)
	b.imports[module][name] = table
	return b
}

func (b *ImportBuilder) AddGlobal(
	module, name string,
	value any,
	mutable bool,
	valueType ValueType,
) *ImportBuilder {
	b.ensureModule(module)
	b.imports[module][name] = &Global{
		Value:   value,
		Mutable: mutable,
		Type:    valueType,
	}
	return b
}

// AddModuleExports adds all exports from a ModuleInstance as imports.
// This is useful when you want to import functions, memories, tables, or
// globals from one module into another.
func (b *ImportBuilder) AddModuleExports(
	module string,
	instance *ModuleInstance,
) *ImportBuilder {
	b.ensureModule(module)
	for _, export := range instance.exports {
		b.imports[module][export.name] = export.value
	}
	return b
}

func (b *ImportBuilder) Build() map[string]map[string]any {
	return b.imports
}

func (b *ImportBuilder) ensureModule(module string) {
	if _, exists := b.imports[module]; !exists {
		b.imports[module] = make(map[string]any)
	}
}
