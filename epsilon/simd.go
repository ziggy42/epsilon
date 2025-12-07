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
	"math/bits"
)

func GetBytes(v V128Value) []byte {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], v.Low)
	binary.LittleEndian.PutUint64(buf[8:16], v.High)
	return buf[:]
}

func SimdV128Load8x8S(data []byte) V128Value {
	p0 := uint64(uint16(int8(data[0])))
	p1 := uint64(uint16(int8(data[1])))
	p2 := uint64(uint16(int8(data[2])))
	p3 := uint64(uint16(int8(data[3])))
	low := p0 | p1<<16 | p2<<32 | p3<<48

	p4 := uint64(uint16(int8(data[4])))
	p5 := uint64(uint16(int8(data[5])))
	p6 := uint64(uint16(int8(data[6])))
	p7 := uint64(uint16(int8(data[7])))
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
	v0 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[0:2]))))
	v1 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[2:4]))))
	low := v0 | v1<<32

	v2 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[4:6]))))
	v3 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[6:8]))))
	high := v2 | v3<<32

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
	sources := [4]uint64{v1.Low, v1.High, v2.Low, v2.High}
	var low, high uint64
	for i := range 8 {
		lane := lanes[i]
		if lane < 32 {
			val := (sources[lane/8] >> ((lane & 7) * 8)) & 0xFF
			low |= val << (uint(i) * 8)
		}
	}

	for i := range 8 {
		lane := lanes[i+8]
		if lane < 32 {
			val := (sources[lane/8] >> ((lane & 7) * 8)) & 0xFF
			high |= val << (uint(i) * 8)
		}
	}

	return V128Value{Low: low, High: high}
}

// SimdI8x16Swizzle performs a byte swizzle operation.
func SimdI8x16Swizzle(v1, v2 V128Value) V128Value {
	sources := [2]uint64{v1.Low, v1.High}
	var low, high uint64
	for i := range 8 {
		index := (v2.Low >> (uint(i) * 8)) & 0xFF
		if index < 16 {
			val := (sources[index/8] >> ((index & 7) * 8)) & 0xFF
			low |= val << (uint(i) * 8)
		}
	}

	for i := range 8 {
		index := (v2.High >> (uint(i) * 8)) & 0xFF
		if index < 16 {
			val := (sources[index/8] >> ((index & 7) * 8)) & 0xFF
			high |= val << (uint(i) * 8)
		}
	}

	return V128Value{Low: low, High: high}
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
	v := uint64(uint32(val))
	low := v | (v << 32)
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
	v := uint64(math.Float32bits(val))
	low := v | (v << 32)
	return V128Value{Low: low, High: low}
}

func SimdF64x2Splat(val float64) V128Value {
	bits := math.Float64bits(val)
	return V128Value{Low: bits, High: bits}
}

// SimdI8x16ExtractLaneS extracts a signed 8-bit integer from the specified
// lane.
func SimdI8x16ExtractLaneS(v V128Value, laneIndex uint32) int32 {
	bytes := ExtractLane(v, 8, laneIndex)
	return int32(int8(bytes[0]))
}

// SimdI8x16ExtractLaneU extracts an unsigned 8-bit integer from the specified
// lane.
func SimdI8x16ExtractLaneU(v V128Value, laneIndex uint32) int32 {
	bytes := ExtractLane(v, 8, laneIndex)
	return int32(bytes[0])
}

func SimdI8x16ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	return SetLane(v, laneIndex, []byte{byte(laneValue)})
}

// SimdI16x8ExtractLaneS extracts a signed 16-bit integer from the specified
// lane.
func SimdI16x8ExtractLaneS(v V128Value, laneIndex uint32) int32 {
	bytes := ExtractLane(v, 16, laneIndex)
	return int32(int16(binary.LittleEndian.Uint16(bytes)))
}

// SimdI16x8ExtractLaneU extracts an unsigned 16-bit integer from the specified
// lane.
func SimdI16x8ExtractLaneU(v V128Value, laneIndex uint32) int32 {
	bytes := ExtractLane(v, 16, laneIndex)
	return int32(binary.LittleEndian.Uint16(bytes))
}

func SimdI16x8ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uint16(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdI32x4ExtractLane extracts a 32-bit integer from the specified lane.
func SimdI32x4ExtractLane(v V128Value, laneIndex uint32) int32 {
	bytes := ExtractLane(v, 32, laneIndex)
	return int32(binary.LittleEndian.Uint32(bytes))
}

func SimdI32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdI64x2ExtractLane extracts a 64-bit integer from the specified lane.
func SimdI64x2ExtractLane(v V128Value, laneIndex uint32) int64 {
	bytes := ExtractLane(v, 64, laneIndex)
	return int64(binary.LittleEndian.Uint64(bytes))
}

func SimdI64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int64,
) V128Value {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdF32x4ExtractLane extracts a 32-bit float from the specified lane.
func SimdF32x4ExtractLane(v V128Value, laneIndex uint32) float32 {
	bytes := ExtractLane(v, 32, laneIndex)
	return math.Float32frombits(binary.LittleEndian.Uint32(bytes))
}

func SimdF32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float32,
) V128Value {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, math.Float32bits(laneValue))
	return SetLane(v, laneIndex, buf)
}

// SimdF64x2ExtractLane extracts a 64-bit float from the specified lane.
func SimdF64x2ExtractLane(v V128Value, laneIndex uint32) float64 {
	bytes := ExtractLane(v, 64, laneIndex)
	return math.Float64frombits(binary.LittleEndian.Uint64(bytes))
}

func SimdF64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float64,
) V128Value {
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
	return V128Value{
		Low:  narrow32x4To16x8(v1, saturateS32ToS16),
		High: narrow32x4To16x8(v2, saturateS32ToS16),
	}
}

func SimdI16x8NarrowI32x4U(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow32x4To16x8(v1, saturateS32ToU16),
		High: narrow32x4To16x8(v2, saturateS32ToU16),
	}
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

func SetLane(v V128Value, laneIndex uint32, value []byte) V128Value {
	width := uint(len(value) * 8)
	var val uint64
	switch width {
	case 8:
		val = uint64(value[0])
	case 16:
		val = uint64(binary.LittleEndian.Uint16(value))
	case 32:
		val = uint64(binary.LittleEndian.Uint32(value))
	case 64:
		val = binary.LittleEndian.Uint64(value)
	}

	target := &v.Low
	shift := uint(laneIndex) * width
	if shift >= 64 {
		target = &v.High
		shift -= 64
	}

	mask := ^((uint64(1)<<width - 1) << shift)
	*target = (*target & mask) | (val << shift)
	return v
}

func ExtractLane(value V128Value, laneSize, laneIndex uint32) []byte {
	shift := laneIndex * laneSize
	source := value.Low
	if shift >= 64 {
		source = value.High
		shift -= 64
	}

	val := source >> shift

	bytes := make([]byte, laneSize/8)
	switch laneSize {
	case 8:
		bytes[0] = byte(val)
	case 16:
		binary.LittleEndian.PutUint16(bytes, uint16(val))
	case 32:
		binary.LittleEndian.PutUint32(bytes, uint32(val))
	case 64:
		binary.LittleEndian.PutUint64(bytes, val)
	}
	return bytes
}

func SimdF32x4DemoteF64x2Zero(v V128Value) V128Value {
	f64Low := math.Float64frombits(v.Low)
	f64High := math.Float64frombits(v.High)

	f32Low := float32(f64Low)
	if math.IsNaN(float64(f32Low)) {
		f32Low = float32(math.NaN())
	}

	f32High := float32(f64High)
	if math.IsNaN(float64(f32High)) {
		f32High = float32(math.NaN())
	}

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

	low := float64(f32Low)
	if math.IsNaN(low) {
		low = math.NaN()
	}

	high := float64(f32High)
	if math.IsNaN(high) {
		high = math.NaN()
	}

	return V128Value{
		Low:  math.Float64bits(low),
		High: math.Float64bits(high),
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
	return V128Value{
		Low:  narrow16x8To8x16(v1, saturateS16ToS8),
		High: narrow16x8To8x16(v2, saturateS16ToS8),
	}
}

func SimdI8x16NarrowI16x8U(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow16x8To8x16(v1, saturateS16ToU8),
		High: narrow16x8To8x16(v2, saturateS16ToU8),
	}
}

// SimdI8x16AllTrue returns true if all 8-bit lanes of a V128Value are non-zero.
func SimdI8x16AllTrue(v V128Value) bool {
	// https://graphics.stanford.edu/~seander/bithacks.html#ZeroInWord
	mask64bit := uint64(0x7F7F7F7F7F7F7F7F)
	hasLowZero := ^((((v.Low & mask64bit) + mask64bit) | v.Low) | mask64bit)
	hasHighZero := ^((((v.High & mask64bit) + mask64bit) | v.High) | mask64bit)
	return hasLowZero == 0 && hasHighZero == 0
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
		return byte(int8(b) >> s)
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
	return extmul(v1, v2, 1, false, true)
}

func SimdI16x8ExtmulHighI8x16S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, true, true)
}

func SimdI16x8ExtmulLowI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, false, false)
}

func SimdI16x8ExtmulHighI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, true, false)
}

func SimdI32x4ExtmulLowI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, false, true)
}

func SimdI32x4ExtmulHighI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, true, true)
}

func SimdI32x4ExtmulLowI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, false, false)
}

func SimdI32x4ExtmulHighI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, true, false)
}

func SimdI64x2ExtmulLowI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, false, true)
}

func SimdI64x2ExtmulHighI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, true, true)
}

func SimdI64x2ExtmulLowI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, false, false)
}

func SimdI64x2ExtmulHighI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, true, false)
}

func SimdI16x8ExtaddPairwiseI8x16S(v V128Value) V128Value {
	l0 := uint64(uint16(int16(int8(v.Low)) + int16(int8(v.Low>>8))))
	l1 := uint64(uint16(int16(int8(v.Low>>16)) + int16(int8(v.Low>>24))))
	l2 := uint64(uint16(int16(int8(v.Low>>32)) + int16(int8(v.Low>>40))))
	l3 := uint64(uint16(int16(int8(v.Low>>48)) + int16(int8(v.Low>>56))))
	low := l0 | l1<<16 | l2<<32 | l3<<48

	h0 := uint64(uint16(int16(int8(v.High)) + int16(int8(v.High>>8))))
	h1 := uint64(uint16(int16(int8(v.High>>16)) + int16(int8(v.High>>24))))
	h2 := uint64(uint16(int16(int8(v.High>>32)) + int16(int8(v.High>>40))))
	h3 := uint64(uint16(int16(int8(v.High>>48)) + int16(int8(v.High>>56))))
	high := h0 | h1<<16 | h2<<32 | h3<<48

	return V128Value{Low: low, High: high}
}

func SimdI16x8ExtaddPairwiseI8x16U(v V128Value) V128Value {
	l0 := uint64(uint16(byte(v.Low)) + uint16(byte(v.Low>>8)))
	l1 := uint64(uint16(byte(v.Low>>16)) + uint16(byte(v.Low>>24)))
	l2 := uint64(uint16(byte(v.Low>>32)) + uint16(byte(v.Low>>40)))
	l3 := uint64(uint16(byte(v.Low>>48)) + uint16(byte(v.Low>>56)))
	low := l0 | l1<<16 | l2<<32 | l3<<48

	h0 := uint64(uint16(byte(v.High)) + uint16(byte(v.High>>8)))
	h1 := uint64(uint16(byte(v.High>>16)) + uint16(byte(v.High>>24)))
	h2 := uint64(uint16(byte(v.High>>32)) + uint16(byte(v.High>>40)))
	h3 := uint64(uint16(byte(v.High>>48)) + uint16(byte(v.High>>56)))
	high := h0 | h1<<16 | h2<<32 | h3<<48

	return V128Value{Low: low, High: high}
}

func SimdI32x4ExtaddPairwiseI16x8U(v V128Value) V128Value {
	l0 := uint64(uint32(uint16(v.Low)) + uint32(uint16(v.Low>>16)))
	l1 := uint64(uint32(uint16(v.Low>>32)) + uint32(uint16(v.Low>>48)))
	low := l0 | l1<<32

	h0 := uint64(uint32(uint16(v.High)) + uint32(uint16(v.High>>16)))
	h1 := uint64(uint32(uint16(v.High>>32)) + uint32(uint16(v.High>>48)))
	high := h0 | h1<<32

	return V128Value{Low: low, High: high}
}

func SimdI32x4ExtaddPairwiseI16x8S(v V128Value) V128Value {
	l0 := uint64(uint32(int32(int16(v.Low)) + int32(int16(v.Low>>16))))
	l1 := uint64(uint32(int32(int16(v.Low>>32)) + int32(int16(v.Low>>48))))
	low := l0 | l1<<32

	h0 := uint64(uint32(int32(int16(v.High)) + int32(int16(v.High>>16))))
	h1 := uint64(uint32(int32(int16(v.High>>32)) + int32(int16(v.High>>48))))
	high := h0 | h1<<32

	return V128Value{Low: low, High: high}
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
	l0 := int32(int16(v1.Low)) * int32(int16(v2.Low))
	l1 := int32(int16(v1.Low>>16)) * int32(int16(v2.Low>>16))
	l2 := int32(int16(v1.Low>>32)) * int32(int16(v2.Low>>32))
	l3 := int32(int16(v1.Low>>48)) * int32(int16(v2.Low>>48))
	low := uint64(uint32(l0+l1)) | uint64(uint32(l2+l3))<<32

	h0 := int32(int16(v1.High)) * int32(int16(v2.High))
	h1 := int32(int16(v1.High>>16)) * int32(int16(v2.High>>16))
	h2 := int32(int16(v1.High>>32)) * int32(int16(v2.High>>32))
	h3 := int32(int16(v1.High>>48)) * int32(int16(v2.High>>48))
	high := uint64(uint32(h0+h1)) | uint64(uint32(h2+h3))<<32

	return V128Value{Low: low, High: high}
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
		return min(a, b)
	})
}

func SimdF32x4Max(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
			return float32(math.NaN())
		}
		return max(a, b)
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
		return min(a, b)
	})
}

func SimdF64x2Max(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if math.IsNaN(a) || math.IsNaN(b) {
			return math.NaN()
		}
		return max(a, b)
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
	i0 := saturateF32toInt32(math.Float32frombits(uint32(v.Low)))
	i1 := saturateF32toInt32(math.Float32frombits(uint32(v.Low >> 32)))
	low := uint64(uint32(i0)) | (uint64(uint32(i1)) << 32)

	i2 := saturateF32toInt32(math.Float32frombits(uint32(v.High)))
	i3 := saturateF32toInt32(math.Float32frombits(uint32(v.High >> 32)))
	high := uint64(uint32(i2)) | (uint64(uint32(i3)) << 32)

	return V128Value{Low: low, High: high}
}

func SimdI32x4TruncSatF32x4U(v V128Value) V128Value {
	u0 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.Low))))
	u1 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.Low >> 32))))
	low := u0 | (u1 << 32)

	u2 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.High))))
	u3 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.High >> 32))))
	high := u2 | (u3 << 32)

	return V128Value{Low: low, High: high}
}

func SimdI32x4TruncSatF64x2SZero(v V128Value) V128Value {
	lowLowHalf := saturateF64toInt32(math.Float64frombits(v.Low))
	lowHighHalf := saturateF64toInt32(math.Float64frombits(v.High))
	return V128Value{
		Low:  uint64(uint32(lowLowHalf)) | (uint64(uint32(lowHighHalf)) << 32),
		High: 0,
	}
}

func SimdI32x4TruncSatF64x2UZero(v V128Value) V128Value {
	lowLowHalf := saturateF64toUint32(math.Float64frombits(v.Low))
	lowHighHalf := saturateF64toUint32(math.Float64frombits(v.High))
	return V128Value{
		Low:  uint64(lowLowHalf) | (uint64(lowHighHalf) << 32),
		High: 0,
	}
}

func SimdF32x4ConvertI32x4S(v V128Value) V128Value {
	f0 := float32(int32(v.Low))
	f1 := float32(int32(v.Low >> 32))
	low := uint64(math.Float32bits(f0)) | (uint64(math.Float32bits(f1)) << 32)

	f2 := float32(int32(v.High))
	f3 := float32(int32(v.High >> 32))
	high := uint64(math.Float32bits(f2)) | (uint64(math.Float32bits(f3)) << 32)

	return V128Value{Low: low, High: high}
}

func SimdF32x4ConvertI32x4U(v V128Value) V128Value {
	f0 := float32(uint32(v.Low))
	f1 := float32(uint32(v.Low >> 32))
	low := uint64(math.Float32bits(f0)) | (uint64(math.Float32bits(f1)) << 32)

	f2 := float32(uint32(v.High))
	f3 := float32(uint32(v.High >> 32))
	high := uint64(math.Float32bits(f2)) | (uint64(math.Float32bits(f3)) << 32)

	return V128Value{Low: low, High: high}
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
	return V128Value{
		Low:  math.Float64bits(float64(int32(v.Low))),
		High: math.Float64bits(float64(int32(v.Low >> 32))),
	}
}

func SimdF64x2ConvertLowI32x4U(v V128Value) V128Value {
	return V128Value{
		Low:  math.Float64bits(float64(uint32(v.Low))),
		High: math.Float64bits(float64(uint32(v.Low >> 32))),
	}
}

func unaryOpI8x16(v V128Value, op func(byte) byte) V128Value {
	var low, high uint64
	for i := range 8 {
		shift := i * 8
		r := op(byte(v.Low >> shift))
		low |= uint64(r) << shift
	}
	for i := range 8 {
		shift := i * 8
		r := op(byte(v.High >> shift))
		high |= uint64(r) << shift
	}
	return V128Value{Low: low, High: high}
}

func unaryOpI16x8(v V128Value, op func(uint16) uint16) V128Value {
	var low, high uint64
	for i := range 4 {
		shift := i * 16
		r := op(uint16(v.Low >> shift))
		low |= uint64(r) << shift
	}
	for i := range 4 {
		shift := i * 16
		r := op(uint16(v.High >> shift))
		high |= uint64(r) << shift
	}
	return V128Value{Low: low, High: high}
}

func unaryOpI32x4(v V128Value, op func(uint32) uint32) V128Value {
	r0 := uint64(op(uint32(v.Low)))
	r1 := uint64(op(uint32(v.Low >> 32)))
	low := r0 | (r1 << 32)

	r2 := uint64(op(uint32(v.High)))
	r3 := uint64(op(uint32(v.High >> 32)))
	high := r2 | (r3 << 32)

	return V128Value{Low: low, High: high}
}

func binaryOpI32x4(v1, v2 V128Value, op func(int32, int32) int32) V128Value {
	r0 := uint64(uint32(op(int32(v1.Low), int32(v2.Low))))
	r1 := uint64(uint32(op(int32(v1.Low>>32), int32(v2.Low>>32))))
	low := r0 | (r1 << 32)

	r2 := uint64(uint32(op(int32(v1.High), int32(v2.High))))
	r3 := uint64(uint32(op(int32(v1.High>>32), int32(v2.High>>32))))
	high := r2 | (r3 << 32)

	return V128Value{Low: low, High: high}
}

func binaryOpUI32x4(
	v1, v2 V128Value,
	op func(uint32, uint32) uint32,
) V128Value {
	r0 := uint64(op(uint32(v1.Low), uint32(v2.Low)))
	r1 := uint64(op(uint32(v1.Low>>32), uint32(v2.Low>>32)))
	low := r0 | (r1 << 32)

	r2 := uint64(op(uint32(v1.High), uint32(v2.High)))
	r3 := uint64(op(uint32(v1.High>>32), uint32(v2.High>>32)))
	high := r2 | (r3 << 32)

	return V128Value{Low: low, High: high}
}

func binaryOpI8x16(v1, v2 V128Value, op func(int8, int8) int8) V128Value {
	var low, high uint64
	for i := range 8 {
		shift := i * 8
		r := op(int8(v1.Low>>shift), int8(v2.Low>>shift))
		low |= uint64(uint8(r)) << shift
	}
	for i := range 8 {
		shift := i * 8
		r := op(int8(v1.High>>shift), int8(v2.High>>shift))
		high |= uint64(uint8(r)) << shift
	}
	return V128Value{Low: low, High: high}
}

func binaryOpUI8x16(v1, v2 V128Value, op func(byte, byte) byte) V128Value {
	var low, high uint64
	for i := range 8 {
		shift := i * 8
		r := op(byte(v1.Low>>shift), byte(v2.Low>>shift))
		low |= uint64(r) << shift
	}
	for i := range 8 {
		shift := i * 8
		r := op(byte(v1.High>>shift), byte(v2.High>>shift))
		high |= uint64(r) << shift
	}
	return V128Value{Low: low, High: high}
}

func binaryOpI16x8(v1, v2 V128Value, op func(int16, int16) int16) V128Value {
	var low, high uint64
	for i := range 4 {
		shift := i * 16
		r := op(int16(v1.Low>>shift), int16(v2.Low>>shift))
		low |= uint64(uint16(r)) << shift
	}
	for i := range 4 {
		shift := i * 16
		r := op(int16(v1.High>>shift), int16(v2.High>>shift))
		high |= uint64(uint16(r)) << shift
	}
	return V128Value{Low: low, High: high}
}

func binaryOpUI16x8(v1, v2 V128Value, op func(uint16, uint16) uint16) V128Value {
	var low, high uint64
	for i := range 4 {
		shift := i * 16
		r := op(uint16(v1.Low>>shift), uint16(v2.Low>>shift))
		low |= uint64(r) << shift
	}
	for i := range 4 {
		shift := i * 16
		r := op(uint16(v1.High>>shift), uint16(v2.High>>shift))
		high |= uint64(r) << shift
	}
	return V128Value{Low: low, High: high}
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
	f1_0 := math.Float32frombits(uint32(v1.Low))
	f1_1 := math.Float32frombits(uint32(v1.Low >> 32))
	f2_0 := math.Float32frombits(uint32(v2.Low))
	f2_1 := math.Float32frombits(uint32(v2.Low >> 32))

	r0 := uint64(math.Float32bits(op(f1_0, f2_0)))
	r1 := uint64(math.Float32bits(op(f1_1, f2_1)))
	low := r0 | (r1 << 32)

	f1_2 := math.Float32frombits(uint32(v1.High))
	f1_3 := math.Float32frombits(uint32(v1.High >> 32))
	f2_2 := math.Float32frombits(uint32(v2.High))
	f2_3 := math.Float32frombits(uint32(v2.High >> 32))

	r2 := uint64(math.Float32bits(op(f1_2, f2_2)))
	r3 := uint64(math.Float32bits(op(f1_3, f2_3)))
	high := r2 | (r3 << 32)

	return V128Value{Low: low, High: high}
}

func unaryOpF32x4(v V128Value, op func(float32) float32) V128Value {
	f0 := math.Float32frombits(uint32(v.Low))
	f1 := math.Float32frombits(uint32(v.Low >> 32))
	r0 := math.Float32bits(op(f0))
	r1 := math.Float32bits(op(f1))
	low := uint64(r0) | (uint64(r1) << 32)

	f2 := math.Float32frombits(uint32(v.High))
	f3 := math.Float32frombits(uint32(v.High >> 32))
	r2 := math.Float32bits(op(f2))
	r3 := math.Float32bits(op(f3))
	high := uint64(r2) | (uint64(r3) << 32)

	return V128Value{Low: low, High: high}
}

func unaryOpF64x2(v V128Value, op func(float64) float64) V128Value {
	return V128Value{
		Low:  math.Float64bits(op(math.Float64frombits(v.Low))),
		High: math.Float64bits(op(math.Float64frombits(v.High))),
	}
}

func extend(v V128Value, fromBytes, toBytes int, high, signed bool) V128Value {
	var src uint64
	if high {
		src = v.High
	} else {
		src = v.Low
	}

	var resLow, resHigh uint64
	numLanes := 8 / fromBytes
	halfLanes := numLanes / 2

	getLane := func(idx int) uint64 {
		shift := uint(idx * fromBytes * 8)
		if signed {
			switch fromBytes {
			case 1:
				return uint64(int64(int8(src >> shift)))
			case 2:
				return uint64(int64(int16(src >> shift)))
			case 4:
				return uint64(int64(int32(src >> shift)))
			}
		} else {
			mask := uint64(1<<(fromBytes*8)) - 1
			return (src >> shift) & mask
		}
		return 0
	}

	for i := range halfLanes {
		val := getLane(i)
		shift := uint(i * toBytes * 8)
		if toBytes == 8 {
			resLow = val
		} else {
			mask := uint64(1<<(toBytes*8)) - 1
			resLow |= (val & mask) << shift
		}
	}

	for i := range halfLanes {
		val := getLane(i + halfLanes)
		shift := uint(i * toBytes * 8)
		if toBytes == 8 {
			resHigh = val
		} else {
			mask := uint64(1<<(toBytes*8)) - 1
			resHigh |= (val & mask) << shift
		}
	}

	return V128Value{Low: resLow, High: resHigh}
}

func extmul(v1, v2 V128Value, fromBytes int, high, signed bool) V128Value {
	var half1, half2 uint64
	if high {
		half1, half2 = v1.High, v2.High
	} else {
		half1, half2 = v1.Low, v2.Low
	}

	var resLow, resHigh uint64
	numLanes := 8 / fromBytes
	halfLanes := numLanes / 2

	getProduct := func(idx int) uint64 {
		shift := uint(idx * fromBytes * 8)
		if signed {
			switch fromBytes {
			case 1:
				return uint64(int64(int8(half1>>shift)) * int64(int8(half2>>shift)))
			case 2:
				return uint64(int64(int16(half1>>shift)) * int64(int16(half2>>shift)))
			case 4:
				return uint64(int64(int32(half1>>shift)) * int64(int32(half2>>shift)))
			}
		} else {
			mask := uint64(1<<(fromBytes*8)) - 1
			v1 := (half1 >> shift) & mask
			v2 := (half2 >> shift) & mask
			return v1 * v2
		}
		return 0
	}

	for i := range halfLanes {
		prod := getProduct(i)
		shift := uint(i * (fromBytes * 2) * 8)
		if fromBytes == 4 {
			resLow = prod
		} else {
			mask := uint64(1<<(fromBytes*2*8)) - 1
			resLow |= (prod & mask) << shift
		}
	}

	for j := range halfLanes {
		i := j + halfLanes
		prod := getProduct(i)
		shift := uint(j * (fromBytes * 2) * 8)
		if fromBytes == 4 {
			resHigh = prod
		} else {
			mask := uint64(1<<(fromBytes*2*8)) - 1
			resHigh |= (prod & mask) << shift
		}
	}

	return V128Value{Low: resLow, High: resHigh}
}

// narrow32x4To16x8 takes a single V128 (treated as 4x32-bit lanes), saturates
// them, and packs them into a uint64 (4x16-bit lanes).
func narrow32x4To16x8(v V128Value, saturate func(int32) uint64) uint64 {
	r0 := saturate(int32(v.Low))
	r1 := saturate(int32(v.Low >> 32))
	r2 := saturate(int32(v.High))
	r3 := saturate(int32(v.High >> 32))
	return r0 | (r1 << 16) | (r2 << 32) | (r3 << 48)
}

// narrow16x8To8x16 takes a single V128 (treated as 8x16-bit lanes), saturates
// them, and packs them into a uint64 (8x8-bit lanes).
func narrow16x8To8x16(v V128Value, saturate func(int16) uint64) uint64 {
	r0 := saturate(int16(v.Low))
	r1 := saturate(int16(v.Low >> 16))
	r2 := saturate(int16(v.Low >> 32))
	r3 := saturate(int16(v.Low >> 48))
	r4 := saturate(int16(v.High))
	r5 := saturate(int16(v.High >> 16))
	r6 := saturate(int16(v.High >> 32))
	r7 := saturate(int16(v.High >> 48))
	return r0 | (r1 << 8) | (r2 << 16) | (r3 << 24) | (r4 << 32) | (r5 << 40) |
		(r6 << 48) | (r7 << 56)
}

// saturateS32ToS16: Signed 32-bit -> Signed 16-bit
func saturateS32ToS16(v int32) uint64 {
	if v < math.MinInt16 {
		return 0x8000
	}
	if v > math.MaxInt16 {
		return 0x7FFF
	}
	return uint64(uint16(int16(v)))
}

// saturateS32ToU16: Signed 32-bit -> Unsigned 16-bit
func saturateS32ToU16(v int32) uint64 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint64(uint16(v))
}

// saturateS16ToS8: Signed 16-bit -> Signed 8-bit
func saturateS16ToS8(v int16) uint64 {
	if v < math.MinInt8 {
		return 0x80
	}
	if v > math.MaxInt8 {
		return 0x7F
	}
	return uint64(uint8(int8(v)))
}

// saturateS16ToU8: Signed 16-bit -> Unsigned 8-bit
func saturateS16ToU8(v int16) uint64 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint8 {
		return math.MaxUint8
	}
	return uint64(uint8(v))
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

func saturateF32toInt32(f float32) int32 {
	if math.IsNaN(float64(f)) {
		return 0
	}
	if f > math.MaxInt32 {
		return math.MaxInt32
	}
	if f < math.MinInt32 {
		return math.MinInt32
	}
	return int32(f)
}

func saturateF32toUint32(f float32) uint32 {
	f64 := float64(f)
	if math.IsNaN(f64) {
		return 0
	}
	if f64 > float64(math.MaxUint32) {
		return math.MaxUint32
	}
	if f64 < 0 {
		return 0
	}
	return uint32(f64)
}

// allTrue checks if all lanes of a given size are non-zero.
func allTrue(v V128Value, laneSizeBytes int) bool {
	laneBits := laneSizeBytes * 8
	mask := uint64(1<<laneBits) - 1
	halfLanes := 8 / laneSizeBytes

	for i := range halfLanes {
		if (v.Low>>(i*laneBits))&mask == 0 {
			return false
		}
	}

	for i := range halfLanes {
		if (v.High>>(i*laneBits))&mask == 0 {
			return false
		}
	}
	return true
}

// bitmask extracts the most significant bit from each lane.
func bitmask(v V128Value, laneSizeBytes int) int32 {
	var res int32
	laneBits := laneSizeBytes * 8
	msbOffset := laneBits - 1
	halfLanes := 8 / laneSizeBytes

	for i := range halfLanes {
		if (v.Low>>((i*laneBits)+msbOffset))&1 != 0 {
			res |= 1 << i
		}
	}

	for i := range halfLanes {
		if (v.High>>((i*laneBits)+msbOffset))&1 != 0 {
			res |= 1 << (i + halfLanes)
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

func identityV128(v V128Value) V128Value { return v }
