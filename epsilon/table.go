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

var errTableOutOfBounds = errors.New("out of bounds table access")

// Table represents a WebAssembly table instance.
type Table struct {
	Type     TableType
	elements []int32
}

// NewTable creates a new Table instance from a TableType.
// The table is initialized with 'NullReference' elements up to the min size.
func NewTable(tt TableType) *Table {
	elements := make([]int32, tt.Limits.Min)
	for i := range elements {
		elements[i] = NullReference
	}
	return &Table{Type: tt, elements: elements}
}

// Get returns the element at the given index.
func (t *Table) Get(index int32) (int32, error) {
	if index < 0 || index >= int32(len(t.elements)) {
		return 0, errTableOutOfBounds
	}
	return t.elements[index], nil
}

// Set places a value at the given index.
func (t *Table) Set(index int32, value int32) error {
	if index < 0 || index >= int32(len(t.elements)) {
		return errTableOutOfBounds
	}
	t.elements[index] = value
	return nil
}

func (t *Table) Size() int32 {
	return int32(len(t.elements))
}

// Grow increases the table size by n, initializing new elements with val.
// It returns the previous size on success, or -1 if the growth is not possible.
func (t *Table) Grow(n int32, val int32) int32 {
	if n < 0 {
		return -1
	}
	previousSize := t.Size()
	if t.Type.Limits.Max != nil {
		if uint32(previousSize)+uint32(n) > *t.Type.Limits.Max {
			return -1
		}
	}

	for range n {
		t.elements = append(t.elements, val)
	}

	return previousSize
}

// Init copies elements from a slice of function indexes into the table.
func (t *Table) Init(
	n, tableStartIndex, funcStartIndex int32,
	funcIndexes []int32,
) error {
	if n < 0 || tableStartIndex < 0 || funcStartIndex < 0 ||
		uint64(uint32(funcStartIndex))+uint64(uint32(n)) >
			uint64(len(funcIndexes)) ||
		uint64(uint32(tableStartIndex))+uint64(uint32(n)) > uint64(t.Size()) {
		return errTableOutOfBounds
	}

	for i := range n {
		t.elements[tableStartIndex+i] = funcIndexes[funcStartIndex+i]
	}
	return nil
}

// InitFromSlice copies an entire slice of function indexes into the table at a
// starting index.
func (t *Table) InitFromSlice(startIndex int32, funcIndexes []int32) error {
	return t.Init(int32(len(funcIndexes)), startIndex, 0, funcIndexes)
}

// Copy copies n elements from a source table to a destination table.
func (t *Table) Copy(
	destTable *Table,
	n, sourceStartIndex, destStartIndex int32,
) error {
	if n < 0 || sourceStartIndex < 0 || destStartIndex < 0 {
		return errTableOutOfBounds
	}

	sourceEnd := uint64(uint32(sourceStartIndex)) + uint64(uint32(n))
	destEnd := uint64(uint32(destStartIndex)) + uint64(uint32(n))
	if sourceEnd > uint64(t.Size()) || destEnd > uint64(destTable.Size()) {
		return errTableOutOfBounds
	}

	copy(
		destTable.elements[destStartIndex:destStartIndex+n],
		t.elements[sourceStartIndex:sourceStartIndex+n],
	)
	return nil
}

// Fill sets n elements to a given value, starting from an index.
func (t *Table) Fill(n, startIndex int32, val int32) error {
	if uint64(uint32(startIndex))+uint64(uint32(n)) > uint64(uint32(t.Size())) {
		return errTableOutOfBounds
	}

	for i := range n {
		t.elements[startIndex+i] = val
	}
	return nil
}
