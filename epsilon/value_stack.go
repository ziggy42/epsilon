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

type valueStackItem struct {
	low, high uint64
}

type valueStack struct {
	data []valueStackItem
}

func newValueStack() *valueStack {
	return &valueStack{data: make([]valueStackItem, 512)}
}

func (s *valueStack) pushInt32(v int32) {
	s.data = append(s.data, valueStackItem{low: uint64(v)})
}

func (s *valueStack) pushInt64(v int64) {
	s.data = append(s.data, valueStackItem{low: uint64(v)})
}

func (s *valueStack) pushFloat32(v float32) {
	s.data = append(s.data, valueStackItem{low: uint64(math.Float32bits(v))})
}

func (s *valueStack) pushFloat64(v float64) {
	s.data = append(s.data, valueStackItem{low: math.Float64bits(v)})
}

func (s *valueStack) pushV128(v V128Value) {
	s.data = append(s.data, valueStackItem{low: v.Low, high: v.High})
}

func (s *valueStack) pushNull() {
	s.pushInt32(NullReference)
}

func (s *valueStack) pushRaw(v valueStackItem) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushValueType(v any, t ValueType) {
	switch t {
	case I32, FuncRefType, ExternRefType:
		s.pushInt32(v.(int32))
	case I64:
		s.pushInt64(v.(int64))
	case F32:
		s.pushFloat32(v.(float32))
	case F64:
		s.pushFloat64(v.(float64))
	case V128:
		s.pushV128(v.(V128Value))
	default:
		panic("unreachable")
	}
}

func (s *valueStack) pushAll(values []any) {
	for _, v := range values {
		switch val := v.(type) {
		case int32:
			s.pushInt32(val)
		case int64:
			s.pushInt64(val)
		case float32:
			s.pushFloat32(val)
		case float64:
			s.pushFloat64(val)
		case V128Value:
			s.pushV128(val)
		default:
			panic("unreachable")
		}
	}
}

func (s *valueStack) drop() {
	s.data = s.data[:len(s.data)-1]
}

func (s *valueStack) popInt32() int32 {
	return int32(s.pop().low)
}

func (s *valueStack) pop3Int32() (int32, int32, int32) {
	data := s.data
	n := len(data)
	c := int32(data[n-3].low)
	b := int32(data[n-2].low)
	a := int32(data[n-1].low)
	s.data = data[:n-3]
	return a, b, c
}

func (s *valueStack) popInt64() int64 {
	return int64(s.pop().low)
}

func (s *valueStack) popFloat32() float32 {
	return math.Float32frombits(uint32(s.pop().low))
}

func (s *valueStack) popFloat64() float64 {
	return math.Float64frombits(s.pop().low)
}

func (s *valueStack) popV128() V128Value {
	item := s.pop()
	return V128Value{Low: item.low, High: item.high}
}

func (s *valueStack) pop() valueStackItem {
	// Due to validation, we know the stack is never empty if we call Pop.
	index := len(s.data) - 1
	element := s.data[index]
	s.data = s.data[:index]
	return element
}

func (s *valueStack) popValueType(t ValueType) any {
	switch t {
	case I32, FuncRefType, ExternRefType:
		return s.popInt32()
	case I64:
		return s.popInt64()
	case F32:
		return s.popFloat32()
	case F64:
		return s.popFloat64()
	case V128:
		return s.popV128()
	default:
		panic("unreachable")
	}
}

func (s *valueStack) popValueTypes(types []ValueType) []any {
	n := len(types)
	newLen := len(s.data) - n
	values := s.data[newLen:]
	s.data = s.data[:newLen]

	results := make([]any, n)
	for i, t := range types {
		item := values[i]
		switch t {
		case I32, FuncRefType, ExternRefType:
			results[i] = int32(item.low)
		case I64:
			results[i] = int64(item.low)
		case F32:
			results[i] = math.Float32frombits(uint32(item.low))
		case F64:
			results[i] = math.Float64frombits(item.low)
		case V128:
			results[i] = V128Value{Low: item.low, High: item.high}
		default:
			panic("unreachable")
		}
	}
	return results
}

func (s *valueStack) unwind(targetHeight, preserveCount uint) {
	valuesToPreserve := s.data[s.size()-preserveCount:]
	s.data = s.data[:targetHeight]
	s.data = append(s.data, valuesToPreserve...)
}

func (s *valueStack) size() uint {
	return uint(len(s.data))
}
