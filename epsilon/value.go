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

// NullReference is the internal representation of a null reference for
// funcref and externref types. It is a sentinel value that is invalid
// as a function or external object index.
// It is represented as -1.
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
