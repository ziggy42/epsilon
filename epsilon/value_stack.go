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

type valueStack struct {
	data []uint64
}

func newValueStack() *valueStack {
	return &valueStack{data: make([]uint64, 0, 64)}
}

func (s *valueStack) pushInt32(v int32) {
	s.data = append(s.data, uint64(v))
}

func (s *valueStack) pushInt64(v int64) {
	s.data = append(s.data, uint64(v))
}

func (s *valueStack) pushFloat32(v float32) {
	s.data = append(s.data, uint64(math.Float32bits(v)))
}

func (s *valueStack) pushFloat64(v float64) {
	s.data = append(s.data, math.Float64bits(v))
}

func (s *valueStack) pushV128(v V128Value) {
	s.data = append(s.data, v.Low, v.High)
}

func (s *valueStack) pushV128Raw(low, high uint64) {
	s.data = append(s.data, low, high)
}

func (s *valueStack) pushUint64(v uint64) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushAll(values []any, types []ValueType) {
	for i, v := range values {
		low, high := anyToUint64(v)
		s.data = append(s.data, low)
		if types[i] == V128 {
			s.data = append(s.data, high)
		}
	}
}

func (s *valueStack) drop() {
	s.data = s.data[:len(s.data)-1]
}

func (s *valueStack) popInt32() int32 {
	n := len(s.data) - 1
	result := int32(s.data[n])
	s.data = s.data[:n]
	return result
}

func (s *valueStack) pop3Int32() (int32, int32, int32) {
	data := s.data
	n := len(data)
	c := int32(data[n-3])
	b := int32(data[n-2])
	a := int32(data[n-1])
	s.data = data[:n-3]
	return a, b, c
}

func (s *valueStack) popInt64() int64 {
	n := len(s.data) - 1
	result := int64(s.data[n])
	s.data = s.data[:n]
	return result
}

func (s *valueStack) popFloat32() float32 {
	n := len(s.data) - 1
	result := math.Float32frombits(uint32(s.data[n]))
	s.data = s.data[:n]
	return result
}

func (s *valueStack) popFloat64() float64 {
	n := len(s.data) - 1
	result := math.Float64frombits(s.data[n])
	s.data = s.data[:n]
	return result
}

func (s *valueStack) popV128() V128Value {
	n := len(s.data)
	result := V128Value{Low: s.data[n-2], High: s.data[n-1]}
	s.data = s.data[:n-2]
	return result
}

func (s *valueStack) popV128Raw() (low, high uint64) {
	n := len(s.data)
	low, high = s.data[n-2], s.data[n-1]
	s.data = s.data[:n-2]
	return
}

func (s *valueStack) popUint64() uint64 {
	n := len(s.data) - 1
	result := s.data[n]
	s.data = s.data[:n]
	return result
}

func (s *valueStack) popValueTypes(types []ValueType) []any {
	result := make([]any, len(types))
	for i := len(types) - 1; i >= 0; i-- {
		switch types[i] {
		case I32:
			result[i] = s.popInt32()
		case I64:
			result[i] = s.popInt64()
		case F32:
			result[i] = s.popFloat32()
		case F64:
			result[i] = s.popFloat64()
		case V128:
			result[i] = s.popV128()
		case FuncRefType, ExternRefType:
			result[i] = s.popInt32()
		}
	}
	return result
}

// unwind removes values from the stack down to targetHeight while preserving
// the top preserveCount values (measured in stack slots, v128=2 slots).
func (s *valueStack) unwind(targetHeight, preserveCount uint32) {
	valuesToPreserve := s.data[s.size()-preserveCount:]
	s.data = s.data[:targetHeight]
	s.data = append(s.data, valuesToPreserve...)
}

func (s *valueStack) size() uint32 {
	return uint32(len(s.data))
}
