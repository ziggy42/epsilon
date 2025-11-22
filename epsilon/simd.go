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
	"fmt"
	"math"
	"math/bits"
	"slices"
)

// V128Value represents a 128-bit SIMD value.
type V128Value struct {
	Low, High uint64
}

func NewV128ValueFromSlice(bytes []byte) V128Value {
	return NewV128Value([16]byte(bytes))
}

func NewV128Value(bytes [16]byte) V128Value {
	return V128Value{
		Low:  binary.LittleEndian.Uint64(bytes[0:8]),
		High: binary.LittleEndian.Uint64(bytes[8:16]),
	}
}

func (v V128Value) Bytes() [16]byte {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], v.Low)
	binary.LittleEndian.PutUint64(buf[8:16], v.High)
	return buf
}

func GetBytes(v V128Value) []byte {
	bytes := v.Bytes()
	return bytes[:]
}

func SimdV128Load8x8S(data []byte) V128Value {
	p0 := uint64(uint16(int16(int8(data[0]))))
	p1 := uint64(uint16(int16(int8(data[1]))))
	p2 := uint64(uint16(int16(int8(data[2]))))
	p3 := uint64(uint16(int16(int8(data[3]))))
	low := p0 | p1<<16 | p2<<32 | p3<<48

	p4 := uint64(uint16(int16(int8(data[4]))))
	p5 := uint64(uint16(int16(int8(data[5]))))
	p6 := uint64(uint16(int16(int8(data[6]))))
	p7 := uint64(uint16(int16(int8(data[7]))))
	high := p4 | p5<<16 | p6<<32 | p7<<48

	return V128Value{Low: low, High: high}
}

func SimdV128Load8x8U(data []byte) V128Value {
	p0 := uint64(data[0])
	p1 := uint64(data[1])
	p2 := uint64(data[2])
	p3 := uint64(data[3])
	low := p0 | p1<<16 | p2<<32 | p3<<48

	p4 := uint64(data[4])
	p5 := uint64(data[5])
	p6 := uint64(data[6])
	p7 := uint64(data[7])
	high := p4 | p5<<16 | p6<<32 | p7<<48

	return V128Value{Low: low, High: high}
}

func SimdV128Load16x4S(data []byte) V128Value {
	v0 := int32(int16(binary.LittleEndian.Uint16(data[0:2])))
	v1 := int32(int16(binary.LittleEndian.Uint16(data[2:4])))
	low := uint64(uint32(v0)) | uint64(uint32(v1))<<32

	v2 := int32(int16(binary.LittleEndian.Uint16(data[4:6])))
	v3 := int32(int16(binary.LittleEndian.Uint16(data[6:8])))
	high := uint64(uint32(v2)) | uint64(uint32(v3))<<32

	return V128Value{Low: low, High: high}
}

func SimdV128Load16x4U(data []byte) V128Value {
	v0 := uint64(binary.LittleEndian.Uint16(data[0:2]))
	v1 := uint64(binary.LittleEndian.Uint16(data[2:4]))
	low := v0 | v1<<32

	v2 := uint64(binary.LittleEndian.Uint16(data[4:6]))
	v3 := uint64(binary.LittleEndian.Uint16(data[6:8]))
	high := v2 | v3<<32

	return V128Value{Low: low, High: high}
}

func SimdV128Load32x2S(data []byte) V128Value {
	low := uint64(int64(int32(binary.LittleEndian.Uint32(data[0:4]))))
	high := uint64(int64(int32(binary.LittleEndian.Uint32(data[4:8]))))
	return V128Value{Low: low, High: high}
}

func SimdV128Load32x2U(data []byte) V128Value {
	low := uint64(binary.LittleEndian.Uint32(data[0:4]))
	high := uint64(binary.LittleEndian.Uint32(data[4:8]))
	return V128Value{Low: low, High: high}
}

// SimdI8x16Shuffle performs a byte shuffle operation.
func SimdI8x16Shuffle(v1, v2 V128Value, lanes [16]byte) V128Value {
	bytes1 := v1.Bytes()
	bytes2 := v2.Bytes()

	var resultBytes [16]byte
	for i, lane := range lanes {
		if lane < 16 {
			resultBytes[i] = bytes1[lane]
		} else if lane < 32 {
			resultBytes[i] = bytes2[lane-16]
		} else {
			resultBytes[i] = 0
		}
	}

	return NewV128Value(resultBytes)
}

// SimdI8x16Swizzle performs a byte swizzle operation.
func SimdI8x16Swizzle(v1, v2 V128Value) V128Value {
	bytes1 := v1.Bytes()
	bytes2 := v2.Bytes()

	var resultBytes [16]byte
	for i := range 16 {
		index := int(bytes2[i])
		if index < 16 {
			resultBytes[i] = bytes1[index]
		} else {
			resultBytes[i] = 0 // Out of bounds indices result in 0.
		}
	}

	return NewV128Value(resultBytes)
}

func SimdI8x16Splat(val int32) V128Value {
	v8 := uint64(byte(val))
	v16 := v8 | (v8 << 8)
	v32 := v16 | (v16 << 16)
	v64 := v32 | (v32 << 32)
	return V128Value{Low: v64, High: v64}
}

func SimdI8x16SplatFromBytes(data []byte) V128Value {
	return SimdI8x16Splat(int32(data[0]))
}

func SimdI16x8Splat(val int32) V128Value {
	v16 := uint64(uint16(val))
	v32 := v16 | (v16 << 16)
	v64 := v32 | (v32 << 32)
	return V128Value{Low: v64, High: v64}
}

func SimdI16x8SplatFromBytes(data []byte) V128Value {
	return SimdI16x8Splat(int32(binary.LittleEndian.Uint16(data)))
}

func SimdI32x4Splat(val int32) V128Value {
	v := uint32(val)
	low := uint64(v) | (uint64(v) << 32)
	return V128Value{Low: low, High: low}
}

func SimdI32x4SplatFromBytes(data []byte) V128Value {
	return SimdI32x4Splat(int32(binary.LittleEndian.Uint32(data)))
}

func SimdI64x2Splat(val int64) V128Value {
	v := uint64(val)
	return V128Value{Low: v, High: v}
}

func SimdI64x2SplatFromBytes(data []byte) V128Value {
	return SimdI64x2Splat(int64(binary.LittleEndian.Uint64(data)))
}

func SimdF32x4Splat(val float32) V128Value {
	bits := math.Float32bits(val)
	v := uint64(bits)
	low := v | (v << 32)
	return V128Value{Low: low, High: low}
}

func SimdF64x2Splat(val float64) V128Value {
	bits := math.Float64bits(val)
	return V128Value{Low: bits, High: bits}
}

// SimdI8x16ExtractLaneS extracts a signed 8-bit integer from the specified
// lane.
func SimdI8x16ExtractLaneS(v V128Value, laneIndex uint32) (int32, error) {
	bytes, err := ExtractLane(v, 8, laneIndex)
	if err != nil {
		return 0, err
	}
	return int32(int8(bytes[0])), nil
}

// SimdI8x16ExtractLaneU extracts an unsigned 8-bit integer from the specified
// lane.
func SimdI8x16ExtractLaneU(v V128Value, laneIndex uint32) (int32, error) {
	bytes, err := ExtractLane(v, 8, laneIndex)
	if err != nil {
		return 0, err
	}
	return int32(bytes[0]), nil
}

func SimdI8x16ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) (V128Value, error) {
	return SetLane(v, laneIndex, []byte{byte(laneValue)})
}

// SimdI16x8ExtractLaneS extracts a signed 16-bit integer from the specified
// lane.
func SimdI16x8ExtractLaneS(v V128Value, laneIndex uint32) (int32, error) {
	bytes, err := ExtractLane(v, 16, laneIndex)
	if err != nil {
		return 0, err
	}
	return int32(int16(binary.LittleEndian.Uint16(bytes))), nil
}

// SimdI16x8ExtractLaneU extracts an unsigned 16-bit integer from the specified
// lane.
func SimdI16x8ExtractLaneU(v V128Value, laneIndex uint32) (int32, error) {
	bytes, err := ExtractLane(v, 16, laneIndex)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint16(bytes)), nil
}

func SimdI16x8ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) (V128Value, error) {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uint16(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdI32x4ExtractLane extracts a 32-bit integer from the specified lane.
func SimdI32x4ExtractLane(v V128Value, laneIndex uint32) (int32, error) {
	bytes, err := ExtractLane(v, 32, laneIndex)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(bytes)), nil
}

func SimdI32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) (V128Value, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdI64x2ExtractLane extracts a 64-bit integer from the specified lane.
func SimdI64x2ExtractLane(v V128Value, laneIndex uint32) (int64, error) {
	bytes, err := ExtractLane(v, 64, laneIndex)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(bytes)), nil
}

func SimdI64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int64,
) (V128Value, error) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdF32x4ExtractLane extracts a 32-bit float from the specified lane.
func SimdF32x4ExtractLane(v V128Value, laneIndex uint32) (float32, error) {
	bytes, err := ExtractLane(v, 32, laneIndex)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(bytes)), nil
}

func SimdF32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float32,
) (V128Value, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, math.Float32bits(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdF64x2ExtractLane extracts a 64-bit float from the specified lane.
func SimdF64x2ExtractLane(v V128Value, laneIndex uint32) (float64, error) {
	bytes, err := ExtractLane(v, 64, laneIndex)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(bytes)), nil
}

func SimdF64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float64,
) (V128Value, error) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdI8x16Eq performs an equality comparison on each 8-bit lane of two
// V128Value.
func SimdI8x16Eq(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a == b {
			return -1
		}
		return 0
	})
}

// SimdI8x16Ne performs an inequality comparison on each 8-bit lane of two
// V128Value.
func SimdI8x16Ne(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a != b {
			return -1
		}
		return 0
	})
}

// SimdI8x16LtS performs a signed less-than comparison on each 8-bit lane.
func SimdI8x16LtS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a < b {
			return -1
		}
		return 0
	})
}

// SimdI8x16LtU performs an unsigned less-than comparison on each 8-bit lane.
func SimdI8x16LtU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte {
		return boolToUint[byte](a < b)
	})
}

// SimdI8x16GtS performs a signed greater-than comparison on each 8-bit lane.
func SimdI8x16GtS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a > b {
			return -1
		}
		return 0
	})
}

// SimdI8x16GtU performs an unsigned greater-than comparison on each 8-bit lane.
func SimdI8x16GtU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte {
		return boolToUint[byte](a > b)
	})
}

// SimdI8x16LeS performs a signed less-than-or-equal comparison on each 8-bit
// lane.
func SimdI8x16LeS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a <= b {
			return -1
		}
		return 0
	})
}

// SimdI8x16LeU performs an unsigned less-than-or-equal comparison on each 8-bit
// lane.
func SimdI8x16LeU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte {
		return boolToUint[byte](a <= b)
	})
}

// SimdI8x16GeS performs a signed greater-than-or-equal comparison on each 8-bit
// lane.
func SimdI8x16GeS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		if a >= b {
			return -1
		}
		return 0
	})
}

// SimdI8x16GeU performs an unsigned greater-than-or-equal comparison on each
// 8-bit lane.
func SimdI8x16GeU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte {
		return boolToUint[byte](a >= b)
	})
}

// SimdI16x8Eq performs an equality comparison on each 16-bit lane of two
// V128Value.
func SimdI16x8Eq(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a == b {
			return -1
		}
		return 0
	})
}

// SimdI16x8Ne performs an inequality comparison on each 16-bit lane of two
// V128Value.
func SimdI16x8Ne(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a != b {
			return -1
		}
		return 0
	})
}

// SimdI16x8LtS performs a signed less-than comparison on each 16-bit lane.
func SimdI16x8LtS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a < b {
			return -1
		}
		return 0
	})
}

// SimdI16x8LtU performs an unsigned less-than comparison on each 16-bit lane.
func SimdI16x8LtU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return boolToUint[uint16](a < b)
	})
}

// SimdI16x8GtS performs a signed greater-than comparison on each 16-bit lane.
func SimdI16x8GtS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a > b {
			return -1
		}
		return 0
	})
}

// SimdI16x8GtU performs an unsigned greater-than comparison on each 16-bit
// lane.
func SimdI16x8GtU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return boolToUint[uint16](a > b)
	})
}

// SimdI16x8LeS performs a signed less-than-or-equal comparison on each 16-bit
// lane.
func SimdI16x8LeS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a <= b {
			return -1
		}
		return 0
	})
}

// SimdI16x8LeU performs an unsigned less-than-or-equal comparison on each
// 16-bit lane.
func SimdI16x8LeU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return boolToUint[uint16](a <= b)
	})
}

// SimdI16x8GeS performs a signed greater-than-or-equal comparison on each 16-bit
// lane.
func SimdI16x8GeS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		if a >= b {
			return -1
		}
		return 0
	})
}

// SimdI16x8GeU performs an unsigned greater-than-or-equal comparison on each
// 16-bit lane.
func SimdI16x8GeU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return boolToUint[uint16](a >= b)
	})
}

func SimdI16x8Abs(v V128Value) V128Value {
	return unaryOpI16x8(v, func(val uint16) uint16 {
		s := int16(val)
		if s < 0 {
			s = -s
		}
		return uint16(s)
	})
}

func SimdI16x8Neg(v V128Value) V128Value {
	return unaryOpI16x8(v, func(val uint16) uint16 { return uint16(-int16(val)) })
}

func SimdI16x8Q15mulrSatS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		prod := int32(a) * int32(b)
		res := (prod + 16384) >> 15 // 16384 = 2^14
		if res > math.MaxInt16 {
			return math.MaxInt16
		}
		if res < math.MinInt16 {
			return math.MinInt16
		}
		return int16(res)
	})
}

func SimdI16x8NarrowI32x4S(v1, v2 V128Value) V128Value {
	return narrow(v1, v2, 4, true)
}

func SimdI16x8NarrowI32x4U(v1, v2 V128Value) V128Value {
	return narrow(v1, v2, 4, false)
}

// SimdI32x4Eq performs an equality comparison on each 32-bit lane of two
// V128Value.
func SimdI32x4Eq(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a == b {
			return -1
		}
		return 0
	})
}

// SimdI32x4Ne performs an inequality comparison on each 32-bit lane of two
// V128Value.
func SimdI32x4Ne(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a != b {
			return -1
		}
		return 0
	})
}

// SimdI32x4LtS performs a signed less-than comparison on each 32-bit lane.
func SimdI32x4LtS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a < b {
			return -1
		}
		return 0
	})
}

// SimdI32x4LtU performs an unsigned less-than comparison on each 32-bit lane.
func SimdI32x4LtU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 {
		return boolToUint[uint32](a < b)
	})
}

// SimdI32x4GtS performs a signed greater-than comparison on each 32-bit lane.
func SimdI32x4GtS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a > b {
			return -1
		}
		return 0
	})
}

// SimdI32x4GtU performs an unsigned greater-than comparison on each 32-bit
// lane.
func SimdI32x4GtU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 {
		return boolToUint[uint32](a > b)
	})
}

// SimdI32x4LeS performs a signed less-than-or-equal comparison on each 32-bit
// lane.
func SimdI32x4LeS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a <= b {
			return -1
		}
		return 0
	})
}

// SimdI32x4LeU performs an unsigned less-than-or-equal comparison on each
// 32-bit lane.
func SimdI32x4LeU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 {
		return boolToUint[uint32](a <= b)
	})
}

// SimdI32x4GeS performs a signed greater-than-or-equal comparison on each
// 32-bit lane.
func SimdI32x4GeS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 {
		if a >= b {
			return -1
		}
		return 0
	})
}

// SimdI32x4GeU performs an unsigned greater-than-or-equal comparison on each
// 32-bit lane.
func SimdI32x4GeU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 {
		return boolToUint[uint32](a >= b)
	})
}

// SimdF32x4Eq performs an equality comparison on each 32-bit float lane of two
// V128Value.
func SimdF32x4Eq(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a == b)
	})
}

// SimdF32x4Ne performs an inequality comparison on each 32-bit float lane of
// two V128Value.
func SimdF32x4Ne(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a != b)
	})
}

// SimdF32x4Lt performs a less-than comparison on each 32-bit float lane of two
// V128Value.
func SimdF32x4Lt(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a < b)
	})
}

// SimdF32x4Gt performs a greater-than comparison on each 32-bit float lane of
// two V128Value.
func SimdF32x4Gt(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a > b)
	})
}

// SimdF32x4Le performs a less-than-or-equal comparison on each 32-bit float
// lane of two V128Value.
func SimdF32x4Le(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a <= b)
	})
}

// SimdF32x4Ge performs a greater-than-or-equal comparison on each 32-bit float
// lane of two V128Value.
func SimdF32x4Ge(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a >= b)
	})
}

// SimdF64x2Eq performs an equality comparison on each 64-bit float lane of two
// V128Value.
func SimdF64x2Eq(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a == b)
	})
}

// SimdF64x2Ne performs an inequality comparison on each 64-bit float lane of
// two V128Value.
func SimdF64x2Ne(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a != b)
	})
}

// SimdF64x2Lt performs a less-than comparison on each 64-bit float lane of two
// V128Value.
func SimdF64x2Lt(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a < b)
	})
}

// SimdF64x2Gt performs a greater-than comparison on each 64-bit float lane of
// two V128Value.
func SimdF64x2Gt(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a > b)
	})
}

// SimdF64x2Le performs a less-than-or-equal comparison on each 64-bit float
// lane of two V128Value.
func SimdF64x2Le(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a <= b)
	})
}

// SimdF64x2Ge performs a greater-than-or-equal comparison on each 64-bit float
// lane of two V128Value.
func SimdF64x2Ge(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a >= b)
	})
}

// SimdV128Not performs a bitwise NOT operation on a V128Value.
func SimdV128Not(v V128Value) V128Value {
	return V128Value{
		Low:  ^v.Low,
		High: ^v.High,
	}
}

// SimdV128And performs a bitwise AND operation on two V128Value.
func SimdV128And(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low & v2.Low,
		High: v1.High & v2.High,
	}
}

func SimdV128Andnot(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low & ^v2.Low,
		High: v1.High & ^v2.High,
	}
}

// SimdV128Or performs a bitwise OR operation on two V128Value.
func SimdV128Or(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low | v2.Low,
		High: v1.High | v2.High,
	}
}

// SimdV128Xor performs a bitwise XOR operation on two V128Value.
func SimdV128Xor(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low ^ v2.Low,
		High: v1.High ^ v2.High,
	}
}

func SimdV128Bitselect(v1, v2, v3 V128Value) V128Value {
	return V128Value{
		Low:  (v1.Low & v3.Low) | (v2.Low & ^v3.Low),
		High: (v1.High & v3.High) | (v2.High & ^v3.High),
	}
}

// SimdV128AnyTrue returns true if any bit in the 128-bit SIMD value is set to
// 1.
func SimdV128AnyTrue(v V128Value) bool {
	return v.Low != 0 || v.High != 0
}

func SimdV128Load32Zero(data []byte) V128Value {
	low := uint64(binary.LittleEndian.Uint32(data))
	return V128Value{Low: low, High: 0}
}

func SimdV128Load64Zero(data []byte) V128Value {
	low := binary.LittleEndian.Uint64(data)
	return V128Value{Low: low, High: 0}
}

func SetLane(v V128Value, laneIndex uint32, value []byte) (V128Value, error) {
	result := V128Value{Low: v.Low, High: v.High}
	switch len(value) {
	case 1:
		val := uint64(value[0])
		shift := uint(laneIndex % 8 * 8)
		mask := ^(uint64(0xFF) << shift)
		if laneIndex < 8 {
			result.Low &= mask
			result.Low |= val << shift
		} else {
			result.High &= mask
			result.High |= val << shift
		}
	case 2:
		val := uint64(binary.LittleEndian.Uint16(value))
		shift := uint(laneIndex % 4 * 16)
		mask := ^(uint64(0xFFFF) << shift)
		if laneIndex < 4 {
			result.Low &= mask
			result.Low |= val << shift
		} else {
			result.High &= mask
			result.High |= val << shift
		}
	case 4:
		val := uint64(binary.LittleEndian.Uint32(value))
		shift := uint(laneIndex % 2 * 32)
		mask := ^(uint64(0xFFFFFFFF) << shift)
		if laneIndex < 2 {
			result.Low &= mask
			result.Low |= val << shift
		} else {
			result.High &= mask
			result.High |= val << shift
		}
	case 8:
		if laneIndex == 0 {
			result.Low = binary.LittleEndian.Uint64(value)
		} else {
			result.High = binary.LittleEndian.Uint64(value)
		}
	default:
		return V128Value{}, fmt.Errorf("unsupported lane size: %d", len(value))
	}
	return result, nil
}

func ExtractLane(value V128Value, laneSize, laneIndex uint32) ([]byte, error) {
	laneSizeBytes := laneSize / 8
	bytes := make([]byte, laneSizeBytes)

	if laneSize == 64 { // Handle the simple 64-bit case separately
		var section uint64
		if laneIndex == 0 {
			section = value.Low
		} else {
			section = value.High
		}
		binary.LittleEndian.PutUint64(bytes, section)
		return bytes, nil
	}

	lanesPer64 := 64 / laneSize
	var section uint64
	var localIndex uint32
	if laneIndex < lanesPer64 {
		section = value.Low
		localIndex = laneIndex
	} else {
		section = value.High
		localIndex = laneIndex - lanesPer64
	}

	switch laneSize {
	case 8:
		val := section >> (localIndex * 8)
		bytes[0] = byte(val)
	case 16:
		val := section >> (localIndex * 16)
		binary.LittleEndian.PutUint16(bytes, uint16(val))
	case 32:
		val := section >> (localIndex * 32)
		binary.LittleEndian.PutUint32(bytes, uint32(val))
	default:
		return nil, fmt.Errorf("unsupported lane size: %d", laneSize)
	}
	return bytes, nil
}

func SimdF32x4DemoteF64x2Zero(v V128Value) V128Value {
	f64Low := math.Float64frombits(v.Low)
	f64High := math.Float64frombits(v.High)

	f32Low := float32(f64Low)
	f32High := float32(f64High)

	buf := [8]byte{}
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(f32Low))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(f32High))

	return V128Value{
		Low:  binary.LittleEndian.Uint64(buf[0:8]),
		High: 0,
	}
}

func SimdF64x2PromoteLowF32x4(v V128Value) V128Value {
	f32Low := math.Float32frombits(uint32(v.Low))
	f32High := math.Float32frombits(uint32(v.Low >> 32))

	return V128Value{
		Low:  math.Float64bits(float64(f32Low)),
		High: math.Float64bits(float64(f32High)),
	}
}

func SimdI8x16Abs(v V128Value) V128Value {
	return unaryOpI8x16(v, func(b byte) byte {
		val := int8(b)
		if val < 0 {
			val = -val
		}
		return byte(val)
	})
}

func SimdI8x16Neg(v V128Value) V128Value {
	return unaryOpI8x16(v, func(b byte) byte { return byte(-int8(b)) })
}

func SimdI8x16Popcnt(v V128Value) V128Value {
	return unaryOpI8x16(v, func(b byte) byte { return byte(bits.OnesCount8(b)) })
}

func SimdI8x16NarrowI16x8S(v1, v2 V128Value) V128Value {
	return narrow(v1, v2, 2, true)
}

func SimdI8x16NarrowI16x8U(v1, v2 V128Value) V128Value {
	return narrow(v1, v2, 2, false)
}

// SimdI8x16AllTrue returns true if all 8-bit lanes of a V128Value are non-zero.
func SimdI8x16AllTrue(v V128Value) bool {
	buf := v.Bytes()
	return !slices.Contains(buf[:], 0)
}

// SimdI8x16Bitmask returns a 16-bit integer wrapped in an int32 where each bit
// corresponds to the most significant bit of each of the 16 8-bit lanes of a
// V128Value.
func SimdI8x16Bitmask(v V128Value) int32 {
	return bitmask(v, 1)
}

// SimdI8x16Shl performs a left shift on each 8-bit lane of a V128Value.
func SimdI8x16Shl(v V128Value, shift int32) V128Value {
	s := shift & 7 // shift amount is modulo 8
	return unaryOpI8x16(v, func(b byte) byte { return b << s })
}

// SimdI8x16ShrU performs an unsigned right shift on each 8-bit lane
// of a V128Value.
func SimdI8x16ShrU(v V128Value, shift int32) V128Value {
	s := shift & 7 // shift amount is modulo 8
	return unaryOpI8x16(v, func(b byte) byte { return b >> s })
}

// SimdI8x16ShrS performs a signed right shift on each 8-bit lane of a
// V128Value.
func SimdI8x16ShrS(v V128Value, shift int32) V128Value {
	s := shift & 7 // shift amount is modulo 8
	return unaryOpI8x16(v, func(b byte) byte {
		// convert to int8 to perform a signed right shift
		val := int8(b) >> s
		return byte(val)
	})
}

// SimdI8x16Add performs an addition on each 8-bit lane of two V128Value.
func SimdI8x16Add(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 { return a + b })
}

// SimdI8x16AddSatS performs a saturating signed addition on each 8-bit lane of
// two V128Value.
func SimdI8x16AddSatS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		res := int16(a) + int16(b)
		if res > math.MaxInt8 {
			return math.MaxInt8
		}
		if res < math.MinInt8 {
			return math.MinInt8
		}
		return int8(res)
	})
}

// SimdI8x16AddSatU performs a saturating unsigned addition on each 8-bit lane
// of two V128Value.
func SimdI8x16AddSatU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b uint8) uint8 {
		return uint8(min(uint16(a)+uint16(b), math.MaxUint8))
	})
}

// SimdI8x16Sub performs a subtraction on each 8-bit lane of two V128Value.
func SimdI8x16Sub(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 { return a - b })
}

// SimdI8x16SubSatS performs a saturating signed subtraction on each 8-bit lane
// of two V128Value.
func SimdI8x16SubSatS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 {
		res := int16(a) - int16(b)
		if res > math.MaxInt8 {
			return math.MaxInt8
		} else if res < math.MinInt8 {
			return math.MinInt8
		}
		return int8(res)
	})
}

// SimdI8x16SubSatU performs a saturating unsigned subtraction on each 8-bit
// lane of two V128Value.
func SimdI8x16SubSatU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b uint8) uint8 {
		return uint8(max(int16(a)-int16(b), 0))
	})
}

func SimdI8x16MinS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 { return min(a, b) })
}

func SimdI8x16MinU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte { return min(a, b) })
}

func SimdI8x16MaxS(v1, v2 V128Value) V128Value {
	return binaryOpI8x16(v1, v2, func(a, b int8) int8 { return max(a, b) })
}

func SimdI8x16MaxU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte { return max(a, b) })
}

func SimdI8x16AvgrU(v1, v2 V128Value) V128Value {
	return binaryOpUI8x16(v1, v2, func(a, b byte) byte {
		return byte((uint16(a) + uint16(b) + 1) >> 1)
	})
}

func SimdI16x8AllTrue(v V128Value) bool {
	return allTrue(v, 2)
}

func SimdI16x8Bitmask(v V128Value) int32 {
	return bitmask(v, 2)
}

func SimdI16x8ExtendLowI8x16S(v V128Value) V128Value {
	return extend(v, 1, 2, false, true)
}

func SimdI16x8ExtendHighI8x16S(v V128Value) V128Value {
	return extend(v, 1, 2, true, true)
}

func SimdI16x8ExtendLowI8x16U(v V128Value) V128Value {
	return extend(v, 1, 2, false, false)
}

func SimdI16x8ExtendHighI8x16U(v V128Value) V128Value {
	return extend(v, 1, 2, true, false)
}

func SimdI32x4ExtendLowI16x8S(v V128Value) V128Value {
	return extend(v, 2, 4, false, true)
}

func SimdI32x4ExtendHighI16x8S(v V128Value) V128Value {
	return extend(v, 2, 4, true, true)
}

func SimdI32x4ExtendLowI16x8U(v V128Value) V128Value {
	return extend(v, 2, 4, false, false)
}

func SimdI32x4ExtendHighI16x8U(v V128Value) V128Value {
	return extend(v, 2, 4, true, false)
}

func SimdI64x2ExtendLowI32x4S(v V128Value) V128Value {
	return extend(v, 4, 8, false, true)
}

func SimdI64x2ExtendHighI32x4S(v V128Value) V128Value {
	return extend(v, 4, 8, true, true)
}

func SimdI64x2ExtendLowI32x4U(v V128Value) V128Value {
	return extend(v, 4, 8, false, false)
}

func SimdI64x2ExtendHighI32x4U(v V128Value) V128Value {
	return extend(v, 4, 8, true, false)
}

func SimdI16x8Shl(v V128Value, shift int32) V128Value {
	s := shift & 15 // shift amount is modulo 16
	return unaryOpI16x8(v, func(val uint16) uint16 { return val << s })
}

// SimdI16x8ShrS performs a signed right shift on each 16-bit lane of a
// V128Value.
func SimdI16x8ShrS(v V128Value, shift int32) V128Value {
	s := shift & 15 // shift amount is modulo 16
	return unaryOpI16x8(v, func(val uint16) uint16 {
		// convert to int16 to perform a signed right shift
		signedVal := int16(val)
		signedVal >>= s
		return uint16(signedVal)
	})
}

// SimdI16x8ShrU performs an unsigned right shift on each 16-bit lane of a
// V128Value.
func SimdI16x8ShrU(v V128Value, shift int32) V128Value {
	s := shift & 15 // shift amount is modulo 16
	return unaryOpI16x8(v, func(val uint16) uint16 { return val >> s })
}

// SimdI16x8Add performs an addition on each 16-bit lane of two V128Value.
func SimdI16x8Add(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 { return a + b })
}

// SimdI16x8AddSatS performs a saturating signed addition on each 16-bit lane of
// two V128Value.
func SimdI16x8AddSatS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		res := int32(a) + int32(b)
		if res > math.MaxInt16 {
			return math.MaxInt16
		} else if res < math.MinInt16 {
			return math.MinInt16
		}
		return int16(res)
	})
}

// SimdI16x8AddSatU performs a saturating unsigned addition on each 16-bit lane
// of two V128Value.
func SimdI16x8AddSatU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return uint16(min(uint32(a)+uint32(b), math.MaxUint16))
	})
}

// SimdI16x8Sub performs a subtraction on each 16-bit lane of two V128Value.
func SimdI16x8Sub(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 { return a - b })
}

// SimdI16x8SubSatS performs a saturating signed subtraction on each 16-bit lane
// of two V128Value.
func SimdI16x8SubSatS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 {
		res := int32(a) - int32(b)
		if res > math.MaxInt16 {
			return math.MaxInt16
		} else if res < math.MinInt16 {
			return math.MinInt16
		}
		return int16(res)
	})
}

// SimdI16x8SubSatU performs a saturating unsigned subtraction on each 16-bit
// lane of two V128Value.
func SimdI16x8SubSatU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return uint16(max(int32(a)-int32(b), 0))
	})
}

// SimdI16x8Mul performs a multiplication on each 16-bit lane of two V128Value.
func SimdI16x8Mul(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 { return a * b })
}

func SimdI16x8MinS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 { return min(a, b) })
}

func SimdI16x8MinU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 { return min(a, b) })
}

func SimdI16x8MaxS(v1, v2 V128Value) V128Value {
	return binaryOpI16x8(v1, v2, func(a, b int16) int16 { return max(a, b) })
}

func SimdI16x8MaxU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 { return max(a, b) })
}

func SimdI16x8AvgrU(v1, v2 V128Value) V128Value {
	return binaryOpUI16x8(v1, v2, func(a, b uint16) uint16 {
		return uint16((uint32(a) + uint32(b) + 1) >> 1)
	})
}

func SimdI16x8ExtmulLowI8x16S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, 2, false, true)
}

func SimdI16x8ExtmulHighI8x16S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, 2, true, true)
}

func SimdI16x8ExtmulLowI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, 2, false, false)
}

func SimdI16x8ExtmulHighI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, 2, true, false)
}

func SimdI32x4ExtmulLowI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, 4, false, true)
}

func SimdI32x4ExtmulHighI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, 4, true, true)
}

func SimdI32x4ExtmulLowI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, 4, false, false)
}

func SimdI32x4ExtmulHighI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, 4, true, false)
}

func SimdI64x2ExtmulLowI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, 8, false, true)
}

func SimdI64x2ExtmulHighI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, 8, true, true)
}

func SimdI64x2ExtmulLowI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, 8, false, false)
}

func SimdI64x2ExtmulHighI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, 8, true, false)
}

func SimdI16x8ExtaddPairwiseI8x16S(v V128Value) V128Value {
	inBytes := v.Bytes()

	var resultBytes [16]byte
	for i := range 8 {
		val1 := int16(int8(inBytes[i*2]))
		val2 := int16(int8(inBytes[i*2+1]))
		res := val1 + val2
		binary.LittleEndian.PutUint16(resultBytes[i*2:i*2+2], uint16(res))
	}

	return NewV128Value(resultBytes)
}

func SimdI16x8ExtaddPairwiseI8x16U(v V128Value) V128Value {
	inBytes := v.Bytes()

	var resultBytes [16]byte
	for i := range 8 {
		val1 := uint16(inBytes[i*2])
		val2 := uint16(inBytes[i*2+1])
		res := val1 + val2
		binary.LittleEndian.PutUint16(resultBytes[i*2:i*2+2], res)
	}

	return NewV128Value(resultBytes)
}

func SimdI32x4ExtaddPairwiseI16x8U(v V128Value) V128Value {
	inBytes := v.Bytes()

	var resultBytes [16]byte
	for i := range 4 {
		val1 := uint32(binary.LittleEndian.Uint16(inBytes[i*4 : i*4+2]))
		val2 := uint32(binary.LittleEndian.Uint16(inBytes[i*4+2 : i*4+4]))
		res := val1 + val2
		binary.LittleEndian.PutUint32(resultBytes[i*4:i*4+4], res)
	}

	return NewV128Value(resultBytes)
}

func SimdI32x4ExtaddPairwiseI16x8S(v V128Value) V128Value {
	inBytes := v.Bytes()

	var resultBytes [16]byte
	for i := range 4 {
		val1 := int32(int16(binary.LittleEndian.Uint16(inBytes[i*4 : i*4+2])))
		val2 := int32(int16(binary.LittleEndian.Uint16(inBytes[i*4+2 : i*4+4])))
		res := val1 + val2
		binary.LittleEndian.PutUint32(resultBytes[i*4:i*4+4], uint32(res))
	}

	return NewV128Value(resultBytes)
}

func SimdI32x4Abs(v V128Value) V128Value {
	return unaryOpI32x4(v, func(val uint32) uint32 {
		s := int32(val) >> 31
		return uint32((int32(val) ^ s) - s)
	})
}

func SimdI32x4Neg(v V128Value) V128Value {
	return unaryOpI32x4(v, func(val uint32) uint32 { return uint32(-int32(val)) })
}

func SimdI32x4AllTrue(v V128Value) bool {
	return allTrue(v, 4)
}

func SimdI32x4Bitmask(v V128Value) int32 {
	return bitmask(v, 4)
}

// SimdI32x4Shl performs a left shift on each 32-bit lane of a V128Value.
func SimdI32x4Shl(v V128Value, shift int32) V128Value {
	s := shift & 31 // shift amount is modulo 32
	return unaryOpI32x4(v, func(val uint32) uint32 { return val << s })
}

// SimdI32x4ShrS performs a signed right shift on each 32-bit lane of a
// V128Value.
func SimdI32x4ShrS(v V128Value, shift int32) V128Value {
	s := shift & 31 // shift amount is modulo 32
	return unaryOpI32x4(v, func(val uint32) uint32 {
		// convert to int32 to perform a signed right shift
		signedVal := int32(val)
		signedVal >>= s
		return uint32(signedVal)
	})
}

// SimdI32x4ShrU performs an unsigned right shift on each 32-bit lane of a
// V128Value.
func SimdI32x4ShrU(v V128Value, shift int32) V128Value {
	s := shift & 31 // shift amount is modulo 32
	return unaryOpI32x4(v, func(val uint32) uint32 { return val >> s })
}

func SimdI32x4Add(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 { return a + b })
}

func SimdI32x4Sub(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 { return a - b })
}

func SimdI32x4Mul(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 { return a * b })
}

func SimdI32x4MinS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 { return min(a, b) })
}

func SimdI32x4MinU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 { return min(a, b) })
}

func SimdI32x4MaxS(v1, v2 V128Value) V128Value {
	return binaryOpI32x4(v1, v2, func(a, b int32) int32 { return max(a, b) })
}

func SimdI32x4MaxU(v1, v2 V128Value) V128Value {
	return binaryOpUI32x4(v1, v2, func(a, b uint32) uint32 { return max(a, b) })
}

func SimdI32x4DotI16x8S(v1, v2 V128Value) V128Value {
	v1Bytes := v1.Bytes()
	v2Bytes := v2.Bytes()

	var resultBytes [16]byte
	for i := range 4 {
		a1 := int32(int16(binary.LittleEndian.Uint16(v1Bytes[i*4 : i*4+2])))
		b1 := int32(int16(binary.LittleEndian.Uint16(v2Bytes[i*4 : i*4+2])))

		a2 := int32(int16(binary.LittleEndian.Uint16(v1Bytes[i*4+2 : i*4+4])))
		b2 := int32(int16(binary.LittleEndian.Uint16(v2Bytes[i*4+2 : i*4+4])))

		res := (a1 * b1) + (a2 * b2)
		binary.LittleEndian.PutUint32(resultBytes[i*4:i*4+4], uint32(res))
	}

	return NewV128Value(resultBytes)
}

func SimdI64x2Abs(v V128Value) V128Value {
	sLow := int64(v.Low) >> 63
	sHigh := int64(v.High) >> 63
	return V128Value{
		Low:  uint64((int64(v.Low) ^ sLow) - sLow),
		High: uint64((int64(v.High) ^ sHigh) - sHigh),
	}
}

func SimdI64x2Neg(v V128Value) V128Value {
	return V128Value{
		Low:  uint64(-int64(v.Low)),
		High: uint64(-int64(v.High)),
	}
}

func SimdI64x2AllTrue(v V128Value) bool {
	return v.Low != 0 && v.High != 0
}

func SimdI64x2Bitmask(v V128Value) int32 {
	return bitmask(v, 8)
}

// SimdI64x2Shl performs a left shift on each 64-bit lane of a V128Value.
func SimdI64x2Shl(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  v.Low << s,
		High: v.High << s,
	}
}

// SimdI64x2ShrS performs a signed right shift on each 64-bit lane of a
// V128Value.
func SimdI64x2ShrS(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  uint64(int64(v.Low) >> s),
		High: uint64(int64(v.High) >> s),
	}
}

// SimdI64x2ShrU performs an unsigned right shift on each 64-bit lane of a
// V128Value.
func SimdI64x2ShrU(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  v.Low >> s,
		High: v.High >> s,
	}
}

func SimdI64x2Add(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low + v2.Low,
		High: v1.High + v2.High,
	}
}

func SimdI64x2Sub(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low - v2.Low,
		High: v1.High - v2.High,
	}
}

func SimdI64x2Mul(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low * v2.Low,
		High: v1.High * v2.High,
	}
}

// SimdI64x2Eq performs an equality comparison on each 64-bit lane of two
// V128Value.
func SimdI64x2Eq(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](v1.Low == v2.Low),
		High: boolToUint[uint64](v1.High == v2.High),
	}
}

// SimdI64x2Ne performs an inequality comparison on each 64-bit lane of two
// V128Value.
func SimdI64x2Ne(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](v1.Low != v2.Low),
		High: boolToUint[uint64](v1.High != v2.High),
	}
}

// SimdI64x2LtS performs a signed less-than comparison on each 64-bit lane.
func SimdI64x2LtS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) < int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) < int64(v2.High)),
	}
}

// SimdI64x2GtS performs a signed greater-than comparison on each 64-bit lane.
func SimdI64x2GtS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) > int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) > int64(v2.High)),
	}
}

// SimdI64x2LeS performs a signed less-than-or-equal comparison on each 64-bit
// lane.
func SimdI64x2LeS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) <= int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) <= int64(v2.High)),
	}
}

// SimdI64x2GeS performs a signed greater-than-or-equal comparison on each
// 64-bit lane.
func SimdI64x2GeS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) >= int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) >= int64(v2.High)),
	}
}

// SimdF32x4Abs performs an absolute value operation on each 32-bit float lane.
func SimdF32x4Abs(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		return float32(math.Abs(float64(f)))
	})
}

func SimdF32x4Neg(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 { return -f })
}

func SimdF32x4Sqrt(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		if math.IsNaN(float64(f)) {
			return float32(math.NaN())
		}
		if f < 0 {
			return float32(math.NaN())
		}
		return float32(math.Sqrt(float64(f)))
	})
}

// SimdF32x4Add performs an addition on each 32-bit float lane.
func SimdF32x4Add(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		res := a + b
		if math.IsNaN(float64(res)) {
			return float32(math.NaN())
		}
		return res
	})
}

func SimdF32x4Ceil(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		if math.IsNaN(float64(f)) {
			return math.Float32frombits(0x7fc00000)
		}
		return float32(math.Ceil(float64(f)))
	})
}

func SimdF32x4Floor(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		if math.IsNaN(float64(f)) {
			return math.Float32frombits(0x7fc00000)
		}
		return float32(math.Floor(float64(f)))
	})
}

func SimdF32x4Trunc(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		if math.IsNaN(float64(f)) {
			return math.Float32frombits(0x7fc00000)
		}
		return float32(math.Trunc(float64(f)))
	})
}

func SimdF32x4Nearest(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 {
		if math.IsNaN(float64(f)) {
			return math.Float32frombits(0x7fc00000)
		}
		return float32(math.RoundToEven(float64(f)))
	})
}

// SimdF32x4Sub performs a subtraction on each 32-bit float lane.
func SimdF32x4Sub(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		res := a - b
		if math.IsNaN(float64(res)) {
			return float32(math.NaN())
		}
		return res
	})
}

// SimdF32x4Mul performs a multiplication on each 32-bit float lane.
func SimdF32x4Mul(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		res := a * b
		if math.IsNaN(float64(res)) {
			return float32(math.NaN())
		}
		return res
	})
}

// SimdF32x4Min performs a minimum operation on each 32-bit float lane.
func SimdF32x4Min(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
			return float32(math.NaN())
		}
		return float32(math.Min(float64(a), float64(b)))
	})
}

func SimdF32x4Max(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
			return float32(math.NaN())
		}
		return float32(math.Max(float64(a), float64(b)))
	})
}

func SimdF32x4Pmin(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if b < a {
			return b
		}
		return a
	})
}

func SimdF32x4Pmax(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if a < b {
			return b
		}
		return a
	})
}

// SimdF32x4Div performs a division operation on each 32-bit float lane.
func SimdF32x4Div(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		res := a / b
		if math.IsNaN(float64(res)) {
			return float32(math.NaN())
		}
		return res
	})
}

// SimdF64x2Add performs an addition on each 64-bit float lane of two V128Value.
func SimdF64x2Add(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		res := a + b
		if math.IsNaN(res) {
			return math.NaN()
		}
		return res
	})
}

// SimdF64x2Sub performs a subtraction on each 64-bit float lane of two
// V128Value.
func SimdF64x2Sub(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		res := a - b
		if math.IsNaN(res) {
			return math.NaN()
		}
		return res
	})
}

func SimdF64x2Ceil(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 {
		if math.IsNaN(f) {
			return math.NaN()
		}
		return math.Ceil(f)
	})
}

func SimdF64x2Floor(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 {
		if math.IsNaN(f) {
			return math.NaN()
		}
		return math.Floor(f)
	})
}

func SimdF64x2Trunc(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 {
		if math.IsNaN(f) {
			return math.NaN()
		}
		return math.Trunc(f)
	})
}

func SimdF64x2Nearest(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 {
		if math.IsNaN(f) {
			return math.NaN()
		}
		return math.RoundToEven(f)
	})
}

// SimdF64x2Mul performs a multiplication on each 64-bit float lane of two
// V128Value.
func SimdF64x2Mul(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		res := a * b
		if math.IsNaN(res) {
			return math.NaN()
		}
		return res
	})
}

func SimdF64x2Min(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if math.IsNaN(a) || math.IsNaN(b) {
			return math.NaN()
		}
		return math.Min(a, b)
	})
}

func SimdF64x2Max(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if math.IsNaN(a) || math.IsNaN(b) {
			return math.NaN()
		}
		return math.Max(a, b)
	})
}

func SimdF64x2Pmin(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if math.IsNaN(a) || math.IsNaN(b) {
			return a
		}
		if a <= b {
			return a
		}
		return b
	})
}

func SimdF64x2Pmax(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if math.IsNaN(a) || math.IsNaN(b) {
			return a
		}
		if a >= b {
			return a
		}
		return b
	})
}

func SimdI32x4TruncSatF32x4S(v V128Value) V128Value {
	inBuf := v.Bytes()

	var resultBytes [16]byte
	for i := 0; i < 16; i += 4 {
		f32bits := binary.LittleEndian.Uint32(inBuf[i : i+4])
		f32val := math.Float32frombits(f32bits)
		var i32val int32

		switch {
		case math.IsNaN(float64(f32val)):
			i32val = 0
		case f32val > math.MaxInt32:
			i32val = math.MaxInt32
		case f32val < math.MinInt32:
			i32val = math.MinInt32
		default:
			i32val = int32(f32val)
		}

		binary.LittleEndian.PutUint32(resultBytes[i:i+4], uint32(i32val))
	}

	return NewV128Value(resultBytes)
}

func SimdI32x4TruncSatF32x4U(v V128Value) V128Value {
	inBuf := v.Bytes()

	var resultBytes [16]byte
	for i := 0; i < 16; i += 4 {
		f32bits := binary.LittleEndian.Uint32(inBuf[i : i+4])
		f32val := math.Float32frombits(f32bits)
		var u32val uint32

		f64val := float64(f32val)

		switch {
		case math.IsNaN(f64val):
			u32val = 0
		case f64val > float64(math.MaxUint32):
			u32val = math.MaxUint32
		case f64val < 0:
			u32val = 0
		default:
			u32val = uint32(f64val)
		}

		binary.LittleEndian.PutUint32(resultBytes[i:i+4], u32val)
	}

	return NewV128Value(resultBytes)
}

func SimdI32x4TruncSatF64x2SZero(v V128Value) V128Value {
	f64_1 := math.Float64frombits(v.Low)
	f64_2 := math.Float64frombits(v.High)

	i32_1 := saturateF64toInt32(f64_1)
	i32_2 := saturateF64toInt32(f64_2)

	var resultBytes [16]byte
	binary.LittleEndian.PutUint32(resultBytes[0:4], uint32(i32_1))
	binary.LittleEndian.PutUint32(resultBytes[4:8], uint32(i32_2))
	return NewV128Value(resultBytes)
}

func SimdI32x4TruncSatF64x2UZero(v V128Value) V128Value {
	f64_1 := math.Float64frombits(v.Low)
	f64_2 := math.Float64frombits(v.High)

	u32_1 := saturateF64toUint32(f64_1)
	u32_2 := saturateF64toUint32(f64_2)

	var resultBytes [16]byte
	binary.LittleEndian.PutUint32(resultBytes[0:4], u32_1)
	binary.LittleEndian.PutUint32(resultBytes[4:8], u32_2)
	return NewV128Value(resultBytes)
}

func SimdF32x4ConvertI32x4S(v V128Value) V128Value {
	buf := v.Bytes()

	var resultBytes [16]byte
	for i := 0; i < 16; i += 4 {
		i32Val := int32(binary.LittleEndian.Uint32(buf[i : i+4]))
		f32Val := float32(i32Val)
		binary.LittleEndian.PutUint32(resultBytes[i:i+4], math.Float32bits(f32Val))
	}

	return NewV128Value(resultBytes)
}

func SimdF32x4ConvertI32x4U(v V128Value) V128Value {
	inBuf := v.Bytes()

	var resultBytes [16]byte
	for i := 0; i < 16; i += 4 {
		u32val := binary.LittleEndian.Uint32(inBuf[i : i+4])
		f32val := float32(u32val)
		binary.LittleEndian.PutUint32(resultBytes[i:i+4], math.Float32bits(f32val))
	}

	return NewV128Value(resultBytes)
}

func SimdF64x2Abs(v V128Value) V128Value {
	return unaryOpF64x2(v, math.Abs)
}

func SimdF64x2Neg(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 { return -f })
}

func SimdF64x2Sqrt(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 {
		if math.IsNaN(f) {
			return math.NaN()
		}
		if f < 0 {
			return math.NaN()
		}
		return math.Sqrt(f)
	})
}

func SimdF64x2Div(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		res := a / b
		if math.IsNaN(res) {
			return math.NaN()
		}
		return res
	})
}

func SimdF64x2ConvertLowI32x4S(v V128Value) V128Value {
	inBytes := v.Bytes()

	val1 := float64(int32(binary.LittleEndian.Uint32(inBytes[0:4])))
	val2 := float64(int32(binary.LittleEndian.Uint32(inBytes[4:8])))

	return V128Value{
		Low:  math.Float64bits(val1),
		High: math.Float64bits(val2),
	}
}

func SimdF64x2ConvertLowI32x4U(v V128Value) V128Value {
	inBytes := v.Bytes()

	val1 := float64(binary.LittleEndian.Uint32(inBytes[0:4]))
	val2 := float64(binary.LittleEndian.Uint32(inBytes[4:8]))

	return V128Value{
		Low:  math.Float64bits(val1),
		High: math.Float64bits(val2),
	}
}

func unaryOpI8x16(v V128Value, op func(byte) byte) V128Value {
	buf := v.Bytes()
	for i := range buf {
		buf[i] = op(buf[i])
	}
	return NewV128Value(buf)
}

func unaryOpI16x8(v V128Value, op func(uint16) uint16) V128Value {
	buf := v.Bytes()
	for i := 0; i < 16; i += 2 {
		val := binary.LittleEndian.Uint16(buf[i : i+2])
		val = op(val)
		binary.LittleEndian.PutUint16(buf[i:i+2], val)
	}
	return NewV128Value(buf)
}

func unaryOpI32x4(v V128Value, op func(uint32) uint32) V128Value {
	buf := v.Bytes()
	for i := 0; i < 16; i += 4 {
		val := binary.LittleEndian.Uint32(buf[i : i+4])
		val = op(val)
		binary.LittleEndian.PutUint32(buf[i:i+4], val)
	}
	return NewV128Value(buf)
}

func binaryOpI32x4(v1, v2 V128Value, op func(int32, int32) int32) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := 0; i < 16; i += 4 {
		val1 := binary.LittleEndian.Uint32(buf1[i : i+4])
		val2 := binary.LittleEndian.Uint32(buf2[i : i+4])
		result := op(int32(val1), int32(val2))
		binary.LittleEndian.PutUint32(buf1[i:i+4], uint32(result))
	}

	return NewV128Value(buf1)
}

func binaryOpUI32x4(
	v1, v2 V128Value,
	op func(uint32, uint32) uint32,
) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := 0; i < 16; i += 4 {
		val1 := binary.LittleEndian.Uint32(buf1[i : i+4])
		val2 := binary.LittleEndian.Uint32(buf2[i : i+4])
		result := op(val1, val2)
		binary.LittleEndian.PutUint32(buf1[i:i+4], result)
	}

	return NewV128Value(buf1)
}

func binaryOpI8x16(v1, v2 V128Value, op func(int8, int8) int8) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := range 16 {
		result := op(int8(buf1[i]), int8(buf2[i]))
		buf1[i] = byte(result)
	}

	return NewV128Value(buf1)
}

func binaryOpUI8x16(v1, v2 V128Value, op func(byte, byte) byte) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := range 16 {
		buf1[i] = op(buf1[i], buf2[i])
	}

	return NewV128Value(buf1)
}

func binaryOpI16x8(v1, v2 V128Value, op func(int16, int16) int16) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := 0; i < 16; i += 2 {
		val1 := binary.LittleEndian.Uint16(buf1[i : i+2])
		val2 := binary.LittleEndian.Uint16(buf2[i : i+2])
		result := op(int16(val1), int16(val2))
		binary.LittleEndian.PutUint16(buf1[i:i+2], uint16(result))
	}

	return NewV128Value(buf1)
}

func binaryOpUI16x8(v1, v2 V128Value, op func(uint16, uint16) uint16) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := 0; i < 16; i += 2 {
		val1 := binary.LittleEndian.Uint16(buf1[i : i+2])
		val2 := binary.LittleEndian.Uint16(buf2[i : i+2])
		result := op(val1, val2)
		binary.LittleEndian.PutUint16(buf1[i:i+2], result)
	}

	return NewV128Value(buf1)
}

func binaryOpF64x2(
	v1, v2 V128Value,
	op func(float64, float64) float64,
) V128Value {
	resLow := op(math.Float64frombits(v1.Low), math.Float64frombits(v2.Low))
	resHigh := op(math.Float64frombits(v1.High), math.Float64frombits(v2.High))

	return V128Value{
		Low:  math.Float64bits(resLow),
		High: math.Float64bits(resHigh),
	}
}

func binaryOpF32x4(
	v1, v2 V128Value,
	op func(float32, float32) float32,
) V128Value {
	buf1 := v1.Bytes()
	buf2 := v2.Bytes()

	for i := 0; i < 16; i += 4 {
		val1 := math.Float32frombits(binary.LittleEndian.Uint32(buf1[i : i+4]))
		val2 := math.Float32frombits(binary.LittleEndian.Uint32(buf2[i : i+4]))
		result := op(val1, val2)
		binary.LittleEndian.PutUint32(buf1[i:i+4], math.Float32bits(result))
	}

	return NewV128Value(buf1)
}

func unaryOpF32x4(v V128Value, op func(float32) float32) V128Value {
	buf := v.Bytes()
	for i := 0; i < 16; i += 4 {
		val := math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4]))
		val = op(val)
		binary.LittleEndian.PutUint32(buf[i:i+4], math.Float32bits(val))
	}
	return NewV128Value(buf)
}

func unaryOpF64x2(v V128Value, op func(float64) float64) V128Value {
	return V128Value{
		Low:  math.Float64bits(op(math.Float64frombits(v.Low))),
		High: math.Float64bits(op(math.Float64frombits(v.High))),
	}
}

func extend(v V128Value, fromBytes, toBytes int, high, signed bool) V128Value {
	inBytes := v.Bytes()
	var resultBytes [16]byte
	start := 0
	if high {
		start = 8 // Start from the high half of the input vector.
	}

	numLanes := 8 / fromBytes
	for i := range numLanes {
		inLane := inBytes[start+i*fromBytes : start+(i+1)*fromBytes]
		outLane := resultBytes[i*toBytes : (i+1)*toBytes]

		var val int64
		switch fromBytes {
		case 1:
			if signed {
				val = int64(int8(inLane[0]))
			} else {
				val = int64(inLane[0])
			}
		case 2:
			if signed {
				val = int64(int16(binary.LittleEndian.Uint16(inLane)))
			} else {
				val = int64(binary.LittleEndian.Uint16(inLane))
			}
		case 4:
			if signed {
				val = int64(int32(binary.LittleEndian.Uint32(inLane)))
			} else {
				val = int64(binary.LittleEndian.Uint32(inLane))
			}
		}

		switch toBytes {
		case 2:
			binary.LittleEndian.PutUint16(outLane, uint16(val))
		case 4:
			binary.LittleEndian.PutUint32(outLane, uint32(val))
		case 8:
			binary.LittleEndian.PutUint64(outLane, uint64(val))
		}
	}
	return NewV128Value(resultBytes)
}

func extmul(
	v1, v2 V128Value,
	fromBytes, toBytes int,
	high, signed bool,
) V128Value {
	v1Bytes := v1.Bytes()
	v2Bytes := v2.Bytes()
	var resultBytes [16]byte
	start := 0
	if high {
		start = 8 // Start from the high half of the input vectors
	}

	numLanes := 8 / fromBytes
	for i := range numLanes {
		inLane1 := v1Bytes[start+i*fromBytes : start+(i+1)*fromBytes]
		inLane2 := v2Bytes[start+i*fromBytes : start+(i+1)*fromBytes]
		outLane := resultBytes[i*toBytes : (i+1)*toBytes]

		var product int64 // Use a large type for the product

		if signed {
			var val1, val2 int64
			switch fromBytes {
			case 1:
				val1 = int64(int8(inLane1[0]))
				val2 = int64(int8(inLane2[0]))
			case 2:
				val1 = int64(int16(binary.LittleEndian.Uint16(inLane1)))
				val2 = int64(int16(binary.LittleEndian.Uint16(inLane2)))
			case 4:
				val1 = int64(int32(binary.LittleEndian.Uint32(inLane1)))
				val2 = int64(int32(binary.LittleEndian.Uint32(inLane2)))
			}
			product = val1 * val2
		} else { // Unsigned
			var val1, val2 uint64
			switch fromBytes {
			case 1:
				val1 = uint64(inLane1[0])
				val2 = uint64(inLane2[0])
			case 2:
				val1 = uint64(binary.LittleEndian.Uint16(inLane1))
				val2 = uint64(binary.LittleEndian.Uint16(inLane2))
			case 4:
				val1 = uint64(binary.LittleEndian.Uint32(inLane1))
				val2 = uint64(binary.LittleEndian.Uint32(inLane2))
			}
			product = int64(val1 * val2) // Store as int64 for the write switch
		}

		switch toBytes {
		case 2:
			binary.LittleEndian.PutUint16(outLane, uint16(product))
		case 4:
			binary.LittleEndian.PutUint32(outLane, uint32(product))
		case 8:
			binary.LittleEndian.PutUint64(outLane, uint64(product))
		}
	}
	return NewV128Value(resultBytes)
}

// narrow is a generalized helper for all narrow operations.
func narrow(v1, v2 V128Value, fromBytes int, signed bool) V128Value {
	v1Bytes := v1.Bytes()
	v2Bytes := v2.Bytes()
	var resultBytes [16]byte
	toBytes := fromBytes / 2
	numLanesPerInput := 16 / fromBytes

	// Low half of the result comes from v1
	for i := range numLanesPerInput {
		inLane := v1Bytes[i*fromBytes : (i+1)*fromBytes]
		outLane := resultBytes[i*toBytes : (i+1)*toBytes]
		saturateAndPut(inLane, outLane, signed)
	}

	// High half of the result comes from v2
	offset := 8 / toBytes
	for i := range numLanesPerInput {
		inLane := v2Bytes[i*fromBytes : (i+1)*fromBytes]
		outLane := resultBytes[(offset+i)*toBytes : (offset+i+1)*toBytes]
		saturateAndPut(inLane, outLane, signed)
	}

	return NewV128Value(resultBytes)
}

// saturateAndPut is a helper for narrow that handles saturation logic.
func saturateAndPut(inLane, outLane []byte, signed bool) {
	fromBytes := len(inLane)
	var val int64

	// Read value from input lane
	if fromBytes == 4 { // I32x4 to I16x8
		val = int64(int32(binary.LittleEndian.Uint32(inLane)))
	} else { // I16x8 to I8x16
		val = int64(int16(binary.LittleEndian.Uint16(inLane)))
	}

	if signed {
		var minVal, maxVal int64
		if fromBytes == 4 {
			minVal, maxVal = math.MinInt16, math.MaxInt16
		} else {
			minVal, maxVal = math.MinInt8, math.MaxInt8
		}

		val = min(max(val, minVal), maxVal)

		if fromBytes == 4 {
			binary.LittleEndian.PutUint16(outLane, uint16(int16(val)))
		} else {
			outLane[0] = byte(int8(val))
		}
	} else { // Unsigned
		var max uint64
		if fromBytes == 4 {
			max = math.MaxUint16
		} else {
			max = math.MaxUint8
		}

		if val < 0 {
			val = 0
		}
		if uint64(val) > max {
			val = int64(max)
		}

		if fromBytes == 4 {
			binary.LittleEndian.PutUint16(outLane, uint16(val))
		} else {
			outLane[0] = byte(val)
		}
	}
}

func saturateF64toInt32(f float64) int32 {
	switch {
	case math.IsNaN(f):
		return 0
	case f > math.MaxInt32:
		return math.MaxInt32
	case f < math.MinInt32:
		return math.MinInt32
	default:
		return int32(f)
	}
}

func saturateF64toUint32(f float64) uint32 {
	switch {
	case math.IsNaN(f):
		return 0
	case f > math.MaxUint32:
		return math.MaxUint32
	case f < 0:
		return 0
	default:
		return uint32(f)
	}
}

// allTrue checks if all lanes of a given size are non-zero.
func allTrue(v V128Value, laneSizeBytes int) bool {
	bytes := v.Bytes()
	for i := 0; i < 16; i += laneSizeBytes {
		switch laneSizeBytes {
		case 2:
			if binary.LittleEndian.Uint16(bytes[i:i+2]) == 0 {
				return false
			}
		case 4:
			if binary.LittleEndian.Uint32(bytes[i:i+4]) == 0 {
				return false
			}
		}
	}
	return true
}

// bitmask extracts the most significant bit from each lane.
func bitmask(v V128Value, laneSizeBytes int) int32 {
	var res int32
	buf := v.Bytes()
	numLanes := 16 / laneSizeBytes
	for i := range numLanes {
		// For little-endian, the MSB is in the last byte of the lane
		msbByte := buf[(i+1)*laneSizeBytes-1]
		if (msbByte & 0x80) != 0 {
			res |= (1 << i)
		}
	}
	return res
}

func boolToUint[T byte | uint16 | uint32 | uint64](b bool) T {
	if b {
		return ^T(0)
	}
	return 0
}

func boolToFloat32(b bool) float32 {
	if b {
		return math.Float32frombits(0xFFFFFFFF)
	}
	return 0
}

func boolToFloat64(b bool) float64 {
	if b {
		return math.Float64frombits(0xFFFFFFFFFFFFFFFF)
	}
	return 0
}
