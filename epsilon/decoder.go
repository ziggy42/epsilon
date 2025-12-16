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

import "io"

// This buffer exists for performance reasons: it makes sure only a single
// allocation is done to parse the immediates across multiple Decode invocations
// and multiple Decoders. Note that this means immediates should never be stored
// in between Decode calls since their values will be corrupted.
var immediatesBuffer []uint64 = make([]uint64, 16)

type decoder struct {
	code []uint64
	pc   uint
}

func newDecoder(code []uint64) *decoder {
	return &decoder{code: code, pc: 0}
}

func (d *decoder) hasMore() bool {
	return d.pc < uint(len(d.code))
}

func (d *decoder) decode() (instruction, error) {
	opcodeValue, err := d.next()
	if err != nil {
		return instruction{}, err
	}
	opcode := opcode(opcodeValue)
	immediates, err := d.readOpcodeImmediates(opcode)
	if err != nil {
		return instruction{}, err
	}
	return instruction{opcode: opcode, immediates: immediates}, nil
}

func (d *decoder) readOpcodeImmediates(opcode opcode) ([]uint64, error) {
	switch opcode {
	case block,
		loop,
		ifOp,
		i32Const,
		br,
		brIf,
		call,
		localGet,
		localSet,
		localTee,
		globalGet,
		globalSet,
		tableGet,
		tableSet,
		memoryFill,
		dataDrop,
		elemDrop,
		tableGrow,
		tableSize,
		tableFill,
		refNull,
		refFunc,
		memorySize,
		memoryGrow,
		i8x16ExtractLaneS,
		i8x16ExtractLaneU,
		i16x8ExtractLaneS,
		i16x8ExtractLaneU,
		i32x4ExtractLane,
		i64x2ExtractLane,
		f32x4ExtractLane,
		f64x2ExtractLane,
		i8x16ReplaceLane,
		i16x8ReplaceLane,
		i32x4ReplaceLane,
		i64x2ReplaceLane,
		f32x4ReplaceLane,
		f64x2ReplaceLane,
		i64Const,
		f32Const,
		f64Const:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case brTable:
		size, err := d.next()
		if err != nil {
			return nil, err
		}
		// Use the shared buffer if the table fits, otherwise allocate.
		totalSize := int(size) + 1 // +1 for default label
		var result []uint64
		if totalSize <= len(immediatesBuffer) {
			result = immediatesBuffer[:totalSize]
		} else {
			result = make([]uint64, totalSize)
		}
		for i := range size {
			result[i], err = d.next()
			if err != nil {
				return nil, err
			}
		}
		result[size], err = d.next() // default label
		if err != nil {
			return nil, err
		}
		return result, nil
	case callIndirect,
		memoryInit,
		memoryCopy,
		tableInit,
		tableCopy,
		v128Const:
		immediate1, err := d.next()
		if err != nil {
			return nil, err
		}
		immediate2, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate1
		immediatesBuffer[1] = immediate2
		return immediatesBuffer[:2], nil
	case i32Load,
		i64Load,
		f32Load,
		f64Load,
		i32Load8S,
		i32Load8U,
		i32Load16S,
		i32Load16U,
		i64Load8S,
		i64Load8U,
		i64Load16S,
		i64Load16U,
		i64Load32S,
		i64Load32U,
		i32Store,
		i64Store,
		f32Store,
		f64Store,
		i32Store8,
		i32Store16,
		i64Store8,
		i64Store16,
		i64Store32,
		v128Load,
		v128Load32Zero,
		v128Load64Zero,
		v128Load8Splat,
		v128Load16Splat,
		v128Load32Splat,
		v128Load64Splat,
		v128Load8x8S,
		v128Load8x8U,
		v128Load16x4S,
		v128Load16x4U,
		v128Load32x2S,
		v128Load32x2U,
		v128Store:
		align, err := d.next()
		if err != nil {
			return nil, err
		}
		memoryIndex, err := d.next()
		if err != nil {
			return nil, err
		}
		offset, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = align
		immediatesBuffer[1] = memoryIndex
		immediatesBuffer[2] = offset
		return immediatesBuffer[:3], nil
	case selectT:
		size, err := d.next()
		if err != nil {
			return nil, err
		}
		var result []uint64
		if int(size) <= len(immediatesBuffer) {
			result = immediatesBuffer[:size]
		} else {
			result = make([]uint64, size)
		}
		for i := range size {
			result[i], err = d.next()
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case v128Load8Lane,
		v128Load16Lane,
		v128Load32Lane,
		v128Load64Lane,
		v128Store8Lane,
		v128Store16Lane,
		v128Store32Lane,
		v128Store64Lane:
		align, err := d.next()
		if err != nil {
			return nil, err
		}
		memoryIndex, err := d.next()
		if err != nil {
			return nil, err
		}
		offset, err := d.next()
		if err != nil {
			return nil, err
		}
		laneIndex, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = align
		immediatesBuffer[1] = memoryIndex
		immediatesBuffer[2] = offset
		immediatesBuffer[3] = laneIndex
		return immediatesBuffer[:4], nil
	case i8x16Shuffle:
		for i := range 16 {
			val, err := d.next()
			if err != nil {
				return nil, err
			}
			immediatesBuffer[i] = val
		}
		return immediatesBuffer, nil
	default:
		return []uint64{}, nil
	}
}

func (d *decoder) next() (uint64, error) {
	if d.pc >= uint(len(d.code)) {
		return 0, io.EOF
	}
	val := d.code[d.pc]
	d.pc++
	return val, nil
}
