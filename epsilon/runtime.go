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
	"maps"
)

// Runtime provides the main API for instantiating and interacting with WASM
// modules.
type Runtime struct {
	vm     *vm
	config Config
}

// NewRuntime creates a new Runtime with default settings.
func NewRuntime() *Runtime {
	return &Runtime{config: DefaultConfig()}
}

// WithConfig sets the configuration for the runtime. Must be called before
// instantiating any modules.
func (r *Runtime) WithConfig(config Config) *Runtime {
	r.config = config
	return r
}

// InstantiateModule parses and instantiates a WASM module from an io.Reader.
func (r *Runtime) InstantiateModule(wasm io.Reader) (*ModuleInstance, error) {
	return r.InstantiateModuleWithImports(wasm, map[string]map[string]any{})
}

// InstantiateModuleWithImports parses and instantiates a WASM module with
// imports.
func (r *Runtime) InstantiateModuleWithImports(
	wasm io.Reader,
	imports ...map[string]map[string]any,
) (*ModuleInstance, error) {
	r.ensureVm()
	module, err := newParser(wasm).parse()
	if err != nil {
		return nil, err
	}

	merged := make(map[string]map[string]any)
	for _, importMap := range imports {
		for moduleName, exports := range importMap {
			if _, exists := merged[moduleName]; !exists {
				merged[moduleName] = make(map[string]any)
			}
			maps.Copy(merged[moduleName], exports)
		}
	}

	return r.vm.instantiate(module, merged)
}

// InstantiateModuleFromBytes is a convenience method to instantiate a WASM
// module from a byte slice.
func (r *Runtime) InstantiateModuleFromBytes(
	data []byte,
) (*ModuleInstance, error) {
	return r.InstantiateModule(bytes.NewReader(data))
}

func (r *Runtime) ensureVm() {
	if r.vm == nil {
		r.vm = newVm(r.config)
	}
}

// ModuleImportBuilder provides a fluent, type-safe API for building import
// objects for a specific WASM module.
//
// Example:
//
//	envImports := epsilon.NewModuleImportBuilder("env").
//	    AddHostFunc("log", func(x int32) { fmt.Println("WASM says:", x) }).
//	    AddMemory("memory", epsilon.NewMemory(epsilon.MemoryType{
//	        Limits: epsilon.Limits{Min: 1},
//	    })).
//	    AddGlobal("offset", int32(1024), false, epsilon.I32).
//	    Build()
//
//	instance, err := runtime.InstantiateModuleWithImports(wasmReader, envImports)
type ModuleImportBuilder struct {
	moduleName string
	imports    map[string]any
}

func (b *ModuleImportBuilder) AddExport(param any, param2 any) {
	panic("unimplemented")
}

func NewModuleImportBuilder(moduleName string) *ModuleImportBuilder {
	return &ModuleImportBuilder{
		moduleName: moduleName,
		imports:    make(map[string]any),
	}
}

func (b *ModuleImportBuilder) AddHostFunc(
	name string,
	fn func(*ModuleInstance, ...any) []any,
) *ModuleImportBuilder {
	b.imports[name] = fn
	return b
}

func (b *ModuleImportBuilder) AddMemory(
	name string,
	memory *Memory,
) *ModuleImportBuilder {
	b.imports[name] = memory
	return b
}

func (b *ModuleImportBuilder) AddTable(
	name string,
	table *Table,
) *ModuleImportBuilder {
	b.imports[name] = table
	return b
}

func (b *ModuleImportBuilder) AddGlobal(
	name string,
	value any,
	mutable bool,
	valueType ValueType,
) *ModuleImportBuilder {
	b.imports[name] = &Global{
		value:   newValue(value),
		Mutable: mutable,
		Type:    valueType,
	}
	return b
}

// AddModuleExports adds all exports from a ModuleInstance as imports.
// This is useful when you want to import functions, memories, tables, or
// globals from one module into another.
func (b *ModuleImportBuilder) AddModuleExports(
	instance *ModuleInstance,
) *ModuleImportBuilder {
	for _, export := range instance.exports {
		b.imports[export.name] = export.value
	}
	return b
}

func (b *ModuleImportBuilder) Build() map[string]map[string]any {
	return map[string]map[string]any{
		b.moduleName: b.imports,
	}
}
