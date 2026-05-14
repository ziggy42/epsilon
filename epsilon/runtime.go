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
	"fmt"
	"io"
)

// Runtime provides the main API for instantiating and interacting with WASM
// modules.
type Runtime struct {
	vm     *vm
	config Config
}

// NewRuntime creates a new Runtime with default configuration.
func NewRuntime() *Runtime {
	return NewRuntimeWithConfig(DefaultConfig())
}

// NewRuntimeWithConfig creates a new Runtime with the given configuration.
func NewRuntimeWithConfig(config Config) *Runtime {
	return &Runtime{config: config, vm: newVm(config)}
}

// NewMemory creates a new Memory instance from a MemoryType.
func (r *Runtime) NewMemory(memType MemoryType) *Memory {
	return newMemory(r.vm, memType)
}

// NewTable creates a new Table instance from a TableType.
func (r *Runtime) NewTable(tt TableType) *Table {
	return newTable(r.vm, tt)
}

// NewGlobal creates a new Global instance.
func (r *Runtime) NewGlobal(
	value any,
	mutable bool,
	valueType ValueType,
) *Global {
	return newGlobal(r.vm, value, mutable, valueType)
}

// NewGlobalI32 creates a new I32 Global instance.
func (r *Runtime) NewGlobalI32(value int32, mutable bool) *Global {
	return r.NewGlobal(value, mutable, I32)
}

// NewGlobalI64 creates a new I64 Global instance.
func (r *Runtime) NewGlobalI64(value int64, mutable bool) *Global {
	return r.NewGlobal(value, mutable, I64)
}

// NewGlobalF32 creates a new F32 Global instance.
func (r *Runtime) NewGlobalF32(value float32, mutable bool) *Global {
	return r.NewGlobal(value, mutable, F32)
}

// NewGlobalF64 creates a new F64 Global instance.
func (r *Runtime) NewGlobalF64(value float64, mutable bool) *Global {
	return r.NewGlobal(value, mutable, F64)
}

// InstantiateModule parses and instantiates a WASM module from an io.Reader.
func (r *Runtime) InstantiateModule(wasm io.Reader) (*ModuleInstance, error) {
	return r.InstantiateModuleWithImports(wasm)
}

// InstantiateModuleWithImports parses and instantiates a WASM module with
// imports.
func (r *Runtime) InstantiateModuleWithImports(
	wasm io.Reader,
	imports ...*ModuleImports,
) (*ModuleInstance, error) {
	module, err := newParser(wasm).parse()
	if err != nil {
		return nil, err
	}

	merged := make(map[string]map[string]any)
	for _, mi := range imports {
		if _, exists := merged[mi.moduleName]; !exists {
			merged[mi.moduleName] = make(map[string]any)
		}
		for name, obj := range mi.imports {
			if err := r.checkOwner(obj, mi.moduleName, name); err != nil {
				return nil, err
			}
			merged[mi.moduleName][name] = obj
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

// checkOwner rejects imports that belong to a different Runtime, as mixing
// objects across Runtimes breaks isolation.
func (r *Runtime) checkOwner(obj any, mod, name string) error {
	var owner *vm
	switch t := obj.(type) {
	case *Memory:
		owner = t.owner
	case *Table:
		owner = t.owner
	case *Global:
		owner = t.owner
	case *wasmFunction:
		owner = t.module.vm
	default:
		// Host functions and other unowned types are always safe to import.
		return nil
	}

	if owner != r.vm {
		return fmt.Errorf("cross-runtime import of %s.%s is forbidden", mod, name)
	}
	return nil
}

// ModuleImports provides a fluent, type-safe API for building import
// objects for a specific WASM module.
//
// Example:
//
//	runtime := epsilon.NewRuntime()
//	imports := epsilon.NewModuleImports("env").
//	    AddHostFunc("log", func(m *epsilon.ModuleInstance, args ...any) []any {
//	        fmt.Println("WASM says:", args[0])
//	        return nil
//	    }).
//	    AddMemory("memory", runtime.NewMemory(epsilon.MemoryType{
//	        Limits: epsilon.Limits{Min: 1},
//	    })).
//	    AddGlobal("offset", runtime.NewGlobal(int32(1024), false, epsilon.I32))
//
//	instance, err := runtime.InstantiateModuleWithImports(wasmReader, imports)
type ModuleImports struct {
	moduleName string
	imports    map[string]any
}

func NewModuleImports(moduleName string) *ModuleImports {
	return &ModuleImports{
		moduleName: moduleName,
		imports:    make(map[string]any),
	}
}

func (b *ModuleImports) AddHostFunc(
	name string,
	fn func(*ModuleInstance, ...any) []any,
) *ModuleImports {
	b.imports[name] = fn
	return b
}

func (b *ModuleImports) AddMemory(name string, memory *Memory) *ModuleImports {
	b.imports[name] = memory
	return b
}

func (b *ModuleImports) AddTable(name string, table *Table) *ModuleImports {
	b.imports[name] = table
	return b
}

func (b *ModuleImports) AddGlobal(name string, global *Global) *ModuleImports {
	b.imports[name] = global
	return b
}

// AddModuleExports adds all exports from a ModuleInstance as imports.
// This is useful when you want to import functions, memories, tables, or
// globals from one module into another.
func (b *ModuleImports) AddModuleExports(
	instance *ModuleInstance,
) *ModuleImports {
	for _, export := range instance.exports {
		b.imports[export.name] = export.value
	}
	return b
}
