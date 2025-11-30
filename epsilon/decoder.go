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
	ErrUnexpectedEOF            = errors.New("unexpected end of byte stream")
	ErrIntRepresentationTooLong = errors.New("integer representation too long")
	ErrIntegerTooLarge          = errors.New("integer too large")
	ErrMalformedMemopFlags      = errors.New("malformed memop flags")
)

const (
	continuationBit = 0x80
	payloadMask     = 0x7F
	signBit         = 0x40
	sixthBitMask    = uint64(1 << 6)
)

// This buffer exists for perfomance reasons: it makes sure only a single
// allocation is done to parse the immediates across multiple Decode
// invocations and multiple Decoders. Note that this means immediates should
// never be stored in between Decode calls since their values will be corrupted.
var immediatesBuffer []uint64 = make([]uint64, 16)

type Decoder struct {
	Code []byte
	Pc   uint
}

func NewDecoder(code []byte) *Decoder {
	return &Decoder{Code: code, Pc: 0}
}

func (d *Decoder) HasMore() bool {
	return d.Pc < uint(len(d.Code))
}

func (d *Decoder) Decode() (Instruction, error) {
	opcode, err := d.readOpcode()
	if err != nil {
		return Instruction{}, err
	}
	immediates, err := d.readOpcodeImmediates(opcode)
	if err != nil {
		return Instruction{}, err
	}
	return Instruction{Opcode: opcode, Immediates: immediates}, nil
}

func (d *Decoder) readOpcodeImmediates(opcode Opcode) ([]uint64, error) {
	switch opcode {
	case Block, Loop, If:
		immediate, err := d.readBlockType()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case I32Const:
		immediate, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case Br,
		BrIf,
		Call,
		LocalGet,
		LocalSet,
		LocalTee,
		GlobalGet,
		GlobalSet,
		TableGet,
		TableSet,
		MemoryFill,
		DataDrop,
		ElemDrop,
		TableGrow,
		TableSize,
		TableFill,
		RefNull,
		RefFunc,
		I8x16ExtractLaneS,
		I8x16ExtractLaneU,
		I16x8ExtractLaneS,
		I16x8ExtractLaneU,
		I32x4ExtractLane,
		I64x2ExtractLane,
		F32x4ExtractLane,
		F64x2ExtractLane,
		I8x16ReplaceLane,
		I16x8ReplaceLane,
		I32x4ReplaceLane,
		I64x2ReplaceLane,
		F32x4ReplaceLane,
		F64x2ReplaceLane:
		immediate, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case MemorySize, MemoryGrow:
		immediate, err := d.readByte()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = uint64(immediate)
		return immediatesBuffer[:1], nil
	case BrTable:
		vector, err := d.readImmediateVector()
		if err != nil {
			return nil, err
		}
		immediate, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return append(vector, immediate), nil
	case CallIndirect,
		MemoryInit,
		MemoryCopy,
		TableInit,
		TableCopy:
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
	case I32Load,
		I64Load,
		F32Load,
		F64Load,
		I32Load8S,
		I32Load8U,
		I32Load16S,
		I32Load16U,
		I64Load8S,
		I64Load8U,
		I64Load16S,
		I64Load16U,
		I64Load32S,
		I64Load32U,
		I32Store,
		I64Store,
		F32Store,
		F64Store,
		I32Store8,
		I32Store16,
		I64Store8,
		I64Store16,
		I64Store32,
		V128Load,
		V128Load32Zero,
		V128Load64Zero,
		V128Load8Splat,
		V128Load16Splat,
		V128Load32Splat,
		V128Load64Splat,
		V128Load8x8S,
		V128Load8x8U,
		V128Load16x4S,
		V128Load16x4U,
		V128Load32x2S,
		V128Load32x2U,
		V128Store:
		align, memoryIndex, offset, err := d.readMemArg()
		if err != nil {
			return nil, err
		}

		immediatesBuffer[0] = align
		immediatesBuffer[1] = memoryIndex
		immediatesBuffer[2] = offset
		return immediatesBuffer[:3], nil
	case SelectT:
		return d.readImmediateVector()
	case I64Const:
		immediate, err := d.readSleb128(10)
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case F32Const:
		immediate, err := d.readFloat32()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case F64Const:
		immediate, err := d.readFloat64()
		if err != nil {
			return nil, err
		}
		immediatesBuffer[0] = immediate
		return immediatesBuffer[:1], nil
	case V128Const:
		bytes, err := d.readBytes(16)
		if err != nil {
			return nil, err
		}

		immediatesBuffer[0] = binary.LittleEndian.Uint64(bytes[0:8])
		immediatesBuffer[1] = binary.LittleEndian.Uint64(bytes[8:16])
		return immediatesBuffer[:2], nil
	case V128Load8Lane,
		V128Load16Lane,
		V128Load32Lane,
		V128Load64Lane,
		V128Store8Lane,
		V128Store16Lane,
		V128Store32Lane,
		V128Store64Lane:
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
	case I8x16Shuffle:
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
func (d *Decoder) readOpcode() (Opcode, error) {
	opcode, err := d.readByte()
	if err != nil {
		return 0, err
	}

	// Standard single-byte opcode.
	if opcode < 0xFC {
		return Opcode(opcode), nil
	}

	// Multi-byte opcode (prefixed with 0xFC or 0xFD).
	val, err := d.readUint32()
	if err != nil {
		return 0, err
	}

	var compositeOpcode uint32
	switch opcode {
	case 0xFC:
		compositeOpcode = 0xFC00 + uint32(val)
	case 0xFD:
		compositeOpcode = 0xFD00 + uint32(val)
	default:
		// This case should ideally not be reached if opcodeByte is guaranteed to be
		// < 0xFC or 0xFD. However, as a safeguard, we can return an error.
		return 0, fmt.Errorf("unrecognized multi-byte opcode prefix: 0x%X", opcode)
	}

	return Opcode(compositeOpcode), nil
}

func (d *Decoder) readImmediateVector() ([]uint64, error) {
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

func (d *Decoder) readFloat32() (uint64, error) {
	bytes, err := d.readBytes(4)
	if err != nil {
		return 0, err
	}
	return uint64(binary.LittleEndian.Uint32(bytes)), nil
}

func (d *Decoder) readFloat64() (uint64, error) {
	bytes, err := d.readBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(bytes), nil
}

func (d *Decoder) readMemArg() (uint64, uint64, uint64, error) {
	align, err := d.readUint32()
	if err != nil {
		return 0, 0, 0, err
	}

	// The alignment exponent must be < 32.
	// We also have to remove bit 6, used for multi memory.
	if (align & ^sixthBitMask) >= 32 {
		return 0, 0, 0, ErrMalformedMemopFlags
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

func (d *Decoder) readBlockType() (uint64, error) {
	blockType, err := d.readSleb128(5)
	if err != nil {
		return 0, err
	}
	// BlockType is encoded as a 33 bit, signed integer.
	val := int64(blockType)
	const minS33 = -1 << 32
	const maxS33 = (1 << 32) - 1
	if val < minS33 || val > maxS33 {
		return 0, ErrIntegerTooLarge
	}
	return blockType, nil
}

// readSleb128 decodes a signed 64-bit integer immediate (SLEB128).
func (d *Decoder) readSleb128(maxBytes int) (uint64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	bytesRead := 0

	for {
		b, err = d.readByte()
		if err != nil {
			return 0, ErrUnexpectedEOF // Reached end of stream mid-integer.
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, ErrIntRepresentationTooLong
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
				return 0, ErrIntegerTooLarge
			} else if sign == 1 && remainingBits != 0x3F {
				return 0, ErrIntegerTooLarge
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
func (d *Decoder) readUint32() (uint64, error) {
	val, err := d.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint32 {
		return 0, ErrIntegerTooLarge
	}
	return val, nil
}

func (d *Decoder) readInt32() (uint64, error) {
	val, err := d.readSleb128(5)
	if err != nil {
		return 0, err
	}
	if int64(val) < math.MinInt32 || int64(val) > math.MaxInt32 {
		return 0, ErrIntegerTooLarge
	}
	return val, nil
}

// readUint8 still returns a uint64, but checks that the value can be
// interpreted as a WASM u8.
func (d *Decoder) readUint8() (uint64, error) {
	val, err := d.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint8 {
		return 0, ErrIntegerTooLarge
	}
	return val, nil
}

// readUleb128 decodes an unsigned 64-bit integer immediate (ULEB128).
func (d *Decoder) readUleb128(maxBytes int) (uint64, error) {
	var result uint64
	var shift uint
	bytesRead := 0

	for {
		b, err := d.readByte()
		if err != nil {
			return 0, ErrUnexpectedEOF // Reached end of stream mid-integer.
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, ErrIntRepresentationTooLong
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

func (d *Decoder) readByte() (byte, error) {
	if !d.HasMore() {
		return 0, io.EOF
	}
	b := d.Code[d.Pc]
	d.Pc++
	return b, nil
}

func (d *Decoder) readBytes(n uint) ([]byte, error) {
	if d.Pc+n > uint(len(d.Code)) {
		return nil, io.EOF
	}
	bytes := d.Code[d.Pc : d.Pc+n]
	d.Pc += n
	return bytes, nil
}

// DecodeUntilMatchingEnd continues decoding instructions until the matching
// 'end' of the current block is found. The next instruction to be decoded will
// be the one AFTER the 'end' instruction.
func (d *Decoder) DecodeUntilMatchingEnd() error {
	nesting := 1
	for nesting > 0 {
		instruction, err := d.Decode()
		if err != nil {
			return err
		}
		opcode := instruction.Opcode
		switch opcode {
		case End:
			nesting--
		case If, Block, Loop:
			nesting++
		}
	}
	return nil
}

// DecodeUntilMatchingElseOrEnd continues decoding instructions until the
// matching 'else' or 'end' of the current block is found. It returns the opcode
// found (either Else or End). The next instruction to be decoded will be the
// found instruction itself.
func (d *Decoder) DecodeUntilMatchingElseOrEnd() (Opcode, error) {
	nesting := 1
	var lastPc uint
	var lastOpcode Opcode
	for nesting > 0 {
		lastPc = d.Pc
		instruction, err := d.Decode()
		if err != nil {
			return 0, err
		}
		lastOpcode = instruction.Opcode
		switch lastOpcode {
		case Else:
			// Only decrement nesting for an 'else' if it matches the 'if' we are
			// currently scanning for.
			if nesting == 1 {
				nesting--
			}
		case End:
			nesting--
		case If, Block, Loop:
			nesting++
		}
	}
	// We rewind the PC to the last instruction parsed.
	d.Pc = lastPc
	return lastOpcode, nil
}
