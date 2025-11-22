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

// ValueType classifies the individual values that WebAssembly code can compute
// with and the values that a variable accepts. They are either NumberType,
// VectorType, or ReferenceType.
type ValueType interface {
	isValueType()
}

// NumberType classifies numeric values.
// See https://webassembly.github.io/spec/core/syntax/types.html#number-types.
type NumberType int

const (
	I32 NumberType = 0x7f
	I64 NumberType = 0x7e
	F32 NumberType = 0x7d
	F64 NumberType = 0x7c
)

func (NumberType) isValueType() {}

// VectorType classifies vectors of numeric values processed by vector
// instructions.
// See https://webassembly.github.io/spec/core/syntax/types.html#vector-types.
type VectorType int

const (
	V128 VectorType = 0x7b
)

func (VectorType) isValueType() {}

func DefaultValueForType(vt ValueType) any {
	switch vt {
	case I32:
		return int32(0)
	case I64:
		return int64(0)
	case F32:
		return float32(0)
	case F64:
		return float64(0)
	case V128:
		return V128Value{}
	case FuncRefType, ExternRefType:
		return NullVal
	default:
		// Should ideally not be reached with a valid module.
		return nil
	}
}

// ReferenceType classifies first-class references to objects in the runtime
// store.
// https://webassembly.github.io/spec/core/syntax/types.html#reference-types.
type ReferenceType int

const (
	FuncRefType   ReferenceType = 0x70
	ExternRefType ReferenceType = 0x6f
)

func (ReferenceType) isValueType() {}

// FunctionType classifies the signature of functions, mapping a vector of
// parameters to a vector of results.
// See https://webassembly.github.io/spec/core/syntax/types.html#function-types.
type FunctionType struct {
	ParamTypes  []ValueType
	ResultTypes []ValueType
}

func (ft *FunctionType) Equal(other *FunctionType) bool {
	if ft == other {
		return true
	}
	if ft == nil || other == nil {
		return false
	}
	if len(ft.ParamTypes) != len(other.ParamTypes) {
		return false
	}
	if len(ft.ResultTypes) != len(other.ResultTypes) {
		return false
	}
	for i, param := range ft.ParamTypes {
		if param != other.ParamTypes[i] {
			return false
		}
	}
	for i, result := range ft.ResultTypes {
		if result != other.ResultTypes[i] {
			return false
		}
	}
	return true
}

type Function struct {
	TypeIndex uint32
	Locals    []ValueType
	Body      []byte
}

type IndexType int

const (
	FunctionIndexType IndexType = 0x0
	TableIndexType    IndexType = 0x1
	MemoryIndexType   IndexType = 0x2
	GlobalIndexType   IndexType = 0x3
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
	IndexType IndexType
	Index     uint32
}

// See https://webassembly.github.io/spec/core/binary/types.html#limits
type Limits struct {
	Min uint64
	Max *uint64
}

type TableType struct {
	ReferenceType ReferenceType
	Limits        Limits
}

type MemoryType struct {
	Limits Limits
}

// Element is an interface representing any type of element segment.
// In the WebAssembly specification, this can be an active, passive, or
// declarative element.
// See https://webassembly.github.io/spec/core/syntax/modules.html#syntax-elem
type Element interface {
	isElement()
}

// ElementData holds the common fields for all element segment types.
// It is embedded into ActiveElement, PassiveElement, and DeclarativeElement.
type ElementData struct {
	// Kind specifies the type of references in the element segment.
	// Defaults to FuncRef.
	Kind ReferenceType

	// FuncIndexes is a list of function indices.
	// This field is used when FuncIndexesExpressions is empty.
	FuncIndexes []int32

	// FuncIndexesExpressions is a list of constant expressions that produce
	// function references. This field is used when FuncIndexes is empty.
	FuncIndexesExpressions [][]byte
}

// isElement is a marker method to satisfy the Element interface.
func (ElementData) isElement() {}

// ActiveElement represents an element segment that should be loaded into a
// table at module instantiation time.
type ActiveElement struct {
	ElementData

	// TableIndex is the index of the table to initialize.
	TableIndex uint32

	// OffsetExpression is a constant expression that computes the starting offset
	// in the table.
	OffsetExpression []byte
}

// PassiveElement represents an element segment that can be loaded into a table
// on demand using the `table.init` instruction.
type PassiveElement struct {
	ElementData
}

// DeclarativeElement represents an element segment that is not loaded into a
// table by the runtime but may be used by tools like linkers.
type DeclarativeElement struct {
	ElementData
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

// See https://webassembly.github.io/spec/core/syntax/modules.html#data-segments
type Data interface {
	isData()
}

type ActiveData struct {
	MemoryIndex      uint32
	OffsetExpression []byte
	Content          []byte
}

type PassiveData struct {
	Content []byte
}

func (ad *ActiveData) isData()  {}
func (pd *PassiveData) isData() {}

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
	Elements        []Element
	GlobalVariables []GlobalVariable
	DataSegments    []Data
}
