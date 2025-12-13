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
	"errors"
	"fmt"
)

var errFailedToFindExport = errors.New("failed to find export")

// exportInstance represents the runtime representation of an export.
type exportInstance struct {
	name  string
	value any
}

// ModuleInstance is the runtime representation of a module.
type ModuleInstance struct {
	types       []FunctionType
	funcAddrs   []uint32
	tableAddrs  []uint32
	memAddrs    []uint32
	globalAddrs []uint32
	dlemAddrs   []uint32
	dataAddrs   []uint32
	exports     []exportInstance
	vm          *vm // Internal reference to resolve exports
}

// Invoke calls an exported function by name with the given arguments.
//
// Args can be int32, int64, float32, or float64. The function returns a slice
// of results as []any, which can be type-asserted to the appropriate types.
func (m *ModuleInstance) Invoke(name string, args ...any) ([]any, error) {
	function, err := m.GetFunction(name)
	if err != nil {
		return nil, err
	}
	return m.vm.invoke(function, args)
}

// GetMemory returns an exported memory by name.
func (m *ModuleInstance) GetMemory(name string) (*Memory, error) {
	export, err := m.findExport(name)
	if err != nil {
		return nil, err
	}
	mem, ok := export.(*Memory)
	if !ok {
		return nil, fmt.Errorf("export %s is not a memory", name)
	}
	return mem, nil
}

// GetTable returns an exported table by name.
func (m *ModuleInstance) GetTable(name string) (*Table, error) {
	export, err := m.findExport(name)
	if err != nil {
		return nil, err
	}
	table, ok := export.(*Table)
	if !ok {
		return nil, fmt.Errorf("export %s is not a table", name)
	}
	return table, nil
}

// GetGlobal returns the value of an exported global by name.
func (m *ModuleInstance) GetGlobal(name string) (any, error) {
	export, err := m.findExport(name)
	if err != nil {
		return nil, err
	}
	global, ok := export.(*Global)
	if !ok {
		return nil, fmt.Errorf("export %s is not a global", name)
	}
	return global.Get(), nil
}

// GetFunction returns an exported function by name.
func (m *ModuleInstance) GetFunction(name string) (FunctionInstance, error) {
	export, err := m.findExport(name)
	if err != nil {
		return nil, err
	}
	fn, ok := export.(FunctionInstance)
	if !ok {
		return nil, fmt.Errorf("export %s is not a function", name)
	}
	return fn, nil
}

// ExportNames returns a slice of all export names in the module.
func (m *ModuleInstance) ExportNames() []string {
	names := make([]string, len(m.exports))
	for i, export := range m.exports {
		names[i] = export.name
	}
	return names
}

func (m *ModuleInstance) findExport(name string) (any, error) {
	for _, export := range m.exports {
		if export.name == name {
			return export.value, nil
		}
	}
	return nil, errFailedToFindExport
}

type FunctionInstance interface {
	GetType() *FunctionType
}

// wasmFunction is the runtime representation of a function defined in WASM.
type wasmFunction struct {
	functionType FunctionType
	module       *ModuleInstance
	code         function
	// We cache the continuation pc for each block-like opcde we encounter so we
	// can compute it only once. The key is the pc of the first instruction inside
	// the block.
	// These caches are stored in a WasmFunction and not in e.g. a callFrame so
	// that multiple invocation of the same WasmFunction share the same caches.
	jumpCache     map[uint]uint
	jumpElseCache map[uint]uint
}

func newWasmFunction(
	funType FunctionType,
	module *ModuleInstance,
	function function,
) *wasmFunction {
	return &wasmFunction{
		functionType:  funType,
		module:        module,
		code:          function,
		jumpCache:     map[uint]uint{},
		jumpElseCache: map[uint]uint{},
	}
}

func (wf *wasmFunction) GetType() *FunctionType { return &wf.functionType }

// hostFunction represents a function defined by the host environment.
type hostFunction struct {
	functionType FunctionType
	hostCode     func(...any) []any
}

func (hf *hostFunction) GetType() *FunctionType { return &hf.functionType }

// Global is a global variable.
type Global struct {
	value   value
	Mutable bool
	Type    ValueType
}

func (g *Global) Get() any {
	return g.value.any(g.Type)
}
