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

// Table represents a WebAssembly table instance.
type Table struct {
	Type     TableType
	Elements []any
}

// NewTable creates a new Table instance from a TableType.
// The table is initialized with 'NullVal' elements up to the minimum size.
func NewTable(tt TableType) *Table {
	elements := make([]any, tt.Limits.Min)
	for i := range elements {
		elements[i] = NullVal
	}
	return &Table{Type: tt, Elements: elements}
}

// Get returns the element at the given index.
func (t *Table) Get(index int32) (any, error) {
	if index < 0 || index >= int32(len(t.Elements)) {
		return nil, ErrTableOutOfBounds
	}
	return t.Elements[index], nil
}

// Set places a value at the given index.
func (t *Table) Set(index int32, value any) error {
	if index < 0 || index >= int32(len(t.Elements)) {
		return ErrTableOutOfBounds
	}
	t.Elements[index] = value
	return nil
}

func (t *Table) Size() int32 {
	return int32(len(t.Elements))
}

// Grow increases the table size by n, initializing new elements with val.
// It returns the previous size on success, or -1 if the growth is not possible.
func (t *Table) Grow(n int32, val any) int32 {
	if n < 0 {
		return -1
	}
	previousSize := t.Size()
	if t.Type.Limits.Max != nil {
		if uint32(previousSize)+uint32(n) > *t.Type.Limits.Max {
			return -1
		}
	}

	newElements := make([]any, n)
	for i := range newElements {
		newElements[i] = val
	}
	t.Elements = append(t.Elements, newElements...)

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
		return ErrTableOutOfBounds
	}

	for i := range n {
		t.Elements[tableStartIndex+i] = funcIndexes[funcStartIndex+i]
	}
	return nil
}

// InitFromSlice copies an entire slice of function indexes into the table at a
// starting index.
func (t *Table) InitFromSlice(startIndex int32, funcIndexes []int32) error {
	return t.Init(int32(len(funcIndexes)), startIndex, 0, funcIndexes)
}

// InitFromAnySlice copies an entire slice of any values (function indexes or
// nils) into the table at a starting index.
func (t *Table) InitFromAnySlice(startIndex int32, values []any) error {
	if startIndex < 0 || startIndex > t.Size() ||
		uint64(uint32(startIndex))+uint64(len(values)) > uint64(t.Size()) {
		return ErrTableOutOfBounds
	}

	for i, val := range values {
		t.Elements[startIndex+int32(i)] = val
	}
	return nil
}

// Copy copies n elements from a source table to a destination table.
func (t *Table) Copy(
	destTable *Table,
	n, sourceStartIndex, destStartIndex int32,
) error {
	if n < 0 || sourceStartIndex < 0 || destStartIndex < 0 {
		return ErrTableOutOfBounds
	}

	sourceEnd := uint64(uint32(sourceStartIndex)) + uint64(uint32(n))
	destEnd := uint64(uint32(destStartIndex)) + uint64(uint32(n))
	if sourceEnd > uint64(t.Size()) || destEnd > uint64(destTable.Size()) {
		return ErrTableOutOfBounds
	}

	copy(
		destTable.Elements[destStartIndex:destStartIndex+n],
		t.Elements[sourceStartIndex:sourceStartIndex+n],
	)
	return nil
}

// Fill sets n elements to a given value, starting from an index.
func (t *Table) Fill(n, startIndex int32, val any) error {
	if uint64(uint32(startIndex))+uint64(uint32(n)) > uint64(uint32(t.Size())) {
		return ErrTableOutOfBounds
	}

	for i := range n {
		t.Elements[startIndex+i] = val
	}
	return nil
}
