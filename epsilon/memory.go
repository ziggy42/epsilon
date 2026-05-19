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
)

const (
	// pageSize defines the size of a WebAssembly page in bytes (64KiB).
	pageSize = 65536
	// maxPages defines the maximum number of pages allowed.
	maxPages = uint32(1 << 15)
)

var errMemoryOutOfBounds = errors.New("out of bounds memory access")

// Memory represents a linear memory instance.
type Memory struct {
	Limits Limits
	data   []byte

	// owner identifies which vm and therefore which runtime this instance
	// belongs to.
	owner *vm
}

// newMemory creates a new Memory instance from a MemoryType.
func newMemory(owner *vm, memType MemoryType) *Memory {
	size := uint64(memType.Limits.Min) * pageSize
	data := make([]byte, size)
	return &Memory{owner: owner, Limits: memType.Limits, data: data}
}

// Grow extends the memory by the given number of pages.
// It returns the original size in pages if successful, otherwise -1.
func (m *Memory) Grow(pages int32) int32 {
	if pages < 0 {
		return -1
	}
	currentSize := uint32(m.Size())
	max := maxPages
	if m.Limits.Max != nil {
		max = *m.Limits.Max
	}

	newSizePages := uint64(pages) + uint64(currentSize)
	if newSizePages > uint64(max) {
		return -1
	}

	growthBytes := uint64(pages) * pageSize
	// Ensure we don't overflow the host's 'int' type which is used by make/append
	if uint64(int(growthBytes)) != growthBytes {
		return -1
	}

	// Append a new zero-initialized slice of the required size.
	m.data = append(m.data, make([]byte, growthBytes)...)
	return int32(currentSize)
}

// Size returns the size of the memory in pages.
func (m *Memory) Size() int32 {
	return int32(len(m.data) / pageSize)
}

// Set writes the given byte slice into memory.
func (m *Memory) Set(offset, index uint32, values []byte) error {
	dst, err := m.Get(offset, index, uint32(len(values)))
	if err != nil {
		return err
	}
	copy(dst, values)
	return nil
}

// Get reads data from memory.
func (m *Memory) Get(offset, index, length uint32) ([]byte, error) {
	start := uint64(index) + uint64(offset)
	end := start + uint64(length)
	if end > uint64(len(m.data)) {
		return nil, errMemoryOutOfBounds
	}
	return m.data[start:end], nil
}

// Init copies n bytes from a data segment to the memory.
func (m *Memory) Init(n, srcOffset, destOffset uint32, content []byte) error {
	if uint64(srcOffset)+uint64(n) > uint64(len(content)) {
		return errMemoryOutOfBounds
	}
	dst, err := m.Get(destOffset, 0, n)
	if err != nil {
		return err
	}
	copy(dst, content[srcOffset:srcOffset+n])
	return nil
}

// Copy copies n elements from a source memory to a destination memory.
func (m *Memory) Copy(dest *Memory, n, srcOffset, destOffset uint32) error {
	src, err := m.Get(srcOffset, 0, n)
	if err != nil {
		return err
	}
	dst, err := dest.Get(destOffset, 0, n)
	if err != nil {
		return err
	}
	copy(dst, src)
	return nil
}

// Fill sets n elements to a given value.
func (m *Memory) Fill(n, offset uint32, val byte) error {
	mem, err := m.Get(offset, 0, n)
	if err != nil {
		return err
	}
	for i := range mem {
		mem[i] = val
	}
	return nil
}

func (m *Memory) LoadByte(offset, index uint32) (byte, error) {
	addr := uint64(index) + uint64(offset)
	if addr >= uint64(len(m.data)) {
		return 0, errMemoryOutOfBounds
	}
	return m.data[addr], nil
}

func (m *Memory) LoadUint16(offset, index uint32) (uint16, error) {
	addr := uint64(index) + uint64(offset)
	if addr+2 > uint64(len(m.data)) {
		return 0, errMemoryOutOfBounds
	}
	return binary.LittleEndian.Uint16(m.data[addr : addr+2]), nil
}

func (m *Memory) LoadUint32(offset, index uint32) (uint32, error) {
	addr := uint64(index) + uint64(offset)
	if addr+4 > uint64(len(m.data)) {
		return 0, errMemoryOutOfBounds
	}
	return binary.LittleEndian.Uint32(m.data[addr : addr+4]), nil
}

func (m *Memory) LoadUint64(offset, index uint32) (uint64, error) {
	addr := uint64(index) + uint64(offset)
	if addr+8 > uint64(len(m.data)) {
		return 0, errMemoryOutOfBounds
	}
	return binary.LittleEndian.Uint64(m.data[addr : addr+8]), nil
}

func (m *Memory) LoadV128(offset, index uint32) (V128Value, error) {
	addr := uint64(index) + uint64(offset)
	if addr+16 > uint64(len(m.data)) {
		return V128Value{}, errMemoryOutOfBounds
	}
	return V128Value{
		Low:  binary.LittleEndian.Uint64(m.data[addr : addr+8]),
		High: binary.LittleEndian.Uint64(m.data[addr+8 : addr+16]),
	}, nil
}

func (m *Memory) StoreByte(offset, index uint32, val byte) error {
	addr := uint64(index) + uint64(offset)
	if addr >= uint64(len(m.data)) {
		return errMemoryOutOfBounds
	}
	m.data[addr] = val
	return nil
}

func (m *Memory) StoreUint16(offset, index uint32, val uint16) error {
	addr := uint64(index) + uint64(offset)
	if addr+2 > uint64(len(m.data)) {
		return errMemoryOutOfBounds
	}
	binary.LittleEndian.PutUint16(m.data[addr:addr+2], val)
	return nil
}

func (m *Memory) StoreUint32(offset, index uint32, val uint32) error {
	addr := uint64(index) + uint64(offset)
	if addr+4 > uint64(len(m.data)) {
		return errMemoryOutOfBounds
	}
	binary.LittleEndian.PutUint32(m.data[addr:addr+4], val)
	return nil
}

func (m *Memory) StoreUint64(offset, index uint32, val uint64) error {
	addr := uint64(index) + uint64(offset)
	if addr+8 > uint64(len(m.data)) {
		return errMemoryOutOfBounds
	}
	binary.LittleEndian.PutUint64(m.data[addr:addr+8], val)
	return nil
}

func (m *Memory) StoreV128(offset, index uint32, val V128Value) error {
	addr := uint64(index) + uint64(offset)
	if addr+16 > uint64(len(m.data)) {
		return errMemoryOutOfBounds
	}
	binary.LittleEndian.PutUint64(m.data[addr:addr+8], val.Low)
	binary.LittleEndian.PutUint64(m.data[addr+8:addr+16], val.High)
	return nil
}
