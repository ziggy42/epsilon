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

import "math"

// NullReference is the internal representation of a null reference.
// It is represented as -1.
const NullReference int32 = -1

type value struct {
	low, high uint64
}

func i32(v int32) value {
	return value{low: uint64(v)}
}

func i64(v int64) value {
	return value{low: uint64(v)}
}

func f32(v float32) value {
	return value{low: uint64(math.Float32bits(v))}
}

func f64(v float64) value {
	return value{low: math.Float64bits(v)}
}

func v128(v V128Value) value {
	return value{low: v.Low, high: v.High}
}

func (v value) int32() int32 {
	return int32(v.low)
}

func (v value) int64() int64 {
	return int64(v.low)
}

func (v value) float32() float32 {
	return math.Float32frombits(uint32(v.low))
}

func (v value) float64() float64 {
	return math.Float64frombits(v.low)
}

func (v value) v128() V128Value {
	return V128Value{Low: v.low, High: v.high}
}

func (v value) anyValueType(t ValueType) any {
	switch t {
	case I32:
		return v.int32()
	case I64:
		return v.int64()
	case F32:
		return v.float32()
	case F64:
		return v.float64()
	case V128:
		return v.v128()
	case FuncRefType, ExternRefType:
		return v.int32()
	default:
		panic("unreachable")
	}
}

func defaultValue(vt ValueType) value {
	switch vt {
	case I32, I64, F32, F64, V128:
		return value{}
	case FuncRefType, ExternRefType:
		return i32(NullReference)
	default:
		panic("unreachable")
	}
}
