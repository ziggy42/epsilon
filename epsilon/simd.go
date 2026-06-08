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

// Per-lane bit masks for the SWAR (SIMD-within-a-register) helpers at the
// bottom of this file, which treat a uint64 as a vector of 8-, 16- or 32-bit
// lanes. The *MSB masks set each lane's high bit; the *Ones masks set each
// lane's low bit, so multiplying a single-lane value by one of them broadcasts
// it to every lane. Helpers that build a full-lane mask also take laneShift
// (laneBits-1) and a per-lane all-ones fill (0xFF, 0xFFFF or 0xFFFFFFFF).
const (
	lanes8MSB  = 0x8080808080808080
	lanes16MSB = 0x8000800080008000
	lanes32MSB = 0x8000000080000000

	lanes8Ones  = 0x0101010101010101
	lanes16Ones = 0x0001000100010001
	lanes32Ones = 0x0000000100000001
)

func simdV128Load8x8S(data []byte) V128Value {
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

func simdV128Load8x8U(data []byte) V128Value {
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

func simdV128Load16x4S(data []byte) V128Value {
	v0 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[0:2]))))
	v1 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[2:4]))))
	low := v0 | v1<<32

	v2 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[4:6]))))
	v3 := uint64(uint32(int16(binary.LittleEndian.Uint16(data[6:8]))))
	high := v2 | v3<<32

	return V128Value{Low: low, High: high}
}

func simdV128Load16x4U(data []byte) V128Value {
	v0 := uint64(binary.LittleEndian.Uint16(data[0:2]))
	v1 := uint64(binary.LittleEndian.Uint16(data[2:4]))
	low := v0 | v1<<32

	v2 := uint64(binary.LittleEndian.Uint16(data[4:6]))
	v3 := uint64(binary.LittleEndian.Uint16(data[6:8]))
	high := v2 | v3<<32

	return V128Value{Low: low, High: high}
}

func simdV128Load32x2S(data []byte) V128Value {
	low := uint64(int32(binary.LittleEndian.Uint32(data[0:4])))
	high := uint64(int32(binary.LittleEndian.Uint32(data[4:8])))
	return V128Value{Low: low, High: high}
}

func simdV128Load32x2U(data []byte) V128Value {
	low := uint64(binary.LittleEndian.Uint32(data[0:4]))
	high := uint64(binary.LittleEndian.Uint32(data[4:8]))
	return V128Value{Low: low, High: high}
}

// simdI8x16Shuffle performs a byte shuffle operation.
func simdI8x16Shuffle(
	v1, v2 V128Value,
	l0, l1, l2, l3, l4, l5, l6, l7, l8, l9, l10, l11, l12, l13, l14, l15 byte,
) V128Value {
	sources := [4]uint64{v1.Low, v1.High, v2.Low, v2.High}

	low := (sources[l0>>3] >> ((l0 & 7) << 3)) & 0xFF
	low |= ((sources[l1>>3] >> ((l1 & 7) << 3)) & 0xFF) << 8
	low |= ((sources[l2>>3] >> ((l2 & 7) << 3)) & 0xFF) << 16
	low |= ((sources[l3>>3] >> ((l3 & 7) << 3)) & 0xFF) << 24
	low |= ((sources[l4>>3] >> ((l4 & 7) << 3)) & 0xFF) << 32
	low |= ((sources[l5>>3] >> ((l5 & 7) << 3)) & 0xFF) << 40
	low |= ((sources[l6>>3] >> ((l6 & 7) << 3)) & 0xFF) << 48
	low |= ((sources[l7>>3] >> ((l7 & 7) << 3)) & 0xFF) << 56

	high := (sources[l8>>3] >> ((l8 & 7) << 3)) & 0xFF
	high |= ((sources[l9>>3] >> ((l9 & 7) << 3)) & 0xFF) << 8
	high |= ((sources[l10>>3] >> ((l10 & 7) << 3)) & 0xFF) << 16
	high |= ((sources[l11>>3] >> ((l11 & 7) << 3)) & 0xFF) << 24
	high |= ((sources[l12>>3] >> ((l12 & 7) << 3)) & 0xFF) << 32
	high |= ((sources[l13>>3] >> ((l13 & 7) << 3)) & 0xFF) << 40
	high |= ((sources[l14>>3] >> ((l14 & 7) << 3)) & 0xFF) << 48
	high |= ((sources[l15>>3] >> ((l15 & 7) << 3)) & 0xFF) << 56

	return V128Value{Low: low, High: high}
}

// simdI8x16Swizzle performs a byte swizzle operation.
func simdI8x16Swizzle(v1, v2 V128Value) V128Value {
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

func simdI8x16Splat(val int32) V128Value {
	v8 := uint64(byte(val))
	v16 := v8 | (v8 << 8)
	v32 := v16 | (v16 << 16)
	v64 := v32 | (v32 << 32)
	return V128Value{Low: v64, High: v64}
}

func simdI8x16SplatFromBytes(data []byte) V128Value {
	return simdI8x16Splat(int32(data[0]))
}

func simdI16x8Splat(val int32) V128Value {
	v16 := uint64(uint16(val))
	v32 := v16 | (v16 << 16)
	v64 := v32 | (v32 << 32)
	return V128Value{Low: v64, High: v64}
}

func simdI16x8SplatFromBytes(data []byte) V128Value {
	return simdI16x8Splat(int32(binary.LittleEndian.Uint16(data)))
}

func simdI32x4Splat(val int32) V128Value {
	v := uint64(uint32(val))
	low := v | (v << 32)
	return V128Value{Low: low, High: low}
}

func simdI32x4SplatFromBytes(data []byte) V128Value {
	return simdI32x4Splat(int32(binary.LittleEndian.Uint32(data)))
}

func simdI64x2Splat(val int64) V128Value {
	v := uint64(val)
	return V128Value{Low: v, High: v}
}

func simdI64x2SplatFromBytes(data []byte) V128Value {
	return simdI64x2Splat(int64(binary.LittleEndian.Uint64(data)))
}

func simdF32x4Splat(val float32) V128Value {
	v := uint64(math.Float32bits(val))
	low := v | (v << 32)
	return V128Value{Low: low, High: low}
}

func simdF64x2Splat(val float64) V128Value {
	bits := math.Float64bits(val)
	return V128Value{Low: bits, High: bits}
}

// simdI8x16ExtractLaneS extracts a signed 8-bit integer from the specified
// lane.
func simdI8x16ExtractLaneS(v V128Value, laneIndex uint32) int32 {
	if laneIndex < 8 {
		return int32(int8(v.Low >> (laneIndex * 8)))
	}
	return int32(int8(v.High >> ((laneIndex - 8) * 8)))
}

// simdI8x16ExtractLaneU extracts an unsigned 8-bit integer from the specified
// lane.
func simdI8x16ExtractLaneU(v V128Value, laneIndex uint32) int32 {
	if laneIndex < 8 {
		return int32(uint8(v.Low >> (laneIndex * 8)))
	}
	return int32(uint8(v.High >> ((laneIndex - 8) * 8)))
}

func simdI8x16ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	// We use the modulo operator so shift is valid for both v.Low and v.High.
	shift := (laneIndex % 8) * 8

	val := uint64(byte(laneValue)) << shift
	mask := uint64(0xFF) << shift

	if laneIndex < 8 {
		v.Low = (v.Low &^ mask) | val
	} else {
		v.High = (v.High &^ mask) | val
	}

	return v
}

// simdI16x8ExtractLaneS extracts a signed 16-bit integer from the specified
// lane.
func simdI16x8ExtractLaneS(v V128Value, laneIndex uint32) int32 {
	if laneIndex < 4 {
		return int32(int16(v.Low >> (laneIndex * 16)))
	}
	return int32(int16(v.High >> ((laneIndex - 4) * 16)))
}

// simdI16x8ExtractLaneU extracts an unsigned 16-bit integer from the specified
// lane.
func simdI16x8ExtractLaneU(v V128Value, laneIndex uint32) int32 {
	if laneIndex < 4 {
		return int32(uint16(v.Low >> (laneIndex * 16)))
	}
	return int32(uint16(v.High >> ((laneIndex - 4) * 16)))
}

func simdI16x8ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	shift := (uint(laneIndex) % 4) * 16

	val := uint64(uint16(laneValue)) << shift
	mask := uint64(0xFFFF) << shift

	if laneIndex < 4 {
		v.Low = (v.Low &^ mask) | val
	} else {
		v.High = (v.High &^ mask) | val
	}

	return v
}

// simdI32x4ExtractLane extracts a 32-bit integer from the specified lane.
func simdI32x4ExtractLane(v V128Value, laneIndex uint32) int32 {
	if laneIndex < 2 {
		return int32(uint32(v.Low >> (laneIndex * 32)))
	}
	return int32(uint32(v.High >> ((laneIndex - 2) * 32)))
}

func simdI32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int32,
) V128Value {
	shift := (uint(laneIndex) % 2) * 32

	val := uint64(uint32(laneValue)) << shift
	mask := uint64(0xFFFFFFFF) << shift

	if laneIndex < 2 {
		v.Low = (v.Low &^ mask) | val
	} else {
		v.High = (v.High &^ mask) | val
	}

	return v
}

// simdI64x2ExtractLane extracts a 64-bit integer from the specified lane.
func simdI64x2ExtractLane(v V128Value, laneIndex uint32) int64 {
	if laneIndex == 0 {
		return int64(v.Low)
	}
	return int64(v.High)
}

func simdI64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue int64,
) V128Value {
	if laneIndex == 0 {
		v.Low = uint64(laneValue)
	} else {
		v.High = uint64(laneValue)
	}
	return v
}

// simdF32x4ExtractLane extracts a 32-bit float from the specified lane.
func simdF32x4ExtractLane(v V128Value, laneIndex uint32) float32 {
	if laneIndex < 2 {
		return math.Float32frombits(uint32(v.Low >> (laneIndex * 32)))
	}
	return math.Float32frombits(uint32(v.High >> ((laneIndex - 2) * 32)))
}

func simdF32x4ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float32,
) V128Value {
	// We use the modulo operator so shift is valid for both v.Low and v.High.
	shift := (laneIndex % 2) * 32

	val := uint64(math.Float32bits(laneValue)) << shift
	mask := uint64(0xFFFFFFFF) << shift

	if laneIndex < 2 {
		v.Low = (v.Low &^ mask) | val
	} else {
		v.High = (v.High &^ mask) | val
	}

	return v
}

// simdF64x2ExtractLane extracts a 64-bit float from the specified lane.
func simdF64x2ExtractLane(v V128Value, laneIndex uint32) float64 {
	if laneIndex == 0 {
		return math.Float64frombits(v.Low)
	}
	return math.Float64frombits(v.High)
}

func simdF64x2ReplaceLane(
	v V128Value,
	laneIndex uint32,
	laneValue float64,
) V128Value {
	if laneIndex == 0 {
		v.Low = math.Float64bits(laneValue)
	} else {
		v.High = math.Float64bits(laneValue)
	}
	return v
}

// simdI8x16Eq performs an equality comparison on each 8-bit lane of two
// V128Value.
func simdI8x16Eq(v1, v2 V128Value) V128Value {
	return eqLanes(v1, v2, lanes8MSB, 7, 0xFF)
}

// simdI8x16Ne performs an inequality comparison on each 8-bit lane of two
// V128Value.
func simdI8x16Ne(v1, v2 V128Value) V128Value {
	return simdV128Not(eqLanes(v1, v2, lanes8MSB, 7, 0xFF))
}

// simdI8x16LtS performs a signed less-than comparison on each 8-bit lane.
func simdI8x16LtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v1, v2, lanes8MSB, 7, 0xFF)
}

// simdI8x16LtU performs an unsigned less-than comparison on each 8-bit lane.
func simdI8x16LtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v1, v2, lanes8MSB, 7, 0xFF)
}

// simdI8x16GtS performs a signed greater-than comparison on each 8-bit lane.
func simdI8x16GtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v2, v1, lanes8MSB, 7, 0xFF)
}

// simdI8x16GtU performs an unsigned greater-than comparison on each 8-bit lane.
func simdI8x16GtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v2, v1, lanes8MSB, 7, 0xFF)
}

// simdI8x16LeS performs a signed less-than-or-equal comparison on each 8-bit
// lane.
func simdI8x16LeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v2, v1, lanes8MSB, 7, 0xFF))
}

// simdI8x16LeU performs an unsigned less-than-or-equal comparison on each 8-bit
// lane.
func simdI8x16LeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v2, v1, lanes8MSB, 7, 0xFF))
}

// simdI8x16GeS performs a signed greater-than-or-equal comparison on each 8-bit
// lane.
func simdI8x16GeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v1, v2, lanes8MSB, 7, 0xFF))
}

// simdI8x16GeU performs an unsigned greater-than-or-equal comparison on each
// 8-bit lane.
func simdI8x16GeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v1, v2, lanes8MSB, 7, 0xFF))
}

// simdI16x8Eq performs an equality comparison on each 16-bit lane of two
// V128Value.
func simdI16x8Eq(v1, v2 V128Value) V128Value {
	return eqLanes(v1, v2, lanes16MSB, 15, 0xFFFF)
}

// simdI16x8Ne performs an inequality comparison on each 16-bit lane of two
// V128Value.
func simdI16x8Ne(v1, v2 V128Value) V128Value {
	return simdV128Not(eqLanes(v1, v2, lanes16MSB, 15, 0xFFFF))
}

// simdI16x8LtS performs a signed less-than comparison on each 16-bit lane.
func simdI16x8LtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v1, v2, lanes16MSB, 15, 0xFFFF)
}

// simdI16x8LtU performs an unsigned less-than comparison on each 16-bit lane.
func simdI16x8LtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v1, v2, lanes16MSB, 15, 0xFFFF)
}

// simdI16x8GtS performs a signed greater-than comparison on each 16-bit lane.
func simdI16x8GtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v2, v1, lanes16MSB, 15, 0xFFFF)
}

// simdI16x8GtU performs an unsigned greater-than comparison on each 16-bit
// lane.
func simdI16x8GtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v2, v1, lanes16MSB, 15, 0xFFFF)
}

// simdI16x8LeS performs a signed less-than-or-equal comparison on each 16-bit
// lane.
func simdI16x8LeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v2, v1, lanes16MSB, 15, 0xFFFF))
}

// simdI16x8LeU performs an unsigned less-than-or-equal comparison on each
// 16-bit lane.
func simdI16x8LeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v2, v1, lanes16MSB, 15, 0xFFFF))
}

// simdI16x8GeS performs a signed greater-than-or-equal comparison on each
// 16-bit lane.
func simdI16x8GeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v1, v2, lanes16MSB, 15, 0xFFFF))
}

// simdI16x8GeU performs an unsigned greater-than-or-equal comparison on each
// 16-bit lane.
func simdI16x8GeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v1, v2, lanes16MSB, 15, 0xFFFF))
}

func simdI16x8Abs(v V128Value) V128Value {
	return V128Value{
		Low:  absLanes(v.Low, lanes16MSB, 15, 0xFFFF),
		High: absLanes(v.High, lanes16MSB, 15, 0xFFFF),
	}
}

func simdI16x8Neg(v V128Value) V128Value {
	return V128Value{
		Low:  swarSub(0, v.Low, lanes16MSB),
		High: swarSub(0, v.High, lanes16MSB),
	}
}

func simdI16x8Q15mulrSatS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  q15mulrSatLanesI16(v1.Low, v2.Low),
		High: q15mulrSatLanesI16(v1.High, v2.High),
	}
}

func simdI16x8NarrowI32x4S(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow32x4To16x8(v1, saturateS32ToS16),
		High: narrow32x4To16x8(v2, saturateS32ToS16),
	}
}

func simdI16x8NarrowI32x4U(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow32x4To16x8(v1, saturateS32ToU16),
		High: narrow32x4To16x8(v2, saturateS32ToU16),
	}
}

// simdI32x4Eq performs an equality comparison on each 32-bit lane of two
// V128Value.
func simdI32x4Eq(v1, v2 V128Value) V128Value {
	return eqLanes(v1, v2, lanes32MSB, 31, 0xFFFFFFFF)
}

// simdI32x4Ne performs an inequality comparison on each 32-bit lane of two
// V128Value.
func simdI32x4Ne(v1, v2 V128Value) V128Value {
	return simdV128Not(eqLanes(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

// simdI32x4LtS performs a signed less-than comparison on each 32-bit lane.
func simdI32x4LtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v1, v2, lanes32MSB, 31, 0xFFFFFFFF)
}

// simdI32x4LtU performs an unsigned less-than comparison on each 32-bit lane.
func simdI32x4LtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v1, v2, lanes32MSB, 31, 0xFFFFFFFF)
}

// simdI32x4GtS performs a signed greater-than comparison on each 32-bit lane.
func simdI32x4GtS(v1, v2 V128Value) V128Value {
	return ltLanesS(v2, v1, lanes32MSB, 31, 0xFFFFFFFF)
}

// simdI32x4GtU performs an unsigned greater-than comparison on each 32-bit
// lane.
func simdI32x4GtU(v1, v2 V128Value) V128Value {
	return ltLanesU(v2, v1, lanes32MSB, 31, 0xFFFFFFFF)
}

// simdI32x4LeS performs a signed less-than-or-equal comparison on each 32-bit
// lane.
func simdI32x4LeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v2, v1, lanes32MSB, 31, 0xFFFFFFFF))
}

// simdI32x4LeU performs an unsigned less-than-or-equal comparison on each
// 32-bit lane.
func simdI32x4LeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v2, v1, lanes32MSB, 31, 0xFFFFFFFF))
}

// simdI32x4GeS performs a signed greater-than-or-equal comparison on each
// 32-bit lane.
func simdI32x4GeS(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesS(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

// simdI32x4GeU performs an unsigned greater-than-or-equal comparison on each
// 32-bit lane.
func simdI32x4GeU(v1, v2 V128Value) V128Value {
	return simdV128Not(ltLanesU(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

// simdF32x4Eq performs an equality comparison on each 32-bit float lane of two
// V128Value.
func simdF32x4Eq(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a == b)
	})
}

// simdF32x4Ne performs an inequality comparison on each 32-bit float lane of
// two V128Value.
func simdF32x4Ne(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a != b)
	})
}

// simdF32x4Lt performs a less-than comparison on each 32-bit float lane of two
// V128Value.
func simdF32x4Lt(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a < b)
	})
}

// simdF32x4Gt performs a greater-than comparison on each 32-bit float lane of
// two V128Value.
func simdF32x4Gt(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a > b)
	})
}

// simdF32x4Le performs a less-than-or-equal comparison on each 32-bit float
// lane of two V128Value.
func simdF32x4Le(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a <= b)
	})
}

// simdF32x4Ge performs a greater-than-or-equal comparison on each 32-bit float
// lane of two V128Value.
func simdF32x4Ge(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		return boolToFloat32(a >= b)
	})
}

// simdF64x2Eq performs an equality comparison on each 64-bit float lane of two
// V128Value.
func simdF64x2Eq(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a == b)
	})
}

// simdF64x2Ne performs an inequality comparison on each 64-bit float lane of
// two V128Value.
func simdF64x2Ne(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a != b)
	})
}

// simdF64x2Lt performs a less-than comparison on each 64-bit float lane of two
// V128Value.
func simdF64x2Lt(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a < b)
	})
}

// simdF64x2Gt performs a greater-than comparison on each 64-bit float lane of
// two V128Value.
func simdF64x2Gt(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a > b)
	})
}

// simdF64x2Le performs a less-than-or-equal comparison on each 64-bit float
// lane of two V128Value.
func simdF64x2Le(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a <= b)
	})
}

// simdF64x2Ge performs a greater-than-or-equal comparison on each 64-bit float
// lane of two V128Value.
func simdF64x2Ge(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		return boolToFloat64(a >= b)
	})
}

// simdV128Not performs a bitwise NOT operation on a V128Value.
func simdV128Not(v V128Value) V128Value {
	return V128Value{
		Low:  ^v.Low,
		High: ^v.High,
	}
}

// simdV128And performs a bitwise AND operation on two V128Value.
func simdV128And(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low & v2.Low,
		High: v1.High & v2.High,
	}
}

func simdV128Andnot(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low & ^v2.Low,
		High: v1.High & ^v2.High,
	}
}

// simdV128Or performs a bitwise OR operation on two V128Value.
func simdV128Or(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low | v2.Low,
		High: v1.High | v2.High,
	}
}

// simdV128Xor performs a bitwise XOR operation on two V128Value.
func simdV128Xor(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low ^ v2.Low,
		High: v1.High ^ v2.High,
	}
}

func simdV128Bitselect(v1, v2, v3 V128Value) V128Value {
	return V128Value{
		Low:  (v1.Low & v3.Low) | (v2.Low & ^v3.Low),
		High: (v1.High & v3.High) | (v2.High & ^v3.High),
	}
}

// simdV128AnyTrue returns true if any bit in the 128-bit SIMD value is set to
// 1.
func simdV128AnyTrue(v V128Value) bool {
	return v.Low != 0 || v.High != 0
}

func simdV128Load32Zero(data []byte) V128Value {
	low := uint64(binary.LittleEndian.Uint32(data))
	return V128Value{Low: low, High: 0}
}

func simdV128Load64Zero(data []byte) V128Value {
	low := binary.LittleEndian.Uint64(data)
	return V128Value{Low: low, High: 0}
}

func simdLoadLane(v V128Value, idx uint32, data []byte) V128Value {
	switch len(data) {
	case 1:
		return simdI8x16ReplaceLane(v, idx, int32(data[0]))
	case 2:
		return simdI16x8ReplaceLane(v, idx, int32(binary.LittleEndian.Uint16(data)))
	case 4:
		return simdI32x4ReplaceLane(v, idx, int32(binary.LittleEndian.Uint32(data)))
	case 8:
		return simdI64x2ReplaceLane(v, idx, int64(binary.LittleEndian.Uint64(data)))
	}
	return v
}

func simdF32x4DemoteF64x2Zero(v V128Value) V128Value {
	lowBits := math.Float32bits(float32(math.Float64frombits(v.Low)))
	highBits := math.Float32bits(float32(math.Float64frombits(v.High)))

	return V128Value{
		Low:  uint64(lowBits) | (uint64(highBits) << 32),
		High: 0,
	}
}

func simdF64x2PromoteLowF32x4(v V128Value) V128Value {
	low := math.Float64bits(float64(math.Float32frombits(uint32(v.Low))))
	high := math.Float64bits(float64(math.Float32frombits(uint32(v.Low >> 32))))
	return V128Value{Low: low, High: high}
}

func simdI8x16Abs(v V128Value) V128Value {
	return V128Value{
		Low:  absLanes(v.Low, lanes8MSB, 7, 0xFF),
		High: absLanes(v.High, lanes8MSB, 7, 0xFF),
	}
}

func simdI8x16Neg(v V128Value) V128Value {
	return V128Value{
		Low:  swarSub(0, v.Low, lanes8MSB),
		High: swarSub(0, v.High, lanes8MSB),
	}
}

func simdI8x16Popcnt(v V128Value) V128Value {
	return V128Value{Low: popcntLanesI8(v.Low), High: popcntLanesI8(v.High)}
}

func simdI8x16NarrowI16x8S(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow16x8To8x16(v1, saturateS16ToS8),
		High: narrow16x8To8x16(v2, saturateS16ToS8),
	}
}

func simdI8x16NarrowI16x8U(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  narrow16x8To8x16(v1, saturateS16ToU8),
		High: narrow16x8To8x16(v2, saturateS16ToU8),
	}
}

// simdI8x16AllTrue returns true if all 8-bit lanes of a V128Value are non-zero.
func simdI8x16AllTrue(v V128Value) bool {
	// https://graphics.stanford.edu/~seander/bithacks.html#ZeroInWord
	mask64bit := uint64(0x7F7F7F7F7F7F7F7F)
	hasLowZero := ^((((v.Low & mask64bit) + mask64bit) | v.Low) | mask64bit)
	hasHighZero := ^((((v.High & mask64bit) + mask64bit) | v.High) | mask64bit)
	return hasLowZero == 0 && hasHighZero == 0
}

// simdI8x16Bitmask returns a 16-bit integer wrapped in an int32 where each bit
// corresponds to the most significant bit of each of the 16 8-bit lanes of a
// V128Value.
func simdI8x16Bitmask(v V128Value) int32 {
	return bitmask(v, 1)
}

// simdI8x16Shl performs a left shift on each 8-bit lane of a V128Value.
func simdI8x16Shl(v V128Value, shift int32) V128Value {
	s := uint(shift & 7) // shift amount is modulo 8
	// Shift the whole word, then mask off the bits that crossed lane
	// boundaries (each lane keeps bits [s, 8)).
	mask := uint64(byte(0xFF<<s)) * lanes8Ones
	return V128Value{Low: (v.Low << s) & mask, High: (v.High << s) & mask}
}

// simdI8x16ShrU performs an unsigned right shift on each 8-bit lane
// of a V128Value.
func simdI8x16ShrU(v V128Value, shift int32) V128Value {
	s := uint(shift & 7) // shift amount is modulo 8
	// Each lane keeps bits [0, 8-s) after the word-wide shift.
	mask := uint64(0xFF>>s) * lanes8Ones
	return V128Value{Low: (v.Low >> s) & mask, High: (v.High >> s) & mask}
}

// simdI8x16ShrS performs a signed right shift on each 8-bit lane of a
// V128Value.
func simdI8x16ShrS(v V128Value, shift int32) V128Value {
	s := uint(shift & 7) // shift amount is modulo 8
	maskLow := uint64(0xFF>>s) * lanes8Ones
	const signs = 0x8080808080808080
	// negLow/negHigh hold 0xFF in every lane whose sign bit is set; OR their
	// high s bits into the logical shift to sign-extend each lane.
	negLow := ((v.Low & signs) >> 7) * 0xFF
	negHigh := ((v.High & signs) >> 7) * 0xFF
	return V128Value{
		Low:  ((v.Low >> s) & maskLow) | (negLow &^ maskLow),
		High: ((v.High >> s) & maskLow) | (negHigh &^ maskLow),
	}
}

// simdI8x16Add performs an addition on each 8-bit lane of two V128Value.
func simdI8x16Add(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarAdd(v1.Low, v2.Low, lanes8MSB),
		High: swarAdd(v1.High, v2.High, lanes8MSB),
	}
}

// simdI8x16AddSatS performs a saturating signed addition on each 8-bit lane of
// two V128Value.
func simdI8x16AddSatS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  addSatLanesS(v1.Low, v2.Low, lanes8MSB, 7, 0xFF),
		High: addSatLanesS(v1.High, v2.High, lanes8MSB, 7, 0xFF),
	}
}

// simdI8x16AddSatU performs a saturating unsigned addition on each 8-bit lane
// of two V128Value.
func simdI8x16AddSatU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  addSatLanesU(v1.Low, v2.Low, lanes8MSB, 7, 0xFF),
		High: addSatLanesU(v1.High, v2.High, lanes8MSB, 7, 0xFF),
	}
}

// simdI8x16Sub performs a subtraction on each 8-bit lane of two V128Value.
func simdI8x16Sub(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarSub(v1.Low, v2.Low, lanes8MSB),
		High: swarSub(v1.High, v2.High, lanes8MSB),
	}
}

// simdI8x16SubSatS performs a saturating signed subtraction on each 8-bit lane
// of two V128Value.
func simdI8x16SubSatS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  subSatLanesS(v1.Low, v2.Low, lanes8MSB, 7, 0xFF),
		High: subSatLanesS(v1.High, v2.High, lanes8MSB, 7, 0xFF),
	}
}

// simdI8x16SubSatU performs a saturating unsigned subtraction on each 8-bit
// lane of two V128Value.
func simdI8x16SubSatU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  subSatLanesU(v1.Low, v2.Low, lanes8MSB, 7, 0xFF),
		High: subSatLanesU(v1.High, v2.High, lanes8MSB, 7, 0xFF),
	}
}

func simdI8x16MinS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesS(v1, v2, lanes8MSB, 7, 0xFF))
}

func simdI8x16MinU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesU(v1, v2, lanes8MSB, 7, 0xFF))
}

func simdI8x16MaxS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesS(v1, v2, lanes8MSB, 7, 0xFF))
}

func simdI8x16MaxU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesU(v1, v2, lanes8MSB, 7, 0xFF))
}

func simdI8x16AvgrU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarAvgrU(v1.Low, v2.Low, lanes8MSB),
		High: swarAvgrU(v1.High, v2.High, lanes8MSB),
	}
}

func simdI16x8AllTrue(v V128Value) bool {
	return allTrue(v, 2)
}

func simdI16x8Bitmask(v V128Value) int32 {
	return bitmask(v, 2)
}

func simdI16x8ExtendLowI8x16S(v V128Value) V128Value {
	return extend(v, 1, 2, false, true)
}

func simdI16x8ExtendHighI8x16S(v V128Value) V128Value {
	return extend(v, 1, 2, true, true)
}

func simdI16x8ExtendLowI8x16U(v V128Value) V128Value {
	return extend(v, 1, 2, false, false)
}

func simdI16x8ExtendHighI8x16U(v V128Value) V128Value {
	return extend(v, 1, 2, true, false)
}

func simdI32x4ExtendLowI16x8S(v V128Value) V128Value {
	return extend(v, 2, 4, false, true)
}

func simdI32x4ExtendHighI16x8S(v V128Value) V128Value {
	return extend(v, 2, 4, true, true)
}

func simdI32x4ExtendLowI16x8U(v V128Value) V128Value {
	return extend(v, 2, 4, false, false)
}

func simdI32x4ExtendHighI16x8U(v V128Value) V128Value {
	return extend(v, 2, 4, true, false)
}

func simdI64x2ExtendLowI32x4S(v V128Value) V128Value {
	return extend(v, 4, 8, false, true)
}

func simdI64x2ExtendHighI32x4S(v V128Value) V128Value {
	return extend(v, 4, 8, true, true)
}

func simdI64x2ExtendLowI32x4U(v V128Value) V128Value {
	return extend(v, 4, 8, false, false)
}

func simdI64x2ExtendHighI32x4U(v V128Value) V128Value {
	return extend(v, 4, 8, true, false)
}

func simdI16x8Shl(v V128Value, shift int32) V128Value {
	s := uint(shift & 15) // shift amount is modulo 16
	// Shift the whole word, then mask off the bits that crossed lane
	// boundaries (each lane keeps bits [s, 16)).
	mask := uint64(uint16(0xFFFF<<s)) * lanes16Ones
	return V128Value{Low: (v.Low << s) & mask, High: (v.High << s) & mask}
}

// simdI16x8ShrS performs a signed right shift on each 16-bit lane of a
// V128Value.
func simdI16x8ShrS(v V128Value, shift int32) V128Value {
	s := uint(shift & 15) // shift amount is modulo 16
	maskLow := uint64(0xFFFF>>s) * lanes16Ones
	const signs = 0x8000800080008000
	// negLow/negHigh hold 0xFFFF in every lane whose sign bit is set; OR their
	// high s bits into the logical shift to sign-extend each lane.
	negLow := ((v.Low & signs) >> 15) * 0xFFFF
	negHigh := ((v.High & signs) >> 15) * 0xFFFF
	return V128Value{
		Low:  ((v.Low >> s) & maskLow) | (negLow &^ maskLow),
		High: ((v.High >> s) & maskLow) | (negHigh &^ maskLow),
	}
}

// simdI16x8ShrU performs an unsigned right shift on each 16-bit lane of a
// V128Value.
func simdI16x8ShrU(v V128Value, shift int32) V128Value {
	s := uint(shift & 15) // shift amount is modulo 16
	// Each lane keeps bits [0, 16-s) after the word-wide shift.
	mask := uint64(0xFFFF>>s) * lanes16Ones
	return V128Value{Low: (v.Low >> s) & mask, High: (v.High >> s) & mask}
}

// simdI16x8Add performs an addition on each 16-bit lane of two V128Value.
func simdI16x8Add(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarAdd(v1.Low, v2.Low, lanes16MSB),
		High: swarAdd(v1.High, v2.High, lanes16MSB),
	}
}

// simdI16x8AddSatS performs a saturating signed addition on each 16-bit lane of
// two V128Value.
func simdI16x8AddSatS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  addSatLanesS(v1.Low, v2.Low, lanes16MSB, 15, 0xFFFF),
		High: addSatLanesS(v1.High, v2.High, lanes16MSB, 15, 0xFFFF),
	}
}

// simdI16x8AddSatU performs a saturating unsigned addition on each 16-bit lane
// of two V128Value.
func simdI16x8AddSatU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  addSatLanesU(v1.Low, v2.Low, lanes16MSB, 15, 0xFFFF),
		High: addSatLanesU(v1.High, v2.High, lanes16MSB, 15, 0xFFFF),
	}
}

// simdI16x8Sub performs a subtraction on each 16-bit lane of two V128Value.
func simdI16x8Sub(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarSub(v1.Low, v2.Low, lanes16MSB),
		High: swarSub(v1.High, v2.High, lanes16MSB),
	}
}

// simdI16x8SubSatS performs a saturating signed subtraction on each 16-bit lane
// of two V128Value.
func simdI16x8SubSatS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  subSatLanesS(v1.Low, v2.Low, lanes16MSB, 15, 0xFFFF),
		High: subSatLanesS(v1.High, v2.High, lanes16MSB, 15, 0xFFFF),
	}
}

// simdI16x8SubSatU performs a saturating unsigned subtraction on each 16-bit
// lane of two V128Value.
func simdI16x8SubSatU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  subSatLanesU(v1.Low, v2.Low, lanes16MSB, 15, 0xFFFF),
		High: subSatLanesU(v1.High, v2.High, lanes16MSB, 15, 0xFFFF),
	}
}

// simdI16x8Mul performs a multiplication on each 16-bit lane of two V128Value.
func simdI16x8Mul(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  mulLanesI16(v1.Low, v2.Low),
		High: mulLanesI16(v1.High, v2.High),
	}
}

func simdI16x8MinS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesS(v1, v2, lanes16MSB, 15, 0xFFFF))
}

func simdI16x8MinU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesU(v1, v2, lanes16MSB, 15, 0xFFFF))
}

func simdI16x8MaxS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesS(v1, v2, lanes16MSB, 15, 0xFFFF))
}

func simdI16x8MaxU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesU(v1, v2, lanes16MSB, 15, 0xFFFF))
}

func simdI16x8AvgrU(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarAvgrU(v1.Low, v2.Low, lanes16MSB),
		High: swarAvgrU(v1.High, v2.High, lanes16MSB),
	}
}

func simdI16x8ExtmulLowI8x16S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, false, true)
}

func simdI16x8ExtmulHighI8x16S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, true, true)
}

func simdI16x8ExtmulLowI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, false, false)
}

func simdI16x8ExtmulHighI8x16U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 1, true, false)
}

func simdI32x4ExtmulLowI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, false, true)
}

func simdI32x4ExtmulHighI16x8S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, true, true)
}

func simdI32x4ExtmulLowI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, false, false)
}

func simdI32x4ExtmulHighI16x8U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 2, true, false)
}

func simdI64x2ExtmulLowI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, false, true)
}

func simdI64x2ExtmulHighI32x4S(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, true, true)
}

func simdI64x2ExtmulLowI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, false, false)
}

func simdI64x2ExtmulHighI32x4U(v1, v2 V128Value) V128Value {
	return extmul(v1, v2, 4, true, false)
}

func simdI16x8ExtaddPairwiseI8x16S(v V128Value) V128Value {
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

func simdI16x8ExtaddPairwiseI8x16U(v V128Value) V128Value {
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

func simdI32x4ExtaddPairwiseI16x8U(v V128Value) V128Value {
	l0 := uint64(uint32(uint16(v.Low)) + uint32(uint16(v.Low>>16)))
	l1 := uint64(uint32(uint16(v.Low>>32)) + uint32(uint16(v.Low>>48)))
	low := l0 | l1<<32

	h0 := uint64(uint32(uint16(v.High)) + uint32(uint16(v.High>>16)))
	h1 := uint64(uint32(uint16(v.High>>32)) + uint32(uint16(v.High>>48)))
	high := h0 | h1<<32

	return V128Value{Low: low, High: high}
}

func simdI32x4ExtaddPairwiseI16x8S(v V128Value) V128Value {
	l0 := uint64(uint32(int32(int16(v.Low)) + int32(int16(v.Low>>16))))
	l1 := uint64(uint32(int32(int16(v.Low>>32)) + int32(int16(v.Low>>48))))
	low := l0 | l1<<32

	h0 := uint64(uint32(int32(int16(v.High)) + int32(int16(v.High>>16))))
	h1 := uint64(uint32(int32(int16(v.High>>32)) + int32(int16(v.High>>48))))
	high := h0 | h1<<32

	return V128Value{Low: low, High: high}
}

func simdI32x4Abs(v V128Value) V128Value {
	return V128Value{
		Low:  absLanes(v.Low, lanes32MSB, 31, 0xFFFFFFFF),
		High: absLanes(v.High, lanes32MSB, 31, 0xFFFFFFFF),
	}
}

func simdI32x4Neg(v V128Value) V128Value {
	return V128Value{
		Low:  swarSub(0, v.Low, lanes32MSB),
		High: swarSub(0, v.High, lanes32MSB),
	}
}

func simdI32x4AllTrue(v V128Value) bool {
	return allTrue(v, 4)
}

func simdI32x4Bitmask(v V128Value) int32 {
	return bitmask(v, 4)
}

// simdI32x4Shl performs a left shift on each 32-bit lane of a V128Value.
func simdI32x4Shl(v V128Value, shift int32) V128Value {
	s := uint(shift & 31) // shift amount is modulo 32
	// Shift the whole word, then mask off the bits that crossed the lane
	// boundary (each lane keeps bits [s, 32)).
	mask := uint64(uint32(0xFFFFFFFF<<s)) * lanes32Ones
	return V128Value{Low: (v.Low << s) & mask, High: (v.High << s) & mask}
}

// simdI32x4ShrS performs a signed right shift on each 32-bit lane of a
// V128Value.
func simdI32x4ShrS(v V128Value, shift int32) V128Value {
	s := uint(shift & 31) // shift amount is modulo 32
	maskLow := uint64(0xFFFFFFFF>>s) * lanes32Ones
	const signs = 0x8000000080000000
	// negLow/negHigh hold all-ones in every lane whose sign bit is set; OR
	// their high s bits into the logical shift to sign-extend each lane.
	negLow := ((v.Low & signs) >> 31) * 0xFFFFFFFF
	negHigh := ((v.High & signs) >> 31) * 0xFFFFFFFF
	return V128Value{
		Low:  ((v.Low >> s) & maskLow) | (negLow &^ maskLow),
		High: ((v.High >> s) & maskLow) | (negHigh &^ maskLow),
	}
}

// simdI32x4ShrU performs an unsigned right shift on each 32-bit lane of a
// V128Value.
func simdI32x4ShrU(v V128Value, shift int32) V128Value {
	s := uint(shift & 31) // shift amount is modulo 32
	// Each lane keeps bits [0, 32-s) after the word-wide shift.
	mask := uint64(0xFFFFFFFF>>s) * lanes32Ones
	return V128Value{Low: (v.Low >> s) & mask, High: (v.High >> s) & mask}
}

func simdI32x4Add(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarAdd(v1.Low, v2.Low, lanes32MSB),
		High: swarAdd(v1.High, v2.High, lanes32MSB),
	}
}

func simdI32x4Sub(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  swarSub(v1.Low, v2.Low, lanes32MSB),
		High: swarSub(v1.High, v2.High, lanes32MSB),
	}
}

func simdI32x4Mul(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  mulLanesI32(v1.Low, v2.Low),
		High: mulLanesI32(v1.High, v2.High),
	}
}

func simdI32x4MinS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesS(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

func simdI32x4MinU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v1, v2, ltLanesU(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

func simdI32x4MaxS(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesS(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

func simdI32x4MaxU(v1, v2 V128Value) V128Value {
	return simdV128Bitselect(v2, v1, ltLanesU(v1, v2, lanes32MSB, 31, 0xFFFFFFFF))
}

func simdI32x4DotI16x8S(v1, v2 V128Value) V128Value {
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

func simdI64x2Abs(v V128Value) V128Value {
	sLow := int64(v.Low) >> 63
	sHigh := int64(v.High) >> 63
	return V128Value{
		Low:  uint64((int64(v.Low) ^ sLow) - sLow),
		High: uint64((int64(v.High) ^ sHigh) - sHigh),
	}
}

func simdI64x2Neg(v V128Value) V128Value {
	return V128Value{
		Low:  uint64(-int64(v.Low)),
		High: uint64(-int64(v.High)),
	}
}

func simdI64x2AllTrue(v V128Value) bool {
	return v.Low != 0 && v.High != 0
}

func simdI64x2Bitmask(v V128Value) int32 {
	return bitmask(v, 8)
}

// simdI64x2Shl performs a left shift on each 64-bit lane of a V128Value.
func simdI64x2Shl(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  v.Low << s,
		High: v.High << s,
	}
}

// simdI64x2ShrS performs a signed right shift on each 64-bit lane of a
// V128Value.
func simdI64x2ShrS(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  uint64(int64(v.Low) >> s),
		High: uint64(int64(v.High) >> s),
	}
}

// simdI64x2ShrU performs an unsigned right shift on each 64-bit lane of a
// V128Value.
func simdI64x2ShrU(v V128Value, shift int32) V128Value {
	s := shift & 63 // shift amount is modulo 64
	return V128Value{
		Low:  v.Low >> s,
		High: v.High >> s,
	}
}

func simdI64x2Add(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low + v2.Low,
		High: v1.High + v2.High,
	}
}

func simdI64x2Sub(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low - v2.Low,
		High: v1.High - v2.High,
	}
}

func simdI64x2Mul(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  v1.Low * v2.Low,
		High: v1.High * v2.High,
	}
}

// simdI64x2Eq performs an equality comparison on each 64-bit lane of two
// V128Value.
func simdI64x2Eq(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](v1.Low == v2.Low),
		High: boolToUint[uint64](v1.High == v2.High),
	}
}

// simdI64x2Ne performs an inequality comparison on each 64-bit lane of two
// V128Value.
func simdI64x2Ne(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](v1.Low != v2.Low),
		High: boolToUint[uint64](v1.High != v2.High),
	}
}

// simdI64x2LtS performs a signed less-than comparison on each 64-bit lane.
func simdI64x2LtS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) < int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) < int64(v2.High)),
	}
}

// simdI64x2GtS performs a signed greater-than comparison on each 64-bit lane.
func simdI64x2GtS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) > int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) > int64(v2.High)),
	}
}

// simdI64x2LeS performs a signed less-than-or-equal comparison on each 64-bit
// lane.
func simdI64x2LeS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) <= int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) <= int64(v2.High)),
	}
}

// simdI64x2GeS performs a signed greater-than-or-equal comparison on each
// 64-bit lane.
func simdI64x2GeS(v1, v2 V128Value) V128Value {
	return V128Value{
		Low:  boolToUint[uint64](int64(v1.Low) >= int64(v2.Low)),
		High: boolToUint[uint64](int64(v1.High) >= int64(v2.High)),
	}
}

// simdF32x4Abs performs an absolute value operation on each 32-bit float lane.
func simdF32x4Abs(v V128Value) V128Value {
	return unaryOpF32x4(v, abs[float32])
}

func simdF32x4Neg(v V128Value) V128Value {
	return unaryOpF32x4(v, func(f float32) float32 { return -f })
}

func simdF32x4Sqrt(v V128Value) V128Value {
	return unaryOpF32x4(v, sqrt[float32])
}

// simdF32x4Add performs an addition on each 32-bit float lane.
func simdF32x4Add(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, add[float32])
}

func simdF32x4Ceil(v V128Value) V128Value {
	return unaryOpF32x4(v, ceil[float32])
}

func simdF32x4Floor(v V128Value) V128Value {
	return unaryOpF32x4(v, floor[float32])
}

func simdF32x4Trunc(v V128Value) V128Value {
	return unaryOpF32x4(v, trunc[float32])
}

func simdF32x4Nearest(v V128Value) V128Value {
	return unaryOpF32x4(v, nearest[float32])
}

// simdF32x4Sub performs a subtraction on each 32-bit float lane.
func simdF32x4Sub(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, sub[float32])
}

// simdF32x4Mul performs a multiplication on each 32-bit float lane.
func simdF32x4Mul(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, mul[float32])
}

// simdF32x4Min performs a minimum operation on each 32-bit float lane.
func simdF32x4Min(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, wasmMin[float32])
}

func simdF32x4Max(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, wasmMax[float32])
}

func simdF32x4Pmin(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if b < a {
			return b
		}
		return a
	})
}

func simdF32x4Pmax(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, func(a, b float32) float32 {
		if a < b {
			return b
		}
		return a
	})
}

// simdF32x4Div performs a division operation on each 32-bit float lane.
func simdF32x4Div(v1, v2 V128Value) V128Value {
	return binaryOpF32x4(v1, v2, div[float32])
}

// simdF64x2Add performs an addition on each 64-bit float lane of two V128Value.
func simdF64x2Add(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, add[float64])
}

// simdF64x2Sub performs a subtraction on each 64-bit float lane of two
// V128Value.
func simdF64x2Sub(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, sub[float64])
}

func simdF64x2Ceil(v V128Value) V128Value {
	return unaryOpF64x2(v, ceil[float64])
}

func simdF64x2Floor(v V128Value) V128Value {
	return unaryOpF64x2(v, floor[float64])
}

func simdF64x2Trunc(v V128Value) V128Value {
	return unaryOpF64x2(v, trunc[float64])
}

func simdF64x2Nearest(v V128Value) V128Value {
	return unaryOpF64x2(v, nearest[float64])
}

// simdF64x2Mul performs a multiplication on each 64-bit float lane of two
// V128Value.
func simdF64x2Mul(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, mul[float64])
}

func simdF64x2Min(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, wasmMin[float64])
}

func simdF64x2Max(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, wasmMax[float64])
}

func simdF64x2Pmin(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if b < a {
			return b
		}
		return a
	})
}

func simdF64x2Pmax(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, func(a, b float64) float64 {
		if a < b {
			return b
		}
		return a
	})
}

func simdI32x4TruncSatF32x4S(v V128Value) V128Value {
	i0 := saturateF32toInt32(math.Float32frombits(uint32(v.Low)))
	i1 := saturateF32toInt32(math.Float32frombits(uint32(v.Low >> 32)))
	low := uint64(uint32(i0)) | (uint64(uint32(i1)) << 32)

	i2 := saturateF32toInt32(math.Float32frombits(uint32(v.High)))
	i3 := saturateF32toInt32(math.Float32frombits(uint32(v.High >> 32)))
	high := uint64(uint32(i2)) | (uint64(uint32(i3)) << 32)

	return V128Value{Low: low, High: high}
}

func simdI32x4TruncSatF32x4U(v V128Value) V128Value {
	u0 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.Low))))
	u1 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.Low >> 32))))
	low := u0 | (u1 << 32)

	u2 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.High))))
	u3 := uint64(saturateF32toUint32(math.Float32frombits(uint32(v.High >> 32))))
	high := u2 | (u3 << 32)

	return V128Value{Low: low, High: high}
}

func simdI32x4TruncSatF64x2SZero(v V128Value) V128Value {
	lowLowHalf := saturateF64toInt32(math.Float64frombits(v.Low))
	lowHighHalf := saturateF64toInt32(math.Float64frombits(v.High))
	return V128Value{
		Low:  uint64(uint32(lowLowHalf)) | (uint64(uint32(lowHighHalf)) << 32),
		High: 0,
	}
}

func simdI32x4TruncSatF64x2UZero(v V128Value) V128Value {
	lowLowHalf := saturateF64toUint32(math.Float64frombits(v.Low))
	lowHighHalf := saturateF64toUint32(math.Float64frombits(v.High))
	return V128Value{
		Low:  uint64(lowLowHalf) | (uint64(lowHighHalf) << 32),
		High: 0,
	}
}

func simdF32x4ConvertI32x4S(v V128Value) V128Value {
	f0 := float32(int32(v.Low))
	f1 := float32(int32(v.Low >> 32))
	low := uint64(math.Float32bits(f0)) | (uint64(math.Float32bits(f1)) << 32)

	f2 := float32(int32(v.High))
	f3 := float32(int32(v.High >> 32))
	high := uint64(math.Float32bits(f2)) | (uint64(math.Float32bits(f3)) << 32)

	return V128Value{Low: low, High: high}
}

func simdF32x4ConvertI32x4U(v V128Value) V128Value {
	f0 := float32(uint32(v.Low))
	f1 := float32(uint32(v.Low >> 32))
	low := uint64(math.Float32bits(f0)) | (uint64(math.Float32bits(f1)) << 32)

	f2 := float32(uint32(v.High))
	f3 := float32(uint32(v.High >> 32))
	high := uint64(math.Float32bits(f2)) | (uint64(math.Float32bits(f3)) << 32)

	return V128Value{Low: low, High: high}
}

func simdF64x2Abs(v V128Value) V128Value {
	return unaryOpF64x2(v, abs[float64])
}

func simdF64x2Neg(v V128Value) V128Value {
	return unaryOpF64x2(v, func(f float64) float64 { return -f })
}

func simdF64x2Sqrt(v V128Value) V128Value {
	return unaryOpF64x2(v, sqrt[float64])
}

func simdF64x2Div(v1, v2 V128Value) V128Value {
	return binaryOpF64x2(v1, v2, div[float64])
}

func simdF64x2ConvertLowI32x4S(v V128Value) V128Value {
	return V128Value{
		Low:  math.Float64bits(float64(int32(v.Low))),
		High: math.Float64bits(float64(int32(v.Low >> 32))),
	}
}

func simdF64x2ConvertLowI32x4U(v V128Value) V128Value {
	return V128Value{
		Low:  math.Float64bits(float64(uint32(v.Low))),
		High: math.Float64bits(float64(uint32(v.Low >> 32))),
	}
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
	src := v.Low
	if high {
		src = v.High
	}
	// Each half of the result widens half of src's lanes. Branching on the
	// source width once (rather than per lane) keeps the packing branch-free.
	switch fromBytes {
	case 1: // 8x8 -> 8x16; src low/high 32 bits feed the two result halves.
		return V128Value{
			Low:  widen8to16(uint32(src), signed),
			High: widen8to16(uint32(src>>32), signed),
		}
	case 2: // 4x16 -> 4x32
		return V128Value{
			Low:  widen16to32(uint32(src), signed),
			High: widen16to32(uint32(src>>32), signed),
		}
	default: // 2x32 -> 2x64
		if signed {
			return V128Value{
				Low:  uint64(int64(int32(src))),
				High: uint64(int64(int32(src >> 32))),
			}
		}
		return V128Value{Low: uint64(uint32(src)), High: uint64(uint32(src >> 32))}
	}
}

// widen8to16 expands four packed bytes into four packed 16-bit lanes, with sign
// or zero extension.
func widen8to16(b uint32, signed bool) uint64 {
	if signed {
		l0 := uint64(uint16(int8(b)))
		l1 := uint64(uint16(int8(b >> 8)))
		l2 := uint64(uint16(int8(b >> 16)))
		l3 := uint64(uint16(int8(b >> 24)))
		return l0 | l1<<16 | l2<<32 | l3<<48
	}
	l0 := uint64(b & 0xFF)
	l1 := uint64(b>>8) & 0xFF
	l2 := uint64(b>>16) & 0xFF
	l3 := uint64(b>>24) & 0xFF
	return l0 | l1<<16 | l2<<32 | l3<<48
}

// widen16to32 expands two packed 16-bit lanes into two packed 32-bit lanes,
// with sign or zero extension.
func widen16to32(h uint32, signed bool) uint64 {
	if signed {
		l0 := uint64(uint32(int16(h)))
		l1 := uint64(uint32(int16(h >> 16)))
		return l0 | l1<<32
	}
	return uint64(h&0xFFFF) | uint64(h>>16)<<32
}

func extmul(v1, v2 V128Value, fromBytes int, high, signed bool) V128Value {
	half1, half2 := v1.Low, v2.Low
	if high {
		half1, half2 = v1.High, v2.High
	}
	// As in extend, branch on the source width once and pack the widened
	// products of each half's lanes directly.
	switch fromBytes {
	case 1: // 8x8 * 8x8 -> 8x16
		return V128Value{
			Low:  extmul8(uint32(half1), uint32(half2), signed),
			High: extmul8(uint32(half1>>32), uint32(half2>>32), signed),
		}
	case 2: // 4x16 * 4x16 -> 4x32
		return V128Value{
			Low:  extmul16(uint32(half1), uint32(half2), signed),
			High: extmul16(uint32(half1>>32), uint32(half2>>32), signed),
		}
	default: // 2x32 * 2x32 -> 2x64
		if signed {
			return V128Value{
				Low:  uint64(int64(int32(half1)) * int64(int32(half2))),
				High: uint64(int64(int32(half1>>32)) * int64(int32(half2>>32))),
			}
		}
		return V128Value{
			Low:  uint64(uint32(half1)) * uint64(uint32(half2)),
			High: uint64(uint32(half1>>32)) * uint64(uint32(half2>>32)),
		}
	}
}

// extmul8 multiplies four pairs of packed bytes into four packed 16-bit
// products, with signed or unsigned interpretation.
func extmul8(a, b uint32, signed bool) uint64 {
	if signed {
		p0 := uint64(uint16(int16(int8(a)) * int16(int8(b))))
		p1 := uint64(uint16(int16(int8(a>>8)) * int16(int8(b>>8))))
		p2 := uint64(uint16(int16(int8(a>>16)) * int16(int8(b>>16))))
		p3 := uint64(uint16(int16(int8(a>>24)) * int16(int8(b>>24))))
		return p0 | p1<<16 | p2<<32 | p3<<48
	}
	p0 := uint64(uint16(a&0xFF) * uint16(b&0xFF))
	p1 := uint64(uint16((a>>8)&0xFF) * uint16((b>>8)&0xFF))
	p2 := uint64(uint16((a>>16)&0xFF) * uint16((b>>16)&0xFF))
	p3 := uint64(uint16((a>>24)&0xFF) * uint16((b>>24)&0xFF))
	return p0 | p1<<16 | p2<<32 | p3<<48
}

// extmul16 multiplies two pairs of packed 16-bit lanes into two packed 32-bit
// products, with signed or unsigned interpretation.
func extmul16(a, b uint32, signed bool) uint64 {
	if signed {
		p0 := uint64(uint32(int32(int16(a)) * int32(int16(b))))
		p1 := uint64(uint32(int32(int16(a>>16)) * int32(int16(b>>16))))
		return p0 | p1<<32
	}
	p0 := uint64(uint32(a&0xFFFF) * uint32(b&0xFFFF))
	p1 := uint64(uint32(a>>16) * uint32(b>>16))
	return p0 | p1<<32
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
	return uint64(uint16(v))
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
	return uint64(uint8(v))
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

	// We use an explicit float rather than comparing against math.MaxInt32
	// to avoid subtle differences in behavior between architectures.
	if f >= 2147483648.0 {
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

// swarAdd adds the packed lanes of a and b, where msb marks the high bit of
// each lane. Carries are confined to their lane: the low bits are summed with
// the lane's top bit cleared, then the top bit is restored via XOR.
func swarAdd(a, b, msb uint64) uint64 {
	low := ^msb
	return ((a & low) + (b & low)) ^ ((a ^ b) & msb)
}

// swarSub subtracts the packed lanes of b from a, with msb marking the high bit
// of each lane. Forcing a's top bit set and b's top bit clear keeps any borrow
// inside its lane; the top bit is then corrected via XOR.
func swarSub(a, b, msb uint64) uint64 {
	t := (a | msb) - (b &^ msb)
	return t ^ (^(a ^ b) & msb)
}

// swarAvgrU is the rounding unsigned average (a+b+1)/2 of each lane. The
// shifted term has its lane top bit cleared so the >>1 cannot pull a
// neighbour's low bit across the lane boundary.
func swarAvgrU(a, b, msb uint64) uint64 {
	return (a | b) - (((a ^ b) >> 1) &^ msb)
}

// spreadLaneMSB turns a word whose only set bits are each lane's high bit into
// a full-lane mask: lanes with the bit set become all-ones, the rest stay zero.
// laneShift is laneBits-1 and fill is a single lane of all-ones.
func spreadLaneMSB(bits uint64, laneShift uint, fill uint64) uint64 {
	return (bits >> laneShift) * fill
}

// swarEqMask returns all-ones in every lane where a and b are equal. It detects
// the zero lanes of a^b with a carry-contained test (so a zero lane cannot
// falsely mark its neighbour), leaving the lane's top bit set, then expands
// that marker to a full-lane mask.
func swarEqMask(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	x := a ^ b
	low := ^msb
	marker := ^(((x & low) + low) | x | low)
	return spreadLaneMSB(marker, laneShift, fill)
}

// swarLtMaskU returns all-ones in every lane where a < b (unsigned). It
// performs a lane-contained subtraction (top bit reserved as a borrow catcher)
// and folds in the lanes' own top bits, then expands the marker to a full-lane
// mask.
func swarLtMaskU(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	t := (a | msb) - (b &^ msb)
	marker := msb & ((^a & b) | (^(a ^ b) &^ t))
	return spreadLaneMSB(marker, laneShift, fill)
}

// swarLtMaskS returns all-ones in every lane where a < b (signed). Flipping
// each lane's sign bit turns signed order into unsigned order.
func swarLtMaskS(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	return swarLtMaskU(a^msb, b^msb, msb, laneShift, fill)
}

// eqLanes applies swarEqMask to both halves of a V128Value.
func eqLanes(
	a, b V128Value, msb uint64, laneShift uint, fill uint64,
) V128Value {
	return V128Value{
		Low:  swarEqMask(a.Low, b.Low, msb, laneShift, fill),
		High: swarEqMask(a.High, b.High, msb, laneShift, fill),
	}
}

// ltLanesS applies swarLtMaskS (signed less-than) to both halves.
func ltLanesS(
	a, b V128Value, msb uint64, laneShift uint, fill uint64,
) V128Value {
	return V128Value{
		Low:  swarLtMaskS(a.Low, b.Low, msb, laneShift, fill),
		High: swarLtMaskS(a.High, b.High, msb, laneShift, fill),
	}
}

// ltLanesU applies swarLtMaskU (unsigned less-than) to both halves.
func ltLanesU(
	a, b V128Value, msb uint64, laneShift uint, fill uint64,
) V128Value {
	return V128Value{
		Low:  swarLtMaskU(a.Low, b.Low, msb, laneShift, fill),
		High: swarLtMaskU(a.High, b.High, msb, laneShift, fill),
	}
}

// mulLanesI16 multiplies the four packed 16-bit lanes of a and b.
// Multiplication is not lane-separable, so each lane is handled explicitly.
func mulLanesI16(a, b uint64) uint64 {
	r0 := uint64(uint16(int16(a) * int16(b)))
	r1 := uint64(uint16(int16(a>>16) * int16(b>>16)))
	r2 := uint64(uint16(int16(a>>32) * int16(b>>32)))
	r3 := uint64(uint16(int16(a>>48) * int16(b>>48)))
	return r0 | r1<<16 | r2<<32 | r3<<48
}

// mulLanesI32 multiplies the two packed 32-bit lanes of a and b.
func mulLanesI32(a, b uint64) uint64 {
	r0 := uint64(uint32(int32(a) * int32(b)))
	r1 := uint64(uint32(int32(a>>32) * int32(b>>32)))
	return r0 | r1<<32
}

// absLanes computes the lane-wise wrapping absolute value. Each lane's sign bit
// is broadcast to a full-lane mask, then abs = (v ^ mask) - mask.
func absLanes(v, msb uint64, laneShift uint, fill uint64) uint64 {
	signMask := spreadLaneMSB(v&msb, laneShift, fill)
	return swarSub(v^signMask, signMask, msb)
}

// q15mulrSatLanesI16 multiplies the four packed signed 16-bit lanes in Q15
// fixed point with rounding and signed saturation. The multiply is not
// lane-separable, so each lane is handled explicitly.
func q15mulrSatLanesI16(a, b uint64) uint64 {
	var res uint64
	for shift := uint(0); shift < 64; shift += 16 {
		prod := int32(int16(a>>shift)) * int32(int16(b>>shift))
		v := (prod + 16384) >> 15 // 16384 = 2^14
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		res |= uint64(uint16(v)) << shift
	}
	return res
}

// addSatLanesU is the unsigned saturating add. A lane overflows when a > ^b,
// and those lanes are forced to all-ones.
func addSatLanesU(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	return swarAdd(a, b, msb) | swarLtMaskU(^b, a, msb, laneShift, fill)
}

// subSatLanesU is the unsigned saturating sub. Lanes where a < b underflow and
// are clamped to zero.
func subSatLanesU(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	return swarSub(a, b, msb) &^ swarLtMaskU(a, b, msb, laneShift, fill)
}

// addSatLanesS is the signed saturating add. A lane overflows when the operands
// share a sign but the wrapped sum does not; such lanes saturate to MaxInt when
// the sum looks negative (positive overflow) and to MinInt otherwise.
func addSatLanesS(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	s := swarAdd(a, b, msb)
	ovMask := spreadLaneMSB(^(a^b)&(a^s)&msb, laneShift, fill)
	sat := msb ^ spreadLaneMSB(s&msb, laneShift, fill)
	return (sat & ovMask) | (s &^ ovMask)
}

// subSatLanesS is the signed saturating sub. A lane overflows when the operands
// differ in sign and the wrapped difference's sign differs from a; the
// saturation value follows the same rule as addSatLanesS.
func subSatLanesS(a, b, msb uint64, laneShift uint, fill uint64) uint64 {
	d := swarSub(a, b, msb)
	ovMask := spreadLaneMSB((a^b)&(a^d)&msb, laneShift, fill)
	sat := msb ^ spreadLaneMSB(d&msb, laneShift, fill)
	return (sat & ovMask) | (d &^ ovMask)
}

// popcntLanesI8 returns the per-byte population count, each byte holding the
// number of set bits in the corresponding input byte.
func popcntLanesI8(x uint64) uint64 {
	const (
		m1 = 0x5555555555555555
		m2 = 0x3333333333333333
		m4 = 0x0F0F0F0F0F0F0F0F
	)
	x -= (x >> 1) & m1
	x = (x & m2) + ((x >> 2) & m2)
	return (x + (x >> 4)) & m4
}
