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
	"fmt"
	"math"
)

var (
	errAlignmentTooLarge           = errors.New("alignment too large")
	errBrLabelArityMismatch        = errors.New("br label arity mismatch")
	errBrLabelIndexOutOfBounds     = errors.New("br label index out of bounds")
	errControlStackEmpty           = errors.New("control stack empty")
	errDataCountNotSet             = errors.New("data count not set")
	errDataIndexOutOfBounds        = errors.New("data index out of bounds")
	errDuplicateExport             = errors.New("duplicate export")
	errElementIndexOutOfBounds     = errors.New("element index out of bounds")
	errElseMustMatchIf             = errors.New("else must match if")
	errFunctionIndexOutOfBounds    = errors.New("function index out of bounds")
	errGlobalIndexOutOfBounds      = errors.New("global index out of bounds")
	errGlobalIsImmutable           = errors.New("global is immutable")
	errInvalidCallIndirectType     = errors.New("invalid call_indirect type")
	errInvalidConstantExpression   = errors.New("invalid constant expression")
	errInvalidLimits               = errors.New("invalid limits")
	errInvalidRefNullType          = errors.New("invalid ref.null type")
	errInvalidStartFunction        = errors.New("invalid start function")
	errInvalidTableType            = errors.New("invalid table type")
	errLocalIndexOutOfBounds       = errors.New("local index out of bounds")
	errMemoryIndexOutOfBounds      = errors.New("memory index out of bounds")
	errMultipleMemoriesNotEnabled  = errors.New("multiple memories not enabled")
	errReturnTypeNotSet            = errors.New("return type not set")
	errSimdLaneIndexOutOfBounds    = errors.New("simd lane index out of bounds")
	errStackHeightMismatch         = errors.New("stack height mismatch")
	errTableIndexOutOfBounds       = errors.New("table index out of bounds")
	errTypeMismatch                = errors.New("type mismatch")
	errUnclosedControlFrame        = errors.New("unclosed control frame")
	errUndeclaredFunctionReference = errors.New("undeclared function reference")
	errValueStackUnderflow         = errors.New("value stack underflow")
)

type bottomType struct{}

func (bottomType) isValueType() {}

var bottom ValueType = bottomType{}

func isNumber(vt ValueType) bool {
	_, ok := vt.(NumberType)
	return ok || vt == bottom
}

func isVector(vt ValueType) bool {
	_, ok := vt.(VectorType)
	return ok || vt == bottom
}

type validationControlFrame struct {
	opcode      opcode
	startTypes  []ValueType
	endTypes    []ValueType
	height      int // The height of valueStack when the frame was pushed.
	unreachable bool
}

type validator struct {
	valueStack          []ValueType
	controlStack        []validationControlFrame
	locals              []ValueType
	returnType          []ValueType
	typeDefs            []FunctionType
	funcTypes           []FunctionType
	tableTypes          []TableType
	memTypes            []MemoryType
	globalTypes         []GlobalType
	importedTypes       []GlobalType // Only includes imported globals.
	elemTypes           []ReferenceType
	dataCount           *uint64
	referencedFunctions map[uint32]bool
	config              Config
	code                []uint64
	pc                  uint
}

func newValidator(config Config) *validator {
	return &validator{
		valueStack:          make([]ValueType, 0),
		controlStack:        make([]validationControlFrame, 0),
		locals:              make([]ValueType, 0),
		returnType:          make([]ValueType, 0),
		referencedFunctions: make(map[uint32]bool),
		config:              config,
	}
}

func (v *validator) validateModule(module *moduleDefinition) error {
	v.typeDefs = module.types
	v.funcTypes = make([]FunctionType, 0, len(module.imports)+len(module.funcs))
	v.tableTypes = make([]TableType, 0, len(module.imports)+len(module.tables))
	v.memTypes = make([]MemoryType, 0, len(module.imports)+len(module.memories))
	v.globalTypes = make(
		[]GlobalType,
		0,
		len(module.imports)+len(module.globalVariables),
	)
	v.importedTypes = make([]GlobalType, 0, len(module.imports))

	for _, imp := range module.imports {
		switch t := imp.importType.(type) {
		case functionTypeIndex:
			if uint32(t) >= uint32(len(v.typeDefs)) {
				return errTypeMismatch
			}

			v.funcTypes = append(v.funcTypes, module.types[t])
		case TableType:
			v.tableTypes = append(v.tableTypes, t)
		case MemoryType:
			v.memTypes = append(v.memTypes, t)
		case GlobalType:
			v.globalTypes = append(v.globalTypes, t)
			v.importedTypes = append(v.importedTypes, t)
		}
	}

	for _, function := range module.funcs {
		if function.typeIndex >= uint32(len(module.types)) {
			return errFunctionIndexOutOfBounds
		}
		v.funcTypes = append(v.funcTypes, module.types[function.typeIndex])
	}

	for _, table := range module.tables {
		if err := validateLimits(table.Limits, math.MaxUint32); err != nil {
			return err
		}
		v.tableTypes = append(v.tableTypes, table)
	}

	for _, memoryType := range module.memories {
		if err := validateLimits(memoryType.Limits, uint32(1)<<16); err != nil {
			return err
		}
		v.memTypes = append(v.memTypes, memoryType)
	}

	if !v.config.ExperimentalMultipleMemories && len(v.memTypes) > 1 {
		return errMultipleMemoriesNotEnabled
	}

	for _, globalVariable := range module.globalVariables {
		if err := v.validateGlobal(&globalVariable); err != nil {
			return err
		}
		v.globalTypes = append(v.globalTypes, globalVariable.globalType)
	}

	exportNamesSet := make(map[string]struct{}, len(module.exports))
	for _, export := range module.exports {
		if _, ok := exportNamesSet[export.name]; ok {
			return errDuplicateExport
		}
		exportNamesSet[export.name] = struct{}{}
		if err := v.validateExport(&export); err != nil {
			return err
		}
	}

	if err := v.validateStartIndex(module.startIndex); err != nil {
		return err
	}

	v.elemTypes = make([]ReferenceType, len(module.elementSegments))
	for i, elem := range module.elementSegments {
		if err := v.validateElementSegment(&elem); err != nil {
			return err
		}
		v.elemTypes[i] = elem.kind
	}

	v.dataCount = module.dataCount
	for _, data := range module.dataSegments {
		if err := v.validateDataSegment(&data); err != nil {
			return err
		}
	}

	for _, function := range module.funcs {
		if err := v.validateFunction(&function); err != nil {
			return err
		}
	}

	return nil
}

func (v *validator) validateGlobal(global *globalVariable) error {
	globalType := global.globalType
	initExpr := global.initExpression
	return v.validateConstExpression(initExpr, globalType.ValueType)
}

func (v *validator) validateExport(export *export) error {
	switch export.indexType {
	case functionExportKind:
		if err := v.validateFunctionTypeExists(export.index); err != nil {
			return err
		}
		v.referencedFunctions[export.index] = true
	case tableExportKind:
		if err := v.validateTableExists(export.index); err != nil {
			return err
		}
	case memoryExportKind:
		if err := v.validateMemoryExists(export.index); err != nil {
			return err
		}
	case globalExportKind:
		if err := v.validateGlobalExists(export.index); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) validateStartIndex(index *uint32) error {
	if index == nil {
		return nil
	}
	if err := v.validateFunctionTypeExists(*index); err != nil {
		return err
	}

	startFunctionType := v.funcTypes[*index]
	if len(startFunctionType.ParamTypes) != 0 {
		return errInvalidStartFunction
	}
	if len(startFunctionType.ResultTypes) != 0 {
		return errInvalidStartFunction
	}

	return nil
}

func (v *validator) validateElementSegment(elem *elementSegment) error {
	if elem.mode == activeElementMode {
		if err := v.validateTableExists(elem.tableIndex); err != nil {
			return err
		}

		expression := elem.offsetExpression
		if err := v.validateConstExpression(expression, I32); err != nil {
			return err
		}

		if v.tableTypes[elem.tableIndex].ReferenceType != elem.kind {
			return errTypeMismatch
		}
	}

	for _, expr := range elem.functionIndexesExpressions {
		if err := v.validateConstExpression(expr, elem.kind); err != nil {
			return err
		}
	}

	for _, funcIndex := range elem.functionIndexes {
		v.referencedFunctions[uint32(funcIndex)] = true
		if elem.kind != FuncRefType {
			return errTypeMismatch
		}
		if err := v.validateFunctionTypeExists(uint32(funcIndex)); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) validateDataSegment(data *dataSegment) error {
	if data.mode != activeDataMode {
		return nil
	}
	if err := v.validateMemoryExists(data.memoryIndex); err != nil {
		return err
	}
	return v.validateConstExpression(data.offsetExpression, I32)
}

func (v *validator) validateFunction(function *function) error {
	functionType := v.typeDefs[function.typeIndex]
	v.locals = append(functionType.ParamTypes, function.locals...)
	v.returnType = functionType.ResultTypes
	v.valueStack = v.valueStack[:0]
	v.controlStack = v.controlStack[:0]
	v.code = function.body
	v.pc = 0

	v.pushControlFrame(block, []ValueType{}, functionType.ResultTypes)

	for v.pc < uint(len(v.code)) {
		op := opcode(v.next())
		if err := v.validate(op); err != nil {
			return err
		}
	}

	// The parser strips the trailing End instruction from the function body,
	// but we still need to validate that the control frame is properly closed.
	// We don't call validateEnd() here because that would push endTypes onto
	// the stack, which is only needed for nested blocks.
	_, err := v.popControlFrame()
	if err != nil {
		return err
	}
	if len(v.controlStack) != 0 {
		return errUnclosedControlFrame
	}
	return nil
}

func (v *validator) validateConstExpression(
	data []uint64,
	expectedReturnType ValueType,
) error {
	v.locals = nil
	v.returnType = []ValueType{expectedReturnType}
	v.valueStack = v.valueStack[:0]
	v.controlStack = v.controlStack[:0]
	v.code = data
	v.pc = 0

	v.pushControlFrame(block, []ValueType{}, []ValueType{expectedReturnType})

	for v.pc < uint(len(v.code)) {
		op := opcode(v.next())

		if !v.isConstantOpcode(op) {
			return errInvalidConstantExpression
		}

		// The order of the statements is important here. The validation for RefFunc
		// checks whether the function index is referenced. Therefore, we must add
		// the function index to the referencedFunctions map before validating the
		// instruction.
		if op == refFunc {
			v.referencedFunctions[uint32(v.code[v.pc])] = true
		}

		if err := v.validate(op); err != nil {
			return err
		}
	}

	// Same as validateFunction, we are working around the fact the parser
	// strips the trailing End instruction from the function body.
	_, err := v.popControlFrame()
	return err
}

func (v *validator) isConstantOpcode(op opcode) bool {
	if op == globalGet {
		globalIndex := v.code[v.pc]
		if globalIndex >= uint64(len(v.importedTypes)) {
			return false
		}

		return !v.importedTypes[globalIndex].IsMutable
	}

	return op == i32Const ||
		op == i64Const ||
		op == f32Const ||
		op == f64Const ||
		op == v128Const ||
		op == refNull ||
		op == refFunc
}

func (v *validator) validate(op opcode) error {
	switch op {
	case unreachable:
		return v.markFrameUnreachable()
	case nop:
		return nil
	case block, loop:
		return v.validateBlock(op)
	case ifOp:
		return v.validateIf()
	case elseOp:
		return v.validateElse()
	case end:
		return v.validateEnd()
	case br:
		return v.validateBr()
	case brIf:
		return v.validateBrIf()
	case brTable:
		return v.validateBrTable()
	case returnOp:
		return v.validateReturn()
	case call:
		return v.validateCall()
	case callIndirect:
		return v.validateCallIndirect()
	case drop:
		return v.validateDrop()
	case selectOp:
		return v.validateSelect(bottom)
	case selectT:
		return v.validateSelectT()
	case localGet:
		return v.validateLocalGet()
	case localSet:
		return v.validateLocalSet()
	case memorySize:
		return v.validateMemorySize()
	case memoryGrow:
		return v.validateMemoryGrow()
	case i32Const:
		return v.validateConst(I32)
	case i64Const:
		return v.validateConst(I64)
	case f32Const:
		return v.validateConst(F32)
	case f64Const:
		return v.validateConst(F64)
	case i32Eq, i32Ne, i32LtS, i32LtU, i32GtS, i32GtU, i32LeS, i32LeU, i32GeS,
		i32GeU, i32Add, i32Sub, i32Mul, i32DivS, i32DivU, i32RemS, i32RemU, i32And,
		i32Or, i32Xor, i32Shl, i32ShrS, i32ShrU, i32Rotl, i32Rotr:
		return v.validateBinaryOp(I32, I32)
	case i64Eq, i64Ne, i64LtS, i64LtU, i64GtS, i64GtU, i64LeS, i64LeU, i64GeS,
		i64GeU:
		return v.validateBinaryOp(I64, I32)
	case f32Eq, f32Ne, f32Lt, f32Gt, f32Le, f32Ge:
		return v.validateBinaryOp(F32, I32)
	case f64Eq, f64Ne, f64Lt, f64Gt, f64Le, f64Ge:
		return v.validateBinaryOp(F64, I32)
	case i64Add, i64Sub, i64Mul, i64DivS, i64DivU, i64RemS, i64RemU, i64And,
		i64Or, i64Xor, i64Shl, i64ShrS, i64ShrU, i64Rotl, i64Rotr:
		return v.validateBinaryOp(I64, I64)
	case f32Add, f32Sub, f32Mul, f32Div, f32Min, f32Max, f32Copysign:
		return v.validateBinaryOp(F32, F32)
	case f64Add, f64Sub, f64Mul, f64Div, f64Min, f64Max, f64Copysign:
		return v.validateBinaryOp(F64, F64)
	case i32Eqz, i32Clz, i32Ctz, i32Popcnt, i32Extend8S, i32Extend16S:
		return v.validateUnaryOp(I32, I32)
	case i64Eqz, i32WrapI64:
		return v.validateUnaryOp(I64, I32)
	case i64Clz, i64Ctz, i64Popcnt, i64Extend8S, i64Extend16S, i64Extend32S:
		return v.validateUnaryOp(I64, I64)
	case f32Abs, f32Neg, f32Ceil, f32Floor, f32Trunc, f32Nearest, f32Sqrt:
		return v.validateUnaryOp(F32, F32)
	case f64Abs, f64Neg, f64Ceil, f64Floor, f64Trunc, f64Nearest, f64Sqrt:
		return v.validateUnaryOp(F64, F64)
	case i32TruncF32S, i32TruncF32U, i32ReinterpretF32, i32TruncSatF32S,
		i32TruncSatF32U:
		return v.validateUnaryOp(F32, I32)
	case i32TruncF64S, i32TruncF64U, i32TruncSatF64S, i32TruncSatF64U:
		return v.validateUnaryOp(F64, I32)
	case i64ExtendI32S, i64ExtendI32U:
		return v.validateUnaryOp(I32, I64)
	case i64TruncF32S, i64TruncF32U, i64TruncSatF32S, i64TruncSatF32U:
		return v.validateUnaryOp(F32, I64)
	case i64TruncF64S, i64TruncF64U, i64ReinterpretF64, i64TruncSatF64S,
		i64TruncSatF64U:
		return v.validateUnaryOp(F64, I64)
	case f32ConvertI32S, f32ConvertI32U, f32ReinterpretI32:
		return v.validateUnaryOp(I32, F32)
	case f32ConvertI64S, f32ConvertI64U:
		return v.validateUnaryOp(I64, F32)
	case f32DemoteF64:
		return v.validateUnaryOp(F64, F32)
	case f64ConvertI32S, f64ConvertI32U:
		return v.validateUnaryOp(I32, F64)
	case f64ConvertI64S, f64ConvertI64U, f64ReinterpretI64:
		return v.validateUnaryOp(I64, F64)
	case f64PromoteF32:
		return v.validateUnaryOp(F32, F64)
	case refNull:
		return v.validateRefNull()
	case refIsNull:
		return v.validateRefIsNull()
	case refFunc:
		return v.validateRefFunc()
	case tableGet:
		return v.validateTableGet()
	case tableSet:
		return v.validateTableSet()
	case tableInit:
		return v.validateTableInit()
	case tableCopy:
		return v.validateTableCopy()
	case tableGrow:
		return v.validateTableGrow()
	case tableSize:
		return v.validateTableSize()
	case tableFill:
		return v.validateTableFill()
	case elemDrop:
		return v.validateElemDrop()
	case dataDrop:
		return v.validateDataDrop()
	case localTee:
		return v.validateLocalTee()
	case globalGet:
		return v.validateGlobalGet()
	case globalSet:
		return v.validateGlobalSet()
	case i32Load:
		return v.validateLoad(I32, 4)
	case i32Load8S, i32Load8U:
		return v.validateLoad(I32, 1)
	case i32Load16S, i32Load16U:
		return v.validateLoad(I32, 2)
	case i64Load:
		return v.validateLoad(I64, 8)
	case i64Load8S, i64Load8U:
		return v.validateLoad(I64, 1)
	case i64Load16S, i64Load16U:
		return v.validateLoad(I64, 2)
	case i64Load32S, i64Load32U:
		return v.validateLoad(I64, 4)
	case f32Load:
		return v.validateLoad(F32, 4)
	case f64Load:
		return v.validateLoad(F64, 8)
	case i32Store:
		return v.validateStore(I32, 4)
	case i32Store8:
		return v.validateStore(I32, 1)
	case i32Store16:
		return v.validateStore(I32, 2)
	case i64Store:
		return v.validateStore(I64, 8)
	case i64Store8:
		return v.validateStore(I64, 1)
	case i64Store16:
		return v.validateStore(I64, 2)
	case i64Store32:
		return v.validateStore(I64, 4)
	case f32Store:
		return v.validateStore(F32, 4)
	case f64Store:
		return v.validateStore(F64, 8)
	case v128Load:
		return v.validateLoad(V128, 16)
	case v128Load8x8S, v128Load8x8U, v128Load16x4S, v128Load16x4U, v128Load32x2S,
		v128Load32x2U, v128Load64Splat, v128Load64Zero:
		return v.validateLoad(V128, 8)
	case v128Load8Splat:
		return v.validateLoad(V128, 1)
	case v128Load16Splat:
		return v.validateLoad(V128, 2)
	case v128Load32Splat, v128Load32Zero:
		return v.validateLoad(V128, 4)
	case v128Store:
		return v.validateStore(V128, 16)
	case v128Const:
		return v.validateV128Const()
	case v128Load8Lane:
		return v.validateSimdLoadLane(1)
	case v128Load16Lane:
		return v.validateSimdLoadLane(2)
	case v128Load32Lane:
		return v.validateSimdLoadLane(4)
	case v128Load64Lane:
		return v.validateSimdLoadLane(8)
	case v128Store8Lane:
		return v.validateSimdStoreLane(1)
	case v128Store16Lane:
		return v.validateSimdStoreLane(2)
	case v128Store32Lane:
		return v.validateSimdStoreLane(4)
	case v128Store64Lane:
		return v.validateSimdStoreLane(8)
	case i8x16ExtractLaneS, i8x16ExtractLaneU:
		return v.validateSimdExtractLane(16, I32)
	case i16x8ExtractLaneS, i16x8ExtractLaneU:
		return v.validateSimdExtractLane(8, I32)
	case i32x4ExtractLane:
		return v.validateSimdExtractLane(4, I32)
	case i64x2ExtractLane:
		return v.validateSimdExtractLane(2, I64)
	case f32x4ExtractLane:
		return v.validateSimdExtractLane(4, F32)
	case f64x2ExtractLane:
		return v.validateSimdExtractLane(2, F64)
	case v128AnyTrue, i8x16AllTrue,
		i8x16Bitmask, i16x8AllTrue, i16x8Bitmask, i32x4AllTrue, i32x4Bitmask,
		i64x2AllTrue, i64x2Bitmask:
		return v.validateUnaryOp(V128, I32)
	case v128Not, i8x16Abs, i8x16Neg, i8x16Popcnt, i16x8Abs, i16x8Neg,
		i16x8ExtaddPairwiseI8x16S, i16x8ExtaddPairwiseI8x16U, i32x4Abs, i32x4Neg,
		i32x4ExtaddPairwiseI16x8S, i32x4ExtaddPairwiseI16x8U, i64x2Abs, i64x2Neg,
		f32x4Abs, f32x4Neg, f32x4Sqrt, f32x4Ceil, f32x4Floor, f32x4Trunc,
		f32x4Nearest, f64x2Abs, f64x2Neg, f64x2Sqrt, f64x2Ceil, f64x2Floor,
		f64x2Trunc, f64x2Nearest, i32x4TruncSatF32x4S, i32x4TruncSatF32x4U,
		f32x4ConvertI32x4S, f32x4ConvertI32x4U, i16x8ExtendLowI8x16S,
		i16x8ExtendHighI8x16S, i16x8ExtendLowI8x16U, i16x8ExtendHighI8x16U,
		i32x4ExtendLowI16x8S, i32x4ExtendHighI16x8S, i32x4ExtendLowI16x8U,
		i32x4ExtendHighI16x8U, i64x2ExtendLowI32x4S, i64x2ExtendHighI32x4S,
		i64x2ExtendLowI32x4U, i64x2ExtendHighI32x4U, f64x2PromoteLowF32x4,
		f32x4DemoteF64x2Zero, i32x4TruncSatF64x2SZero, i32x4TruncSatF64x2UZero,
		f64x2ConvertLowI32x4S, f64x2ConvertLowI32x4U:
		return v.validateUnaryOp(V128, V128)
	case i8x16Shuffle:
		return v.validateShuffle()
	case v128And, v128Andnot, v128Or, v128Xor, i8x16Swizzle,
		i8x16Eq, i8x16Ne, i8x16LtS, i8x16LtU, i8x16GtS, i8x16GtU, i8x16LeS,
		i8x16LeU, i8x16GeS, i8x16GeU, i16x8Eq, i16x8Ne, i16x8LtS, i16x8LtU,
		i16x8GtS, i16x8GtU, i16x8LeS, i16x8LeU, i16x8GeS, i16x8GeU, i32x4Eq,
		i32x4Ne, i32x4LtS, i32x4LtU, i32x4GtS, i32x4GtU, i32x4LeS, i32x4LeU,
		i32x4GeS, i32x4GeU, i64x2Eq, i64x2Ne, i64x2LtS, i64x2GtS, i64x2LeS,
		i64x2GeS, f32x4Eq, f32x4Ne, f32x4Lt, f32x4Gt, f32x4Le, f32x4Ge, f64x2Eq,
		f64x2Ne, f64x2Lt, f64x2Gt, f64x2Le, f64x2Ge, i8x16Add, i8x16AddSatS,
		i8x16AddSatU, i8x16Sub, i8x16SubSatS, i8x16SubSatU, i8x16MinS, i8x16MinU,
		i8x16MaxS, i8x16MaxU, i8x16AvgrU, i16x8Add, i16x8AddSatS, i16x8AddSatU,
		i16x8Sub, i16x8SubSatS, i16x8SubSatU, i16x8Mul, i16x8MinS, i16x8MinU,
		i16x8MaxS, i16x8MaxU, i16x8AvgrU, i16x8Q15mulrSatS, i32x4Add, i32x4Sub,
		i32x4Mul, i32x4MinS, i32x4MinU, i32x4MaxS, i32x4MaxU, i32x4DotI16x8S,
		i64x2Add, i64x2Sub, i64x2Mul, f32x4Add, f32x4Sub, f32x4Mul, f32x4Div,
		f32x4Min, f32x4Max, f32x4Pmin, f32x4Pmax, f64x2Add, f64x2Sub, f64x2Mul,
		f64x2Div, f64x2Min, f64x2Max, f64x2Pmin, f64x2Pmax, i8x16NarrowI16x8S,
		i8x16NarrowI16x8U, i16x8NarrowI32x4S, i16x8NarrowI32x4U,
		i16x8ExtmulLowI8x16S, i16x8ExtmulHighI8x16S, i16x8ExtmulLowI8x16U,
		i16x8ExtmulHighI8x16U, i32x4ExtmulLowI16x8S, i32x4ExtmulHighI16x8S,
		i32x4ExtmulLowI16x8U, i32x4ExtmulHighI16x8U, i64x2ExtmulLowI32x4S,
		i64x2ExtmulHighI32x4S, i64x2ExtmulLowI32x4U, i64x2ExtmulHighI32x4U:
		return v.validateBinaryOp(V128, V128)
	case v128Bitselect:
		return v.validateBitselect()
	case i8x16Splat, i16x8Splat, i32x4Splat:
		return v.validateUnaryOp(I32, V128)
	case i64x2Splat:
		return v.validateUnaryOp(I64, V128)
	case f32x4Splat:
		return v.validateUnaryOp(F32, V128)
	case f64x2Splat:
		return v.validateUnaryOp(F64, V128)
	case i8x16ReplaceLane:
		return v.validateSimdReplaceLane(16, I32)
	case i16x8ReplaceLane:
		return v.validateSimdReplaceLane(8, I32)
	case i32x4ReplaceLane:
		return v.validateSimdReplaceLane(4, I32)
	case i64x2ReplaceLane:
		return v.validateSimdReplaceLane(2, I64)
	case f32x4ReplaceLane:
		return v.validateSimdReplaceLane(4, F32)
	case f64x2ReplaceLane:
		return v.validateSimdReplaceLane(2, F64)
	case i8x16Shl, i8x16ShrS, i8x16ShrU, i16x8Shl, i16x8ShrS, i16x8ShrU,
		i32x4Shl, i32x4ShrS, i32x4ShrU, i64x2Shl, i64x2ShrS, i64x2ShrU:
		return v.validateVectorScalar(I32)
	case memoryInit:
		return v.validateMemoryInit()
	case memoryCopy:
		return v.validateMemoryCopy()
	case memoryFill:
		return v.validateMemoryFill()
	default:
		return fmt.Errorf("unknown opcode %d", op)
	}
}

func (v *validator) validateBlock(op opcode) error {
	startTypes, endTypes := v.getBlockTypes(int32(v.next()))
	if _, err := v.popExpectedValues(startTypes); err != nil {
		return err
	}

	v.pushControlFrame(op, startTypes, endTypes)
	return nil
}

func (v *validator) validateIf() error {
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	return v.validateBlock(ifOp)
}

func (v *validator) validateElse() error {
	frame, err := v.popControlFrame()
	if err != nil {
		return err
	}

	if frame.opcode != ifOp {
		return errElseMustMatchIf
	}

	v.pushControlFrame(elseOp, frame.startTypes, frame.endTypes)
	return nil
}

func (v *validator) validateEnd() error {
	frame, err := v.popControlFrame()
	if err != nil {
		return err
	}

	if frame.opcode == ifOp {
		// This is the end of an if without an else, therefore the end types
		// should match the start types.
		if len(frame.startTypes) != len(frame.endTypes) {
			return errTypeMismatch
		}
		for i := range frame.startTypes {
			if frame.startTypes[i] != frame.endTypes[i] {
				return errTypeMismatch
			}
		}
	}

	v.pushValues(frame.endTypes)
	return nil
}

func (v *validator) validateBr() error {
	labelIndex := uint32(v.next())
	if labelIndex >= uint32(len(v.controlStack)) {
		return errBrLabelIndexOutOfBounds
	}

	frameIndex := len(v.controlStack) - 1 - int(labelIndex)
	labelTypes := v.labelTypes(v.controlStack[frameIndex])
	if _, err := v.popExpectedValues(labelTypes); err != nil {
		return err
	}

	return v.markFrameUnreachable()
}

func (v *validator) validateBrIf() error {
	labelIndex := uint32(v.next())
	if labelIndex >= uint32(len(v.controlStack)) {
		return errBrLabelIndexOutOfBounds
	}

	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}

	frameIndex := len(v.controlStack) - 1 - int(labelIndex)
	labelTypes := v.labelTypes(v.controlStack[frameIndex])
	if _, err := v.popExpectedValues(labelTypes); err != nil {
		return err
	}

	v.pushValues(labelTypes)
	return nil
}

func (v *validator) validateBrTable() error {
	size := v.next()
	table := v.code[v.pc : v.pc+uint(size)]
	v.pc += uint(size)
	labelIndex := uint32(v.next())

	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}

	if labelIndex >= uint32(len(v.controlStack)) {
		return errBrLabelIndexOutOfBounds
	}

	frameIndex := len(v.controlStack) - 1 - int(labelIndex)
	labelTypes := v.labelTypes(v.controlStack[frameIndex])
	arity := len(labelTypes)

	for _, index := range table {
		if index >= uint64(len(v.controlStack)) {
			return errBrLabelIndexOutOfBounds
		}

		frameIndex := len(v.controlStack) - 1 - int(index)
		labelTypes := v.labelTypes(v.controlStack[frameIndex])
		if len(labelTypes) != arity {
			return errBrLabelArityMismatch
		}

		values, err := v.popExpectedValues(labelTypes)
		if err != nil {
			return err
		}
		v.pushValues(values)
	}

	if _, err := v.popExpectedValues(labelTypes); err != nil {
		return err
	}
	return v.markFrameUnreachable()
}

func (v *validator) validateReturn() error {
	if v.returnType == nil {
		return errReturnTypeNotSet
	}
	if _, err := v.popExpectedValues(v.returnType); err != nil {
		return err
	}
	return v.markFrameUnreachable()
}

func (v *validator) validateCall() error {
	functionIndex := uint32(v.next())
	if err := v.validateFunctionTypeExists(functionIndex); err != nil {
		return err
	}
	functionType := v.funcTypes[functionIndex]
	if _, err := v.popExpectedValues(functionType.ParamTypes); err != nil {
		return err
	}
	v.pushValues(functionType.ResultTypes)
	return nil
}

func (v *validator) validateCallIndirect() error {
	typeIndex := uint32(v.next())
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}

	tableType := v.tableTypes[tableIndex]
	if tableType.ReferenceType != FuncRefType {
		return errInvalidTableType
	}

	if typeIndex >= uint32(len(v.typeDefs)) {
		return errInvalidCallIndirectType
	}
	functionType := v.typeDefs[typeIndex]

	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	if _, err := v.popExpectedValues(functionType.ParamTypes); err != nil {
		return err
	}
	v.pushValues(functionType.ResultTypes)
	return nil
}

func (v *validator) validateDrop() error {
	_, err := v.popValue()
	return err
}

func (v *validator) validateSelect(t ValueType) error {
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	type2, err := v.popExpectedValue(t)
	if err != nil {
		return err
	}
	type1, err := v.popExpectedValue(t)
	if err != nil {
		return err
	}

	if t == bottom {
		if !((isNumber(type1) && isNumber(type2)) ||
			(isVector(type1) && isVector(type2))) {
			return errTypeMismatch
		}
		if type1 != type2 && type1 != bottom && type2 != bottom {
			return errTypeMismatch
		}
	}

	if type1 == bottom {
		v.pushValue(type2)
	} else {
		v.pushValue(type1)
	}

	return nil
}

func (v *validator) validateSelectT() error {
	size := v.next()
	t := toValueType(v.next())
	v.pc += uint(size - 1)
	return v.validateSelect(t)
}

func (v *validator) validateLocalTee() error {
	localIndex := v.next()
	localType, err := v.getLocalType(localIndex)
	if err != nil {
		return err
	}
	return v.validateUnaryOp(localType, localType)
}

func (v *validator) validateLocalGet() error {
	localType, err := v.getLocalType(v.next())
	if err != nil {
		return err
	}
	v.pushValue(localType)
	return nil
}

func (v *validator) validateLocalSet() error {
	localType, err := v.getLocalType(v.next())
	if err != nil {
		return err
	}
	_, err = v.popExpectedValue(localType)
	return err
}

func (v *validator) validateGlobalGet() error {
	globalIndex := v.next()
	if globalIndex >= uint64(len(v.globalTypes)) {
		return errGlobalIndexOutOfBounds
	}
	v.pushValue(v.globalTypes[globalIndex].ValueType)
	return nil
}

func (v *validator) validateGlobalSet() error {
	globalIndex := v.next()
	if globalIndex >= uint64(len(v.globalTypes)) {
		return errGlobalIndexOutOfBounds
	}

	globalType := v.globalTypes[globalIndex]
	if !globalType.IsMutable {
		return errGlobalIsImmutable
	}

	_, err := v.popExpectedValue(globalType.ValueType)
	return err
}

func (v *validator) validateLoad(valueType ValueType, sizeBytes uint32) error {
	align, _, _ := v.nextMemArg()

	if err := v.validateMemoryExists(0); err != nil {
		return err
	}

	if err := v.validateMemArg(align, sizeBytes); err != nil {
		return err
	}

	return v.validateUnaryOp(I32, valueType)
}

func (v *validator) validateStore(valueType ValueType, sizeBytes uint32) error {
	align, _, _ := v.nextMemArg()

	if err := v.validateMemoryExists(0); err != nil {
		return err
	}

	if err := v.validateMemArg(align, sizeBytes); err != nil {
		return err
	}

	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateMemArg(align uint64, nBytes uint32) error {
	if 1<<align > nBytes {
		return errAlignmentTooLarge
	}
	return nil
}

func (v *validator) validateMemorySize() error {
	memoryIndex := uint32(v.next())
	if !v.config.ExperimentalMultipleMemories && memoryIndex != 0 {
		return errMultipleMemoriesNotEnabled
	}

	if err := v.validateMemoryExists(memoryIndex); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateMemoryGrow() error {
	memoryIndex := uint32(v.next())
	if !v.config.ExperimentalMultipleMemories && memoryIndex != 0 {
		return errMultipleMemoriesNotEnabled
	}

	if err := v.validateMemoryExists(memoryIndex); err != nil {
		return err
	}
	return v.validateUnaryOp(I32, I32)
}

func (v *validator) validateMemoryFill() error {
	memoryIndex := uint32(v.next())
	if err := v.validateMemoryExists(memoryIndex); err != nil {
		return err
	}
	_, err := v.popExpectedValues([]ValueType{I32, I32, I32})
	return err
}

func (v *validator) validateMemoryInit() error {
	dataIndex := uint32(v.next())
	memoryIndex := uint32(v.next())
	if v.dataCount == nil {
		return errDataCountNotSet
	}

	if dataIndex >= uint32(*v.dataCount) {
		return errDataIndexOutOfBounds
	}
	if err := v.validateMemoryExists(memoryIndex); err != nil {
		return err
	}
	_, err := v.popExpectedValues([]ValueType{I32, I32, I32})
	return err
}

func (v *validator) validateMemoryCopy() error {
	destMemoryIndex := uint32(v.next())
	srcMemoryIndex := uint32(v.next())
	if err := v.validateMemoryExists(destMemoryIndex); err != nil {
		return err
	}
	if err := v.validateMemoryExists(srcMemoryIndex); err != nil {
		return err
	}
	_, err := v.popExpectedValues([]ValueType{I32, I32, I32})
	return err
}

func (v *validator) validateFunctionTypeExists(index uint32) error {
	if index >= uint32(len(v.funcTypes)) {
		return errFunctionIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateTableExists(tableIndex uint32) error {
	if tableIndex >= uint32(len(v.tableTypes)) {
		return errTableIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateMemoryExists(memoryIndex uint32) error {
	if memoryIndex >= uint32(len(v.memTypes)) {
		return errMemoryIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateGlobalExists(globalIndex uint32) error {
	if globalIndex >= uint32(len(v.globalTypes)) {
		return errGlobalIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateConst(valueType ValueType) error {
	v.next()
	v.pushValue(valueType)
	return nil
}

func (v *validator) validateV128Const() error {
	v.next() // low bits
	v.next() // high bits
	v.pushValue(V128)
	return nil
}

func (v *validator) validateRefNull() error {
	refTypeVal := v.next()
	refType := toValueType(refTypeVal)
	if _, ok := refType.(ReferenceType); !ok {
		return errInvalidRefNullType
	}
	v.pushValue(refType)
	return nil
}

func (v *validator) validateRefIsNull() error {
	if _, err := v.popValue(); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateRefFunc() error {
	funcIndex := uint32(v.next())
	if err := v.validateFunctionTypeExists(funcIndex); err != nil {
		return err
	}
	if !v.referencedFunctions[funcIndex] {
		return errUndeclaredFunctionReference
	}
	v.pushValue(FuncRefType)
	return nil
}

func (v *validator) validateTableGet() error {
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	return v.validateUnaryOp(I32, v.tableTypes[tableIndex].ReferenceType)
}

func (v *validator) validateSimdExtractLane(
	rangeVal uint64,
	scalarType ValueType,
) error {
	laneIndex := v.next()
	if laneIndex >= rangeVal {
		return errSimdLaneIndexOutOfBounds
	}
	return v.validateUnaryOp(V128, scalarType)
}

func (v *validator) validateSimdReplaceLane(
	rangeVal uint64,
	scalarType ValueType,
) error {
	laneIndex := v.next()
	if laneIndex >= rangeVal {
		return errSimdLaneIndexOutOfBounds
	}
	return v.validateVectorScalar(scalarType)
}

func (v *validator) validateTableSet() error {
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	referenceType := v.tableTypes[tableIndex].ReferenceType
	if _, err := v.popExpectedValue(referenceType); err != nil {
		return err
	}
	_, err := v.popExpectedValue(I32)
	return err
}

func (v *validator) validateTableInit() error {
	elemIndex := uint32(v.next())
	tableIndex := uint32(v.next())
	if elemIndex >= uint32(len(v.elemTypes)) {
		return errElementIndexOutOfBounds
	}
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}

	tableType := v.tableTypes[tableIndex]
	elemType := v.elemTypes[elemIndex]
	if tableType.ReferenceType != elemType {
		return errTypeMismatch
	}

	return nil
}

func (v *validator) validateTableCopy() error {
	destTableIndex := uint32(v.next())
	srcTableIndex := uint32(v.next())
	if err := v.validateTableExists(destTableIndex); err != nil {
		return err
	}
	if err := v.validateTableExists(srcTableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}

	destTableType := v.tableTypes[destTableIndex]
	srcTableType := v.tableTypes[srcTableIndex]
	if destTableType.ReferenceType != srcTableType.ReferenceType {
		return errTypeMismatch
	}

	return nil
}

func (v *validator) validateTableGrow() error {
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	referenceType := v.tableTypes[tableIndex].ReferenceType
	if _, err := v.popExpectedValue(referenceType); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateTableSize() error {
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateTableFill() error {
	tableIndex := uint32(v.next())
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	referenceType := v.tableTypes[tableIndex].ReferenceType
	if _, err := v.popExpectedValue(referenceType); err != nil {
		return err
	}
	_, err := v.popExpectedValue(I32)
	return err
}

func (v *validator) validateElemDrop() error {
	elemIndex := uint32(v.next())
	if elemIndex >= uint32(len(v.elemTypes)) {
		return errElementIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateDataDrop() error {
	if v.dataCount == nil {
		return errDataCountNotSet
	}

	dataIndex := uint32(v.next())
	if dataIndex >= uint32(*v.dataCount) {
		return errDataIndexOutOfBounds
	}
	return nil
}

func (v *validator) validateUnaryOp(input ValueType, output ValueType) error {
	if _, err := v.popExpectedValue(input); err != nil {
		return err
	}
	v.pushValue(output)
	return nil
}

func (v *validator) validateBinaryOp(inputs ValueType, output ValueType) error {
	if _, err := v.popExpectedValue(inputs); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(inputs); err != nil {
		return err
	}
	v.pushValue(output)
	return nil
}

func (v *validator) validateBitselect() error {
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	v.pushValue(V128)
	return nil
}

func (v *validator) validateVectorScalar(scalar ValueType) error {
	if _, err := v.popExpectedValue(scalar); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	v.pushValue(V128)
	return nil
}

func (v *validator) validateSimdLoadLane(sizeBytes uint32) error {
	align, _, _ := v.nextMemArg()
	laneIndex := uint32(v.next())
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	if err := v.validateMemArg(align, sizeBytes); err != nil {
		return err
	}
	if laneIndex >= 16/sizeBytes {
		return errSimdLaneIndexOutOfBounds
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	v.pushValue(V128)
	return nil
}

func (v *validator) validateSimdStoreLane(sizeBytes uint32) error {
	align, _, _ := v.nextMemArg()
	v.next() // laneIndex
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	if err := v.validateMemArg(align, sizeBytes); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	_, err := v.popExpectedValue(I32)
	return err
}

func (v *validator) validateShuffle() error {
	v.pc += 16
	return v.validateBinaryOp(V128, V128)
}

func (v *validator) getLocalType(index uint64) (ValueType, error) {
	if index >= uint64(len(v.locals)) {
		return nil, errLocalIndexOutOfBounds
	}
	return v.locals[index], nil
}

func (v *validator) pushValue(value ValueType) {
	v.valueStack = append(v.valueStack, value)
}

func (v *validator) pushValues(values []ValueType) {
	for _, value := range values {
		v.pushValue(value)
	}
}

func (v *validator) popValue() (ValueType, error) {
	currentFrame, err := v.peekControlFrame()
	if err != nil {
		return nil, err
	}

	if len(v.valueStack) == currentFrame.height && currentFrame.unreachable {
		// Special case, can occur after an unconditional branch when the stack is
		// typed polymorphically.
		return bottom, nil
	}

	if len(v.valueStack) == currentFrame.height {
		return nil, errValueStackUnderflow
	}

	value := v.valueStack[len(v.valueStack)-1]
	v.valueStack = v.valueStack[:len(v.valueStack)-1]
	return value, nil
}

func (v *validator) popExpectedValue(expected ValueType) (ValueType, error) {
	val, err := v.popValue()
	if err != nil {
		return nil, err
	}
	if val != expected && val != bottom && expected != bottom {
		return nil, errTypeMismatch
	}
	return val, nil
}

func (v *validator) popExpectedValues(
	expected []ValueType,
) ([]ValueType, error) {
	values := make([]ValueType, len(expected))
	var err error
	for i := len(expected) - 1; i >= 0; i-- {
		values[i], err = v.popExpectedValue(expected[i])
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (v *validator) peekControlFrame() (*validationControlFrame, error) {
	if len(v.controlStack) == 0 {
		return nil, errControlStackEmpty
	}
	return &v.controlStack[len(v.controlStack)-1], nil
}

func (v *validator) pushControlFrame(opcode opcode, start, end []ValueType) {
	v.controlStack = append(v.controlStack, validationControlFrame{
		opcode:      opcode,
		startTypes:  start,
		endTypes:    end,
		height:      len(v.valueStack),
		unreachable: false,
	})
	v.pushValues(start)
}

func (v *validator) popControlFrame() (validationControlFrame, error) {
	if len(v.controlStack) == 0 {
		return validationControlFrame{}, errControlStackEmpty
	}
	frame := v.controlStack[len(v.controlStack)-1]
	if _, err := v.popExpectedValues(frame.endTypes); err != nil {
		return validationControlFrame{}, err
	}
	if len(v.valueStack) != frame.height {
		return validationControlFrame{}, errStackHeightMismatch
	}
	v.controlStack = v.controlStack[:len(v.controlStack)-1]
	return frame, nil
}

func (v *validator) labelTypes(frame validationControlFrame) []ValueType {
	if frame.opcode == loop {
		return frame.startTypes
	}
	return frame.endTypes
}

func (v *validator) markFrameUnreachable() error {
	frame, err := v.peekControlFrame()
	if err != nil {
		return err
	}
	v.valueStack = v.valueStack[:frame.height]
	frame.unreachable = true
	return nil
}

func (v *validator) getBlockTypes(blockType int32) ([]ValueType, []ValueType) {
	if blockType == -0x40 { // empty block type.
		return []ValueType{}, []ValueType{}
	}

	if blockType >= 0 {
		funcType := v.typeDefs[blockType]
		return funcType.ParamTypes, funcType.ResultTypes
	}

	return []ValueType{}, []ValueType{toValueType(uint64(blockType & 0x7F))}
}

func toValueType(code uint64) ValueType {
	switch code {
	case uint64(I32):
		return I32
	case uint64(I64):
		return I64
	case uint64(F32):
		return F32
	case uint64(F64):
		return F64
	case uint64(V128):
		return V128
	case uint64(FuncRefType):
		return FuncRefType
	case uint64(ExternRefType):
		return ExternRefType
	default:
		return bottom
	}
}

func validateLimits(limits Limits, maximumRange uint32) error {
	if limits.Min > maximumRange {
		return errInvalidLimits
	}

	if limits.Max == nil {
		return nil
	}

	if *limits.Max > maximumRange {
		return errInvalidLimits
	}

	if limits.Min > *limits.Max {
		return errInvalidLimits
	}

	return nil
}

// nextMemArg reads and returns the align, memoryIndex, and offset.
func (v *validator) nextMemArg() (uint64, uint64, uint64) {
	align := v.next()
	memoryIndex := v.next()
	offset := v.next()
	return align, memoryIndex, offset
}

func (v *validator) next() uint64 {
	val := v.code[v.pc]
	v.pc++
	return val
}
