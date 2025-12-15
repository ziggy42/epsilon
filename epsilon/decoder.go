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
	"io"
)

var (
	errIntRepresentationTooLong = errors.New("integer representation too long")
	errIntegerTooLarge          = errors.New("integer too large")
	errMalformedMemopFlags      = errors.New("malformed memop flags")
)

const (
	continuationBit = 0x80
	payloadMask     = 0x7F
	signBit         = 0x40
	sixthBitMask    = uint64(1 << 6)
)

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
	opcode, err := d.readOpcode()
	if err != nil {
		return instruction{}, err
	}
	immediates, err := d.readOpcodeImmediates(opcode)
	if err != nil {
		return instruction{}, err
	}
	return instruction{opcode: opcode, immediates: immediates}, nil
}

func (d *decoder) readOpcodeImmediates(opcode opcode) ([]uint64, error) {
	switch opcode {
	case block, loop, ifOp:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case i32Const:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case br,
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
		f64x2ReplaceLane:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case memorySize, memoryGrow:
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
		// TODO remove this allocation
		vector := make([]uint64, size)
		for i := uint64(0); i < size; i++ {
			vector[i], err = d.next()
			if err != nil {
				return nil, err
			}
		}
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		return append(vector, immediate), nil
	case callIndirect,
		memoryInit,
		memoryCopy,
		tableInit,
		tableCopy:
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
		// TODO remove this allocation
		vector := make([]uint64, size)
		for i := uint64(0); i < size; i++ {
			vector[i], err = d.next()
			if err != nil {
				return nil, err
			}
		}
		return vector, nil
	case i64Const:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case f32Const:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case f64Const:
		immediate, err := d.next()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case v128Const:
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

// readOpcode reads the next Opcode from the byte stream.
func (d *decoder) readOpcode() (opcode, error) {
	opcodeValue, err := d.next()
	if err != nil {
		return 0, err
	}
	return opcode(opcodeValue), nil
}

func (d *decoder) next() (uint64, error) {
	if d.pc >= uint(len(d.code)) {
		return 0, io.EOF
	}
	val := d.code[d.pc]
	d.pc++
	return val, nil
}

// decodeUntilMatchingEnd continues decoding instructions until the matching
// 'end' of the current block is found. The next instruction to be decoded will
// be the one AFTER the 'end' instruction.
func (d *decoder) decodeUntilMatchingEnd() error {
	nesting := 1
	for nesting > 0 {
		instruction, err := d.decode()
		if err != nil {
			return err
		}
		opcode := instruction.opcode
		switch opcode {
		case end:
			nesting--
		case ifOp, block, loop:
			nesting++
		}
	}
	return nil
}

// decodeUntilMatchingElseOrEnd continues decoding instructions until the
// matching 'else' or 'end' of the current block is found. It returns the opcode
// found (either Else or End). The next instruction to be decoded will be the
// found instruction itself.
func (d *decoder) decodeUntilMatchingElseOrEnd() (opcode, error) {
	nesting := 1
	var lastPc uint
	var lastOpcode opcode
	for nesting > 0 {
		lastPc = d.pc
		instruction, err := d.decode()
		if err != nil {
			return 0, err
		}
		lastOpcode = instruction.opcode
		switch lastOpcode {
		case elseOp:
			// Only decrement nesting for an 'else' if it matches the 'if' we are
			// currently scanning for.
			if nesting == 1 {
				nesting--
			}
		case end:
			nesting--
		case ifOp, block, loop:
			nesting++
		}
	}
	// We rewind the PC to the last instruction parsed.
	d.pc = lastPc
	return lastOpcode, nil
}
