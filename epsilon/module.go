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

type function struct {
	typeIndex uint32
	locals    []ValueType
	body      []uint64
}

type exportIndexKind int

const (
	functionExportKind exportIndexKind = iota
	tableExportKind
	memoryExportKind
	globalExportKind
)

// moduleImport represents a WASM import.
// See https://webassembly.github.io/spec/core/syntax/modules.html#imports
type moduleImport struct {
	moduleName string
	name       string
	importType importType
}

// importType is a marker interface for the type of an import.
type importType interface {
	isImportType()
}

// functionTypeIndex is the type for an imported function, which is represented
// by its type index.
type functionTypeIndex uint32

func (functionTypeIndex) isImportType() {}
func (TableType) isImportType()         {}
func (MemoryType) isImportType()        {}
func (GlobalType) isImportType()        {}

// export defines a set of exports that become accessible to the host
// environment once the module has been instantiated.
// See https://webassembly.github.io/spec/core/syntax/modules.html#exports.
type export struct {
	name      string
	indexType exportIndexKind
	index     uint32
}

// elementMode specifies how an element segment should be handled.
type elementMode int

const (
	activeElementMode elementMode = iota
	passiveElementMode
	declarativeElementMode
)

// elementSegment represents an element segment in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#syntax-elem
type elementSegment struct {
	mode elementMode
	kind ReferenceType

	// functionIndexes is a list of function indices. Used when
	// functionIndexesExpressions is empty.
	functionIndexes []int32

	// functionIndexesExpressions is a list of constant expressions that produce
	// function references. Used when functionIndexes is empty.
	functionIndexesExpressions [][]uint64

	// tableIndex is the index of the table to initialize. Only used when
	// Mode == ActiveElementMode.
	tableIndex uint32

	// offsetExpression is a constant expression that computes the starting offset
	// in the table. Only used when Mode == ActiveElementMode.
	offsetExpression []uint64
}

// globalVariable represents a global variable in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#globals
type globalVariable struct {
	globalType     GlobalType
	initExpression []uint64
}

// dataMode specifies how a data segment should be handled.
type dataMode int

const (
	activeDataMode dataMode = iota
	passiveDataMode
)

// dataSegment represents a data segment in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#data-segments
type dataSegment struct {
	mode    dataMode
	content []byte

	// memoryIndex is the index of the memory to initialize. Only used when
	// Mode == ActiveDataMode.
	memoryIndex uint32

	// offsetExpression is a constant expression that computes the starting offset
	// in memory. Only used when Mode == ActiveDataMode.
	offsetExpression []uint64
}

// moduleDefinition represents a non-instantiated WASM module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#modules.
type moduleDefinition struct {
	types           []FunctionType
	imports         []moduleImport
	exports         []export
	startIndex      *uint32
	tables          []TableType
	memories        []MemoryType
	funcs           []function
	elementSegments []elementSegment
	globalVariables []globalVariable
	dataSegments    []dataSegment
	dataCount       *uint64
}
