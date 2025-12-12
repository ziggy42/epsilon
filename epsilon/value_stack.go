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

type valueStack struct {
	data []any
}

func newValueStack() *valueStack {
	return &valueStack{data: make([]any, 512)}
}

func (s *valueStack) pushInt32(v int32) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushInt64(v int64) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushFloat32(v float32) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushFloat64(v float64) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushV128(v V128Value) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushNull() {
	s.data = append(s.data, NullVal)
}

func (s *valueStack) pushRaw(v any) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushValueType(v any, t ValueType) {
	switch t {
	case I32:
		s.pushInt32(v.(int32))
	case I64:
		s.pushInt64(v.(int64))
	case F32:
		s.pushFloat32(v.(float32))
	case F64:
		s.pushFloat64(v.(float64))
	case V128:
		s.pushV128(v.(V128Value))
	case FuncRefType, ExternRefType:
		// TODO this could be null
		s.pushRaw(v)
	default:
		panic("unreachable")
	}
}

func (s *valueStack) pushAll(values []any) {
	s.data = append(s.data, values...)
}

func (s *valueStack) drop() {
	s.data = s.data[:len(s.data)-1]
}

func (s *valueStack) popInt32() int32 {
	return s.pop().(int32)
}

func (s *valueStack) pop3Int32() (int32, int32, int32) {
	data := s.data
	n := len(data)
	c := data[n-3].(int32)
	b := data[n-2].(int32)
	a := data[n-1].(int32)
	s.data = data[:n-3]
	return a, b, c
}

func (s *valueStack) popInt64() int64 {
	return s.pop().(int64)
}

func (s *valueStack) popFloat32() float32 {
	return s.pop().(float32)
}

func (s *valueStack) popFloat64() float64 {
	return s.pop().(float64)
}

func (s *valueStack) popV128() V128Value {
	return s.pop().(V128Value)
}

func (s *valueStack) pop() any {
	// Due to validation, we know the stack is never empty if we call Pop.
	index := len(s.data) - 1
	element := s.data[index]
	s.data = s.data[:index]
	return element
}

func (s *valueStack) popN(n int) []any {
	newLen := len(s.data) - n
	results := make([]any, n)
	copy(results, s.data[newLen:])
	s.data = s.data[:newLen]
	return results
}

func (s *valueStack) unwind(targetHeight, preserveCount uint) {
	valuesToPreserve := s.data[s.size()-preserveCount:]
	s.data = s.data[:targetHeight]
	s.data = append(s.data, valuesToPreserve...)
}

// popRaw returns the top value with its internal representation. It should only
// be used when the returned value is not going to be used as a value.
func (s *valueStack) popRaw() any {
	return s.pop()
}

func (s *valueStack) popReference() any {
	// TODO we do this just to prove a point but of course this is wrong.
	if s.data[len(s.data)-1] == NullVal {
		return s.pop()
	}
	return s.popInt32()
}

func (s *valueStack) popValueType(t ValueType) any {
	switch t {
	case I32:
		return s.popInt32()
	case I64:
		return s.popInt64()
	case F32:
		return s.popFloat32()
	case F64:
		return s.popFloat64()
	case V128:
		return s.popV128()
	case FuncRefType, ExternRefType:
		// TODO this could be null
		return s.pop()
	default:
		panic("unreachable")
	}
}

func (s *valueStack) size() uint {
	return uint(len(s.data))
}
