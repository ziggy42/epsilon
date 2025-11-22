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
	"errors"
	"fmt"
)

var ErrEmptyStack = errors.New("value stack is empty")

type ValueStack struct {
	data []any
}

func NewValueStack() *ValueStack {
	return &ValueStack{data: make([]any, 512)}
}

func (s *ValueStack) Push(v any) error {
	switch v.(type) {
	case int32, int64, float32, float64, Null, V128Value:
		s.data = append(s.data, v)
		return nil
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
}

func (s *ValueStack) PushAll(values []any) error {
	for _, v := range values {
		if err := s.Push(v); err != nil {
			return err
		}
	}
	return nil
}

func (s *ValueStack) Drop() error {
	_, err := s.Pop()
	return err
}

func (s *ValueStack) PopInt32() (int32, error) {
	return popAs[int32](s)
}

func (s *ValueStack) Pop3Int32() (int32, int32, int32, error) {
	a, err := popAs[int32](s)
	if err != nil {
		return 0, 0, 0, err
	}
	b, err := popAs[int32](s)
	if err != nil {
		return 0, 0, 0, err
	}
	c, err := popAs[int32](s)
	if err != nil {
		return 0, 0, 0, err
	}
	return a, b, c, nil
}

func (s *ValueStack) PopInt64() (int64, error) {
	return popAs[int64](s)
}

func (s *ValueStack) PopFloat32() (float32, error) {
	return popAs[float32](s)
}

func (s *ValueStack) PopFloat64() (float64, error) {
	return popAs[float64](s)
}

func (s *ValueStack) PopV128() (V128Value, error) {
	return popAs[V128Value](s)
}

func (s *ValueStack) Pop() (any, error) {
	if s.Size() == 0 {
		return nil, ErrEmptyStack
	}
	index := len(s.data) - 1
	element := s.data[index]
	s.data = s.data[:index]
	return element, nil
}

func (s *ValueStack) PeekN(n uint) []any {
	start := s.Size() - n
	return s.data[start:]
}

func (s *ValueStack) PopValueType(vt ValueType) (any, error) {
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
		return nil, fmt.Errorf("unsupported value type: %v", vt)
	}
}

func (s *ValueStack) PopValueTypes(valueTypes []ValueType) ([]any, error) {
	results := make([]any, len(valueTypes))
	var err error
	for i := len(valueTypes) - 1; i >= 0; i-- {
		results[i], err = s.PopValueType(valueTypes[i])
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s *ValueStack) Size() uint {
	return uint(len(s.data))
}

func (s *ValueStack) Resize(newSize uint) {
	s.data = s.data[:newSize]
}

func popAs[T any](s *ValueStack) (T, error) {
	var zero T
	val, err := s.Pop()
	if err != nil {
		return zero, err
	}

	typedVal, ok := val.(T)
	if !ok {
		return zero, fmt.Errorf("top element was %T, not %T", val, zero)
	}
	return typedVal, nil
}
