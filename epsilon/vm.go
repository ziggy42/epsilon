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
)

var (
	ErrUnreachable        = errors.New("unreachable")
	ErrCallStackExhausted = errors.New("call stack exhausted")
	// Special error to signal a return instruction was hit.
	errReturn = errors.New("return instruction")
)

const maxCallStackDepth = 1000

type CallFrame struct {
	Decoder      *Decoder
	ControlStack []*ControlFrame
	Locals       []any
	Function     *WasmFunc
}

func NewCallFrame(
	function *WasmFunc,
	locals []any,
	controlFrame ControlFrame,
) *CallFrame {
	return &CallFrame{
		Decoder:      NewDecoder(function.Code.Body),
		ControlStack: []*ControlFrame{&controlFrame},
		Locals:       locals,
		Function:     function,
	}
}

// ControlFrame represents a block of code that can be branched to.
type ControlFrame struct {
	Opcode         Opcode // The opcode that created this control frame.
	Pc             uint   // The pc after the current opcode.
	ContinuationPc uint   // The address to jump to when `br` targets this frame.
	InputCount     uint   // Count of inputs this control instruction consumes.
	OutputCount    uint   // Count of outputs this control instruction produces.
	StackHeight    uint
}

// VM is the WebAssembly Virtual Machine.
type VM struct {
	store          *Store
	stack          *ValueStack
	callStack      []*CallFrame
	callStackDepth int
	features       ExperimentalFeatures
}

func NewVM() *VM {
	return NewVMWithFeatures(ExperimentalFeatures{})
}

func NewVMWithFeatures(features ExperimentalFeatures) *VM {
	return &VM{store: NewStore(), stack: NewValueStack(), features: features}
}

func (vm *VM) Instantiate(
	module *Module,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	validator := NewValidator(vm.features)
	if err := validator.validateModule(module); err != nil {
		return nil, err
	}
	moduleInstance := &ModuleInstance{Types: module.Types}

	resolvedImports, err := ResolveImports(module, imports)
	if err != nil {
		return nil, err
	}

	for _, functionInstance := range resolvedImports.Functions {
		storeIndex := uint32(len(vm.store.funcs))
		moduleInstance.FuncAddrs = append(moduleInstance.FuncAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, functionInstance)
	}

	for _, function := range module.Funcs {
		storeIndex := uint32(len(vm.store.funcs))
		funType := module.Types[function.TypeIndex]
		wasmFunc := NewWasmFunc(funType, moduleInstance, function)
		moduleInstance.FuncAddrs = append(moduleInstance.FuncAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, wasmFunc)
	}

	for _, table := range resolvedImports.Tables {
		storeIndex := uint32(len(vm.store.tables))
		moduleInstance.TableAddrs = append(moduleInstance.TableAddrs, storeIndex)
		vm.store.tables = append(vm.store.tables, table)
	}

	for _, tableType := range module.Tables {
		storeIndex := uint32(len(vm.store.tables))
		table := NewTable(tableType)
		moduleInstance.TableAddrs = append(moduleInstance.TableAddrs, storeIndex)
		vm.store.tables = append(vm.store.tables, table)
	}

	for _, memory := range resolvedImports.Memories {
		storeIndex := uint32(len(vm.store.memories))
		moduleInstance.MemAddrs = append(moduleInstance.MemAddrs, storeIndex)
		vm.store.memories = append(vm.store.memories, memory)
	}

	for _, memoryType := range module.Memories {
		storeIndex := uint32(len(vm.store.memories))
		memory := NewMemory(memoryType)
		moduleInstance.MemAddrs = append(moduleInstance.MemAddrs, storeIndex)
		vm.store.memories = append(vm.store.memories, memory)
	}

	for _, global := range resolvedImports.Globals {
		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.GlobalAddrs = append(moduleInstance.GlobalAddrs, storeIndex)
		vm.store.globals = append(vm.store.globals, global)
	}

	for _, global := range module.GlobalVariables {
		val, err := vm.invokeInitExpression(
			global.InitExpression,
			global.GlobalType.ValueType,
			moduleInstance,
		)
		if err != nil {
			return nil, err
		}

		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.GlobalAddrs = append(moduleInstance.GlobalAddrs, storeIndex)
		vm.store.globals = append(vm.store.globals, &Global{
			Value:   val,
			Mutable: global.GlobalType.IsMutable,
			Type:    global.GlobalType.ValueType,
		})
	}

	// TODO: elements and data segments should at the very least be copied, but we
	// should probably have some runtime representation for them.
	for _, elem := range module.ElementSegments {
		storeIndex := uint32(len(vm.store.elements))
		moduleInstance.ElemAddrs = append(moduleInstance.ElemAddrs, storeIndex)
		vm.store.elements = append(vm.store.elements, elem)
	}

	for _, data := range module.DataSegments {
		storeIndex := uint32(len(vm.store.datas))
		moduleInstance.DataAddrs = append(moduleInstance.DataAddrs, storeIndex)
		vm.store.datas = append(vm.store.datas, data)
	}

	if err := vm.initActiveElements(module, moduleInstance); err != nil {
		return nil, err
	}

	if err := vm.initActiveDatas(module, moduleInstance); err != nil {
		return nil, err
	}

	if module.StartIndex != nil {
		storeFunctionIndex := moduleInstance.FuncAddrs[*module.StartIndex]
		function := vm.store.funcs[storeFunctionIndex]
		if _, err := vm.invoke(function); err != nil {
			return nil, err
		}
	}

	exports := vm.resolveExports(module, moduleInstance)
	moduleInstance.Exports = exports
	return moduleInstance, nil
}

func (vm *VM) Get(module *ModuleInstance, name string) (any, error) {
	export, err := getExport(module, name, GlobalIndexType)
	if err != nil {
		return nil, err
	}
	return export.(*Global).Value, nil
}

func (vm *VM) Invoke(
	module *ModuleInstance,
	name string,
	args ...any,
) ([]any, error) {
	export, err := getExport(module, name, FunctionIndexType)
	if err != nil {
		return nil, err
	}

	vm.stack.PushAll(args)
	return vm.invoke(export.(FunctionInstance))
}

func (vm *VM) invoke(function FunctionInstance) ([]any, error) {
	switch f := function.(type) {
	case *WasmFunc:
		return vm.invokeWasmFunction(f)
	case *HostFunc:
		return vm.invokeHostFunction(f)
	default:
		return nil, fmt.Errorf("unknown function type")
	}
}

func (vm *VM) invokeWasmFunction(function *WasmFunc) ([]any, error) {
	if vm.callStackDepth >= maxCallStackDepth {
		return nil, ErrCallStackExhausted
	}
	vm.callStackDepth++
	defer func() { vm.callStackDepth-- }()

	locals := vm.stack.PopValueTypes(function.Type.ParamTypes)
	for _, local := range function.Code.Locals {
		locals = append(locals, DefaultValueForType(local))
	}

	controlFrame := ControlFrame{
		Opcode:         Block,
		ContinuationPc: uint(len(function.Code.Body)),
		InputCount:     uint(len(function.Type.ParamTypes)),
		OutputCount:    uint(len(function.Type.ResultTypes)),
		StackHeight:    vm.stack.Size(),
	}

	callFrame := NewCallFrame(function, locals, controlFrame)
	vm.callStack = append(vm.callStack, callFrame)

	for callFrame.Decoder.HasMore() {
		instruction, err := callFrame.Decoder.Decode()
		if err != nil {
			return nil, err
		}

		if err = vm.handleInstruction(instruction); err != nil {
			if errors.Is(err, errReturn) {
				break // A 'return' instruction was executed.
			}

			return nil, err
		}
	}

	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	values := vm.stack.PopValueTypes(callFrame.Function.Type.ResultTypes)
	return values, nil
}

func (vm *VM) handleInstruction(instruction Instruction) error {
	var err error
	// Using a switch instead of a map of Opcode -> Handler is significantly
	// faster.
	switch instruction.Opcode {
	case Unreachable:
		return ErrUnreachable
	case Nop:
		// Do nothing.
	case Block, Loop:
		err = vm.handleStructured(instruction)
	case If:
		err = vm.handleIf(instruction)
	case Else:
		vm.handleElse()
	case End:
		vm.handleEnd()
	case Br:
		vm.handleBr(instruction)
	case BrIf:
		vm.handleBrIf(instruction)
	case BrTable:
		vm.handleBrTable(instruction)
	case Return:
		err = errReturn
	case Call:
		err = vm.handleCall(instruction)
	case CallIndirect:
		return vm.handleCallIndirect(instruction)
	case Drop:
		vm.stack.Drop()
	case Select:
		vm.handleSelect()
	case SelectT:
		vm.handleSelect()
	case LocalGet:
		vm.handleLocalGet(instruction)
	case LocalSet:
		vm.handleLocalSet(instruction)
	case LocalTee:
		vm.handleLocalTee(instruction)
	case GlobalGet:
		vm.handleGlobalGet(instruction)
	case GlobalSet:
		vm.handleGlobalSet(instruction)
	case TableGet:
		err = vm.handleTableGet(instruction)
	case TableSet:
		err = vm.handleTableSet(instruction)
	case I32Load:
		err = handleLoad(vm, instruction, Int32From4Bytes, 4)
	case I64Load:
		err = handleLoad(vm, instruction, Int64From8Bytes, 8)
	case F32Load:
		err = handleLoad(vm, instruction, Float32From4Bytes, 4)
	case F64Load:
		err = handleLoad(vm, instruction, Float64From8Bytes, 8)
	case I32Load8S:
		err = handleLoad(vm, instruction, IntFrom1Byte[int32], 1)
	case I32Load8U:
		err = handleLoad(vm, instruction, UintFrom1Byte[int32], 1)
	case I32Load16S:
		err = handleLoad(vm, instruction, IntFrom2Bytes[int32], 2)
	case I32Load16U:
		err = handleLoad(vm, instruction, UintFrom2Bytes[int32], 2)
	case I64Load8S:
		err = handleLoad(vm, instruction, IntFrom1Byte[int64], 1)
	case I64Load8U:
		err = handleLoad(vm, instruction, UintFrom1Byte[int64], 1)
	case I64Load16S:
		err = handleLoad(vm, instruction, IntFrom2Bytes[int64], 2)
	case I64Load16U:
		err = handleLoad(vm, instruction, UintFrom2Bytes[int64], 2)
	case I64Load32S:
		err = handleLoad(vm, instruction, Int64From4Bytes, 4)
	case I64Load32U:
		err = handleLoad(vm, instruction, Uint64From4Bytes, 4)
	case I32Store:
		err = handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 4)
	case I64Store:
		err = handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 8)
	case F32Store:
		err = handleStore(vm, instruction, vm.stack.PopFloat32, BytesFromFloat32, 4)
	case F64Store:
		err = handleStore(vm, instruction, vm.stack.PopFloat64, BytesFromFloat64, 8)
	case I32Store8:
		err = handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 1)
	case I32Store16:
		err = handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 2)
	case I64Store8:
		err = handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 1)
	case I64Store16:
		err = handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 2)
	case I64Store32:
		err = handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 4)
	case MemorySize:
		vm.handleMemorySize(instruction)
	case MemoryGrow:
		vm.handleMemoryGrow(instruction)
	case I32Const:
		vm.stack.Push(Uint64ToInt32(instruction.Immediates[0]))
	case I64Const:
		vm.stack.Push(Uint64ToInt64(instruction.Immediates[0]))
	case F32Const:
		vm.stack.Push(Uint64ToFloat32(instruction.Immediates[0]))
	case F64Const:
		vm.stack.Push(Uint64ToFloat64(instruction.Immediates[0]))
	case I32Eqz:
		handleUnaryBool(vm, vm.stack.PopInt32, EqualZero)
	case I32Eq:
		handleBinaryBool(vm, vm.stack.PopInt32, Equal)
	case I32Ne:
		handleBinaryBool(vm, vm.stack.PopInt32, NotEqual)
	case I32LtS:
		handleBinaryBool(vm, vm.stack.PopInt32, LessThan)
	case I32LtU:
		handleBinaryBool(vm, vm.stack.PopInt32, LessThanUnsigned32)
	case I32GtS:
		handleBinaryBool(vm, vm.stack.PopInt32, GreaterThan)
	case I32GtU:
		handleBinaryBool(vm, vm.stack.PopInt32, GreaterThanUnsigned32)
	case I32LeS:
		handleBinaryBool(vm, vm.stack.PopInt32, LessOrEqual)
	case I32LeU:
		handleBinaryBool(vm, vm.stack.PopInt32, LessOrEqualUnsigned32)
	case I32GeS:
		handleBinaryBool(vm, vm.stack.PopInt32, GreaterOrEqual)
	case I32GeU:
		handleBinaryBool(vm, vm.stack.PopInt32, GreaterOrEqualUnsigned32)
	case I64Eqz:
		handleUnaryBool(vm, vm.stack.PopInt64, EqualZero)
	case I64Eq:
		handleBinaryBool(vm, vm.stack.PopInt64, Equal)
	case I64Ne:
		handleBinaryBool(vm, vm.stack.PopInt64, NotEqual)
	case I64LtS:
		handleBinaryBool(vm, vm.stack.PopInt64, LessThan)
	case I64LtU:
		handleBinaryBool(vm, vm.stack.PopInt64, LessThanUnsigned64)
	case I64GtS:
		handleBinaryBool(vm, vm.stack.PopInt64, GreaterThan)
	case I64GtU:
		handleBinaryBool(vm, vm.stack.PopInt64, GreaterThanUnsigned64)
	case I64LeS:
		handleBinaryBool(vm, vm.stack.PopInt64, LessOrEqual)
	case I64LeU:
		handleBinaryBool(vm, vm.stack.PopInt64, LessOrEqualUnsigned64)
	case I64GeS:
		handleBinaryBool(vm, vm.stack.PopInt64, GreaterOrEqual)
	case I64GeU:
		handleBinaryBool(vm, vm.stack.PopInt64, GreaterOrEqualUnsigned64)
	case F32Eq:
		handleBinaryBool(vm, vm.stack.PopFloat32, Equal)
	case F32Ne:
		handleBinaryBool(vm, vm.stack.PopFloat32, NotEqual)
	case F32Lt:
		handleBinaryBool(vm, vm.stack.PopFloat32, LessThan)
	case F32Gt:
		handleBinaryBool(vm, vm.stack.PopFloat32, GreaterThan)
	case F32Le:
		handleBinaryBool(vm, vm.stack.PopFloat32, LessOrEqual)
	case F32Ge:
		handleBinaryBool(vm, vm.stack.PopFloat32, GreaterOrEqual)
	case F64Eq:
		handleBinaryBool(vm, vm.stack.PopFloat64, Equal)
	case F64Ne:
		handleBinaryBool(vm, vm.stack.PopFloat64, NotEqual)
	case F64Lt:
		handleBinaryBool(vm, vm.stack.PopFloat64, LessThan)
	case F64Gt:
		handleBinaryBool(vm, vm.stack.PopFloat64, GreaterThan)
	case F64Le:
		handleBinaryBool(vm, vm.stack.PopFloat64, LessOrEqual)
	case F64Ge:
		handleBinaryBool(vm, vm.stack.PopFloat64, GreaterOrEqual)
	case I32Clz:
		handleUnary(vm, vm.stack.PopInt32, Clz32)
	case I32Ctz:
		handleUnary(vm, vm.stack.PopInt32, Ctz32)
	case I32Popcnt:
		handleUnary(vm, vm.stack.PopInt32, Popcnt32)
	case I32Add:
		handleBinary(vm, vm.stack.PopInt32, Add)
	case I32Sub:
		handleBinary(vm, vm.stack.PopInt32, Sub)
	case I32Mul:
		handleBinary(vm, vm.stack.PopInt32, Mul)
	case I32DivS:
		err = handleBinarySafe(vm, vm.stack.PopInt32, DivS32)
	case I32DivU:
		err = handleBinarySafe(vm, vm.stack.PopInt32, DivU32)
	case I32RemS:
		err = handleBinarySafe(vm, vm.stack.PopInt32, RemS32)
	case I32RemU:
		err = handleBinarySafe(vm, vm.stack.PopInt32, RemU32)
	case I32And:
		handleBinary(vm, vm.stack.PopInt32, And)
	case I32Or:
		handleBinary(vm, vm.stack.PopInt32, Or)
	case I32Xor:
		handleBinary(vm, vm.stack.PopInt32, Xor)
	case I32Shl:
		handleBinary(vm, vm.stack.PopInt32, Shl32)
	case I32ShrS:
		handleBinary(vm, vm.stack.PopInt32, ShrS32)
	case I32ShrU:
		handleBinary(vm, vm.stack.PopInt32, ShrU32)
	case I32Rotl:
		handleBinary(vm, vm.stack.PopInt32, Rotl32)
	case I32Rotr:
		handleBinary(vm, vm.stack.PopInt32, Rotr32)
	case I64Clz:
		handleUnary(vm, vm.stack.PopInt64, Clz64)
	case I64Ctz:
		handleUnary(vm, vm.stack.PopInt64, Ctz64)
	case I64Popcnt:
		handleUnary(vm, vm.stack.PopInt64, Popcnt64)
	case I64Add:
		handleBinary(vm, vm.stack.PopInt64, Add)
	case I64Sub:
		handleBinary(vm, vm.stack.PopInt64, Sub)
	case I64Mul:
		handleBinary(vm, vm.stack.PopInt64, Mul)
	case I64DivS:
		err = handleBinarySafe(vm, vm.stack.PopInt64, DivS64)
	case I64DivU:
		err = handleBinarySafe(vm, vm.stack.PopInt64, DivU64)
	case I64RemS:
		err = handleBinarySafe(vm, vm.stack.PopInt64, RemS64)
	case I64RemU:
		err = handleBinarySafe(vm, vm.stack.PopInt64, RemU64)
	case I64And:
		handleBinary(vm, vm.stack.PopInt64, And)
	case I64Or:
		handleBinary(vm, vm.stack.PopInt64, Or)
	case I64Xor:
		handleBinary(vm, vm.stack.PopInt64, Xor)
	case I64Shl:
		handleBinary(vm, vm.stack.PopInt64, Shl64)
	case I64ShrS:
		handleBinary(vm, vm.stack.PopInt64, ShrS64)
	case I64ShrU:
		handleBinary(vm, vm.stack.PopInt64, ShrU64)
	case I64Rotl:
		handleBinary(vm, vm.stack.PopInt64, Rotl64)
	case I64Rotr:
		handleBinary(vm, vm.stack.PopInt64, Rotr64)
	case F32Abs:
		handleUnary(vm, vm.stack.PopFloat32, Abs[float32])
	case F32Neg:
		handleUnary(vm, vm.stack.PopFloat32, Neg[float32])
	case F32Ceil:
		handleUnary(vm, vm.stack.PopFloat32, Ceil[float32])
	case F32Floor:
		handleUnary(vm, vm.stack.PopFloat32, Floor[float32])
	case F32Trunc:
		handleUnary(vm, vm.stack.PopFloat32, Trunc[float32])
	case F32Nearest:
		handleUnary(vm, vm.stack.PopFloat32, Nearest[float32])
	case F32Sqrt:
		handleUnary(vm, vm.stack.PopFloat32, Sqrt[float32])
	case F32Add:
		handleBinary(vm, vm.stack.PopFloat32, Add[float32])
	case F32Sub:
		handleBinary(vm, vm.stack.PopFloat32, Sub[float32])
	case F32Mul:
		handleBinary(vm, vm.stack.PopFloat32, Mul[float32])
	case F32Div:
		handleBinary(vm, vm.stack.PopFloat32, Div[float32])
	case F32Min:
		handleBinary(vm, vm.stack.PopFloat32, Min[float32])
	case F32Max:
		handleBinary(vm, vm.stack.PopFloat32, Max[float32])
	case F32Copysign:
		handleBinary(vm, vm.stack.PopFloat32, Copysign[float32])
	case F64Abs:
		handleUnary(vm, vm.stack.PopFloat64, Abs[float64])
	case F64Neg:
		handleUnary(vm, vm.stack.PopFloat64, Neg[float64])
	case F64Ceil:
		handleUnary(vm, vm.stack.PopFloat64, Ceil[float64])
	case F64Floor:
		handleUnary(vm, vm.stack.PopFloat64, Floor[float64])
	case F64Trunc:
		handleUnary(vm, vm.stack.PopFloat64, Trunc[float64])
	case F64Nearest:
		handleUnary(vm, vm.stack.PopFloat64, Nearest[float64])
	case F64Sqrt:
		handleUnary(vm, vm.stack.PopFloat64, Sqrt[float64])
	case F64Add:
		handleBinary(vm, vm.stack.PopFloat64, Add[float64])
	case F64Sub:
		handleBinary(vm, vm.stack.PopFloat64, Sub[float64])
	case F64Mul:
		handleBinary(vm, vm.stack.PopFloat64, Mul[float64])
	case F64Div:
		handleBinary(vm, vm.stack.PopFloat64, Div[float64])
	case F64Min:
		handleBinary(vm, vm.stack.PopFloat64, Min[float64])
	case F64Max:
		handleBinary(vm, vm.stack.PopFloat64, Max[float64])
	case F64Copysign:
		handleBinary(vm, vm.stack.PopFloat64, Copysign[float64])
	case I32WrapI64:
		handleUnary(vm, vm.stack.PopInt64, WrapI64ToI32)
	case I32TruncF32S:
		err = handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32SToI32)
	case I32TruncF32U:
		err = handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32UToI32)
	case I32TruncF64S:
		err = handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64SToI32)
	case I32TruncF64U:
		err = handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64UToI32)
	case I64ExtendI32S:
		handleUnary(vm, vm.stack.PopInt32, ExtendI32SToI64)
	case I64ExtendI32U:
		handleUnary(vm, vm.stack.PopInt32, ExtendI32UToI64)
	case I64TruncF32S:
		err = handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32SToI64)
	case I64TruncF32U:
		err = handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32UToI64)
	case I64TruncF64S:
		err = handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64SToI64)
	case I64TruncF64U:
		err = handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64UToI64)
	case F32ConvertI32S:
		handleUnary(vm, vm.stack.PopInt32, ConvertI32SToF32)
	case F32ConvertI32U:
		handleUnary(vm, vm.stack.PopInt32, ConvertI32UToF32)
	case F32ConvertI64S:
		handleUnary(vm, vm.stack.PopInt64, ConvertI64SToF32)
	case F32ConvertI64U:
		handleUnary(vm, vm.stack.PopInt64, ConvertI64UToF32)
	case F32DemoteF64:
		handleUnary(vm, vm.stack.PopFloat64, DemoteF64ToF32)
	case F64ConvertI32S:
		handleUnary(vm, vm.stack.PopInt32, ConvertI32SToF64)
	case F64ConvertI32U:
		handleUnary(vm, vm.stack.PopInt32, ConvertI32UToF64)
	case F64ConvertI64S:
		handleUnary(vm, vm.stack.PopInt64, ConvertI64SToF64)
	case F64ConvertI64U:
		handleUnary(vm, vm.stack.PopInt64, ConvertI64UToF64)
	case F64PromoteF32:
		handleUnary(vm, vm.stack.PopFloat32, PromoteF32ToF64)
	case I32ReinterpretF32:
		handleUnary(vm, vm.stack.PopFloat32, ReinterpretF32ToI32)
	case I64ReinterpretF64:
		handleUnary(vm, vm.stack.PopFloat64, ReinterpretF64ToI64)
	case F32ReinterpretI32:
		handleUnary(vm, vm.stack.PopInt32, ReinterpretI32ToF32)
	case F64ReinterpretI64:
		handleUnary(vm, vm.stack.PopInt64, ReinterpretI64ToF64)
	case I32Extend8S:
		handleUnary(vm, vm.stack.PopInt32, Extend8STo32)
	case I32Extend16S:
		handleUnary(vm, vm.stack.PopInt32, Extend16STo32)
	case I64Extend8S:
		handleUnary(vm, vm.stack.PopInt64, Extend8STo64)
	case I64Extend16S:
		handleUnary(vm, vm.stack.PopInt64, Extend16STo64)
	case I64Extend32S:
		handleUnary(vm, vm.stack.PopInt64, Extend32STo64)
	case RefNull:
		vm.stack.Push(NullVal)
	case RefIsNull:
		vm.handleRefIsNull()
	case RefFunc:
		vm.handleRefFunc(instruction)
	case I32TruncSatF32S:
		handleUnary(vm, vm.stack.PopFloat32, TruncSatF32SToI32)
	case I32TruncSatF32U:
		handleUnary(vm, vm.stack.PopFloat32, TruncSatF32UToI32)
	case I32TruncSatF64S:
		handleUnary(vm, vm.stack.PopFloat64, TruncSatF64SToI32)
	case I32TruncSatF64U:
		handleUnary(vm, vm.stack.PopFloat64, TruncSatF64UToI32)
	case I64TruncSatF32S:
		handleUnary(vm, vm.stack.PopFloat32, TruncSatF32SToI64)
	case I64TruncSatF32U:
		handleUnary(vm, vm.stack.PopFloat32, TruncSatF32UToI64)
	case I64TruncSatF64S:
		handleUnary(vm, vm.stack.PopFloat64, TruncSatF64SToI64)
	case I64TruncSatF64U:
		handleUnary(vm, vm.stack.PopFloat64, TruncSatF64UToI64)
	case MemoryInit:
		err = vm.handleMemoryInit(instruction)
	case DataDrop:
		vm.handleDataDrop(instruction)
	case MemoryCopy:
		err = vm.handleMemoryCopy(instruction)
	case MemoryFill:
		err = vm.handleMemoryFill(instruction)
	case TableInit:
		err = vm.handleTableInit(instruction)
	case ElemDrop:
		vm.handleElemDrop(instruction)
	case TableCopy:
		err = vm.handleTableCopy(instruction)
	case TableGrow:
		vm.handleTableGrow(instruction)
	case TableSize:
		vm.handleTableSize(instruction)
	case TableFill:
		err = vm.handleTableFill(instruction)
	case V128Load:
		err = handleLoad(vm, instruction, NewV128ValueFromSlice, 16)
	case V128Load8x8S:
		err = handleLoad(vm, instruction, SimdV128Load8x8S, 8)
	case V128Load8x8U:
		err = handleLoad(vm, instruction, SimdV128Load8x8U, 8)
	case V128Load16x4S:
		err = handleLoad(vm, instruction, SimdV128Load16x4S, 8)
	case V128Load16x4U:
		err = handleLoad(vm, instruction, SimdV128Load16x4U, 8)
	case V128Load32x2S:
		err = handleLoad(vm, instruction, SimdV128Load32x2S, 8)
	case V128Load32x2U:
		err = handleLoad(vm, instruction, SimdV128Load32x2U, 8)
	case V128Load8Splat:
		err = handleLoad(vm, instruction, SimdI8x16SplatFromBytes, 1)
	case V128Load16Splat:
		err = handleLoad(vm, instruction, SimdI16x8SplatFromBytes, 2)
	case V128Load32Splat:
		err = handleLoad(vm, instruction, SimdI32x4SplatFromBytes, 4)
	case V128Load64Splat:
		err = handleLoad(vm, instruction, SimdI64x2SplatFromBytes, 8)
	case V128Store:
		err = handleStore(vm, instruction, vm.stack.PopV128, GetBytes, 16)
	case V128Const:
		vm.handleSimdConst(instruction)
	case I8x16Shuffle:
		vm.handleI8x16Shuffle(instruction)
	case I8x16Swizzle:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16Swizzle)
	case I8x16Splat:
		handleUnary(vm, vm.stack.PopInt32, SimdI8x16Splat)
	case I16x8Splat:
		handleUnary(vm, vm.stack.PopInt32, SimdI16x8Splat)
	case I32x4Splat:
		handleUnary(vm, vm.stack.PopInt32, SimdI32x4Splat)
	case I64x2Splat:
		handleUnary(vm, vm.stack.PopInt64, SimdI64x2Splat)
	case F32x4Splat:
		handleUnary(vm, vm.stack.PopFloat32, SimdF32x4Splat)
	case F64x2Splat:
		handleUnary(vm, vm.stack.PopFloat64, SimdF64x2Splat)
	case I8x16ExtractLaneS:
		handleSimdExtractLane(vm, instruction, SimdI8x16ExtractLaneS)
	case I8x16ExtractLaneU:
		handleSimdExtractLane(vm, instruction, SimdI8x16ExtractLaneU)
	case I8x16ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI8x16ReplaceLane)
	case I16x8ExtractLaneS:
		handleSimdExtractLane(vm, instruction, SimdI16x8ExtractLaneS)
	case I16x8ExtractLaneU:
		handleSimdExtractLane(vm, instruction, SimdI16x8ExtractLaneU)
	case I16x8ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI16x8ReplaceLane)
	case I32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, SimdI32x4ExtractLane)
	case I32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI32x4ReplaceLane)
	case I64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, SimdI64x2ExtractLane)
	case I64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopInt64, SimdI64x2ReplaceLane)
	case F32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, SimdF32x4ExtractLane)
	case F32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopFloat32, SimdF32x4ReplaceLane)
	case F64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, SimdF64x2ExtractLane)
	case F64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.PopFloat64, SimdF64x2ReplaceLane)
	case I8x16Eq:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16Eq)
	case I8x16Ne:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16Ne)
	case I8x16LtS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16LtS)
	case I8x16LtU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16LtU)
	case I8x16GtS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16GtS)
	case I8x16GtU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16GtU)
	case I8x16LeS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16LeS)
	case I8x16LeU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16LeU)
	case I8x16GeS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16GeS)
	case I8x16GeU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16GeU)
	case I16x8Eq:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Eq)
	case I16x8Ne:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Ne)
	case I16x8LtS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8LtS)
	case I16x8LtU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8LtU)
	case I16x8GtS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8GtS)
	case I16x8GtU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8GtU)
	case I16x8LeS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8LeS)
	case I16x8LeU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8LeU)
	case I16x8GeS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8GeS)
	case I16x8GeU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8GeU)
	case I32x4Eq:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4Eq)
	case I32x4Ne:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4Ne)
	case I32x4LtS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4LtS)
	case I32x4LtU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4LtU)
	case I32x4GtS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4GtS)
	case I32x4GtU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4GtU)
	case I32x4LeS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4LeS)
	case I32x4LeU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4LeU)
	case I32x4GeS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4GeS)
	case I32x4GeU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4GeU)
	case F32x4Eq:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Eq)
	case F32x4Ne:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Ne)
	case F32x4Lt:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Lt)
	case F32x4Gt:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Gt)
	case F32x4Le:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Le)
	case F32x4Ge:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Ge)
	case F64x2Eq:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Eq)
	case F64x2Ne:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Ne)
	case F64x2Lt:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Lt)
	case F64x2Gt:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Gt)
	case F64x2Le:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Le)
	case F64x2Ge:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Ge)
	case V128Not:
		handleUnary(vm, vm.stack.PopV128, SimdV128Not)
	case V128And:
		handleBinary(vm, vm.stack.PopV128, SimdV128And)
	case V128Andnot:
		handleBinary(vm, vm.stack.PopV128, SimdV128Andnot)
	case V128Or:
		handleBinary(vm, vm.stack.PopV128, SimdV128Or)
	case V128Xor:
		handleBinary(vm, vm.stack.PopV128, SimdV128Xor)
	case V128Bitselect:
		vm.handleSimdTernary(SimdV128Bitselect)
	case V128AnyTrue:
		handleUnaryBool(vm, vm.stack.PopV128, SimdV128AnyTrue)
	case V128Load8Lane:
		err = vm.handleSimdLoadLane(instruction, 8)
	case V128Load16Lane:
		err = vm.handleSimdLoadLane(instruction, 16)
	case V128Load32Lane:
		err = vm.handleSimdLoadLane(instruction, 32)
	case V128Load64Lane:
		err = vm.handleSimdLoadLane(instruction, 64)
	case V128Store8Lane:
		err = vm.handleSimdStoreLane(instruction, 8)
	case V128Store16Lane:
		err = vm.handleSimdStoreLane(instruction, 16)
	case V128Store32Lane:
		err = vm.handleSimdStoreLane(instruction, 32)
	case V128Store64Lane:
		err = vm.handleSimdStoreLane(instruction, 64)
	case V128Load32Zero:
		err = handleLoad(vm, instruction, SimdV128Load32Zero, 4)
	case V128Load64Zero:
		err = handleLoad(vm, instruction, SimdV128Load64Zero, 8)
	case F32x4DemoteF64x2Zero:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4DemoteF64x2Zero)
	case F64x2PromoteLowF32x4:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2PromoteLowF32x4)
	case I8x16Abs:
		handleUnary(vm, vm.stack.PopV128, SimdI8x16Abs)
	case I8x16Neg:
		handleUnary(vm, vm.stack.PopV128, SimdI8x16Neg)
	case I8x16Popcnt:
		handleUnary(vm, vm.stack.PopV128, SimdI8x16Popcnt)
	case I8x16AllTrue:
		handleUnaryBool(vm, vm.stack.PopV128, SimdI8x16AllTrue)
	case I8x16Bitmask:
		handleUnary(vm, vm.stack.PopV128, SimdI8x16Bitmask)
	case I8x16NarrowI16x8S:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16NarrowI16x8S)
	case I8x16NarrowI16x8U:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16NarrowI16x8U)
	case F32x4Ceil:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Ceil)
	case F32x4Floor:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Floor)
	case F32x4Trunc:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Trunc)
	case F32x4Nearest:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Nearest)
	case I8x16Shl:
		vm.handleSimdShift(SimdI8x16Shl)
	case I8x16ShrU:
		vm.handleSimdShift(SimdI8x16ShrU)
	case I8x16ShrS:
		vm.handleSimdShift(SimdI8x16ShrS)
	case I8x16Add:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16Add)
	case I8x16AddSatS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16AddSatS)
	case I8x16AddSatU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16AddSatU)
	case I8x16Sub:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16Sub)
	case I8x16SubSatS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16SubSatS)
	case I8x16SubSatU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16SubSatU)
	case F64x2Ceil:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Ceil)
	case F64x2Floor:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Floor)
	case I8x16MinS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16MinS)
	case I8x16MinU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16MinU)
	case I8x16MaxS:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16MaxS)
	case I8x16MaxU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16MaxU)
	case F64x2Trunc:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Trunc)
	case I8x16AvgrU:
		handleBinary(vm, vm.stack.PopV128, SimdI8x16AvgrU)
	case I16x8ExtaddPairwiseI8x16S:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtaddPairwiseI8x16S)
	case I16x8ExtaddPairwiseI8x16U:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtaddPairwiseI8x16U)
	case I32x4ExtaddPairwiseI16x8S:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtaddPairwiseI16x8S)
	case I32x4ExtaddPairwiseI16x8U:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtaddPairwiseI16x8U)
	case I16x8Abs:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8Abs)
	case I16x8Neg:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8Neg)
	case I16x8Q15mulrSatS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Q15mulrSatS)
	case I16x8AllTrue:
		handleUnaryBool(vm, vm.stack.PopV128, SimdI16x8AllTrue)
	case I16x8Bitmask:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8Bitmask)
	case I16x8NarrowI32x4S:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8NarrowI32x4S)
	case I16x8NarrowI32x4U:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8NarrowI32x4U)
	case I16x8ExtendLowI8x16S:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendLowI8x16S)
	case I16x8ExtendHighI8x16S:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendHighI8x16S)
	case I16x8ExtendLowI8x16U:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendLowI8x16U)
	case I16x8ExtendHighI8x16U:
		handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendHighI8x16U)
	case I16x8Shl:
		vm.handleSimdShift(SimdI16x8Shl)
	case I16x8ShrS:
		vm.handleSimdShift(SimdI16x8ShrS)
	case I16x8ShrU:
		vm.handleSimdShift(SimdI16x8ShrU)
	case I16x8Add:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Add)
	case I16x8AddSatS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8AddSatS)
	case I16x8AddSatU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8AddSatU)
	case I16x8Sub:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Sub)
	case I16x8SubSatS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8SubSatS)
	case I16x8SubSatU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8SubSatU)
	case F64x2Nearest:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Nearest)
	case I16x8Mul:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8Mul)
	case I16x8MinS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8MinS)
	case I16x8MinU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8MinU)
	case I16x8MaxS:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8MaxS)
	case I16x8MaxU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8MaxU)
	case I16x8AvgrU:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8AvgrU)
	case I16x8ExtmulLowI8x16S:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulLowI8x16S)
	case I16x8ExtmulHighI8x16S:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulHighI8x16S)
	case I16x8ExtmulLowI8x16U:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulLowI8x16U)
	case I16x8ExtmulHighI8x16U:
		handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulHighI8x16U)
	case I32x4Abs:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4Abs)
	case I32x4Neg:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4Neg)
	case I32x4AllTrue:
		handleUnaryBool(vm, vm.stack.PopV128, SimdI32x4AllTrue)
	case I32x4Bitmask:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4Bitmask)
	case I32x4ExtendLowI16x8S:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendLowI16x8S)
	case I32x4ExtendHighI16x8S:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendHighI16x8S)
	case I32x4ExtendLowI16x8U:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendLowI16x8U)
	case I32x4ExtendHighI16x8U:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendHighI16x8U)
	case I32x4Shl:
		vm.handleSimdShift(SimdI32x4Shl)
	case I32x4ShrS:
		vm.handleSimdShift(SimdI32x4ShrS)
	case I32x4ShrU:
		vm.handleSimdShift(SimdI32x4ShrU)
	case I32x4Add:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4Add)
	case I32x4Sub:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4Sub)
	case I32x4Mul:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4Mul)
	case I32x4MinS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4MinS)
	case I32x4MinU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4MinU)
	case I32x4MaxS:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4MaxS)
	case I32x4MaxU:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4MaxU)
	case I32x4DotI16x8S:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4DotI16x8S)
	case I32x4ExtmulLowI16x8S:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulLowI16x8S)
	case I32x4ExtmulHighI16x8S:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulHighI16x8S)
	case I32x4ExtmulLowI16x8U:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulLowI16x8U)
	case I32x4ExtmulHighI16x8U:
		handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulHighI16x8U)
	case I64x2Abs:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2Abs)
	case I64x2Neg:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2Neg)
	case I64x2AllTrue:
		handleUnaryBool(vm, vm.stack.PopV128, SimdI64x2AllTrue)
	case I64x2Bitmask:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2Bitmask)
	case I64x2ExtendLowI32x4S:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendLowI32x4S)
	case I64x2ExtendHighI32x4S:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendHighI32x4S)
	case I64x2ExtendLowI32x4U:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendLowI32x4U)
	case I64x2ExtendHighI32x4U:
		handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendHighI32x4U)
	case I64x2Shl:
		vm.handleSimdShift(SimdI64x2Shl)
	case I64x2ShrS:
		vm.handleSimdShift(SimdI64x2ShrS)
	case I64x2ShrU:
		vm.handleSimdShift(SimdI64x2ShrU)
	case I64x2Add:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2Add)
	case I64x2Sub:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2Sub)
	case I64x2Mul:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2Mul)
	case I64x2Eq:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2Eq)
	case I64x2Ne:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2Ne)
	case I64x2LtS:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2LtS)
	case I64x2GtS:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2GtS)
	case I64x2LeS:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2LeS)
	case I64x2GeS:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2GeS)
	case I64x2ExtmulLowI32x4S:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulLowI32x4S)
	case I64x2ExtmulHighI32x4S:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulHighI32x4S)
	case I64x2ExtmulLowI32x4U:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulLowI32x4U)
	case I64x2ExtmulHighI32x4U:
		handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulHighI32x4U)
	case F32x4Abs:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Abs)
	case F32x4Neg:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Neg)
	case F32x4Sqrt:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4Sqrt)
	case F32x4Add:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Add)
	case F32x4Sub:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Sub)
	case F32x4Mul:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Mul)
	case F32x4Div:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Div)
	case F32x4Min:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Min)
	case F32x4Max:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Max)
	case F32x4Pmin:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Pmin)
	case F32x4Pmax:
		handleBinary(vm, vm.stack.PopV128, SimdF32x4Pmax)
	case F64x2Abs:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Abs)
	case F64x2Neg:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Neg)
	case F64x2Sqrt:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2Sqrt)
	case F64x2Add:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Add)
	case F64x2Sub:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Sub)
	case F64x2Mul:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Mul)
	case F64x2Div:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Div)
	case F64x2Min:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Min)
	case F64x2Max:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Max)
	case F64x2Pmin:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Pmin)
	case F64x2Pmax:
		handleBinary(vm, vm.stack.PopV128, SimdF64x2Pmax)
	case I32x4TruncSatF32x4S:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF32x4S)
	case I32x4TruncSatF32x4U:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF32x4U)
	case F32x4ConvertI32x4S:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4ConvertI32x4S)
	case F32x4ConvertI32x4U:
		handleUnary(vm, vm.stack.PopV128, SimdF32x4ConvertI32x4U)
	case I32x4TruncSatF64x2SZero:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF64x2SZero)
	case I32x4TruncSatF64x2UZero:
		handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF64x2UZero)
	case F64x2ConvertLowI32x4S:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2ConvertLowI32x4S)
	case F64x2ConvertLowI32x4U:
		handleUnary(vm, vm.stack.PopV128, SimdF64x2ConvertLowI32x4U)
	default:
		err = fmt.Errorf("unknown opcode %d", instruction.Opcode)
	}
	return err
}

func (vm *VM) currentCallFrame() *CallFrame {
	return vm.callStack[len(vm.callStack)-1]
}

func (vm *VM) currentModuleInstance() *ModuleInstance {
	return vm.currentCallFrame().Function.Module
}

func (vm *VM) pushBlockFrame(opcode Opcode, blockType int32) error {
	callFrame := vm.currentCallFrame()
	originalPc := callFrame.Decoder.Pc
	inputCount, outputCount := vm.getBlockInputOutputCount(blockType)
	frame := &ControlFrame{
		Opcode:      opcode,
		Pc:          originalPc,
		InputCount:  inputCount,
		OutputCount: outputCount,
		StackHeight: vm.stack.Size(),
	}

	// For loops, the continuation is a branch back to the start of the block.
	if opcode == Loop {
		frame.ContinuationPc = originalPc
	} else {
		if cachedPc, ok := callFrame.Function.JumpCache[originalPc]; ok {
			frame.ContinuationPc = cachedPc
		} else {
			// Cache miss: we need to scan forward to find the matching 'end'.
			if err := callFrame.Decoder.DecodeUntilMatchingEnd(); err != nil {
				return err
			}

			callFrame.Function.JumpCache[originalPc] = callFrame.Decoder.Pc
			frame.ContinuationPc = callFrame.Decoder.Pc
			callFrame.Decoder.Pc = originalPc
		}
	}

	vm.pushControlFrame(frame)
	return nil
}

func (vm *VM) handleStructured(instruction Instruction) error {
	blockType := int32(instruction.Immediates[0])
	return vm.pushBlockFrame(instruction.Opcode, blockType)
}

func (vm *VM) handleIf(instruction Instruction) error {
	frame := vm.currentCallFrame()
	blockType := int32(instruction.Immediates[0])
	originalPc := frame.Decoder.Pc

	condition := vm.stack.PopInt32()

	if err := vm.pushBlockFrame(If, blockType); err != nil {
		return err
	}

	if condition != 0 {
		return nil
	}

	// We need to jump to the 'else' or 'end'.
	if elsePc, ok := frame.Function.JumpElseCache[originalPc]; ok {
		frame.Decoder.Pc = elsePc
		return nil
	}

	// Cache miss, we need to find the matching Else or End.
	matchingOpcode, err := frame.Decoder.DecodeUntilMatchingElseOrEnd()
	if err != nil {
		return err
	}

	if matchingOpcode == Else {
		// We need to consume the Else instruction and jump to the next "actual"
		// instruction after it.
		if _, err := frame.Decoder.Decode(); err != nil {
			return err
		}
	}

	frame.Function.JumpElseCache[originalPc] = frame.Decoder.Pc
	return nil
}

func (vm *VM) handleElse() {
	callFrame := vm.currentCallFrame()
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame := vm.popControlFrame()
	callFrame.Decoder.Pc = ifFrame.ContinuationPc
}

func (vm *VM) handleEnd() {
	frame := vm.popControlFrame()
	vm.unwindStack(frame.StackHeight, frame.OutputCount)
}

func (vm *VM) handleBr(instruction Instruction) {
	labelIndex := uint32(instruction.Immediates[0])
	vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrIf(instruction Instruction) {
	labelIndex := uint32(instruction.Immediates[0])
	val := vm.stack.PopInt32()
	if val == 0 {
		return
	}
	vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrTable(instruction Instruction) {
	immediates := instruction.Immediates
	table := immediates[:len(immediates)-1]
	defaultTarget := uint32(immediates[len(immediates)-1])
	index := vm.stack.PopInt32()
	if index >= 0 && int(index) < len(table) {
		vm.brToLabel(uint32(table[index]))
	} else {
		vm.brToLabel(defaultTarget)
	}
}

func (vm *VM) brToLabel(labelIndex uint32) {
	callFrame := vm.currentCallFrame()

	var targetFrame *ControlFrame
	for range int(labelIndex) + 1 {
		targetFrame = vm.popControlFrame()
	}

	var arity uint
	if targetFrame.Opcode == Loop {
		arity = targetFrame.InputCount
	} else {
		arity = targetFrame.OutputCount
	}

	vm.unwindStack(targetFrame.StackHeight, arity)
	if targetFrame.Opcode == Loop {
		vm.pushControlFrame(targetFrame)
	}

	callFrame.Decoder.Pc = targetFrame.ContinuationPc
}

func (vm *VM) handleCall(instruction Instruction) error {
	localIndex := uint32(instruction.Immediates[0])
	function := vm.getFunction(localIndex)
	res, err := vm.invoke(function)
	if err != nil {
		return err
	}
	vm.stack.PushAll(res)
	return nil
}

func (vm *VM) handleCallIndirect(instruction Instruction) error {
	typeIndex := uint32(instruction.Immediates[0])
	tableIndex := uint32(instruction.Immediates[1])

	expectedType := vm.currentModuleInstance().Types[typeIndex]
	table := vm.getTable(tableIndex)

	elementIndex := vm.stack.PopInt32()

	tableElement, err := table.Get(elementIndex)
	if err != nil {
		return err
	}
	if tableElement == NullVal {
		return fmt.Errorf("uninitialized element %d", elementIndex)
	}

	function := vm.store.funcs[uint32(tableElement.(int32))]
	if !function.GetType().Equal(&expectedType) {
		return fmt.Errorf("indirect call type mismatch")
	}

	res, err := vm.invoke(function)
	if err != nil {
		return err
	}
	vm.stack.PushAll(res)
	return nil
}

func (vm *VM) handleSelect() {
	condition := vm.stack.PopInt32()
	val2 := vm.stack.Pop()
	val1 := vm.stack.Pop()
	if condition != 0 {
		vm.stack.Push(val1)
	} else {
		vm.stack.Push(val2)
	}
}

func (vm *VM) handleLocalGet(instruction Instruction) {
	callFrame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	vm.stack.Push(callFrame.Locals[localIndex])
}

func (vm *VM) handleLocalSet(instruction Instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	valueType := getLocalValueType(frame.Function, localIndex)
	frame.Locals[localIndex] = vm.stack.PopValueType(valueType)
}

func (vm *VM) handleLocalTee(instruction Instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	valueType := getLocalValueType(frame.Function, localIndex)
	val := vm.stack.PopValueType(valueType)
	frame.Locals[localIndex] = val
	vm.stack.Push(val)
}

func (vm *VM) handleGlobalGet(instruction Instruction) {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	vm.stack.Push(global.Value)
}

func (vm *VM) handleGlobalSet(instruction Instruction) {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	global.Value = vm.stack.Pop()
}

func (vm *VM) handleTableGet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	index := vm.stack.PopInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}
	vm.stack.Push(element)
	return nil
}

func (vm *VM) handleTableSet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	reference := vm.stack.Pop()
	index := vm.stack.PopInt32()
	return table.Set(index, reference)
}

func (vm *VM) handleMemorySize(instruction Instruction) {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	vm.stack.Push(memory.Size())
}

func (vm *VM) handleMemoryGrow(instruction Instruction) {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	pages := vm.stack.PopInt32()
	oldSize := memory.Grow(pages)
	vm.stack.Push(oldSize)
}

func (vm *VM) handleRefFunc(instruction Instruction) {
	funcIndex := uint32(instruction.Immediates[0])
	storeIndex := vm.currentModuleInstance().FuncAddrs[funcIndex]
	vm.stack.Push(int32(storeIndex))
}

func (vm *VM) handleRefIsNull() {
	top := vm.stack.Pop()
	_, topIsNull := top.(Null)
	vm.stack.Push(BoolToInt32(topIsNull))
}

func (vm *VM) handleMemoryInit(instruction Instruction) error {
	data := vm.getData(uint32(instruction.Immediates[0]))
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.Pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.Content)
}

func (vm *VM) handleDataDrop(instruction Instruction) {
	dataSegment := vm.getData(uint32(instruction.Immediates[0]))
	dataSegment.Content = nil
}

func (vm *VM) handleMemoryCopy(instruction Instruction) error {
	destMemory := vm.getMemory(uint32(instruction.Immediates[0]))
	srcMemory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.Pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *VM) handleMemoryFill(instruction Instruction) error {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	n, val, offset := vm.stack.Pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *VM) handleTableInit(instruction Instruction) error {
	element := vm.getElement(uint32(instruction.Immediates[0]))
	table := vm.getTable(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.Pop3Int32()

	switch element.Mode {
	case ActiveElementMode:
		// Trap if using an active, non-dropped element segment.
		// A dropped segment has its FuncIndexes slice set to nil.
		if element.FuncIndexes != nil {
			return ErrTableOutOfBounds
		}
		return table.Init(n, d, s, element.FuncIndexes)
	case PassiveElementMode:
		moduleInstance := vm.currentModuleInstance()
		storeIndexes := toStoreFuncIndexes(moduleInstance, element.FuncIndexes)
		return table.Init(n, d, s, storeIndexes)
	default:
		return ErrTableOutOfBounds
	}
}

func (vm *VM) handleElemDrop(instruction Instruction) {
	element := vm.getElement(uint32(instruction.Immediates[0]))
	element.FuncIndexes = nil
	element.FuncIndexesExpressions = nil
}

func (vm *VM) handleTableCopy(instruction Instruction) error {
	destTable := vm.getTable(uint32(instruction.Immediates[0]))
	srcTable := vm.getTable(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.Pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *VM) handleTableGrow(instruction Instruction) {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n := vm.stack.PopInt32()
	val := vm.stack.Pop()
	vm.stack.Push(table.Grow(n, val))
}

func (vm *VM) handleTableSize(instruction Instruction) {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	vm.stack.Push(table.Size())
}

func (vm *VM) handleTableFill(instruction Instruction) error {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n := vm.stack.PopInt32()
	val := vm.stack.Pop()
	i := vm.stack.PopInt32()
	return table.Fill(n, i, val)
}

func (vm *VM) handleI8x16Shuffle(instruction Instruction) {
	v2 := vm.stack.PopV128()
	v1 := vm.stack.PopV128()

	var lanes [16]byte
	for i, imm := range instruction.Immediates {
		lanes[i] = byte(imm)
	}

	vm.stack.Push(SimdI8x16Shuffle(v1, v2, lanes))
}

func handleBinary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a, b T) R,
) {
	b := pop()
	a := pop()
	vm.stack.Push(op(a, b))
}

func handleBinarySafe[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a, b T) (R, error),
) error {
	b := pop()
	a := pop()
	result, err := op(a, b)
	if err != nil {
		return err
	}
	vm.stack.Push(result)
	return nil
}

func handleBinaryBool[T WasmNumber](
	vm *VM,
	pop func() T,
	op func(a, b T) bool,
) {
	b := pop()
	a := pop()
	vm.stack.Push(BoolToInt32(op(a, b)))
}

func handleUnary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a T) R,
) {
	vm.stack.Push(op(pop()))
}

func handleUnarySafe[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a T) (R, error),
) error {
	a := pop()
	result, err := op(a)
	if err != nil {
		return err
	}
	vm.stack.Push(result)
	return nil
}

func handleUnaryBool[T WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a T) bool,
) {
	vm.stack.Push(BoolToInt32(op(pop())))
}

func (vm *VM) handleSimdShift(op func(v V128Value, shift int32) V128Value) {
	shift := vm.stack.PopInt32()
	v := vm.stack.PopV128()
	vm.stack.Push(op(v, shift))
}

func (vm *VM) handleSimdTernary(op func(v1, v2, v3 V128Value) V128Value) {
	v3 := vm.stack.PopV128()
	v2 := vm.stack.PopV128()
	v1 := vm.stack.PopV128()
	result := op(v1, v2, v3)
	vm.stack.Push(result)
}

func handleSimdExtractLane[R WasmNumber](
	vm *VM,
	instruction Instruction,
	op func(v V128Value, laneIndex uint32) R,
) {
	laneIndex := uint32(instruction.Immediates[0])
	v := vm.stack.PopV128()
	vm.stack.Push(op(v, laneIndex))
}

func handleStore[T WasmNumber | V128Value](
	vm *VM,
	instruction Instruction,
	pop func() T,
	toBytes func(T) []byte,
	sizeBytes uint32,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	value := pop()
	index := vm.stack.PopInt32()
	data := toBytes(value)
	if len(data) > int(sizeBytes) {
		data = data[:sizeBytes]
	}
	return memory.Set(offset, uint32(index), data)
}

func handleLoad[T WasmNumber | V128Value](
	vm *VM,
	instruction Instruction,
	fromBytes func(bytes []byte) T,
	sizeBytes uint32,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	index := vm.stack.PopInt32()

	data, err := memory.Get(offset, uint32(index), sizeBytes)
	if err != nil {
		return err
	}
	vm.stack.Push(fromBytes(data))
	return nil
}

func (vm *VM) handleSimdConst(instruction Instruction) {
	v := V128Value{
		Low:  instruction.Immediates[0],
		High: instruction.Immediates[1],
	}
	vm.stack.Push(v)
}

func (vm *VM) handleSimdLoadLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v := vm.stack.PopV128()
	index := vm.stack.PopInt32()

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return nil
	}

	vm.stack.Push(SetLane(v, laneIndex, laneValue))
	return nil
}

func (vm *VM) handleSimdStoreLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v := vm.stack.PopV128()
	index := vm.stack.PopInt32()

	laneData := ExtractLane(v, laneSize, laneIndex)
	return memory.Set(offset, uint32(index), laneData)
}

func handleSimdReplaceLane[T WasmNumber](
	vm *VM,
	instruction Instruction,
	pop func() T,
	replaceLane func(V128Value, uint32, T) V128Value,
) {
	laneIndex := uint32(instruction.Immediates[0])
	laneValue := pop()
	vector := vm.stack.PopV128()
	vm.stack.Push(replaceLane(vector, laneIndex, laneValue))
}

func (vm *VM) unwindStack(targetHeight, arity uint) {
	valuesToPreserve := vm.stack.PeekN(arity)
	vm.stack.Resize(targetHeight)
	vm.stack.PushAll(valuesToPreserve)
}

func (vm *VM) getBlockInputOutputCount(blockType int32) (uint, uint) {
	if blockType == -0x40 { // empty block type.
		return 0, 0
	}

	if blockType >= 0 { // type index.
		funcType := vm.currentModuleInstance().Types[blockType]
		return uint(len(funcType.ParamTypes)), uint(len(funcType.ResultTypes))
	}

	return 0, 1 // value type.
}

func getLocalValueType(function *WasmFunc, localIndex int32) ValueType {
	if int(localIndex) < len(function.Type.ParamTypes) {
		return function.Type.ParamTypes[localIndex]
	}

	return function.Code.Locals[localIndex-int32(len(function.Type.ParamTypes))]
}

func getExport(
	module *ModuleInstance,
	name string,
	indexType IndexType,
) (any, error) {
	for _, export := range module.Exports {
		if export.Name != name {
			continue
		}

		switch indexType {
		case FunctionIndexType:
			function, ok := export.Value.(FunctionInstance)
			if !ok {
				return nil, fmt.Errorf("export %s is not a function", name)
			}
			return function, nil
		case GlobalIndexType:
			global, ok := export.Value.(*Global)
			if !ok {
				return nil, fmt.Errorf("export %s is not a global", name)
			}
			return global, nil
		default:
			return nil, fmt.Errorf("unsupported indexType %d", indexType)
		}
	}

	return nil, fmt.Errorf("failed to find export with name: %s", name)
}

func (vm *VM) pushControlFrame(frame *ControlFrame) {
	callFrame := vm.currentCallFrame()
	callFrame.ControlStack = append(callFrame.ControlStack, frame)
}

func (vm *VM) popControlFrame() *ControlFrame {
	callFrame := vm.currentCallFrame()
	// Validation guarantees the control stack is never empty.
	index := len(callFrame.ControlStack) - 1
	frame := callFrame.ControlStack[index]
	callFrame.ControlStack = callFrame.ControlStack[:index]
	return frame
}

func (vm *VM) initActiveElements(
	module *Module,
	moduleInstance *ModuleInstance,
) error {
	for _, element := range module.ElementSegments {
		if element.Mode != ActiveElementMode {
			continue
		}

		expression := element.OffsetExpression
		offsetAny, err := vm.invokeInitExpression(expression, I32, moduleInstance)
		if err != nil {
			return err
		}
		offset := offsetAny.(int32)

		storeTableIndex := moduleInstance.TableAddrs[element.TableIndex]
		table := vm.store.tables[storeTableIndex]
		if offset > int32(table.Size()) {
			return ErrTableOutOfBounds
		}

		if len(element.FuncIndexes) > 0 {
			indexes := toStoreFuncIndexes(moduleInstance, element.FuncIndexes)
			if err := table.InitFromSlice(offset, indexes); err != nil {
				return err
			}
		}

		if len(element.FuncIndexesExpressions) > 0 {
			values := make([]any, len(element.FuncIndexesExpressions))
			for i, expr := range element.FuncIndexesExpressions {
				refAny, err := vm.invokeInitExpression(
					expr,
					element.Kind,
					moduleInstance,
				)
				if err != nil {
					return err
				}
				// Note that this reference might be nil.
				values[i] = refAny
			}

			if err := table.InitFromAnySlice(offset, values); err != nil {
				return err
			}
		}
	}
	return nil
}

func (vm *VM) initActiveDatas(
	module *Module,
	moduleInstance *ModuleInstance,
) error {
	for _, segment := range module.DataSegments {
		if segment.Mode != ActiveDataMode {
			continue
		}

		expression := segment.OffsetExpression
		offsetAny, err := vm.invokeInitExpression(expression, I32, moduleInstance)
		if err != nil {
			return err
		}
		offset := offsetAny.(int32)
		storeMemoryIndex := moduleInstance.MemAddrs[segment.MemoryIndex]
		memory := vm.store.memories[storeMemoryIndex]
		if err := memory.Set(uint32(offset), 0, segment.Content); err != nil {
			return err
		}
	}

	return nil
}

func (vm *VM) resolveExports(
	module *Module,
	instance *ModuleInstance,
) []ExportInstance {
	exports := []ExportInstance{}
	for _, export := range module.Exports {
		var value any
		switch export.IndexType {
		case FunctionIndexType:
			storeIndex := instance.FuncAddrs[export.Index]
			value = vm.store.funcs[storeIndex]
		case GlobalIndexType:
			storeIndex := instance.GlobalAddrs[export.Index]
			value = vm.store.globals[storeIndex]
		case MemoryIndexType:
			storeIndex := instance.MemAddrs[export.Index]
			value = vm.store.memories[storeIndex]
		case TableIndexType:
			storeIndex := instance.TableAddrs[export.Index]
			value = vm.store.tables[storeIndex]
		}
		exports = append(exports, ExportInstance{Name: export.Name, Value: value})
	}
	return exports
}

func (vm *VM) invokeHostFunction(fun *HostFunc) (res []any, err error) {
	defer func() {
		if r := recover(); r != nil {
			var panicErr error
			switch v := r.(type) {
			case error:
				panicErr = v
			default:
				panicErr = fmt.Errorf("panic: %v", v)
			}
			err = panicErr
		}
	}()

	args := vm.stack.PopValueTypes(fun.GetType().ParamTypes)
	res = fun.HostCode(args...)
	return res, nil
}

func (vm *VM) invokeInitExpression(
	expression []byte,
	resultType ValueType,
	moduleInstance *ModuleInstance,
) (any, error) {
	// We create a fake function to execute the expression.
	// The expression is expected to return a single value.
	function := WasmFunc{
		Type: FunctionType{
			ParamTypes:  []ValueType{},
			ResultTypes: []ValueType{resultType},
		},
		Code:   Function{Body: expression},
		Module: moduleInstance,
	}
	results, err := vm.invokeWasmFunction(&function)
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func toStoreFuncIndexes(
	moduleInstance *ModuleInstance,
	localIndexes []int32,
) []int32 {
	storeIndices := make([]int32, len(localIndexes))
	for i, localIndex := range localIndexes {
		storeIndices[i] = int32(moduleInstance.FuncAddrs[localIndex])
	}
	return storeIndices
}

func (vm *VM) getFunction(localIndex uint32) FunctionInstance {
	functionIndex := vm.currentModuleInstance().FuncAddrs[localIndex]
	return vm.store.funcs[functionIndex]
}

func (vm *VM) getTable(localIndex uint32) *Table {
	tableIndex := vm.currentModuleInstance().TableAddrs[localIndex]
	return vm.store.tables[tableIndex]
}

func (vm *VM) getMemory(localIndex uint32) *Memory {
	memoryIndex := vm.currentModuleInstance().MemAddrs[localIndex]
	return vm.store.memories[memoryIndex]
}

func (vm *VM) getGlobal(localIndex uint32) *Global {
	globalIndex := vm.currentModuleInstance().GlobalAddrs[localIndex]
	return vm.store.globals[globalIndex]
}

func (vm *VM) getElement(localIndex uint32) *ElementSegment {
	elementIndex := vm.currentModuleInstance().ElemAddrs[localIndex]
	return &vm.store.elements[elementIndex]
}

func (vm *VM) getData(localIndex uint32) *DataSegment {
	dataIndex := vm.currentModuleInstance().DataAddrs[localIndex]
	return &vm.store.datas[dataIndex]
}
