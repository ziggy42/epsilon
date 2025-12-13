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
	data []value
}

func newValueStack() *valueStack {
	return &valueStack{data: make([]value, 0, 512)}
}

func (s *valueStack) pushInt32(v int32) {
	s.data = append(s.data, i32(v))
}

func (s *valueStack) pushInt64(v int64) {
	s.data = append(s.data, i64(v))
}

func (s *valueStack) pushFloat32(v float32) {
	s.data = append(s.data, f32(v))
}

func (s *valueStack) pushFloat64(v float64) {
	s.data = append(s.data, f64(v))
}

func (s *valueStack) pushV128(v V128Value) {
	s.data = append(s.data, v128(v))
}

func (s *valueStack) push(v value) {
	s.data = append(s.data, v)
}

func (s *valueStack) pushAll(values []any) {
	for _, v := range values {
		s.push(newValue(v))
	}
}

func (s *valueStack) drop() {
	s.data = s.data[:len(s.data)-1]
}

func (s *valueStack) popInt32() int32 {
	return s.pop().int32()
}

func (s *valueStack) pop3Int32() (int32, int32, int32) {
	data := s.data
	n := len(data)
	c := data[n-3].int32()
	b := data[n-2].int32()
	a := data[n-1].int32()
	s.data = data[:n-3]
	return a, b, c
}

func (s *valueStack) popInt64() int64 {
	return s.pop().int64()
}

func (s *valueStack) popFloat32() float32 {
	return s.pop().float32()
}

func (s *valueStack) popFloat64() float64 {
	return s.pop().float64()
}

func (s *valueStack) popV128() V128Value {
	return s.pop().v128()
}

func (s *valueStack) pop() value {
	// Due to validation, we know the stack is never empty if we call Pop.
	index := len(s.data) - 1
	element := s.data[index]
	s.data = s.data[:index]
	return element
}

func (s *valueStack) popValueTypes(types []ValueType) []any {
	n := len(types)
	newLen := len(s.data) - n
	result := make([]any, n)
	for i, t := range types {
		v := s.data[newLen+i]
		result[i] = v.any(t)
	}
	s.data = s.data[:newLen]
	return result
}

func (s *valueStack) unwind(targetHeight, preserveCount uint) {
	valuesToPreserve := s.data[s.size()-preserveCount:]
	s.data = s.data[:targetHeight]
	s.data = append(s.data, valuesToPreserve...)
}

func (s *valueStack) size() uint {
	return uint(len(s.data))
}
