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
	errFunctionTypeMismatch      = errors.New("function and code section have inconsistent lengths")
	errInconsistentDataCount     = errors.New("data count and data section have inconsistent lengths")
	errIntRepresentationTooLong  = errors.New("integer representation too long")
	errIntegerTooLarge           = errors.New("integer too large")
	errInvalidElementKind        = errors.New("invalid element kind")
	errInvalidFunctionTypePrefix = errors.New("invalid function type prefix")
	errInvalidGlobalMutability   = errors.New("malformed mutability")
	errInvalidImportDescriptor   = errors.New("malformed import kind")
	errInvalidMagicNumber        = errors.New("magic header not detected")
	errInvalidReferenceType      = errors.New("malformed reference type")
	errInvalidUTF8               = errors.New("malformed UTF-8 encoding")
	errMalformedMemopFlags       = errors.New("malformed memop flags")
	errMissingEndOpcode          = errors.New("missing end opcode")
	errEndOpcodeExpected         = errors.New("END opcode expected")
	errPrefixedOpcodeOutOfRange  = errors.New("prefixed opcode out of range")
	errSectionSizeMismatch       = errors.New("section size mismatch")
	errUnexpectedContent         = errors.New("unexpected content after last section")
	errUnexpectedEnd             = errors.New("unexpected end")
	errLengthOutOfBounds         = errors.New("length out of bounds")
	errSectionTruncated          = errors.New("unexpected end of section or function")
)

const (
	wasmMagicNumber      = "\x00asm"
	supportedWasmVersion = 1
	defaultTableIndex    = 0
	continuationBit      = 0x80
	payloadMask          = 0x7F
	signBit              = 0x40
	sixthBitMask         = uint64(1 << 6)
)

// maxInitialCapacity caps the up-front allocation parseVector and similar
// callers perform from an attacker-controlled count. Real modules rarely
// exceed a few thousand items per vector, so this is the common-case exact
// size; pathological counts grow via append+EOF instead of OOMing on a
// pre-allocation.
const maxInitialCapacity = 4096

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
	count uint32
	typ   ValueType
}

// wasmReader is an interface that combines io.Reader and io.ByteReader.
type wasmReader interface {
	io.Reader
	io.ByteReader
}

// parser is a parser for WASM modules.
type parser struct {
	reader wasmReader
	// bytesRemaining counts down the bytes still owed to the region being
	// read. Any negative value means the parser is reading outside a bounded
	// region and consumes whatever the input holds.
	bytesRemaining int64
	config         Config
}

func newParser(reader io.Reader, config Config) *parser {
	var wr wasmReader
	if r, ok := reader.(wasmReader); ok {
		wr = r
	} else {
		wr = bufio.NewReader(reader)
	}
	return &parser{
		reader:         wr,
		bytesRemaining: -1,
		config:         config,
	}
}

func (p *parser) Read(bytes []byte) (int, error) {
	if p.bytesRemaining == 0 {
		return 0, errSectionTruncated
	}
	if p.bytesRemaining > 0 && int64(len(bytes)) > p.bytesRemaining {
		bytes = bytes[:p.bytesRemaining]
	}
	n, err := p.reader.Read(bytes)
	p.bytesRemaining -= int64(n)
	if n < len(bytes) && err == io.EOF {
		return n, p.outOfInput()
	}
	return n, err
}

func (p *parser) ReadByte() (byte, error) {
	if p.bytesRemaining == 0 {
		return 0, errSectionTruncated
	}
	b, err := p.reader.ReadByte()
	if err != nil {
		if err == io.EOF {
			return 0, p.outOfInput()
		}
		return 0, err
	}
	p.bytesRemaining--
	return b, nil
}

// discardRemaining consumes whatever is left of the bounded region the parser
// is reading. Reaching its end is the expected outcome here, not a failure.
func (p *parser) discardRemaining() error {
	if p.bytesRemaining == 0 {
		return nil
	}
	_, err := io.Copy(io.Discard, p)
	if err == errSectionTruncated {
		return nil
	}
	return err
}

// outOfInput states why a read found no more input: bytes still owed to a
// section mean the section declared a longer payload than the module holds.
func (p *parser) outOfInput() error {
	if p.bytesRemaining > 0 {
		return errLengthOutOfBounds
	}
	return errUnexpectedEnd
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
	var dataCount *uint32

	// We initialize lastSection to CustomSectionId since custom sections
	// can be in any order.
	lastSection := customSectionId

	for {
		sectionIdByte, err := p.ReadByte()
		if err == errUnexpectedEnd {
			break
		}

		if err != nil {
			return nil, err
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
			return nil, err
		}

		p.bytesRemaining = int64(payloadLen)

		switch sectionId {
		case customSectionId:
			if err := p.parseCustomSection(); err != nil {
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
			count, err := p.parseUint32()
			if err != nil {
				return nil, err
			}

			dataCount = &count
		default:
			return nil, fmt.Errorf("section %d not implemented", sectionId)
		}

		if p.bytesRemaining != 0 {
			return nil, errSectionSizeMismatch
		}
		p.bytesRemaining = -1
	}

	if dataCount != nil && uint64(*dataCount) != uint64(len(dataSegments)) {
		return nil, errInconsistentDataCount
	}

	if len(functionTypeIndexes) != len(functions) {
		return nil, errFunctionTypeMismatch
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
	// The magic number is checked before the version is read, so input too short
	// to hold a header counts as truncated only while what it does hold still
	// looks like WebAssembly.
	var header [8]byte
	magic := header[:len(wasmMagicNumber)]
	if _, err := io.ReadFull(p, magic); err != nil {
		return err
	}
	if !bytes.Equal(magic, []byte(wasmMagicNumber)) {
		return errInvalidMagicNumber
	}

	versionBytes := header[len(wasmMagicNumber):]
	if _, err := io.ReadFull(p, versionBytes); err != nil {
		return err
	}
	version := int32(binary.LittleEndian.Uint32(versionBytes))
	if version != supportedWasmVersion {
		return fmt.Errorf("unknown binary version %d", version)
	}
	return nil
}

func (p *parser) parseCustomSection() error {
	// Custom section is ignored, but we still parse it to return parsing errors
	// if it's not valid.
	nameLength, err := p.parseUint32()
	if err != nil {
		return err
	}

	nameBytes, err := p.readN(uint64(nameLength))
	if err != nil {
		return err
	}
	if !utf8.Valid(nameBytes) {
		return errInvalidUTF8
	}

	// Discard the remaining bytes of the section.
	return p.discardRemaining()
}

func (p *parser) parseFunction() (function, error) {
	size, err := p.parseUint32()
	if err != nil {
		return function{}, err
	}

	sectionBytesRemaining := p.bytesRemaining
	if int64(size) > sectionBytesRemaining {
		return function{}, errLengthOutOfBounds
	}
	p.bytesRemaining = int64(size)

	localEntries, err := parseVector(p, p.parseLocalVariables)
	if err != nil {
		return function{}, err
	}

	var totalLocalsCount uint64
	for _, entry := range localEntries {
		totalLocalsCount += uint64(entry.count)
	}
	if totalLocalsCount > uint64(p.config.MaxLocalsPerFunction) {
		return function{}, fmt.Errorf(
			"too many locals: %d exceeds configured limit %d",
			totalLocalsCount, p.config.MaxLocalsPerFunction,
		)
	}

	locals := make([]ValueType, 0, min(totalLocalsCount, maxInitialCapacity))
	for _, entry := range localEntries {
		for range entry.count {
			locals = append(locals, entry.typ)
		}
	}

	result, err := p.readCode(size, false)
	if err != nil {
		if err == errSectionTruncated && sectionBytesRemaining > int64(size) {
			return function{}, errEndOpcodeExpected
		}
		return function{}, err
	}

	// Discard any bytes of the function body the parser didn't consume (e.g.
	// trailing bytes after an early end opcode) so the underlying reader is
	// positioned at the start of the next function.
	if err := p.discardRemaining(); err != nil {
		return function{}, err
	}
	if p.bytesRemaining != 0 {
		return function{}, errSectionTruncated
	}
	p.bytesRemaining = sectionBytesRemaining - int64(size)

	body := result.bytecode

	var defaultLocals []value
	var hasRef bool
	for _, typ := range locals {
		if typ == FuncRefType || typ == ExternRefType {
			hasRef = true
			break
		}
	}
	if hasRef {
		defaultLocals = make([]value, len(locals))
		for i, typ := range locals {
			if typ == FuncRefType || typ == ExternRefType {
				defaultLocals[i] = i32(NullReference)
			}
		}
	}

	return function{
		locals:        locals,
		body:          body,
		jumpCache:     result.jumpCache,
		jumpElseCache: result.jumpElseCache,
		defaultLocals: defaultLocals,
	}, nil
}

func (p *parser) parseLocalVariables() (localEntry, error) {
	count, err := p.parseUint32()
	if err != nil {
		return localEntry{}, err
	}
	if count > math.MaxInt32 {
		return localEntry{}, fmt.Errorf("too many locals %d", count)
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
	b, err := p.ReadByte()
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
		return moduleImport{}, errInvalidImportDescriptor
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
	b, err := p.ReadByte()
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
		content, err := p.parseByteVector()
		if err != nil {
			return dataSegment{}, err
		}
		return dataSegment{
			mode:             activeDataMode,
			content:          content,
			offsetExpression: offsetExpression,
		}, nil
	case 1:
		content, err := p.parseByteVector()
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
		content, err := p.parseByteVector()
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
	prefix, err := p.readSleb128(1)
	if err != nil {
		return FunctionType{}, err
	}
	if int64(prefix) != -0x20 {
		return FunctionType{}, errInvalidFunctionTypePrefix
	}

	paramTypes, err := parseVector(p, p.parseValueType)
	if err != nil {
		return FunctionType{}, err
	}

	resultTypes, err := parseVector(p, p.parseValueType)
	if err != nil {
		return FunctionType{}, err
	}
	return FunctionType{ParamTypes: paramTypes, ResultTypes: resultTypes}, nil
}

func (p *parser) parseValueType() (ValueType, error) {
	b, err := p.ReadByte()
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
	referenceType, err := p.parseReferenceType()
	if err != nil {
		return TableType{}, err
	}
	limits, err := p.parseLimits()
	if err != nil {
		return TableType{}, err
	}
	return TableType{ReferenceType: referenceType, Limits: limits}, nil
}

func (p *parser) parseReferenceType() (ReferenceType, error) {
	b, err := p.ReadByte()
	if err != nil {
		return 0, err
	}
	if b != byte(FuncRefType) && b != byte(ExternRefType) {
		return 0, errInvalidReferenceType
	}
	return ReferenceType(b), nil
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
	isMutable, err := p.ReadByte()
	if err != nil {
		return GlobalType{}, err
	}
	if isMutable != 0 && isMutable != 1 {
		return GlobalType{}, errInvalidGlobalMutability
	}
	return GlobalType{ValueType: valueType, IsMutable: isMutable == 1}, nil
}

func (p *parser) parseElementSegment() (elementSegment, error) {
	flags, err := p.parseUint32()
	if err != nil {
		return elementSegment{}, err
	}

	switch flags {
	case 0: // Active element with func indexes.
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		indexes, err := parseVector(p, p.parseUint32)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:             activeElementMode,
			kind:             FuncRefType,
			functionIndexes:  indexes,
			tableIndex:       defaultTableIndex,
			offsetExpression: offset,
		}, nil
	case 1: // Passive element with func indexes.
		elemkind, err := p.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errInvalidElementKind
		}
		indexes, err := parseVector(p, p.parseUint32)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:            passiveElementMode,
			kind:            FuncRefType,
			functionIndexes: indexes,
		}, nil
	case 2: // Active element with explicit table index and func indexes.
		tableIndex, err := p.parseUint32()
		if err != nil {
			return elementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		elemkind, err := p.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errInvalidElementKind
		}
		indexes, err := parseVector(p, p.parseUint32)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:             activeElementMode,
			kind:             FuncRefType,
			functionIndexes:  indexes,
			tableIndex:       tableIndex,
			offsetExpression: offset,
		}, nil
	case 3: // Declarative element with func indexes.
		elemkind, err := p.ReadByte()
		if err != nil {
			return elementSegment{}, err
		}
		if elemkind != 0x00 {
			return elementSegment{}, errInvalidElementKind
		}
		indexes, err := parseVector(p, p.parseUint32)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:            declarativeElementMode,
			kind:            FuncRefType,
			functionIndexes: indexes,
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
		kind, err := p.parseReferenceType()
		if err != nil {
			return elementSegment{}, err
		}
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
		tableIndex, err := p.parseUint32()
		if err != nil {
			return elementSegment{}, err
		}
		offset, err := p.parseExpression()
		if err != nil {
			return elementSegment{}, err
		}
		kind, err := p.parseReferenceType()
		if err != nil {
			return elementSegment{}, err
		}
		exprs, err := parseVector(p, p.parseExpression)
		if err != nil {
			return elementSegment{}, err
		}
		return elementSegment{
			mode:                       activeElementMode,
			kind:                       kind,
			functionIndexesExpressions: exprs,
			tableIndex:                 tableIndex,
			offsetExpression:           offset,
		}, nil
	case 7: // Declarative element with expressions.
		kind, err := p.parseReferenceType()
		if err != nil {
			return elementSegment{}, err
		}
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
	result, err := p.readCode(0, true)
	if err != nil {
		return nil, err
	}
	return result.bytecode, nil
}

func (p *parser) parseLimits() (Limits, error) {
	// The flags are a single-bit LEB128, so an over-long encoding or a value
	// that does not fit is rejected as the malformed integer it is, before
	// anything is read on its behalf.
	flags, err := p.readUleb128(1)
	if err != nil {
		return Limits{}, err
	}

	min, err := p.parseUint32()
	if err != nil {
		return Limits{}, err
	}
	if flags == 0 {
		return Limits{Min: min}, nil
	}
	max, err := p.parseUint32()
	if err != nil {
		return Limits{}, err
	}
	return Limits{Min: min, Max: &max}, nil
}

func parseVector[T any](parser *parser, parse func() (T, error)) ([]T, error) {
	count, err := parser.parseUint32()
	if err != nil {
		return nil, err
	}
	items := make([]T, 0, min(count, maxInitialCapacity))
	for range count {
		parsed, err := parse()
		if err != nil {
			return nil, err
		}
		items = append(items, parsed)
	}
	return items, nil
}

func (p *parser) parseUint32() (uint32, error) {
	val, err := p.readUleb128(32)
	if err != nil {
		return 0, err
	}
	return uint32(val), nil
}

func (p *parser) parseByteVector() ([]byte, error) {
	length, err := p.parseUint32()
	if err != nil {
		return nil, err
	}
	return p.readN(uint64(length))
}

func (p *parser) parseUtf8String() (string, error) {
	length, err := p.parseUint32()
	if err != nil {
		return "", err
	}
	stringBytes, err := p.readN(uint64(length))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(stringBytes) {
		return "", errInvalidUTF8
	}
	return string(stringBytes), nil
}

// readN reads exactly length bytes from the reader. The initial buffer
// capacity is capped at maxInitialCapacity so an attacker-controlled
// length cannot force a huge up-front allocation; the buffer grows as needed
// and a short read surfaces as an error.
func (p *parser) readN(length uint64) ([]byte, error) {
	if length <= maxInitialCapacity {
		data := make([]byte, int(length))
		if _, err := io.ReadFull(p, data); err != nil {
			return nil, err
		}
		return data, nil
	}

	buf := bytes.NewBuffer(make([]byte, 0, maxInitialCapacity))
	if _, err := io.CopyN(buf, p, int64(length)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
		return errUnexpectedContent
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

// controlEntry tracks a control flow instruction's position for building jump
// caches.
type controlEntry struct {
	opcode opcode
	pc     uint32 // Program counter of the first instruction in the block.
}

// bytecodeResult contains the parsed bytecode and precomputed jump caches.
type bytecodeResult struct {
	bytecode      []uint64
	jumpCache     map[uint32]uint32
	jumpElseCache map[uint32]uint32
}

// readCode decodes a sequence of WASM instructions into the flat uint64
// bytecode the VM executes, and returns it with the jump caches that map each
// block/if to its branch targets.
//
// Decoding stops at the first end opcode when stopAtEnd is true, or when the
// reader runs out otherwise. A sequence the input ran out of is rejected as
// truncated, one that merely lacks its end opcode with errMissingEndOpcode.
//
// sizeHint only seeds the bytecode buffer capacity to avoid regrowth: it is the
// body's declared byte length, or 0 if unknown, and need not be accurate.
func (p *parser) readCode(
	sizeHint uint32,
	stopAtEnd bool,
) (bytecodeResult, error) {
	// sizeHint is attacker-controlled (the function body's declared size), so cap
	// the initial capacity at maxInitialCapacity. The buffer still grows via
	// append as real bytes are decoded; a bogus huge size cannot force a large
	// up-front allocation.
	bytecode := make([]uint64, 0, min(sizeHint, maxInitialCapacity))
	// The jump caches are allocated lazily: a function with no control flow
	// never branches, so it needs neither map.
	var jumpCache map[uint32]uint32
	var jumpElseCache map[uint32]uint32

	controlStack := []controlEntry{}
	var lastOp opcode

	for {
		opcodeVal, err := p.readOpcode()
		if err != nil {
			if err == errSectionTruncated || err == errUnexpectedEnd {
				// Input that ran out mid-sequence is truncated, whatever it stopped
				// inside; only a sequence that reads to completion can be missing its
				// end opcode.
				if lastOp != end || len(controlStack) > 0 {
					return bytecodeResult{}, err
				}
				break
			}
			return bytecodeResult{}, err
		}

		lastOp = opcodeVal
		bytecode = append(bytecode, uint64(opcodeVal))

		switch opcodeVal {
		case block, loop, ifOp:
			if jumpCache == nil {
				jumpCache = map[uint32]uint32{}
				jumpElseCache = map[uint32]uint32{}
			}
			immediate, err := p.readBlockType()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, immediate)
			controlStack = append(controlStack, controlEntry{
				opcode: opcodeVal,
				pc:     uint32(len(bytecode)),
			})
		case elseOp:
			if len(controlStack) > 0 {
				top := &controlStack[len(controlStack)-1]
				if top.opcode == ifOp {
					jumpElseCache[top.pc] = uint32(len(bytecode))
				}
			}
		case end:
			if len(controlStack) > 0 {
				top := controlStack[len(controlStack)-1]
				controlStack = controlStack[:len(controlStack)-1]

				// Loops branch back to their start so we do not need to cache their end
				// position.
				if top.opcode != loop {
					jumpCache[top.pc] = uint32(len(bytecode))
				}

				// If this is an if without an else, record the position of the end
				// opcode in the jumpElseCache as this is the opcode to execute if the
				// if is not taken.
				if top.opcode == ifOp {
					if _, hasElse := jumpElseCache[top.pc]; !hasElse {
						jumpElseCache[top.pc] = uint32(len(bytecode)) - 1
					}
				}
			}
		case i32Const:
			immediate, err := p.readInt32()
			if err != nil {
				return bytecodeResult{}, err
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
			refFunc:
			immediate, err := p.readUint32()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, immediate)
		case i8x16ExtractLaneS,
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
			immediate, err := p.ReadByte()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, uint64(immediate))
		case memorySize, memoryGrow:
			immediate, err := p.ReadByte()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, uint64(immediate))
		case brTable:
			vector, err := p.readImmediateVector()
			if err != nil {
				return bytecodeResult{}, err
			}
			immediate, err := p.readUint32()
			if err != nil {
				return bytecodeResult{}, err
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
				return bytecodeResult{}, err
			}
			immediate2, err := p.readUint32()
			if err != nil {
				return bytecodeResult{}, err
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
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, align, memoryIndex, offset)
		case selectT:
			vector, err := p.readImmediateVector()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, uint64(len(vector)))
			bytecode = append(bytecode, vector...)
		case i64Const:
			immediate, err := p.readSleb128(10)
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, immediate)
		case f32Const:
			immediate, err := p.readFloat32()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, immediate)
		case f64Const:
			immediate, err := p.readFloat64()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(bytecode, immediate)
		case v128Const:
			bytes, err := p.readBytes(16)
			if err != nil {
				return bytecodeResult{}, err
			}

			bytecode = append(
				bytecode,
				binary.LittleEndian.Uint64(bytes[0:8]),
				binary.LittleEndian.Uint64(bytes[8:16]),
			)
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
				return bytecodeResult{}, err
			}

			laneIndex, err := p.ReadByte()
			if err != nil {
				return bytecodeResult{}, err
			}
			bytecode = append(
				bytecode,
				align,
				memoryIndex,
				offset,
				uint64(laneIndex),
			)
		case i8x16Shuffle:
			for range 16 {
				val, err := p.ReadByte()
				if err != nil {
					return bytecodeResult{}, err
				}
				bytecode = append(bytecode, uint64(val))
			}
		default:
			// No operands
		}

		if stopAtEnd && opcodeVal == end {
			break
		}
	}

	if len(bytecode) == 0 || lastOp != end {
		return bytecodeResult{}, errMissingEndOpcode
	}

	return bytecodeResult{
		bytecode:      bytecode,
		jumpCache:     jumpCache,
		jumpElseCache: jumpElseCache,
	}, nil
}

func (p *parser) readOpcode() (opcode, error) {
	opcodeByte, err := p.ReadByte()
	if err != nil {
		return 0, err
	}

	// Standard single-byte opcode.
	if opcodeByte < 0xFC {
		return opcode(opcodeByte), nil
	}

	// Multi-byte opcode (prefixed with 0xFC or 0xFD).
	if opcodeByte != 0xFC && opcodeByte != 0xFD {
		return 0, fmt.Errorf("unrecognized opcode prefix: 0x%X", opcodeByte)
	}

	val, err := p.readUint32()
	if err != nil {
		return 0, err
	}
	if val > math.MaxUint8 {
		return 0, errPrefixedOpcodeOutOfRange
	}

	return opcode(uint32(opcodeByte)<<8 | uint32(val)), nil
}

func (p *parser) readImmediateVector() ([]uint64, error) {
	size, err := p.readUint32()
	if err != nil {
		return nil, err
	}

	immediates := make([]uint64, 0, min(size, maxInitialCapacity))
	for range size {
		val, err := p.readUint32()
		if err != nil {
			return nil, err
		}
		immediates = append(immediates, val)
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

	// The alignment exponent must be < 32. Bit 6 is not part of it: it marks an
	// explicit memory index, which only multi-memory allows.
	if (align & ^sixthBitMask) >= 32 {
		return 0, 0, 0, errMalformedMemopFlags
	}

	memoryIndex := uint64(0)
	if align&sixthBitMask != 0 {
		if !p.config.ExperimentalMultipleMemories {
			return 0, 0, 0, errMalformedMemopFlags
		}
		memoryIndex, err = p.readUint32()
		if err != nil {
			return 0, 0, 0, err
		}
	}

	offset, err := p.readUleb128(64)
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

func (p *parser) readBytes(n uint) ([]byte, error) {
	bytes := make([]byte, n)
	if _, err := io.ReadFull(p, bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// readUint32 still returns a uint64, but checks that the value can be
// interpreted as a WASM u32.
func (p *parser) readUint32() (uint64, error) {
	return p.readUleb128(32)
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

// readUleb128 decodes an unsigned LEB128-encoded integer.
func (p *parser) readUleb128(bitWidth uint) (uint64, error) {
	var value uint64
	maxBytes := (bitWidth + 6) / 7
	for byteIndex := uint(0); byteIndex < maxBytes; byteIndex++ {
		b, err := p.ReadByte()
		if err != nil {
			if byteIndex > 0 && err == errSectionTruncated {
				return 0, errIntRepresentationTooLong
			}
			return 0, err
		}

		shift := byteIndex * 7
		group := b & payloadMask
		remainingBits := bitWidth - shift
		if remainingBits < 7 && uint(group) >= 1<<remainingBits {
			return 0, errIntegerTooLarge
		}
		value |= uint64(group) << shift
		if b&continuationBit == 0 {
			return value, nil
		}
	}
	return 0, errIntRepresentationTooLong
}

// readSleb128 decodes a signed 64-bit integer immediate (SLEB128).
func (p *parser) readSleb128(maxBytes int) (uint64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	bytesRead := 0

	for {
		b, err = p.ReadByte()
		if err != nil {
			if bytesRead > 0 && err == errSectionTruncated {
				return 0, errIntRepresentationTooLong
			}
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
