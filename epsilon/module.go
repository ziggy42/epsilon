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

type Function struct {
	TypeIndex uint32
	Locals    []ValueType
	Body      []byte
}

type ExportIndexKind int

const (
	FunctionExportKind ExportIndexKind = 0x0
	TableExportKind    ExportIndexKind = 0x1
	MemoryExportKind   ExportIndexKind = 0x2
	GlobalExportKind   ExportIndexKind = 0x3
)

// Import represents a WASM import.
// See https://webassembly.github.io/spec/core/syntax/modules.html#imports
type Import struct {
	ModuleName string
	Name       string
	Type       ImportType
}

// ImportType is a marker interface for the type of an import.
// It can be a FunctionTypeIndex, TableType, MemoryType, or GlobalType.
type ImportType interface {
	isImportType()
}

// FunctionTypeIndex is the type for an imported function, which is represented
// by its type index.
type FunctionTypeIndex uint32

func (FunctionTypeIndex) isImportType() {}
func (TableType) isImportType()         {}
func (MemoryType) isImportType()        {}
func (GlobalType) isImportType()        {}

// Export defines a set of exports that become accessible to the host
// environment once the module has been instantiated.
// See https://webassembly.github.io/spec/core/syntax/modules.html#exports.
type Export struct {
	Name      string
	IndexType ExportIndexKind
	Index     uint32
}

type TableType struct {
	ReferenceType ReferenceType
	Limits        Limits
}

type MemoryType struct {
	Limits Limits
}

// ElementMode specifies how an element segment should be handled.
type ElementMode int

const (
	ActiveElementMode ElementMode = iota
	PassiveElementMode
	DeclarativeElementMode
)

// ElementSegment represents an element segment in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#syntax-elem
type ElementSegment struct {
	Mode ElementMode
	Kind ReferenceType

	// FuncIndexes is a list of function indices. Used when FuncIndexesExpressions
	// is empty.
	FuncIndexes []int32

	// FuncIndexesExpressions is a list of constant expressions that produce
	// function references. Used when FuncIndexes is empty.
	FuncIndexesExpressions [][]byte

	// TableIndex is the index of the table to initialize. Only used when
	// Mode == ActiveElementMode.
	TableIndex uint32

	// OffsetExpression is a constant expression that computes the starting offset
	// in the table. Only used when Mode == ActiveElementMode.
	OffsetExpression []byte
}

// GlobalType defines the type of a global variable, which includes its value
// type and whether it is mutable.
// See https://webassembly.github.io/spec/core/syntax/modules.html#globals
type GlobalType struct {
	ValueType ValueType
	IsMutable bool
}

// GlobalVariable represents a global variable in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#globals
type GlobalVariable struct {
	GlobalType     GlobalType
	InitExpression []byte
}

// DataMode specifies how a data segment should be handled.
type DataMode int

const (
	ActiveDataMode DataMode = iota
	PassiveDataMode
)

// DataSegment represents a data segment in a WebAssembly module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#data-segments
type DataSegment struct {
	Mode    DataMode
	Content []byte

	// MemoryIndex is the index of the memory to initialize. Only used when
	// Mode == ActiveDataMode.
	MemoryIndex uint32

	// OffsetExpression is a constant expression that computes the starting offset
	// in memory. Only used when Mode == ActiveDataMode.
	OffsetExpression []byte
}

// Module represents a WASM module.
// See https://webassembly.github.io/spec/core/syntax/modules.html#modules.
type Module struct {
	Types           []FunctionType
	Imports         []Import
	Exports         []Export
	StartIndex      *uint32
	Tables          []TableType
	Memories        []MemoryType
	Funcs           []Function
	ElementSegments []ElementSegment
	GlobalVariables []GlobalVariable
	DataSegments    []DataSegment
	DataCount       *uint64
}
