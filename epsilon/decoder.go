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
	"errors"
	"fmt"
	"io"
	"math"
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
	code []byte
	pc   uint
}

func newDecoder(code []byte) *decoder {
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
		immediate, err := d.readBlockType()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case i32Const:
		immediate, err := d.readInt32()
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
		immediate, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case memorySize, memoryGrow:
		immediate, err := d.readByte()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = uint64(immediate)
		return immediatesBuffer[:1], nil
	case brTable:
		vector, err := d.readImmediateVector()
		if err != nil {
			return nil, err
		}
		immediate, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return append(vector, immediate), nil
	case callIndirect,
		memoryInit,
		memoryCopy,
		tableInit,
		tableCopy:
		immediate1, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		immediate2, err := d.readUint32()
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
		align, memoryIndex, offset, err := d.readMemArg()
		if err != nil {
			return nil, err
		}

		immediatesBuffer[0] = align
		immediatesBuffer[1] = memoryIndex
		immediatesBuffer[2] = offset
		return immediatesBuffer[:3], nil
	case selectT:
		return d.readImmediateVector()
	case i64Const:
		immediate, err := d.readSleb128(10)
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case f32Const:
		immediate, err := d.readFloat32()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case f64Const:
		immediate, err := d.readFloat64()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case v128Const:
		bytes, err := d.readBytes(16)
		if err != nil {
			return nil, err
		}

		immediatesBuffer[0] = binary.LittleEndian.Uint64(bytes[0:8])
		immediatesBuffer[1] = binary.LittleEndian.Uint64(bytes[8:16])
		return immediatesBuffer[:2], nil
	case v128Load8Lane,
		v128Load16Lane,
		v128Load32Lane,
		v128Load64Lane,
		v128Store8Lane,
		v128Store16Lane,
		v128Store32Lane,
		v128Store64Lane:
		align, memoryIndex, offset, err := d.readMemArg()
		if err != nil {
			return nil, err
		}

		laneIndex, err := d.readUint8()
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
			val, err := d.readUint8()
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
	opcodeByte, err := d.readByte()
	if err != nil {
		return 0, err
	}

	// Standard single-byte opcode.
	if opcodeByte < 0xFC {
		return opcode(opcodeByte), nil
	}

	// Multi-byte opcode (prefixed with 0xFC or 0xFD).
	val, err := d.readUint32()
	if err != nil {
		return 0, err
	}

	var compositeOpcode uint32
	switch opcodeByte {
	case 0xFC:
		compositeOpcode = 0xFC00 + uint32(val)
	case 0xFD:
		compositeOpcode = 0xFD00 + uint32(val)
	default:
		// This case should ideally not be reached if opcodeByte is guaranteed to be
		// < 0xFC or 0xFD. However, as a safeguard, we can return an error.
		return 0, fmt.Errorf("unrecognized multi-byte opcode prefix: 0x%X", opcodeByte)
	}

	return opcode(compositeOpcode), nil
}

func (d *decoder) readImmediateVector() ([]uint64, error) {
	size, err := d.readUint32()
	if err != nil {
		return nil, err
	}

	var immediates []uint64
	if int(size) <= len(immediatesBuffer) {
		immediates = immediatesBuffer[:size]
	} else {
		immediates = make([]uint64, size)
	}

	for i := range size {
		val, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		immediates[i] = val
	}
	return immediates, nil
}

func (d *decoder) readFloat32() (uint64, error) {
	bytes, err := d.readBytes(4)
	if err != nil {
		return 0, err
	}
	return uint64(binary.LittleEndian.Uint32(bytes)), nil
}

func (d *decoder) readFloat64() (uint64, error) {
	bytes, err := d.readBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(bytes), nil
}

func (d *decoder) readMemArg() (uint64, uint64, uint64, error) {
	align, err := d.readUint32()
	if err != nil {
		return 0, 0, 0, err
	}

	// The alignment exponent must be < 32.
	// We also have to remove bit 6, used for multi memory.
	if (align & ^sixthBitMask) >= 32 {
		return 0, 0, 0, errMalformedMemopFlags
	}

	memoryIndex := uint64(0)
	// If bit 6 is set, this instruction is using an explicit memory index.
	// This is relevant in WASM 3.
	if align&sixthBitMask != 0 {
		memoryIndex, err = d.readUint32()
		if err != nil {
			return 0, 0, 0, err
		}
	}

	offset, err := d.readUleb128(10)
	if err != nil {
		return 0, 0, 0, err
	}

	return align, memoryIndex, offset, nil
}

func (d *decoder) readBlockType() (uint64, error) {
	blockType, err := d.readSleb128(5)
	if err != nil {
		return 0, err
	}
	// BlockType is encoded as a 33 bit, signed integer.
	val := int64(blockType)
	const minS33 = -1 << 32
	const maxS33 = (1 << 32) - 1
	if val < minS33 || val > maxS33 {
		return 0, errIntegerTooLarge
	}
	return blockType, nil
}

// readSleb128 decodes a signed 64-bit integer immediate (SLEB128).
func (d *decoder) readSleb128(maxBytes int) (uint64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	bytesRead := 0

	for {
		b, err = d.readByte()
		if err != nil {
			return 0, err
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, errIntRepresentationTooLong
		}

		// Each byte read contains 7 bits of "integer" and 1 bit to signal if the
		// parsing should continue. When reading int64, we can read up to
		// ceil(64/7) = 10 bytes. The last 10th byte will contain 1 continuation bit
		// (the most significant bit), 6 bits we should not use and the final, least
		// significant bit that we should interpret as the last 64th bit of the
		// integer we are tying to parse, the sign bit. The remaining 6 bits should
		// be all 0s for positive integers and all 1s for negative integers.
		if bytesRead == 10 {
			sign := b & 1
			remainingBits := (b & 0x7E) >> 1
			if sign == 0 && remainingBits != 0 {
				return 0, errIntegerTooLarge
			} else if sign == 1 && remainingBits != 0x3F {
				return 0, errIntegerTooLarge
			}
		}

		result |= int64(b&payloadMask) << shift

		// Check the continuation bit (MSB). If it's 0, this is the last byte.
		if (b & continuationBit) == 0 {
			break
		}

		shift += 7
	}

	if (b & signBit) != 0 {
		result |= -1 << (shift + 7)
	}

	return uint64(result), nil
}

// readUint32 still returns a uint64, but checks that the value can be
// interpreted as a WASM u32.
func (d *decoder) readUint32() (uint64, error) {
	val, err := d.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint32 {
		return 0, errIntegerTooLarge
	}
	return val, nil
}

func (d *decoder) readInt32() (uint64, error) {
	val, err := d.readSleb128(5)
	if err != nil {
		return 0, err
	}
	if int64(val) < math.MinInt32 || int64(val) > math.MaxInt32 {
		return 0, errIntegerTooLarge
	}
	return val, nil
}

// readUint8 still returns a uint64, but checks that the value can be
// interpreted as a WASM u8.
func (d *decoder) readUint8() (uint64, error) {
	val, err := d.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint8 {
		return 0, errIntegerTooLarge
	}
	return val, nil
}

// readUleb128 decodes an unsigned 64-bit integer immediate (ULEB128).
func (d *decoder) readUleb128(maxBytes int) (uint64, error) {
	var result uint64
	var shift uint
	bytesRead := 0

	for {
		b, err := d.readByte()
		if err != nil {
			return 0, err
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, errIntRepresentationTooLong
		}

		group := uint64(b & payloadMask)
		result |= group << shift

		// If the continuation bit (MSB) is 0, we are done.
		if (b & continuationBit) == 0 {
			return result, nil
		}

		shift += 7
	}
}

func (d *decoder) readByte() (byte, error) {
	if !d.hasMore() {
		return 0, io.EOF
	}
	b := d.code[d.pc]
	d.pc++
	return b, nil
}

func (d *decoder) readBytes(n uint) ([]byte, error) {
	if d.pc+n > uint(len(d.code)) {
		return nil, io.EOF
	}
	bytes := d.code[d.pc : d.pc+n]
	d.pc += n
	return bytes, nil
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
