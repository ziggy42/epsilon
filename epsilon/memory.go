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

import "errors"

const (
	// pageSize defines the size of a WebAssembly page in bytes (64KiB).
	pageSize = 65536
	// maxPages defines the maximum number of pages allowed.
	maxPages = uint32(1 << 15)
)

var ErrMemoryOutOfBounds = errors.New("out of bounds memory access")

// Memory represents a linear memory instance.
// https://webassembly.github.io/spec/core/exec/runtime.html#memory-instances
type Memory struct {
	Limits Limits
	data   []byte
}

// NewMemory creates a new Memory instance from a MemoryType.
func NewMemory(memType MemoryType) *Memory {
	return &Memory{
		Limits: memType.Limits,
		data:   make([]byte, memType.Limits.Min*pageSize),
	}
}

// Grow extends the memory by the given number of pages.
// It returns the original size in pages if successful, otherwise -1.
func (m *Memory) Grow(pages int32) int32 {
	currentSize := m.Size()
	max := maxPages
	if m.Limits.Max != nil {
		max = *m.Limits.Max
	}

	if uint32(pages)+uint32(currentSize) > max {
		return -1
	}
	// Append a new zero-initialized slice of the required size.
	m.data = append(m.data, make([]byte, pages*pageSize)...)
	return currentSize
}

// Size returns the size of the memory in pages.
func (m *Memory) Size() int32 {
	return int32(len(m.data) / pageSize)
}

// bytesSize returns the size of the memory in bytes.
func (m *Memory) bytesSize() uint64 {
	return uint64(len(m.data))
}

// Set writes the given byte slice into memory starting at the specified index.
// It returns an ErrOutOfBounds if the write goes beyond the memory bounds.
func (m *Memory) Set(offset, index uint32, values []byte) error {
	// Perform the addition using uint64 to correctly handle potential overflow.
	startIndex := uint64(index) + uint64(offset)
	if startIndex+uint64(len(values)) > m.bytesSize() {
		return ErrMemoryOutOfBounds
	}
	copy(m.data[startIndex:], values)
	return nil
}

// Get reads data from memory between the start and end indices (exclusive).
// It returns a copy of the data or an ErrOutOfBounds if the read is invalid.
func (m *Memory) Get(offset, index, length uint32) ([]byte, error) {
	// Perform the addition using uint64 to correctly handle potential overflow.
	startIndex := uint64(index) + uint64(offset)
	endIndex := startIndex + uint64(length)
	if endIndex > m.bytesSize() {
		return nil, ErrMemoryOutOfBounds
	}
	return m.data[startIndex:endIndex], nil
}

// Init copies n bytes from a data segment to the memory.
func (m *Memory) Init(n, srcOffset, destOffset uint32, content []byte) error {
	if uint64(srcOffset)+uint64(n) > uint64(len(content)) ||
		uint64(destOffset)+uint64(n) > m.bytesSize() {
		return ErrMemoryOutOfBounds
	}
	copy(m.data[destOffset:destOffset+n], content[srcOffset:srcOffset+n])
	return nil
}

// Copy copies n elements from a source memory to a destination memory.
func (m *Memory) Copy(
	destMemory *Memory,
	n, srcOffset, destOffset uint32,
) error {
	if uint64(srcOffset)+uint64(n) > m.bytesSize() ||
		uint64(destOffset)+uint64(n) > destMemory.bytesSize() {
		return ErrMemoryOutOfBounds
	}

	copy(
		destMemory.data[destOffset:destOffset+n],
		m.data[srcOffset:srcOffset+n],
	)
	return nil
}

// Fill sets n elements to a given value, starting from an index.
func (m *Memory) Fill(n, offset uint32, val byte) error {
	if uint64(offset)+uint64(n) > m.bytesSize() {
		return ErrMemoryOutOfBounds
	}

	for i := range n {
		m.data[offset+i] = val
	}
	return nil
}
