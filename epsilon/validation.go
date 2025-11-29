package epsilon

import (
	"errors"
	"fmt"
)

type bottomType struct{}

func (bottomType) isValueType() {}

var Bottom ValueType = bottomType{}

func isNumber(vt ValueType) bool {
	_, ok := vt.(NumberType)
	return ok || vt == Bottom
}

func isVector(vt ValueType) bool {
	_, ok := vt.(VectorType)
	return ok || vt == Bottom
}

func isReference(vt ValueType) bool {
	_, ok := vt.(ReferenceType)
	return ok || vt == Bottom
}

type controlFrame struct {
	opcode      Opcode
	startTypes  []ValueType
	endTypes    []ValueType
	height      int // The height of valueStack when the frame was pushed.
	unreachable bool
}

type validator struct {
	valueStack   []ValueType
	controlStack []controlFrame
	locals       []ValueType
	returnType   []ValueType
	typeDefs     []FunctionType
	funcTypes    []FunctionType
	tableTypes   []TableType
	memTypes     []MemoryType
	globalTypes  []GlobalType
	elemTypes    []ReferenceType
	dataCount    int
}

func NewValidator() *validator {
	return &validator{
		valueStack:   make([]ValueType, 0),
		controlStack: make([]controlFrame, 0),
		locals:       make([]ValueType, 0),
		returnType:   make([]ValueType, 0),
	}
}

func (v *validator) validateModule(module *Module) error {
	v.typeDefs = module.Types
	v.funcTypes = make([]FunctionType, 0, len(module.Imports)+len(module.Funcs))
	v.tableTypes = make([]TableType, 0, len(module.Imports)+len(module.Tables))
	v.memTypes = make([]MemoryType, 0, len(module.Imports)+len(module.Memories))
	v.globalTypes = make(
		[]GlobalType,
		0,
		len(module.Imports)+len(module.GlobalVariables),
	)

	for _, imp := range module.Imports {
		switch t := imp.Type.(type) {
		case FunctionTypeIndex:
			v.funcTypes = append(v.funcTypes, module.Types[t])
		case TableType:
			v.tableTypes = append(v.tableTypes, t)
		case MemoryType:
			v.memTypes = append(v.memTypes, t)
		case GlobalType:
			v.globalTypes = append(v.globalTypes, t)
		}
	}

	for _, function := range module.Funcs {
		v.funcTypes = append(v.funcTypes, module.Types[function.TypeIndex])
	}
	v.tableTypes = append(v.tableTypes, module.Tables...)
	v.memTypes = append(v.memTypes, module.Memories...)
	for _, globalVariable := range module.GlobalVariables {
		v.globalTypes = append(v.globalTypes, globalVariable.GlobalType)
	}

	v.elemTypes = make([]ReferenceType, len(module.ElementSegments))
	for i, elem := range module.ElementSegments {
		v.elemTypes[i] = elem.Kind
	}
	v.dataCount = len(module.DataSegments)

	for _, function := range module.Funcs {
		if err := v.validateFunction(&function); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) validateFunction(function *Function) error {
	functionType := v.typeDefs[function.TypeIndex]
	v.locals = append(functionType.ParamTypes, function.Locals...)
	v.returnType = functionType.ResultTypes
	v.valueStack = v.valueStack[:0]
	v.controlStack = v.controlStack[:0]

	v.pushControlFrame(Block, []ValueType{}, functionType.ResultTypes)

	decoder := NewDecoder(function.Body)
	for decoder.HasMore() {
		instruction, err := decoder.Decode()
		if err != nil {
			return err
		}
		if err := v.validate(instruction); err != nil {
			return err
		}
	}

	// The parser strips the trailing End instruction from the function body,
	// but we still need to validate that the control frame is properly closed.
	// We don't call validateEnd() here because that would push endTypes onto
	// the stack, which is only needed for nested blocks.
	_, err := v.popControlFrame()
	return err
}

func (v *validator) validate(instruction Instruction) error {
	switch instruction.Opcode {
	case Unreachable:
		return v.markFrameUnreachable()
	case Nop:
		return nil
	case Block, Loop:
		return v.validateBlock(instruction)
	case If:
		return v.validateIf(instruction)
	case Else:
		return v.validateElse()
	case End:
		return v.validateEnd()
	case Br:
		return v.validateBr(instruction)
	case BrIf:
		return v.validateBrIf(instruction)
	case BrTable:
		return v.validateBrTable(instruction)
	case Return:
		return v.validateReturn()
	case Call:
		return v.validateCall(instruction)
	case CallIndirect:
		return v.validateCallIndirect(instruction)
	case Drop:
		return v.validateDrop()
	case Select:
		return v.validateSelect(Bottom)
	case SelectT:
		return v.validateSelect(toValueType(instruction.Immediates[0]))
	case LocalGet:
		return v.validateLocalGet(instruction)
	case LocalSet:
		return v.validateLocalSet(instruction)
	case MemorySize:
		return v.validateMemorySize()
	case MemoryGrow:
		return v.validateMemoryGrow()
	case I32Const:
		return v.validateConst(I32)
	case I64Const:
		return v.validateConst(I64)
	case F32Const:
		return v.validateConst(F32)
	case F64Const:
		return v.validateConst(F64)
	case I32Eq,
		I32Ne,
		I32LtS,
		I32LtU,
		I32GtS,
		I32GtU,
		I32LeS,
		I32LeU,
		I32GeS,
		I32GeU:
		return v.validateBinaryOp(I32, I32)
	case I64Eq,
		I64Ne,
		I64LtS,
		I64LtU,
		I64GtS,
		I64GtU,
		I64LeS,
		I64LeU,
		I64GeS,
		I64GeU:
		return v.validateBinaryOp(I64, I32)
	case F32Eq,
		F32Ne,
		F32Lt,
		F32Gt,
		F32Le,
		F32Ge:
		return v.validateBinaryOp(F32, I32)
	case F64Eq,
		F64Ne,
		F64Lt,
		F64Gt,
		F64Le,
		F64Ge:
		return v.validateBinaryOp(F64, I32)
	case I32Add,
		I32Sub,
		I32Mul,
		I32DivS,
		I32DivU,
		I32RemS,
		I32RemU,
		I32And,
		I32Or,
		I32Xor,
		I32Shl,
		I32ShrS,
		I32ShrU,
		I32Rotl,
		I32Rotr:
		return v.validateBinaryOp(I32, I32)
	case I64Add,
		I64Sub,
		I64Mul,
		I64DivS,
		I64DivU,
		I64RemS,
		I64RemU,
		I64And,
		I64Or,
		I64Xor,
		I64Shl,
		I64ShrS,
		I64ShrU,
		I64Rotl,
		I64Rotr:
		return v.validateBinaryOp(I64, I64)
	case F32Add, F32Sub, F32Mul, F32Div, F32Min, F32Max, F32Copysign:
		return v.validateBinaryOp(F32, F32)
	case F64Add, F64Sub, F64Mul, F64Div, F64Min, F64Max, F64Copysign:
		return v.validateBinaryOp(F64, F64)
	case I32Eqz:
		return v.validateUnaryOp(I32, I32)
	case I64Eqz:
		return v.validateUnaryOp(I64, I32)
	case I64Clz, I64Ctz, I64Popcnt:
		return v.validateUnaryOp(I64, I64)
	case I32Clz, I32Ctz, I32Popcnt:
		return v.validateUnaryOp(I32, I32)
	case F32Abs, F32Neg, F32Ceil, F32Floor, F32Trunc, F32Nearest, F32Sqrt:
		return v.validateUnaryOp(F32, F32)
	case F64Abs, F64Neg, F64Ceil, F64Floor, F64Trunc, F64Nearest, F64Sqrt:
		return v.validateUnaryOp(F64, F64)
	case I32WrapI64:
		return v.validateUnaryOp(I64, I32)
	case I32TruncF32S, I32TruncF32U:
		return v.validateUnaryOp(F32, I32)
	case I32TruncF64S, I32TruncF64U:
		return v.validateUnaryOp(F64, I32)
	case I64ExtendI32S, I64ExtendI32U:
		return v.validateUnaryOp(I32, I64)
	case I64TruncF32S, I64TruncF32U:
		return v.validateUnaryOp(F32, I64)
	case I64TruncF64S, I64TruncF64U:
		return v.validateUnaryOp(F64, I64)
	case F32ConvertI32S, F32ConvertI32U:
		return v.validateUnaryOp(I32, F32)
	case F32ConvertI64S, F32ConvertI64U:
		return v.validateUnaryOp(I64, F32)
	case F32DemoteF64:
		return v.validateUnaryOp(F64, F32)
	case F64ConvertI32S, F64ConvertI32U:
		return v.validateUnaryOp(I32, F64)
	case F64ConvertI64S, F64ConvertI64U:
		return v.validateUnaryOp(I64, F64)
	case F64PromoteF32:
		return v.validateUnaryOp(F32, F64)
	case I32ReinterpretF32:
		return v.validateUnaryOp(F32, I32)
	case I64ReinterpretF64:
		return v.validateUnaryOp(F64, I64)
	case F32ReinterpretI32:
		return v.validateUnaryOp(I32, F32)
	case F64ReinterpretI64:
		return v.validateUnaryOp(I64, F64)
	case I32Extend8S, I32Extend16S:
		return v.validateUnaryOp(I32, I32)
	case I64Extend8S, I64Extend16S, I64Extend32S:
		return v.validateUnaryOp(I64, I64)
	case I32TruncSatF32S, I32TruncSatF32U:
		return v.validateUnaryOp(F32, I32)
	case I32TruncSatF64S, I32TruncSatF64U:
		return v.validateUnaryOp(F64, I32)
	case I64TruncSatF32S, I64TruncSatF32U:
		return v.validateUnaryOp(F32, I64)
	case I64TruncSatF64S, I64TruncSatF64U:
		return v.validateUnaryOp(F64, I64)
	case RefNull:
		return v.validateRefNull(instruction)
	case RefIsNull:
		return v.validateRefIsNull()
	case RefFunc:
		return v.validateRefFunc(instruction)
	case TableGet:
		return v.validateTableGet(instruction)
	case TableSet:
		return v.validateTableSet(instruction)
	case TableInit:
		return v.validateTableInit(instruction)
	case TableCopy:
		return v.validateTableCopy(instruction)
	case TableGrow:
		return v.validateTableGrow(instruction)
	case TableSize:
		return v.validateTableSize(instruction)
	case TableFill:
		return v.validateTableFill(instruction)
	case ElemDrop:
		return v.validateElemDrop(instruction)
	case DataDrop:
		return v.validateDataDrop(instruction)
	case LocalTee:
		return v.validateLocalTee(instruction)
	case GlobalGet:
		return v.validateGlobalGet(instruction)
	case GlobalSet:
		return v.validateGlobalSet(instruction)
	case I32Load:
		return v.validateLoad(instruction, I32)
	case I32Load8S, I32Load8U:
		return v.validateLoadN(instruction, I32, 1)
	case I32Load16S, I32Load16U:
		return v.validateLoadN(instruction, I32, 2)
	case I64Load:
		return v.validateLoad(instruction, I64)
	case I64Load8S, I64Load8U:
		return v.validateLoadN(instruction, I64, 1)
	case I64Load16S, I64Load16U:
		return v.validateLoadN(instruction, I64, 2)
	case I64Load32S, I64Load32U:
		return v.validateLoadN(instruction, I64, 4)
	case F32Load:
		return v.validateLoad(instruction, F32)
	case F64Load:
		return v.validateLoad(instruction, F64)
	case I32Store:
		return v.validateStore(instruction, I32)
	case I32Store8:
		return v.validateStoreN(instruction, I32, 1)
	case I32Store16:
		return v.validateStoreN(instruction, I32, 2)
	case I64Store:
		return v.validateStore(instruction, I64)
	case I64Store8:
		return v.validateStoreN(instruction, I64, 1)
	case I64Store16:
		return v.validateStoreN(instruction, I64, 2)
	case I64Store32:
		return v.validateStoreN(instruction, I64, 4)
	case F32Store:
		return v.validateStore(instruction, F32)
	case F64Store:
		return v.validateStore(instruction, F64)
	case V128Const:
		return v.validateConst(V128)
	case V128Load8Lane:
		return v.validateSimdLoadLane(instruction, 1)
	case V128Load16Lane:
		return v.validateSimdLoadLane(instruction, 2)
	case V128Load32Lane:
		return v.validateSimdLoadLane(instruction, 4)
	case V128Load64Lane:
		return v.validateSimdLoadLane(instruction, 8)
	case V128Store8Lane:
		return v.validateSimdStoreLane(instruction, 1)
	case V128Store16Lane:
		return v.validateSimdStoreLane(instruction, 2)
	case V128Store32Lane:
		return v.validateSimdStoreLane(instruction, 4)
	case V128Store64Lane:
		return v.validateSimdStoreLane(instruction, 8)
	case I8x16ExtractLaneS,
		I8x16ExtractLaneU,
		I16x8ExtractLaneS,
		I16x8ExtractLaneU,
		I32x4ExtractLane:
		return v.validateUnaryOp(V128, I32)
	case I64x2ExtractLane:
		return v.validateUnaryOp(V128, I64)
	case F32x4ExtractLane:
		return v.validateUnaryOp(V128, F32)
	case F64x2ExtractLane:
		return v.validateUnaryOp(V128, F64)
	case MemoryInit:
		return v.validateMemoryInit(instruction)
	case MemoryCopy:
		return v.validateMemoryCopy(instruction)
	case MemoryFill:
		return v.validateMemoryFill()
	default:
		// TODO: implement other opcodes
		return nil
	}
}

func (v *validator) validateBlock(instruction Instruction) error {
	startTypes, endTypes := v.getBlockTypes(int32(instruction.Immediates[0]))
	if _, err := v.popExpectedValues(startTypes); err != nil {
		return err
	}

	v.pushControlFrame(instruction.Opcode, startTypes, endTypes)
	return nil
}

func (v *validator) validateIf(instruction Instruction) error {
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	return v.validateBlock(instruction)
}

func (v *validator) validateElse() error {
	frame, err := v.popControlFrame()
	if err != nil {
		return err
	}

	if frame.opcode != If {
		return errors.New("else must match if")
	}

	v.pushControlFrame(Else, frame.startTypes, frame.endTypes)
	return nil
}

func (v *validator) validateEnd() error {
	frame, err := v.popControlFrame()
	if err != nil {
		return err
	}
	v.pushValues(frame.endTypes)
	return nil
}

func (v *validator) validateBr(instruction Instruction) error {
	labelIndex := uint32(instruction.Immediates[0])
	if labelIndex >= uint32(len(v.controlStack)) {
		return errors.New("br label index out of bounds")
	}

	frameIndex := len(v.controlStack) - 1 - int(labelIndex)
	labelTypes := v.labelTypes(v.controlStack[frameIndex])
	if _, err := v.popExpectedValues(labelTypes); err != nil {
		return err
	}

	return v.markFrameUnreachable()
}

func (v *validator) validateBrIf(instruction Instruction) error {
	labelIndex := uint32(instruction.Immediates[0])
	if labelIndex >= uint32(len(v.controlStack)) {
		return errors.New("br label index out of bounds")
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

func (v *validator) validateBrTable(instruction Instruction) error {
	immediates := instruction.Immediates
	table := immediates[:len(immediates)-1]
	labelIndex := uint32(immediates[len(immediates)-1])

	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}

	if labelIndex >= uint32(len(v.controlStack)) {
		return errors.New("br label index out of bounds")
	}

	frameIndex := len(v.controlStack) - 1 - int(labelIndex)
	labelTypes := v.labelTypes(v.controlStack[frameIndex])
	arity := len(labelTypes)

	for _, index := range table {
		if index >= uint64(len(v.controlStack)) {
			return errors.New("br label index out of bounds")
		}

		frameIndex := len(v.controlStack) - 1 - int(index)
		labelTypes := v.labelTypes(v.controlStack[frameIndex])
		if len(labelTypes) != arity {
			return errors.New("br label arity mismatch")
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
		return errors.New("return type not set")
	}
	if _, err := v.popExpectedValues(v.returnType); err != nil {
		return err
	}
	return v.markFrameUnreachable()
}

func (v *validator) validateCall(instruction Instruction) error {
	functionIndex := uint32(instruction.Immediates[0])
	if functionIndex >= uint32(len(v.funcTypes)) {
		return errors.New("call function index out of bounds")
	}
	functionType := v.funcTypes[functionIndex]
	if _, err := v.popExpectedValues(functionType.ParamTypes); err != nil {
		return err
	}
	v.pushValues(functionType.ResultTypes)
	return nil
}

func (v *validator) validateCallIndirect(instruction Instruction) error {
	typeIndex := uint32(instruction.Immediates[0])
	tableIndex := uint32(instruction.Immediates[1])
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}

	tableType := v.tableTypes[tableIndex]
	if tableType.ReferenceType != FuncRefType {
		return errors.New("table type must be func ref")
	}

	if typeIndex >= uint32(len(v.funcTypes)) {
		return errors.New("call indirect type index out of bounds")
	}
	functionType := v.funcTypes[typeIndex]

	if _, err := v.popExpectedValues(functionType.ParamTypes); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
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
	type1, err := v.popExpectedValue(t)
	if err != nil {
		return err
	}
	type2, err := v.popExpectedValue(t)
	if err != nil {
		return err
	}

	if !((isNumber(type1) && isNumber(type2)) ||
		(isVector(type1) && isVector(type2))) {
		return errors.New("types do not match")
	}

	if type1 != type2 && type1 != Bottom && type2 != Bottom {
		return errors.New("types do not match")
	}

	if type1 == Bottom {
		v.pushValue(type2)
	} else {
		v.pushValue(type1)
	}

	return nil
}

func (v *validator) validateLocalTee(instruction Instruction) error {
	localIndex := instruction.Immediates[0]
	if localIndex >= uint64(len(v.locals)) {
		return errors.New("local index out of bounds")
	}
	valueType := v.locals[localIndex]
	return v.validateUnaryOp(valueType, valueType)
}

func (v *validator) validateLocalGet(instruction Instruction) error {
	localIndex := instruction.Immediates[0]
	if localIndex >= uint64(len(v.locals)) {
		return errors.New("local index out of bounds")
	}
	v.pushValue(v.locals[localIndex])
	return nil
}

func (v *validator) validateLocalSet(instruction Instruction) error {
	localIndex := instruction.Immediates[0]
	if localIndex >= uint64(len(v.locals)) {
		return errors.New("local index out of bounds")
	}
	_, err := v.popExpectedValue(v.locals[localIndex])
	return err
}

func (v *validator) validateGlobalGet(instruction Instruction) error {
	globalIndex := instruction.Immediates[0]
	if globalIndex >= uint64(len(v.globalTypes)) {
		return errors.New("global index out of bounds")
	}
	return v.validateConst(v.globalTypes[globalIndex].ValueType)
}

func (v *validator) validateGlobalSet(instruction Instruction) error {
	globalIndex := instruction.Immediates[0]
	if globalIndex >= uint64(len(v.globalTypes)) {
		return errors.New("global index out of bounds")
	}

	globalType := v.globalTypes[globalIndex]
	if !globalType.IsMutable {
		return errors.New("global is immutable")
	}

	_, err := v.popExpectedValue(globalType.ValueType)
	return err
}

func (v *validator) validateLoadN(
	instruction Instruction,
	valueType ValueType,
	sizeBytes uint32,
) error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}

	if err := v.validateMemArg(instruction, sizeBytes); err != nil {
		return err
	}

	return v.validateUnaryOp(I32, valueType)
}

func (v *validator) validateLoad(
	instruction Instruction,
	valueType ValueType,
) error {
	return v.validateLoadN(instruction, valueType, bytesWidth(valueType))
}

func (v *validator) validateStoreN(
	instruction Instruction,
	valueType ValueType,
	sizeBytes uint32,
) error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}

	if err := v.validateMemArg(instruction, sizeBytes); err != nil {
		return err
	}

	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateStore(
	instruction Instruction,
	valueType ValueType,
) error {
	return v.validateStoreN(instruction, valueType, bytesWidth(valueType))
}

func (v *validator) validateMemArg(
	instruction Instruction,
	nBytes uint32,
) error {
	align := instruction.Immediates[0]
	if 1<<align > nBytes {
		return errors.New("alignment too large")
	}
	return nil
}

func (v *validator) validateMemorySize() error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateMemoryGrow() error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	return v.validateUnaryOp(I32, I32)
}

func (v *validator) validateMemoryFill() error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateMemoryInit(instruction Instruction) error {
	dataIndex := uint32(instruction.Immediates[0])
	memoryIndex := uint32(instruction.Immediates[1])
	if dataIndex >= uint32(v.dataCount) {
		return errors.New("data index out of bounds")
	}
	if err := v.validateMemoryExists(memoryIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateMemoryCopy(instruction Instruction) error {
	destMemoryIndex := uint32(instruction.Immediates[0])
	srcMemoryIndex := uint32(instruction.Immediates[1])
	if err := v.validateMemoryExists(destMemoryIndex); err != nil {
		return err
	}
	if err := v.validateMemoryExists(srcMemoryIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateTableExists(tableIndex uint32) error {
	if tableIndex >= uint32(len(v.tableTypes)) {
		return errors.New("table index out of bounds")
	}
	return nil
}

func (v *validator) validateMemoryExists(memoryIndex uint32) error {
	if memoryIndex >= uint32(len(v.memTypes)) {
		return errors.New("memory index out of bounds")
	}
	return nil
}

func (v *validator) validateConst(valueType ValueType) error {
	v.pushValue(valueType)
	return nil
}

func (v *validator) validateRefNull(instruction Instruction) error {
	refType := toValueType(instruction.Immediates[0])
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

func (v *validator) validateRefFunc(instruction Instruction) error {
	funcIndex := uint32(instruction.Immediates[0])
	if funcIndex >= uint32(len(v.funcTypes)) {
		return errors.New("function index out of bounds")
	}
	v.pushValue(FuncRefType)
	return nil
}

func (v *validator) validateTableGet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	return v.validateUnaryOp(I32, v.tableTypes[tableIndex].ReferenceType)
}

func (v *validator) validateTableSet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	referenceType := v.tableTypes[tableIndex].ReferenceType
	if _, err := v.popExpectedValue(referenceType); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateTableInit(instruction Instruction) error {
	elemIndex := uint32(instruction.Immediates[0])
	tableIndex := uint32(instruction.Immediates[1])
	if elemIndex >= uint32(len(v.elemTypes)) {
		return errors.New("element index out of bounds")
	}
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateTableCopy(instruction Instruction) error {
	destTableIndex := uint32(instruction.Immediates[0])
	srcTableIndex := uint32(instruction.Immediates[1])
	if err := v.validateTableExists(destTableIndex); err != nil {
		return err
	}
	if err := v.validateTableExists(srcTableIndex); err != nil {
		return err
	}
	if _, err := v.popExpectedValues([]ValueType{I32, I32, I32}); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateTableGrow(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
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

func (v *validator) validateTableSize(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	if err := v.validateTableExists(tableIndex); err != nil {
		return err
	}
	v.pushValue(I32)
	return nil
}

func (v *validator) validateTableFill(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
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
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
	return nil
}

func (v *validator) validateElemDrop(instruction Instruction) error {
	elemIndex := uint32(instruction.Immediates[0])
	if elemIndex >= uint32(len(v.elemTypes)) {
		return errors.New("element index out of bounds")
	}
	return nil
}

func (v *validator) validateDataDrop(instruction Instruction) error {
	dataIndex := uint32(instruction.Immediates[0])
	if dataIndex >= uint32(v.dataCount) {
		return errors.New("data index out of bounds")
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
		return Bottom, nil
	}

	if len(v.valueStack) == currentFrame.height {
		return nil, errors.New("value stack underflow")
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
	if val != expected && val != Bottom && expected != Bottom {
		return nil, fmt.Errorf("expected value type does not match: expected %v (%T), got %v (%T)", expected, expected, val, val)
	}
	return val, nil
}

func (v *validator) popExpectedValues(
	expected []ValueType,
) ([]ValueType, error) {
	values := make([]ValueType, len(expected))
	var err error
	for i := range expected {
		values[len(values)-1-i], err = v.popExpectedValue(expected[i])
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (v *validator) peekControlFrame() (*controlFrame, error) {
	if len(v.controlStack) == 0 {
		return nil, errors.New("control stack is empty")
	}
	return &v.controlStack[len(v.controlStack)-1], nil
}

func (v *validator) pushControlFrame(opcode Opcode, start, end []ValueType) {
	v.controlStack = append(v.controlStack, controlFrame{
		opcode:      opcode,
		startTypes:  start,
		endTypes:    end,
		height:      len(v.valueStack),
		unreachable: false,
	})
	v.pushValues(start)
}

func (v *validator) popControlFrame() (controlFrame, error) {
	if len(v.controlStack) == 0 {
		return controlFrame{}, errors.New("control stack is empty")
	}
	frame := v.controlStack[len(v.controlStack)-1]
	if _, err := v.popExpectedValues(frame.endTypes); err != nil {
		return controlFrame{}, err
	}
	if len(v.valueStack) != frame.height {
		return controlFrame{}, errors.New("value stack height mismatch")
	}
	v.controlStack = v.controlStack[:len(v.controlStack)-1]
	return frame, nil
}

func (v *validator) labelTypes(frame controlFrame) []ValueType {
	if frame.opcode == Loop {
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

func (v *validator) validateSimdLoadLane(
	instruction Instruction,
	sizeBytes uint32,
) error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	if err := v.validateMemArg(instruction, sizeBytes); err != nil {
		return err
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

func (v *validator) validateSimdStoreLane(
	instruction Instruction,
	sizeBytes uint32,
) error {
	if err := v.validateMemoryExists(0); err != nil {
		return err
	}
	if err := v.validateMemArg(instruction, sizeBytes); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(V128); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(I32); err != nil {
		return err
	}
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

	return []ValueType{}, []ValueType{toValueType(uint64(blockType))}
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
		// TODO: ??
		return Bottom
	}
}

func bytesWidth(valueType ValueType) uint32 {
	switch valueType {
	case I32:
		return 4
	case I64:
		return 8
	case F32:
		return 4
	case F64:
		return 8
	case V128:
		return 16
	default:
		return 0
	}
}
