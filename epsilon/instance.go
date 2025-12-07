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

// ExportInstance represents the runtime representation of an export.
type ExportInstance struct {
	Name  string
	Value any
}

// ModuleInstance is the runtime representation of a module.
type ModuleInstance struct {
	Types       []FunctionType
	FuncAddrs   []uint32
	TableAddrs  []uint32
	MemAddrs    []uint32
	GlobalAddrs []uint32
	ElemAddrs   []uint32
	DataAddrs   []uint32
	Exports     []ExportInstance
	vm          *vm // Internal reference to resolve exports
}

// Invoke calls an exported function by name with the given arguments.
//
// Args can be int32, int64, float32, or float64. The function returns a slice
// of results as []any, which can be type-asserted to the appropriate types.
func (m *ModuleInstance) Invoke(name string, args ...any) ([]any, error) {
	return m.vm.invoke(m, name, args...)
}

// GetMemory returns an exported memory by name.
func (m *ModuleInstance) GetMemory(name string) (*Memory, error) {
	export, err := getExport(m, name, memoryExportKind)
	if err != nil {
		return nil, err
	}
	return export.(*Memory), nil
}

// GetTable returns an exported table by name.
func (m *ModuleInstance) GetTable(name string) (*Table, error) {
	export, err := getExport(m, name, tableExportKind)
	if err != nil {
		return nil, err
	}
	return export.(*Table), nil
}

// GetGlobal returns the value of an exported global by name.
func (m *ModuleInstance) GetGlobal(name string) (any, error) {
	export, err := getExport(m, name, globalExportKind)
	if err != nil {
		return nil, err
	}
	return export.(*Global).Value, nil
}

// GetFunction returns an exported function by name.
func (m *ModuleInstance) GetFunction(name string) (FunctionInstance, error) {
	export, err := getExport(m, name, functionExportKind)
	if err != nil {
		return nil, err
	}
	return export.(FunctionInstance), nil
}

type FunctionInstance interface {
	GetType() *FunctionType
}

// WasmFunction is the runtime representation of a function defined in WASM.
type WasmFunction struct {
	Type   FunctionType
	Module *ModuleInstance
	Code   function
	// We cache the continuation pc for each block-like opcde we encounter so we
	// can compute it only once. The key is the pc of the first instruction inside
	// the block.
	// These caches are stored in a WasmFunction and not in e.g. a callFrame so
	// that multiple invocation of the same WasmFunction share the same caches.
	jumpCache     map[uint]uint
	jumpElseCache map[uint]uint
}

func NewWasmFunction(
	funType FunctionType,
	module *ModuleInstance,
	function function,
) *WasmFunction {
	return &WasmFunction{
		Type:          funType,
		Module:        module,
		Code:          function,
		jumpCache:     map[uint]uint{},
		jumpElseCache: map[uint]uint{},
	}
}

func (wf *WasmFunction) GetType() *FunctionType { return &wf.Type }

// HostFunction represents a function defined by the host environment.
type HostFunction struct {
	Type     FunctionType
	HostCode func(...any) []any
}

func (hf *HostFunction) GetType() *FunctionType { return &hf.Type }

// Global is a global variable.
type Global struct {
	Value   any
	Mutable bool
	Type    ValueType
}
