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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
)

var ErrElementKindNotZero = errors.New("element kind for passive element segment must be 0x00")

const (
	wasmMagicNumber      = "\x00asm"
	supportedWasmVersion = 1
	defaultTableIndex    = 0
)

// SectionId represents the different sections of a WebAssembly module.
// See https://webassembly.github.io/spec/core/binary/modules.html#sections
type SectionId byte

const (
	CustomSectionId SectionId = iota
	TypeSectionId
	ImportSectionId
	FunctionSectionId
	TableSectionId
	MemorySectionId
	GlobalSectionId
	ExportSectionId
	StartSectionId
	ElementSectionId
	CodeSectionId
	DataSectionId
	DataCountSectionId
)

// Parser is a parser for WASM modules.
type Parser struct {
	reader *bufio.Reader
}

func NewParser(reader io.Reader) *Parser {
	return &Parser{reader: bufio.NewReader(reader)}
}

// Parse takes a byte slice and returns a Module.
func (p *Parser) Parse() (*Module, error) {
	if err := p.parseHeader(); err != nil {
		return nil, err
	}

	var types []FunctionType
	var functionTypeIndexes []uint32
	var imports []Import
	var exports []Export
	var startIndex *uint32
	var tables []TableType
	var memories []MemoryType
	var functions []Function
	var elementSegments []ElementSegment
	var globals []GlobalVariable
	var dataSegments []DataSegment
	var dataCount *uint64

	for {
		sectionIdByte, err := p.reader.ReadByte()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read section ID: %w", err)
		}

		sectionId := SectionId(sectionIdByte)
		payloadLen, err := p.parseUleb128()
		if err != nil {
			return nil, fmt.Errorf("failed to read payload length: %w", err)
		}
		switch sectionId {
		case CustomSectionId:
			// CustomSection is just ignored.
			_, err = io.CopyN(io.Discard, p.reader, int64(payloadLen))
			if err != nil {
				return nil, fmt.Errorf("failed to skip custom section: %w", err)
			}
		case TypeSectionId:
			types, err = parseVector(p, p.parseFunctionType)
			if err != nil {
				return nil, err
			}
		case ImportSectionId:
			imports, err = parseVector(p, p.parseImport)
			if err != nil {
				return nil, err
			}
		case FunctionSectionId:
			functionTypeIndexes, err = parseVector(p, p.parseIndex)
			if err != nil {
				return nil, err
			}
		case TableSectionId:
			tables, err = parseVector(p, p.parseTableType)
			if err != nil {
				return nil, err
			}
		case MemorySectionId:
			memories, err = parseVector(p, p.parseMemoryType)
			if err != nil {
				return nil, err
			}
		case GlobalSectionId:
			globals, err = parseVector(p, p.parseGlobalVariable)
			if err != nil {
				return nil, err
			}
		case ExportSectionId:
			exports, err = parseVector(p, p.parseExport)
			if err != nil {
				return nil, err
			}
		case StartSectionId:
			index, err := p.parseIndex()
			if err != nil {
				return nil, err
			}
			startIndex = &index
		case ElementSectionId:
			elementSegments, err = parseVector(p, p.parseElementSegment)
			if err != nil {
				return nil, err
			}
		case CodeSectionId:
			functions, err = parseVector(p, p.parseFunction)
			if err != nil {
				return nil, err
			}
		case DataSectionId:
			dataSegments, err = parseVector(p, p.parseDataSegment)
			if err != nil {
				return nil, err
			}
		case DataCountSectionId:
			count, err := p.parseUleb128()
			if err != nil {
				return nil, err
			}

			dataCount = &count
		default:
			return nil, fmt.Errorf("section %d not implemented", sectionId)
		}
	}

	if dataCount != nil && *dataCount != uint64(len(dataSegments)) {
		return nil, fmt.Errorf("inconsistent data count")
	}

	if len(functionTypeIndexes) != len(functions) {
		return &Module{}, fmt.Errorf("incompatible number of func indexes/bodies")
	}

	for i := range functions {
		functions[i].TypeIndex = functionTypeIndexes[i]
	}

	return &Module{
		Types:           types,
		Imports:         imports,
		Exports:         exports,
		StartIndex:      startIndex,
		Tables:          tables,
		Memories:        memories,
		Funcs:           functions,
		ElementSegments: elementSegments,
		GlobalVariables: globals,
		DataSegments:    dataSegments,
		DataCount:       dataCount,
	}, nil
}

func (p *Parser) parseHeader() error {
	header := make([]byte, 8)
	if _, err := io.ReadFull(p.reader, header); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("file is too short to be valid WASM")
		}
		return fmt.Errorf("could not read header: %w", err)
	}

	if !bytes.HasPrefix(header, []byte(wasmMagicNumber)) {
		return fmt.Errorf("invalid WASM: does not start with magic number")
	}

	version := Int32From4Bytes(header[4:8])
	if version != supportedWasmVersion {
		return fmt.Errorf("unsupported WASM version: %d", version)
	}
	return nil
}

func (p *Parser) parseFunction() (Function, error) {
	size, err := p.parseUleb128()
	if err != nil {
		return Function{}, err
	}

	originalReader := p.reader
	defer func() { p.reader = originalReader }()

	// We create a new reader to limit how many bytes we can read to `size`.
	limitedReader := io.LimitReader(originalReader, int64(size))
	p.reader = bufio.NewReader(limitedReader)

	localsVariables, err := parseVector(p, p.parseLocalVariables)
	if err != nil {
		return Function{}, fmt.Errorf("failed to parse locals: %w", err)
	}

	totalLocalsCount := 0
	for _, variables := range localsVariables {
		totalLocalsCount += len(variables)
	}
	if totalLocalsCount > math.MaxInt32 {
		return Function{}, fmt.Errorf("too many locals: %d", totalLocalsCount)
	}

	locals := []ValueType{}
	for _, variables := range localsVariables {
		locals = append(locals, variables...)
	}

	body, err := io.ReadAll(p.reader)
	if err != nil {
		return Function{}, fmt.Errorf("failed to read function body: %w", err)
	}

	if len(body) == 0 || body[len(body)-1] != byte(End) {
		return Function{}, fmt.Errorf("function body must end with End opcode")
	}

	return Function{Locals: locals, Body: body[:len(body)-1]}, nil
}

func (p *Parser) parseLocalVariables() ([]ValueType, error) {
	count, err := p.parseUleb128()
	if err != nil {
		return nil, err
	}
	if count > math.MaxInt32 {
		return nil, fmt.Errorf("too many local variables: %d", count)
	}

	valueType, err := p.parseValueType()
	if err != nil {
		return nil, err
	}
	variables := make([]ValueType, count)
	for i := range variables {
		variables[i] = valueType
	}
	return variables, nil
}

func (p *Parser) parseImport() (Import, error) {
	moduleName, err := p.parseUtf8String()
	if err != nil {
		return Import{}, err
	}
	name, err := p.parseUtf8String()
	if err != nil {
		return Import{}, err
	}
	b, err := p.reader.ReadByte()
	if err != nil {
		return Import{}, err
	}

	var importType ImportType
	switch b {
	case 0:
		index, err := p.parseUleb128()
		if err != nil {
			return Import{}, err
		}
		importType = FunctionTypeIndex(index)
	case 1:
		importType, err = p.parseTableType()
		if err != nil {
			return Import{}, err
		}
	case 2:
		importType, err = p.parseMemoryType()
		if err != nil {
			return Import{}, err
		}
	case 3:
		importType, err = p.parseGlobalType()
		if err != nil {
			return Import{}, err
		}
	default:
		return Import{}, fmt.Errorf("failed to parse import description")
	}
	return Import{ModuleName: moduleName, Name: name, Type: importType}, nil
}

func (p *Parser) parseExport() (Export, error) {
	name, err := p.parseUtf8String()
	if err != nil {
		return Export{}, err
	}
	b, err := p.reader.ReadByte()
	if err != nil {
		return Export{}, err
	}
	index, err := p.parseUleb128()
	if err != nil {
		return Export{}, err
	}
	return Export{Name: name, IndexType: IndexType(b), Index: uint32(index)}, nil
}

func (p *Parser) parseDataSegment() (DataSegment, error) {
	dataMode, err := p.parseUleb128()
	if err != nil {
		return DataSegment{}, err
	}

	if dataMode&1 != 0 {
		content, err := parseVector(p, p.reader.ReadByte)
		if err != nil {
			return DataSegment{}, err
		}
		return DataSegment{Mode: PassiveDataMode, Content: content}, nil
	}

	memoryIndex := uint64(0)
	if dataMode != 0 {
		memoryIndex, err = p.parseUleb128()
		if err != nil {
			return DataSegment{}, err
		}
	}

	offsetExpression, err := p.parseExpression()
	if err != nil {
		return DataSegment{}, err
	}

	content, err := parseVector(p, p.reader.ReadByte)
	if err != nil {
		return DataSegment{}, err
	}

	return DataSegment{
		Mode:             ActiveDataMode,
		MemoryIndex:      uint32(memoryIndex),
		OffsetExpression: offsetExpression,
		Content:          content,
	}, nil
}

func (p *Parser) parseFunctionType() (FunctionType, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return FunctionType{}, err
	}
	if b != 0x60 {
		return FunctionType{}, fmt.Errorf("invalid function type prefix")
	}

	paramTypes, err := parseVector(p, p.parseValueType)
	if err != nil {
		return FunctionType{}, fmt.Errorf("failed to parse param types: %w", err)
	}

	resultTypes, err := parseVector(p, p.parseValueType)
	if err != nil {
		return FunctionType{}, fmt.Errorf("failed to parse result types: %w", err)
	}

	return FunctionType{ParamTypes: paramTypes, ResultTypes: resultTypes}, nil
}

func (p *Parser) parseValueType() (ValueType, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch b {
	case byte(I32), byte(I64), byte(F32), byte(F64):
		return NumberType(b), nil
	case byte(V128):
		return VectorType(b), nil
	case byte(FuncRefType), byte(ExternRefType):
		return ReferenceType(b), nil
	default:
		return nil, fmt.Errorf("invalid ValueType: 0x%x", b)
	}
}

func (p *Parser) parseTableType() (TableType, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return TableType{}, err
	}
	limits, err := p.parseLimits()
	if err != nil {
		return TableType{}, err
	}
	return TableType{ReferenceType: ReferenceType(b), Limits: limits}, nil
}

func (p *Parser) parseMemoryType() (MemoryType, error) {
	limits, err := p.parseLimits()
	if err != nil {
		return MemoryType{}, err
	}
	return MemoryType{Limits: limits}, nil
}

func (p *Parser) parseGlobalVariable() (GlobalVariable, error) {
	globalType, err := p.parseGlobalType()
	if err != nil {
		return GlobalVariable{}, err
	}
	init, err := p.parseExpression()
	if err != nil {
		return GlobalVariable{}, err
	}
	return GlobalVariable{GlobalType: globalType, InitExpression: init}, nil
}

func (p *Parser) parseGlobalType() (GlobalType, error) {
	valueType, err := p.parseValueType()
	if err != nil {
		return GlobalType{}, err
	}
	isMutable, err := p.reader.ReadByte()
	if err != nil {
		return GlobalType{}, err
	}
	if isMutable != 0 && isMutable != 1 {
		return GlobalType{}, fmt.Errorf("invalid global type mutability")
	}
	return GlobalType{ValueType: valueType, IsMutable: isMutable == 1}, nil
}

func (p *Parser) parseElementSegment() (ElementSegment, error) {
	flags, err := p.parseUleb128()
	if err != nil {
		return ElementSegment{}, fmt.Errorf("failed to read element flags: %w", err)
	}

	switch flags {
	case 0: // Active element with func indexes.
		offset, err := p.parseExpression()
		if err != nil {
			return ElementSegment{}, err
		}
		indexes, err := parseVector(p, p.parseUleb128)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:             ActiveElementMode,
			Kind:             FuncRefType,
			FuncIndexes:      uint64SliceToInt32(indexes),
			TableIndex:       defaultTableIndex,
			OffsetExpression: offset,
		}, nil
	case 1: // Passive element with func indexes.
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		if elemkind != 0x00 {
			return ElementSegment{}, ErrElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUleb128)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:        PassiveElementMode,
			Kind:        FuncRefType,
			FuncIndexes: uint64SliceToInt32(indexes),
		}, nil
	case 2: // Active element with explicit table index and func indexes.
		tableIdx, err := p.parseUleb128()
		if err != nil {
			return ElementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return ElementSegment{}, err
		}
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		if elemkind != 0x00 {
			return ElementSegment{}, ErrElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUleb128)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:             ActiveElementMode,
			Kind:             FuncRefType,
			FuncIndexes:      uint64SliceToInt32(indexes),
			TableIndex:       uint32(tableIdx),
			OffsetExpression: offset,
		}, nil
	case 3: // Declarative element with func indexes.
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		if elemkind != 0x00 {
			return ElementSegment{}, ErrElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUleb128)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:        DeclarativeElementMode,
			Kind:        FuncRefType,
			FuncIndexes: uint64SliceToInt32(indexes),
		}, nil
	case 4: // Active element with expressions.
		offset, err := p.parseExpression()
		if err != nil {
			return ElementSegment{}, err
		}
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:                   ActiveElementMode,
			Kind:                   FuncRefType,
			FuncIndexesExpressions: exprs,
			TableIndex:             defaultTableIndex,
			OffsetExpression:       offset,
		}, nil
	case 5: // Passive element with expressions.
		b, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		kind := ReferenceType(b)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:                   PassiveElementMode,
			Kind:                   kind,
			FuncIndexesExpressions: exprs,
		}, nil
	case 6: // Active element with explicit table index and expressions.
		tableIdx, err := p.parseUleb128()
		if err != nil {
			return ElementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return ElementSegment{}, err
		}
		refTypeByte, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		kind := ReferenceType(refTypeByte)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:                   ActiveElementMode,
			Kind:                   kind,
			FuncIndexesExpressions: exprs,
			TableIndex:             uint32(tableIdx),
			OffsetExpression:       offset,
		}, nil
	case 7: // Declarative element with expressions.
		refTypeByte, err := p.reader.ReadByte()
		if err != nil {
			return ElementSegment{}, err
		}
		kind := ReferenceType(refTypeByte)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return ElementSegment{}, err
		}
		return ElementSegment{
			Mode:                   DeclarativeElementMode,
			Kind:                   kind,
			FuncIndexesExpressions: exprs,
		}, nil
	default:
		return ElementSegment{}, fmt.Errorf("invalid element flags: %d", flags)
	}
}

func (p *Parser) parseExpression() ([]byte, error) {
	// This is a horrible implementation. Basically, we use a decoder instace to
	// parse the expression. But decoder expects a []byte, which we don't have. So
	// we create one, adding one byte at a time until the decoder stops failing.
	// TODO(pivetta): Fix this.
	var buf bytes.Buffer
	for {
		// Read one byte and add it to our buffer
		b, err := p.reader.ReadByte()
		if err != nil {
			return nil, io.ErrUnexpectedEOF
		}
		buf.WriteByte(b)

		// Create a decoder for the bytes we have so far.
		code := buf.Bytes()
		decoder := NewDecoder(code)

		// Try to decode instructions.
		for decoder.HasMore() {
			// If the next byte is the end opcode, we are done.
			if Opcode(code[decoder.Pc]) == End {
				// The expression is the content of the buffer *before* the End opcode.
				return code[:decoder.Pc], nil
			}

			// Try to decode one instruction.
			_, err := decoder.Decode()
			if err != nil {
				// Decoding failed. This is expected if we are in the middle of an
				// immediate. We break the inner loop and read more bytes.
				goto nextByte
			}
		}
	nextByte:
	}
}

func (p *Parser) parseLimits() (Limits, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return Limits{}, err
	}
	switch b {
	case 0:
		min, err := p.parseUleb128()
		if err != nil {
			return Limits{}, err
		}
		return Limits{Min: min}, nil
	case 1:
		min, err := p.parseUleb128()
		if err != nil {
			return Limits{}, err
		}
		max, err := p.parseUleb128()
		if err != nil {
			return Limits{}, err
		}
		return Limits{Min: min, Max: &max}, nil
	default:
		return Limits{}, fmt.Errorf("unexpected limits format")
	}
}

func parseVector[T any](parser *Parser, parse func() (T, error)) ([]T, error) {
	count, err := parser.parseUleb128()
	if err != nil {
		return nil, err
	}
	if count > math.MaxInt32 {
		return nil, fmt.Errorf("too many items in vector")
	}
	items := make([]T, count)
	for i := 0; i < int(count); i++ {
		parsed, err := parse()
		if err != nil {
			return nil, err
		}
		items[i] = parsed
	}
	return items, nil
}

func (p *Parser) parseIndex() (uint32, error) {
	val, err := p.parseUleb128()
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint32 {
		return 0, fmt.Errorf("integer too large")
	}
	return uint32(val), nil
}

func (p *Parser) parseUleb128() (uint64, error) {
	var value uint64
	var shift uint
	for {
		b, err := p.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		group := b & 0b01111111
		value |= uint64(group) << shift
		shift += 7
		if b&0b10000000 == 0 {
			break
		}
	}
	return value, nil
}

func (p *Parser) parseUtf8String() (string, error) {
	length, err := p.parseUleb128()
	if err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(p.reader, buf); err != nil {
		return "", fmt.Errorf("failed to read string bytes: %w", err)
	}
	return string(buf), nil
}

func uint64SliceToInt32(slice []uint64) []int32 {
	result := make([]int32, len(slice))
	for i, val := range slice {
		result[i] = int32(val)
	}
	return result
}
