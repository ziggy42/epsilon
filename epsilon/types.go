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
	"math"
	"slices"
)

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

const V128 VectorType = 0x7b

func (VectorType) isValueType() {}

// ReferenceType classifies first-class references to objects in the runtime
// store.
// https://webassembly.github.io/spec/core/syntax/types.html#reference-types.
type ReferenceType int

const (
	FuncRefType   ReferenceType = 0x70
	ExternRefType ReferenceType = 0x6f
)

func (ReferenceType) isValueType() {}

// V128Value represents a 128-bit SIMD value.
type V128Value struct {
	Low, High uint64
}

type TableType struct {
	ReferenceType ReferenceType
	Limits        Limits
}

type MemoryType struct {
	Limits Limits
}

// GlobalType defines the type of a global variable, which includes its value
// type and whether it is mutable.
// See https://webassembly.github.io/spec/core/syntax/modules.html#globals
type GlobalType struct {
	ValueType ValueType
	IsMutable bool
}

// Limits define min/max constraints for tables and memories.
// See https://webassembly.github.io/spec/core/binary/types.html#limits
type Limits struct {
	Min uint32
	Max *uint32
}

// FunctionType classifies the signature of functions, mapping a vector of
// parameters to a vector of results.
// See https://webassembly.github.io/spec/core/syntax/types.html#function-types.
type FunctionType struct {
	ParamTypes  []ValueType
	ResultTypes []ValueType
}

func (ft *FunctionType) Equal(other FunctionType) bool {
	return slices.Equal(ft.ParamTypes, other.ParamTypes) &&
		slices.Equal(ft.ResultTypes, other.ResultTypes)
}

// NullReference is the internal representation of a null reference for funcref
// and externref types. It is a sentinel value that is invalid as a function or
// external object index. It is represented as -1.
const NullReference int32 = -1

func anyToU64(v any) (low, high uint64) {
	switch val := v.(type) {
	case int32:
		return uint64(val), 0
	case int64:
		return uint64(val), 0
	case float32:
		return uint64(math.Float32bits(val)), 0
	case float64:
		return math.Float64bits(val), 0
	case V128Value:
		return val.Low, val.High
	default:
		panic("unreachable")
	}
}

func u64ToAny(low, high uint64, t ValueType) any {
	switch t {
	case I32:
		return int32(low)
	case I64:
		return int64(low)
	case F32:
		return math.Float32frombits(uint32(low))
	case F64:
		return math.Float64frombits(low)
	case V128:
		return V128Value{Low: low, High: high}
	case FuncRefType, ExternRefType:
		return int32(low)
	default:
		panic("unreachable")
	}
}
