package epsilon

import "errors"

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
	v.globalTypes = make([]GlobalType, 0, len(module.Imports)+len(module.GlobalVariables))

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

	v.pushControlFrame(Block, functionType.ParamTypes, functionType.ResultTypes)

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
	return nil
}

func (v *validator) validate(instruction Instruction) error {
	switch instruction.Opcode {
	case Unreachable:
		return v.markFrameUnreachable()
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
	case Call:
		return v.validateCall(instruction)
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
	case I32Const:
		return v.validateConst(I32)
	case I64Const:
		return v.validateConst(I64)
	case F32Const:
		return v.validateConst(F32)
	case F64Const:
		return v.validateConst(F64)
	case I32Eq, I32LtS:
		return v.validateComparisonOp(I32)
	case I32Add, I32Sub, I32Mul:
		return v.validateBinaryOp(I32)
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

func (v *validator) validateConst(valueType ValueType) error {
	v.pushValue(valueType)
	return nil
}

func (v *validator) validateBinaryOp(valueType ValueType) error {
	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	v.pushValue(valueType)
	return nil
}

func (v *validator) validateComparisonOp(valueType ValueType) error {
	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	if _, err := v.popExpectedValue(valueType); err != nil {
		return err
	}
	v.pushValue(I32)
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
		return nil, errors.New("not enough values on the stack")
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
		return nil, errors.New("expected value type does not match")
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
		return controlFrame{}, errors.New("not enough values on the stack")
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
