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
	"encoding/binary"
	"math"
)

func Int32From4Bytes(bytes []byte) int32 {
	return int32(binary.LittleEndian.Uint32(bytes))
}

func Int64From8Bytes(bytes []byte) int64 {
	return int64(binary.LittleEndian.Uint64(bytes))
}

func Float32From4Bytes(bytes []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(bytes))
}

func Float64From8Bytes(bytes []byte) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(bytes))
}

func IntFrom1Byte[T int32 | int64](bytes []byte) T {
	return T(int8(bytes[0]))
}

func UintFrom1Byte[T int32 | int64](bytes []byte) T {
	return T(uint8(bytes[0]))
}

func IntFrom2Bytes[T int32 | int64](bytes []byte) T {
	value := int16(binary.LittleEndian.Uint16(bytes))
	return T(value)
}

func UintFrom2Bytes[T int32 | int64](bytes []byte) T {
	return T(binary.LittleEndian.Uint16(bytes))
}

func Uint64From4Bytes(bytes []byte) int64 {
	return int64(binary.LittleEndian.Uint32(bytes))
}

func Int64From4Bytes(bytes []byte) int64 {
	return int64(int32(binary.LittleEndian.Uint32(bytes)))
}

func BytesFromInt32(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

func BytesFromInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func BytesFromFloat32(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

func BytesFromFloat64(v float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(v))
	return b
}

func BoolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func U32ToI32(v uint32) int32 { return int32(v) }
func U64ToI64(v uint64) int64 { return int64(v) }

func SignExtend8To32(v byte) int32    { return int32(int8(v)) }
func ZeroExtend8To32(v byte) int32    { return int32(v) }
func SignExtend16To32(v uint16) int32 { return int32(int16(v)) }
func ZeroExtend16To32(v uint16) int32 { return int32(v) }

func SignExtend8To64(v byte) int64    { return int64(int8(v)) }
func ZeroExtend8To64(v byte) int64    { return int64(v) }
func SignExtend16To64(v uint16) int64 { return int64(int16(v)) }
func ZeroExtend16To64(v uint16) int64 { return int64(v) }
func SignExtend32To64(v uint32) int64 { return int64(int32(v)) }
func ZeroExtend32To64(v uint32) int64 { return int64(v) }

func IdentityV128(v V128Value) V128Value { return v }
