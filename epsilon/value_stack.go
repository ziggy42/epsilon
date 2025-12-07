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

type ValueStack struct {
	data []any
}

func NewValueStack() *ValueStack {
	return &ValueStack{data: make([]any, 512)}
}

func (s *ValueStack) Push(v any) {
	// We know, due to validation, this is always safe.
	s.data = append(s.data, v)
}

func (s *ValueStack) PushAll(values []any) {
	s.data = append(s.data, values...)
}

func (s *ValueStack) Drop() {
	s.data = s.data[:len(s.data)-1]
}

func (s *ValueStack) PopInt32() int32 {
	return popAs[int32](s)
}

func (s *ValueStack) Pop3Int32() (int32, int32, int32) {
	data := s.data
	n := len(data)
	c := data[n-3].(int32)
	b := data[n-2].(int32)
	a := data[n-1].(int32)
	s.data = data[:n-3]
	return a, b, c
}

func (s *ValueStack) PopInt64() int64 {
	return popAs[int64](s)
}

func (s *ValueStack) PopFloat32() float32 {
	return popAs[float32](s)
}

func (s *ValueStack) PopFloat64() float64 {
	return popAs[float64](s)
}

func (s *ValueStack) PopV128() V128Value {
	return popAs[V128Value](s)
}

func (s *ValueStack) Pop() any {
	// Due to validation, we know the stack is never empty if we call Pop.
	index := len(s.data) - 1
	element := s.data[index]
	s.data = s.data[:index]
	return element
}

func (s *ValueStack) PopValueType(vt ValueType) any {
	switch vt {
	case I32:
		return s.PopInt32()
	case I64:
		return s.PopInt64()
	case F32:
		return s.PopFloat32()
	case F64:
		return s.PopFloat64()
	case V128:
		return s.PopV128()
	case ExternRefType, FuncRefType:
		return s.Pop()
	default:
		// Due to validation, we know this is never reached.
		panic("unsupported value type")
	}
}

func (s *ValueStack) PopN(n int) []any {
	newLen := len(s.data) - n
	results := make([]any, n)
	copy(results, s.data[newLen:])
	s.data = s.data[:newLen]
	return results
}

func (s *ValueStack) Unwind(targetHeight, preserveCount uint) {
	valuesToPreserve := s.data[s.Size()-preserveCount:]
	s.data = s.data[:targetHeight]
	s.data = append(s.data, valuesToPreserve...)
}

func (s *ValueStack) Size() uint {
	return uint(len(s.data))
}

func popAs[T any](s *ValueStack) T {
	val := s.Pop()
	return val.(T)
}
