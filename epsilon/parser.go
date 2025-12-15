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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

var (
	errElementKindNotZero                = errors.New("element kind for passive element segment must be 0x00")
	errIncompatibleNumberOfFunctionTypes = errors.New("incompatible number of function types")
)

const (
	wasmMagicNumber      = "\x00asm"
	supportedWasmVersion = 1
	defaultTableIndex    = 0
)

// sectionId represents the different sections of a WebAssembly module.
// See https://webassembly.github.io/spec/core/binary/modules.html#sections
type sectionId byte

const (
	customSectionId sectionId = iota
	typeSectionId
	importSectionId
	functionSectionId
	tableSectionId
	memorySectionId
	globalSectionId
	exportSectionId
	startSectionId
	elementSectionId
	codeSectionId
	dataSectionId
	dataCountSectionId
)

type localEntry struct {
	count uint64
	typ   ValueType
}

// parser is a parser for WASM modules.
type parser struct {
	reader *bufio.Reader
}

func newParser(reader io.Reader) *parser {
	return &parser{reader: bufio.NewReader(reader)}
}

// parse takes a byte slice and returns a Module.
func (p *parser) parse() (*moduleDefinition, error) {
	if err := p.parseHeader(); err != nil {
		return nil, err
	}

	var types []FunctionType
	var functionTypeIndexes []uint32
	var imports []moduleImport
	var exports []export
	var startIndex *uint32
	var tables []TableType
	var memories []MemoryType
	var functions []function
	var elementSegments []elementSegment
	var globals []globalVariable
	var dataSegments []dataSegment
	var dataCount *uint64

	// We initialize lastSection to CustomSectionId since custom sections
	// can be in any order.
	lastSection := customSectionId

	for {
		sectionIdByte, err := p.reader.ReadByte()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read section ID: %w", err)
		}

		sectionId := sectionId(sectionIdByte)
		if err := validateSectionOrder(lastSection, sectionId); err != nil {
			return nil, err
		}
		if sectionId != customSectionId {
			lastSection = sectionId
		}

		payloadLen, err := p.parseUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to read payload length: %w", err)
		}
		switch sectionId {
		case customSectionId:
			if err := p.parseCustomSection(payloadLen); err != nil {
				return nil, err
			}
		case typeSectionId:
			types, err = parseVector(p, p.parseFunctionType)
			if err != nil {
				return nil, err
			}
		case importSectionId:
			imports, err = parseVector(p, p.parseImport)
			if err != nil {
				return nil, err
			}
		case functionSectionId:
			functionTypeIndexes, err = parseVector(p, p.parseUint32)
			if err != nil {
				return nil, err
			}
		case tableSectionId:
			tables, err = parseVector(p, p.parseTableType)
			if err != nil {
				return nil, err
			}
		case memorySectionId:
			memories, err = parseVector(p, p.parseMemoryType)
			if err != nil {
				return nil, err
			}
		case globalSectionId:
			globals, err = parseVector(p, p.parseGlobalVariable)
			if err != nil {
				return nil, err
			}
		case exportSectionId:
			exports, err = parseVector(p, p.parseExport)
			if err != nil {
				return nil, err
			}
		case startSectionId:
			index, err := p.parseUint32()
			if err != nil {
				return nil, err
			}
			startIndex = &index
		case elementSectionId:
			elementSegments, err = parseVector(p, p.parseElementSegment)
			if err != nil {
				return nil, err
			}
		case codeSectionId:
			functions, err = parseVector(p, p.parseFunction)
			if err != nil {
				return nil, err
			}
		case dataSectionId:
			dataSegments, err = parseVector(p, p.parseDataSegment)
			if err != nil {
				return nil, err
			}
		case dataCountSectionId:
			count, err := p.parseUint64()
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
		return nil, errIncompatibleNumberOfFunctionTypes
	}

	for i := range functions {
		functions[i].typeIndex = functionTypeIndexes[i]
	}

	return &moduleDefinition{
		types:           types,
		imports:         imports,
		exports:         exports,
		startIndex:      startIndex,
		tables:          tables,
		memories:        memories,
		funcs:           functions,
		elementSegments: elementSegments,
		globalVariables: globals,
		dataSegments:    dataSegments,
		dataCount:       dataCount,
	}, nil
}

func (p *parser) parseHeader() error {
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
	version := int32(binary.LittleEndian.Uint32(header[4:8]))
	if version != supportedWasmVersion {
		return fmt.Errorf("unsupported WASM version: %d", version)
	}
	return nil
}

func (p *parser) parseCustomSection(payloadLen uint32) error {
	// Custom section is ignored, but we still parse it to return parsing errors
	// if it's not valid.
	nameLength, bytesRead, err := p.parseUleb128(5)
	if err != nil {
		return fmt.Errorf("failed to read custom section name length: %w", err)
	}

	if nameLength > math.MaxUint32 {
		return errIntegerTooLarge
	}

	nameBytes := make([]byte, nameLength)
	if _, err := io.ReadFull(p.reader, nameBytes); err != nil {
		return fmt.Errorf("failed to read custom section name: %w", err)
	}
	if !utf8.Valid(nameBytes) {
		return fmt.Errorf("custom section name is not valid UTF-8")
	}

	// Discard the actual bytes of the section.
	remainingBytes := payloadLen - uint32(nameLength) - uint32(bytesRead)
	_, err = io.CopyN(io.Discard, p.reader, int64(remainingBytes))
	if err != nil {
		return fmt.Errorf("failed to skip custom section: %w", err)
	}
	return nil
}

func (p *parser) parseFunction() (function, error) {
	size, err := p.parseUint32()
	if err != nil {
		return function{}, fmt.Errorf("failed to read function size: %w", err)
	}

	originalReader := p.reader
	defer func() { p.reader = originalReader }()

	// We create a new reader to limit how many bytes we can read to `size`.
	limitedReader := io.LimitReader(originalReader, int64(size))
	p.reader = bufio.NewReader(limitedReader)

	localEntries, err := parseVector(p, p.parseLocalVariables)
	if err != nil {
		return function{}, fmt.Errorf("failed to parse locals: %w", err)
	}

	var totalLocalsCount uint64
	for _, entry := range localEntries {
		totalLocalsCount += entry.count
	}
	if totalLocalsCount > math.MaxInt32 {
		return function{}, fmt.Errorf("too many locals: %d", totalLocalsCount)
	}

	locals := make([]ValueType, 0, totalLocalsCount)
	for _, entry := range localEntries {
		for i := uint64(0); i < entry.count; i++ {
			locals = append(locals, entry.typ)
		}
	}

	body, err := p.decodeBytecode()
	if err != nil {
		return function{}, fmt.Errorf("failed to read function body: %w", err)
	}

	if len(body) == 0 || body[len(body)-1] != uint64(end) {
		return function{}, fmt.Errorf("function body must end with End opcode")
	}

	return function{locals: locals, body: body[:len(body)-1]}, nil
}

func (p *parser) parseLocalVariables() (localEntry, error) {
	count, err := p.parseUint64()
	if err != nil {
		return localEntry{}, err
	}
	if count > math.MaxInt32 {
		return localEntry{}, fmt.Errorf("too many local variables: %d", count)
	}

	valueType, err := p.parseValueType()
	if err != nil {
		return localEntry{}, err
	}
	return localEntry{count: count, typ: valueType}, nil
}

func (p *parser) parseImport() (moduleImport, error) {
	moduleName, err := p.parseUtf8String()
	if err != nil {
		return moduleImport{}, err
	}
	name, err := p.parseUtf8String()
	if err != nil {
		return moduleImport{}, err
	}
	b, err := p.reader.ReadByte()
	if err != nil {
		return moduleImport{}, err
	}

	var importType importType
	switch b {
	case 0:
		index, err := p.parseUint32()
		if err != nil {
			return moduleImport{}, err
		}
		importType = functionTypeIndex(index)
	case 1:
		importType, err = p.parseTableType()
		if err != nil {
			return moduleImport{}, err
		}
	case 2:
		importType, err = p.parseMemoryType()
		if err != nil {
			return moduleImport{}, err
		}
	case 3:
		importType, err = p.parseGlobalType()
		if err != nil {
			return moduleImport{}, err
		}
	default:
		return moduleImport{}, fmt.Errorf("failed to parse import description")
	}
	return moduleImport{
		moduleName: moduleName,
		name:       name,
		importType: importType,
	}, nil
}

func (p *parser) parseExport() (export, error) {
	name, err := p.parseUtf8String()
	if err != nil {
		return export{}, err
	}
	b, err := p.reader.ReadByte()
	if err != nil {
		return export{}, err
	}
	index, err := p.parseUint32()
	if err != nil {
		return export{}, err
	}
	return export{name: name, indexType: exportIndexKind(b), index: index}, nil
}

func (p *parser) parseDataSegment() (dataSegment, error) {
	dataMode, err := p.parseUint32()
	if err != nil {
		return dataSegment{}, err
	}

	switch dataMode {
	case 0:
		offsetExpression, err := p.parseExpression()
		if err != nil {
			return dataSegment{}, err
		}
		content, err := parseVector(p, p.reader.ReadByte)
		if err != nil {
			return dataSegment{}, err
		}
		return dataSegment{
			mode:             activeDataMode,
			content:          content,
			offsetExpression: offsetExpression,
		}, nil
	case 1:
		content, err := parseVector(p, p.reader.ReadByte)
		if err != nil {
			return dataSegment{}, err
		}
		return dataSegment{mode: passiveDataMode, content: content}, nil
	case 2:
		memoryIndex, err := p.parseUint32()
		if err != nil {
			return dataSegment{}, err
		}
		offsetExpression, err := p.parseExpression()
		if err != nil {
			return dataSegment{}, err
		}
		content, err := parseVector(p, p.reader.ReadByte)
		if err != nil {
			return dataSegment{}, err
		}
		return dataSegment{
			mode:             activeDataMode,
			content:          content,
			memoryIndex:      memoryIndex,
			offsetExpression: offsetExpression,
		}, nil
	default:
		return dataSegment{}, fmt.Errorf("invalid data mode: %d", dataMode)
	}
}

func (p *parser) parseFunctionType() (FunctionType, error) {
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

func (p *parser) parseValueType() (ValueType, error) {
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

func (p *parser) parseTableType() (TableType, error) {
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

func (p *parser) parseMemoryType() (MemoryType, error) {
	limits, err := p.parseLimits()
	if err != nil {
		return MemoryType{}, err
	}
	return MemoryType{Limits: limits}, nil
}

func (p *parser) parseGlobalVariable() (globalVariable, error) {
	globalType, err := p.parseGlobalType()
	if err != nil {
		return globalVariable{}, err
	}
	init, err := p.parseExpression()
	if err != nil {
		return globalVariable{}, err
	}
	return globalVariable{globalType: globalType, initExpression: init}, nil
}

func (p *parser) parseGlobalType() (GlobalType, error) {
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

func (p *parser) parseElementSegment() (elementSegment, error) {
	flags, err := p.parseUint32()
	if err != nil {
		return elementSegment{}, fmt.Errorf("failed to read element flags: %w", err)
	}

	switch flags {
	case 0: // Active element with func indexes.
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		indexes, err := parseVector(p, p.parseUint64)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:             activeElementMode,
			kind:             FuncRefType,
			functionIndexes:  uint64SliceToInt32(indexes),
			tableIndex:       defaultTableIndex,
			offsetExpression: offset,
		}, nil
	case 1: // Passive element with func indexes.
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUint64)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:            passiveElementMode,
			kind:            FuncRefType,
			functionIndexes: uint64SliceToInt32(indexes),
		}, nil
	case 2: // Active element with explicit table index and func indexes.
		tableIdx, err := p.parseUint64()
		if err != nil {
			return elementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUint64)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:             activeElementMode,
			kind:             FuncRefType,
			functionIndexes:  uint64SliceToInt32(indexes),
			tableIndex:       uint32(tableIdx),
			offsetExpression: offset,
		}, nil
	case 3: // Declarative element with func indexes.
		elemkind, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errElementKindNotZero
		}
		indexes, err := parseVector(p, p.parseUint64)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:            declarativeElementMode,
			kind:            FuncRefType,
			functionIndexes: uint64SliceToInt32(indexes),
		}, nil
	case 4: // Active element with expressions.
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:                       activeElementMode,
			kind:                       FuncRefType,
			functionIndexesExpressions: exprs,
			tableIndex:                 defaultTableIndex,
			offsetExpression:           offset,
		}, nil
	case 5: // Passive element with expressions.
		b, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		kind := ReferenceType(b)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:                       passiveElementMode,
			kind:                       kind,
			functionIndexesExpressions: exprs,
		}, nil
	case 6: // Active element with explicit table index and expressions.
		tableIdx, err := p.parseUint64()
		if err != nil {
			return elementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		refTypeByte, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		kind := ReferenceType(refTypeByte)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:                       activeElementMode,
			kind:                       kind,
			functionIndexesExpressions: exprs,
			tableIndex:                 uint32(tableIdx),
			offsetExpression:           offset,
		}, nil
	case 7: // Declarative element with expressions.
		refTypeByte, err := p.reader.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		kind := ReferenceType(refTypeByte)
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:                       declarativeElementMode,
			kind:                       kind,
			functionIndexesExpressions: exprs,
		}, nil
	default:
		return elementSegment{}, fmt.Errorf("invalid element flags: %d", flags)
	}
}

func (p *parser) parseExpression() ([]uint64, error) {
	var bytecode []uint64
	for {
		instruction, err := p.readInstruction()
		if err != nil {
			return nil, err
		}
		if opcode(instruction[0]) == end {
			break
		}
		bytecode = append(bytecode, instruction...)
	}
	return bytecode, nil
}

func (p *parser) parseLimits() (Limits, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return Limits{}, err
	}
	min, err := p.parseUint32()
	if err != nil {
		return Limits{}, err
	}
	switch b {
	case 0:
		return Limits{Min: min}, nil
	case 1:
		max, err := p.parseUint32()
		if err != nil {
			return Limits{}, err
		}
		return Limits{Min: min, Max: &max}, nil
	default:
		return Limits{}, fmt.Errorf("unexpected limits format")
	}
}

func parseVector[T any](parser *parser, parse func() (T, error)) ([]T, error) {
	count, err := parser.parseUint32()
	if err != nil {
		return nil, err
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

func (p *parser) parseUint32() (uint32, error) {
	val, _, err := p.parseUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint32 {
		return 0, errIntegerTooLarge
	}
	return uint32(val), nil
}

func (p *parser) parseUint64() (uint64, error) {
	val, _, err := p.parseUleb128(9)
	return val, err
}

func (p *parser) parseUleb128(maxBytes int) (uint64, int, error) {
	bytesRead := 0

	var value uint64
	var shift uint
	for {
		b, err := p.reader.ReadByte()
		if err != nil {
			return 0, bytesRead, err
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, bytesRead, fmt.Errorf("uleb128 value too large")
		}

		group := b & 0b01111111
		value |= uint64(group) << shift
		shift += 7
		if b&0b10000000 == 0 {
			break
		}
	}
	return value, bytesRead, nil
}

func (p *parser) parseUtf8String() (string, error) {
	length, err := p.parseUint32()
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

func validateSectionOrder(last sectionId, current sectionId) error {
	if current == customSectionId {
		// Custom sections can be in any order.
		return nil
	}

	order := getSectionOrder(current)
	if order == 0 {
		return fmt.Errorf("malformed section id: %d", current)
	}
	if order <= getSectionOrder(last) {
		return fmt.Errorf("unexpected content after last section")
	}
	return nil
}

func getSectionOrder(id sectionId) int {
	switch id {
	case dataCountSectionId:
		return 10
	case codeSectionId:
		return 11
	case dataSectionId:
		return 12
	default:
		if id > dataCountSectionId {
			return 0
		}
		return int(id)
	}
}

// =============================================================================
// Bytecode
// =============================================================================
func (p *parser) decodeBytecode() ([]uint64, error) {
	bytecode := []uint64{}
	for {
		instruction, err := p.readInstruction()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, instruction...)
	}
	return bytecode, nil
}

func (p *parser) readInstruction() ([]uint64, error) {
	bytecode := []uint64{}
	opcode, err := p.readOpcode()
	if err != nil {
		return nil, err
	}
	bytecode = append(bytecode, uint64(opcode))

	switch opcode {
	case block, loop, ifOp:
		immediate, err := p.readBlockType()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
	case i32Const:
		immediate, err := p.readInt32()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
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
		immediate, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
	case memorySize, memoryGrow:
		immediate, err := p.readByte()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, uint64(immediate))
	case brTable:
		vector, err := p.readImmediateVector()
		if err != nil {
			return nil, err
		}
		immediate, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, uint64(len(vector)))
		bytecode = append(bytecode, vector...)
		bytecode = append(bytecode, immediate)
	case callIndirect,
		memoryInit,
		memoryCopy,
		tableInit,
		tableCopy:
		immediate1, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		immediate2, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate1, immediate2)
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
		align, memoryIndex, offset, err := p.readMemArg()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, align, memoryIndex, offset)
	case selectT:
		vector, err := p.readImmediateVector()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, uint64(len(vector)))
		bytecode = append(bytecode, vector...)
	case i64Const:
		immediate, err := p.readSleb128(10)
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
	case f32Const:
		immediate, err := p.readFloat32()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
	case f64Const:
		immediate, err := p.readFloat64()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, immediate)
	case v128Const:
		bytes, err := p.readBytes(16)
		if err != nil {
			return nil, err
		}

		bytecode = append(bytecode, binary.LittleEndian.Uint64(bytes[0:8]))
		bytecode = append(bytecode, binary.LittleEndian.Uint64(bytes[8:16]))
	case v128Load8Lane,
		v128Load16Lane,
		v128Load32Lane,
		v128Load64Lane,
		v128Store8Lane,
		v128Store16Lane,
		v128Store32Lane,
		v128Store64Lane:
		align, memoryIndex, offset, err := p.readMemArg()
		if err != nil {
			return nil, err
		}

		laneIndex, err := p.readUint8()
		if err != nil {
			return nil, err
		}
		bytecode = append(bytecode, align, memoryIndex, offset, laneIndex)
	case i8x16Shuffle:
		for range 16 {
			val, err := p.readUint8()
			if err != nil {
				return nil, err
			}
			bytecode = append(bytecode, uint64(val))
		}
	default:
		return bytecode, nil
	}
	return bytecode, nil
}

func (p *parser) readOpcode() (opcode, error) {
	opcodeByte, err := p.reader.ReadByte()
	if err != nil {
		return 0, err
	}

	// Standard single-byte opcode.
	if opcodeByte < 0xFC {
		return opcode(opcodeByte), nil
	}

	// Multi-byte opcode (prefixed with 0xFC or 0xFD).
	val, err := p.readUint32()
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
		return 0, fmt.Errorf("unrecognized opcode prefix: 0x%X", opcodeByte)
	}

	return opcode(compositeOpcode), nil
}

func (p *parser) readImmediateVector() ([]uint64, error) {
	size, err := p.readUint32()
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
		val, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		immediates[i] = val
	}
	return immediates, nil
}

func (p *parser) readFloat32() (uint64, error) {
	bytes, err := p.readBytes(4)
	if err != nil {
		return 0, err
	}
	return uint64(binary.LittleEndian.Uint32(bytes)), nil
}

func (p *parser) readFloat64() (uint64, error) {
	bytes, err := p.readBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(bytes), nil
}

func (p *parser) readMemArg() (uint64, uint64, uint64, error) {
	align, err := p.readUint32()
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
		memoryIndex, err = p.readUint32()
		if err != nil {
			return 0, 0, 0, err
		}
	}

	offset, err := p.readUleb128(10)
	if err != nil {
		return 0, 0, 0, err
	}

	return align, memoryIndex, offset, nil
}

func (p *parser) readBlockType() (uint64, error) {
	blockType, err := p.readSleb128(5)
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

// readUint32 still returns a uint64, but checks that the value can be
// interpreted as a WASM u32.
func (p *parser) readUint32() (uint64, error) {
	val, err := p.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint32 {
		return 0, errIntegerTooLarge
	}
	return val, nil
}

func (p *parser) readInt32() (uint64, error) {
	val, err := p.readSleb128(5)
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
func (p *parser) readUint8() (uint64, error) {
	val, err := p.readUleb128(5)
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint8 {
		return 0, errIntegerTooLarge
	}
	return val, nil
}

// readSleb128 decodes a signed 64-bit integer immediate (SLEB128).
func (p *parser) readSleb128(maxBytes int) (uint64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	bytesRead := 0

	for {
		b, err = p.readByte()
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

// readUleb128 decodes an unsigned 64-bit integer immediate (ULEB128).
func (p *parser) readUleb128(maxBytes int) (uint64, error) {
	var result uint64
	var shift uint
	bytesRead := 0

	for {
		b, err := p.readByte()
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

func (p *parser) readByte() (byte, error) {
	b, err := p.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	return b, nil
}

func (p *parser) readBytes(n uint) ([]byte, error) {
	var bytes []byte
	for range n {
		b, err := p.readByte()
		if err != nil {
			return nil, err
		}
		bytes = append(bytes, b)
	}
	return bytes, nil
}
