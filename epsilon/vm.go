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
	ErrTableOutOfBounds   = errors.New("out of bounds table access")
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
}

func NewVM() *VM {
	return &VM{store: NewStore(), stack: NewValueStack()}
}

func (vm *VM) Instantiate(
	module *Module,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	moduleInstance := &ModuleInstance{Types: module.Types}

	functions, tables, memories, globals, err := resolveImports(module, imports)
	if err != nil {
		return nil, err
	}

	for _, functionInstance := range functions {
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

	for _, table := range tables {
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

	for _, memory := range memories {
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

	for _, global := range globals {
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

	exports, err := vm.resolveExports(module, moduleInstance)
	if err != nil {
		return nil, err
	}
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

	if err := vm.stack.PushAll(args); err != nil {
		return nil, err
	}

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

	locals, err := vm.stack.PopValueTypes(function.Type.ParamTypes)
	if err != nil {
		return nil, err
	}

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
	return vm.stack.PopValueTypes(callFrame.Function.Type.ResultTypes)
}

func (vm *VM) handleInstruction(instruction Instruction) error {
	// Using a switch instead of a map of Opcode -> Handler is significantly
	// faster.
	switch instruction.Opcode {
	case Unreachable:
		return ErrUnreachable
	case Nop:
		return nil
	case Block, Loop:
		return vm.handleStructured(instruction)
	case If:
		return vm.handleIf(instruction)
	case Else:
		return vm.handleElse()
	case End:
		return vm.handleEnd()
	case Br:
		return vm.handleBr(instruction)
	case BrIf:
		return vm.handleBrIf(instruction)
	case BrTable:
		return vm.handleBrTable(instruction)
	case Return:
		return errReturn
	case Call:
		return vm.handleCall(instruction)
	case CallIndirect:
		return vm.handleCallIndirect(instruction)
	case Drop:
		return vm.stack.Drop()
	case Select:
		return vm.handleSelect()
	case SelectT:
		return vm.handleSelect()
	case LocalGet:
		return vm.handleLocalGet(instruction)
	case LocalSet:
		return vm.handleLocalSet(instruction)
	case LocalTee:
		return vm.handleLocalTee(instruction)
	case GlobalGet:
		return vm.handleGlobalGet(instruction)
	case GlobalSet:
		return vm.handleGlobalSet(instruction)
	case TableGet:
		return vm.handleTableGet(instruction)
	case TableSet:
		return vm.handleTableSet(instruction)
	case I32Load:
		return handleLoad(vm, instruction, Int32From4Bytes, 4)
	case I64Load:
		return handleLoad(vm, instruction, Int64From8Bytes, 8)
	case F32Load:
		return handleLoad(vm, instruction, Float32From4Bytes, 4)
	case F64Load:
		return handleLoad(vm, instruction, Float64From8Bytes, 8)
	case I32Load8S:
		return handleLoad(vm, instruction, IntFrom1Byte[int32], 1)
	case I32Load8U:
		return handleLoad(vm, instruction, UintFrom1Byte[int32], 1)
	case I32Load16S:
		return handleLoad(vm, instruction, IntFrom2Bytes[int32], 2)
	case I32Load16U:
		return handleLoad(vm, instruction, UintFrom2Bytes[int32], 2)
	case I64Load8S:
		return handleLoad(vm, instruction, IntFrom1Byte[int64], 1)
	case I64Load8U:
		return handleLoad(vm, instruction, UintFrom1Byte[int64], 1)
	case I64Load16S:
		return handleLoad(vm, instruction, IntFrom2Bytes[int64], 2)
	case I64Load16U:
		return handleLoad(vm, instruction, UintFrom2Bytes[int64], 2)
	case I64Load32S:
		return handleLoad(vm, instruction, Int64From4Bytes, 4)
	case I64Load32U:
		return handleLoad(vm, instruction, Uint64From4Bytes, 4)
	case I32Store:
		return handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 4)
	case I64Store:
		return handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 8)
	case F32Store:
		return handleStore(vm, instruction, vm.stack.PopFloat32, BytesFromFloat32, 4)
	case F64Store:
		return handleStore(vm, instruction, vm.stack.PopFloat64, BytesFromFloat64, 8)
	case I32Store8:
		return handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 1)
	case I32Store16:
		return handleStore(vm, instruction, vm.stack.PopInt32, BytesFromInt32, 2)
	case I64Store8:
		return handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 1)
	case I64Store16:
		return handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 2)
	case I64Store32:
		return handleStore(vm, instruction, vm.stack.PopInt64, BytesFromInt64, 4)
	case MemorySize:
		return vm.handleMemorySize(instruction)
	case MemoryGrow:
		return vm.handleMemoryGrow(instruction)
	case I32Const:
		return handleConst(vm, instruction, Uint64ToInt32)
	case I64Const:
		return handleConst(vm, instruction, Uint64ToInt64)
	case F32Const:
		return handleConst(vm, instruction, Uint64ToFloat32)
	case F64Const:
		return handleConst(vm, instruction, Uint64ToFloat64)
	case I32Eqz:
		return handleUnaryBool(vm, vm.stack.PopInt32, EqualZero)
	case I32Eq:
		return handleBinaryBool(vm, vm.stack.PopInt32, Equal)
	case I32Ne:
		return handleBinaryBool(vm, vm.stack.PopInt32, NotEqual)
	case I32LtS:
		return handleBinaryBool(vm, vm.stack.PopInt32, LessThan)
	case I32LtU:
		return handleBinaryBool(vm, vm.stack.PopInt32, LessThanUnsigned32)
	case I32GtS:
		return handleBinaryBool(vm, vm.stack.PopInt32, GreaterThan)
	case I32GtU:
		return handleBinaryBool(vm, vm.stack.PopInt32, GreaterThanUnsigned32)
	case I32LeS:
		return handleBinaryBool(vm, vm.stack.PopInt32, LessOrEqual)
	case I32LeU:
		return handleBinaryBool(vm, vm.stack.PopInt32, LessOrEqualUnsigned32)
	case I32GeS:
		return handleBinaryBool(vm, vm.stack.PopInt32, GreaterOrEqual)
	case I32GeU:
		return handleBinaryBool(vm, vm.stack.PopInt32, GreaterOrEqualUnsigned32)
	case I64Eqz:
		return handleUnaryBool(vm, vm.stack.PopInt64, EqualZero)
	case I64Eq:
		return handleBinaryBool(vm, vm.stack.PopInt64, Equal)
	case I64Ne:
		return handleBinaryBool(vm, vm.stack.PopInt64, NotEqual)
	case I64LtS:
		return handleBinaryBool(vm, vm.stack.PopInt64, LessThan)
	case I64LtU:
		return handleBinaryBool(vm, vm.stack.PopInt64, LessThanUnsigned64)
	case I64GtS:
		return handleBinaryBool(vm, vm.stack.PopInt64, GreaterThan)
	case I64GtU:
		return handleBinaryBool(vm, vm.stack.PopInt64, GreaterThanUnsigned64)
	case I64LeS:
		return handleBinaryBool(vm, vm.stack.PopInt64, LessOrEqual)
	case I64LeU:
		return handleBinaryBool(vm, vm.stack.PopInt64, LessOrEqualUnsigned64)
	case I64GeS:
		return handleBinaryBool(vm, vm.stack.PopInt64, GreaterOrEqual)
	case I64GeU:
		return handleBinaryBool(vm, vm.stack.PopInt64, GreaterOrEqualUnsigned64)
	case F32Eq:
		return handleBinaryBool(vm, vm.stack.PopFloat32, Equal)
	case F32Ne:
		return handleBinaryBool(vm, vm.stack.PopFloat32, NotEqual)
	case F32Lt:
		return handleBinaryBool(vm, vm.stack.PopFloat32, LessThan)
	case F32Gt:
		return handleBinaryBool(vm, vm.stack.PopFloat32, GreaterThan)
	case F32Le:
		return handleBinaryBool(vm, vm.stack.PopFloat32, LessOrEqual)
	case F32Ge:
		return handleBinaryBool(vm, vm.stack.PopFloat32, GreaterOrEqual)
	case F64Eq:
		return handleBinaryBool(vm, vm.stack.PopFloat64, Equal)
	case F64Ne:
		return handleBinaryBool(vm, vm.stack.PopFloat64, NotEqual)
	case F64Lt:
		return handleBinaryBool(vm, vm.stack.PopFloat64, LessThan)
	case F64Gt:
		return handleBinaryBool(vm, vm.stack.PopFloat64, GreaterThan)
	case F64Le:
		return handleBinaryBool(vm, vm.stack.PopFloat64, LessOrEqual)
	case F64Ge:
		return handleBinaryBool(vm, vm.stack.PopFloat64, GreaterOrEqual)
	case I32Clz:
		return handleUnary(vm, vm.stack.PopInt32, Clz32)
	case I32Ctz:
		return handleUnary(vm, vm.stack.PopInt32, Ctz32)
	case I32Popcnt:
		return handleUnary(vm, vm.stack.PopInt32, Popcnt32)
	case I32Add:
		return handleBinary(vm, vm.stack.PopInt32, Add)
	case I32Sub:
		return handleBinary(vm, vm.stack.PopInt32, Sub)
	case I32Mul:
		return handleBinary(vm, vm.stack.PopInt32, Mul)
	case I32DivS:
		return handleBinarySafe(vm, vm.stack.PopInt32, DivS32)
	case I32DivU:
		return handleBinarySafe(vm, vm.stack.PopInt32, DivU32)
	case I32RemS:
		return handleBinarySafe(vm, vm.stack.PopInt32, RemS32)
	case I32RemU:
		return handleBinarySafe(vm, vm.stack.PopInt32, RemU32)
	case I32And:
		return handleBinary(vm, vm.stack.PopInt32, And)
	case I32Or:
		return handleBinary(vm, vm.stack.PopInt32, Or)
	case I32Xor:
		return handleBinary(vm, vm.stack.PopInt32, Xor)
	case I32Shl:
		return handleBinary(vm, vm.stack.PopInt32, Shl32)
	case I32ShrS:
		return handleBinary(vm, vm.stack.PopInt32, ShrS32)
	case I32ShrU:
		return handleBinary(vm, vm.stack.PopInt32, ShrU32)
	case I32Rotl:
		return handleBinary(vm, vm.stack.PopInt32, Rotl32)
	case I32Rotr:
		return handleBinary(vm, vm.stack.PopInt32, Rotr32)
	case I64Clz:
		return handleUnary(vm, vm.stack.PopInt64, Clz64)
	case I64Ctz:
		return handleUnary(vm, vm.stack.PopInt64, Ctz64)
	case I64Popcnt:
		return handleUnary(vm, vm.stack.PopInt64, Popcnt64)
	case I64Add:
		return handleBinary(vm, vm.stack.PopInt64, Add)
	case I64Sub:
		return handleBinary(vm, vm.stack.PopInt64, Sub)
	case I64Mul:
		return handleBinary(vm, vm.stack.PopInt64, Mul)
	case I64DivS:
		return handleBinarySafe(vm, vm.stack.PopInt64, DivS64)
	case I64DivU:
		return handleBinarySafe(vm, vm.stack.PopInt64, DivU64)
	case I64RemS:
		return handleBinarySafe(vm, vm.stack.PopInt64, RemS64)
	case I64RemU:
		return handleBinarySafe(vm, vm.stack.PopInt64, RemU64)
	case I64And:
		return handleBinary(vm, vm.stack.PopInt64, And)
	case I64Or:
		return handleBinary(vm, vm.stack.PopInt64, Or)
	case I64Xor:
		return handleBinary(vm, vm.stack.PopInt64, Xor)
	case I64Shl:
		return handleBinary(vm, vm.stack.PopInt64, Shl64)
	case I64ShrS:
		return handleBinary(vm, vm.stack.PopInt64, ShrS64)
	case I64ShrU:
		return handleBinary(vm, vm.stack.PopInt64, ShrU64)
	case I64Rotl:
		return handleBinary(vm, vm.stack.PopInt64, Rotl64)
	case I64Rotr:
		return handleBinary(vm, vm.stack.PopInt64, Rotr64)
	case F32Abs:
		return handleUnary(vm, vm.stack.PopFloat32, Abs[float32])
	case F32Neg:
		return handleUnary(vm, vm.stack.PopFloat32, Neg[float32])
	case F32Ceil:
		return handleUnary(vm, vm.stack.PopFloat32, Ceil[float32])
	case F32Floor:
		return handleUnary(vm, vm.stack.PopFloat32, Floor[float32])
	case F32Trunc:
		return handleUnary(vm, vm.stack.PopFloat32, Trunc[float32])
	case F32Nearest:
		return handleUnary(vm, vm.stack.PopFloat32, Nearest[float32])
	case F32Sqrt:
		return handleUnary(vm, vm.stack.PopFloat32, Sqrt[float32])
	case F32Add:
		return handleBinary(vm, vm.stack.PopFloat32, Add[float32])
	case F32Sub:
		return handleBinary(vm, vm.stack.PopFloat32, Sub[float32])
	case F32Mul:
		return handleBinary(vm, vm.stack.PopFloat32, Mul[float32])
	case F32Div:
		return handleBinary(vm, vm.stack.PopFloat32, Div[float32])
	case F32Min:
		return handleBinary(vm, vm.stack.PopFloat32, Min[float32])
	case F32Max:
		return handleBinary(vm, vm.stack.PopFloat32, Max[float32])
	case F32Copysign:
		return handleBinary(vm, vm.stack.PopFloat32, Copysign[float32])
	case F64Abs:
		return handleUnary(vm, vm.stack.PopFloat64, Abs[float64])
	case F64Neg:
		return handleUnary(vm, vm.stack.PopFloat64, Neg[float64])
	case F64Ceil:
		return handleUnary(vm, vm.stack.PopFloat64, Ceil[float64])
	case F64Floor:
		return handleUnary(vm, vm.stack.PopFloat64, Floor[float64])
	case F64Trunc:
		return handleUnary(vm, vm.stack.PopFloat64, Trunc[float64])
	case F64Nearest:
		return handleUnary(vm, vm.stack.PopFloat64, Nearest[float64])
	case F64Sqrt:
		return handleUnary(vm, vm.stack.PopFloat64, Sqrt[float64])
	case F64Add:
		return handleBinary(vm, vm.stack.PopFloat64, Add[float64])
	case F64Sub:
		return handleBinary(vm, vm.stack.PopFloat64, Sub[float64])
	case F64Mul:
		return handleBinary(vm, vm.stack.PopFloat64, Mul[float64])
	case F64Div:
		return handleBinary(vm, vm.stack.PopFloat64, Div[float64])
	case F64Min:
		return handleBinary(vm, vm.stack.PopFloat64, Min[float64])
	case F64Max:
		return handleBinary(vm, vm.stack.PopFloat64, Max[float64])
	case F64Copysign:
		return handleBinary(vm, vm.stack.PopFloat64, Copysign[float64])
	case I32WrapI64:
		return handleUnary(vm, vm.stack.PopInt64, WrapI64ToI32)
	case I32TruncF32S:
		return handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32SToI32)
	case I32TruncF32U:
		return handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32UToI32)
	case I32TruncF64S:
		return handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64SToI32)
	case I32TruncF64U:
		return handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64UToI32)
	case I64ExtendI32S:
		return handleUnary(vm, vm.stack.PopInt32, ExtendI32SToI64)
	case I64ExtendI32U:
		return handleUnary(vm, vm.stack.PopInt32, ExtendI32UToI64)
	case I64TruncF32S:
		return handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32SToI64)
	case I64TruncF32U:
		return handleUnarySafe(vm, vm.stack.PopFloat32, TruncF32UToI64)
	case I64TruncF64S:
		return handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64SToI64)
	case I64TruncF64U:
		return handleUnarySafe(vm, vm.stack.PopFloat64, TruncF64UToI64)
	case F32ConvertI32S:
		return handleUnary(vm, vm.stack.PopInt32, ConvertI32SToF32)
	case F32ConvertI32U:
		return handleUnary(vm, vm.stack.PopInt32, ConvertI32UToF32)
	case F32ConvertI64S:
		return handleUnary(vm, vm.stack.PopInt64, ConvertI64SToF32)
	case F32ConvertI64U:
		return handleUnary(vm, vm.stack.PopInt64, ConvertI64UToF32)
	case F32DemoteF64:
		return handleUnary(vm, vm.stack.PopFloat64, DemoteF64ToF32)
	case F64ConvertI32S:
		return handleUnary(vm, vm.stack.PopInt32, ConvertI32SToF64)
	case F64ConvertI32U:
		return handleUnary(vm, vm.stack.PopInt32, ConvertI32UToF64)
	case F64ConvertI64S:
		return handleUnary(vm, vm.stack.PopInt64, ConvertI64SToF64)
	case F64ConvertI64U:
		return handleUnary(vm, vm.stack.PopInt64, ConvertI64UToF64)
	case F64PromoteF32:
		return handleUnary(vm, vm.stack.PopFloat32, PromoteF32ToF64)
	case I32ReinterpretF32:
		return handleUnary(vm, vm.stack.PopFloat32, ReinterpretF32ToI32)
	case I64ReinterpretF64:
		return handleUnary(vm, vm.stack.PopFloat64, ReinterpretF64ToI64)
	case F32ReinterpretI32:
		return handleUnary(vm, vm.stack.PopInt32, ReinterpretI32ToF32)
	case F64ReinterpretI64:
		return handleUnary(vm, vm.stack.PopInt64, ReinterpretI64ToF64)
	case I32Extend8S:
		return handleUnary(vm, vm.stack.PopInt32, Extend8STo32)
	case I32Extend16S:
		return handleUnary(vm, vm.stack.PopInt32, Extend16STo32)
	case I64Extend8S:
		return handleUnary(vm, vm.stack.PopInt64, Extend8STo64)
	case I64Extend16S:
		return handleUnary(vm, vm.stack.PopInt64, Extend16STo64)
	case I64Extend32S:
		return handleUnary(vm, vm.stack.PopInt64, Extend32STo64)
	case RefNull:
		return vm.stack.Push(NullVal)
	case RefIsNull:
		return vm.handleRefIsNull()
	case RefFunc:
		return vm.handleRefFunc(instruction)
	case I32TruncSatF32S:
		return handleUnary(vm, vm.stack.PopFloat32, TruncSatF32SToI32)
	case I32TruncSatF32U:
		return handleUnary(vm, vm.stack.PopFloat32, TruncSatF32UToI32)
	case I32TruncSatF64S:
		return handleUnary(vm, vm.stack.PopFloat64, TruncSatF64SToI32)
	case I32TruncSatF64U:
		return handleUnary(vm, vm.stack.PopFloat64, TruncSatF64UToI32)
	case I64TruncSatF32S:
		return handleUnary(vm, vm.stack.PopFloat32, TruncSatF32SToI64)
	case I64TruncSatF32U:
		return handleUnary(vm, vm.stack.PopFloat32, TruncSatF32UToI64)
	case I64TruncSatF64S:
		return handleUnary(vm, vm.stack.PopFloat64, TruncSatF64SToI64)
	case I64TruncSatF64U:
		return handleUnary(vm, vm.stack.PopFloat64, TruncSatF64UToI64)
	case MemoryInit:
		return vm.handleMemoryInit(instruction)
	case DataDrop:
		return vm.handleDataDrop(instruction)
	case MemoryCopy:
		return vm.handleMemoryCopy(instruction)
	case MemoryFill:
		return vm.handleMemoryFill(instruction)
	case TableInit:
		return vm.handleTableInit(instruction)
	case ElemDrop:
		return vm.handleElemDrop(instruction)
	case TableCopy:
		return vm.handleTableCopy(instruction)
	case TableGrow:
		return vm.handleTableGrow(instruction)
	case TableSize:
		return vm.handleTableSize(instruction)
	case TableFill:
		return vm.handleTableFill(instruction)
	case V128Load:
		return handleLoad(vm, instruction, NewV128ValueFromSlice, 16)
	case V128Load8x8S:
		return handleLoad(vm, instruction, SimdV128Load8x8S, 8)
	case V128Load8x8U:
		return handleLoad(vm, instruction, SimdV128Load8x8U, 8)
	case V128Load16x4S:
		return handleLoad(vm, instruction, SimdV128Load16x4S, 8)
	case V128Load16x4U:
		return handleLoad(vm, instruction, SimdV128Load16x4U, 8)
	case V128Load32x2S:
		return handleLoad(vm, instruction, SimdV128Load32x2S, 8)
	case V128Load32x2U:
		return handleLoad(vm, instruction, SimdV128Load32x2U, 8)
	case V128Load8Splat:
		return handleLoad(vm, instruction, SimdI8x16SplatFromBytes, 1)
	case V128Load16Splat:
		return handleLoad(vm, instruction, SimdI16x8SplatFromBytes, 2)
	case V128Load32Splat:
		return handleLoad(vm, instruction, SimdI32x4SplatFromBytes, 4)
	case V128Load64Splat:
		return handleLoad(vm, instruction, SimdI64x2SplatFromBytes, 8)
	case V128Store:
		return handleStore(vm, instruction, vm.stack.PopV128, GetBytes, 16)
	case V128Const:
		return vm.handleSimdConst(instruction)
	case I8x16Shuffle:
		return vm.handleI8x16Shuffle(instruction)
	case I8x16Swizzle:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16Swizzle)
	case I8x16Splat:
		return handleUnary(vm, vm.stack.PopInt32, SimdI8x16Splat)
	case I16x8Splat:
		return handleUnary(vm, vm.stack.PopInt32, SimdI16x8Splat)
	case I32x4Splat:
		return handleUnary(vm, vm.stack.PopInt32, SimdI32x4Splat)
	case I64x2Splat:
		return handleUnary(vm, vm.stack.PopInt64, SimdI64x2Splat)
	case F32x4Splat:
		return handleUnary(vm, vm.stack.PopFloat32, SimdF32x4Splat)
	case F64x2Splat:
		return handleUnary(vm, vm.stack.PopFloat64, SimdF64x2Splat)
	case I8x16ExtractLaneS:
		return handleSimdExtractLane(vm, instruction, SimdI8x16ExtractLaneS)
	case I8x16ExtractLaneU:
		return handleSimdExtractLane(vm, instruction, SimdI8x16ExtractLaneU)
	case I8x16ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI8x16ReplaceLane)
	case I16x8ExtractLaneS:
		return handleSimdExtractLane(vm, instruction, SimdI16x8ExtractLaneS)
	case I16x8ExtractLaneU:
		return handleSimdExtractLane(vm, instruction, SimdI16x8ExtractLaneU)
	case I16x8ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI16x8ReplaceLane)
	case I32x4ExtractLane:
		return handleSimdExtractLane(vm, instruction, SimdI32x4ExtractLane)
	case I32x4ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopInt32, SimdI32x4ReplaceLane)
	case I64x2ExtractLane:
		return handleSimdExtractLane(vm, instruction, SimdI64x2ExtractLane)
	case I64x2ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopInt64, SimdI64x2ReplaceLane)
	case F32x4ExtractLane:
		return handleSimdExtractLane(vm, instruction, SimdF32x4ExtractLane)
	case F32x4ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopFloat32, SimdF32x4ReplaceLane)
	case F64x2ExtractLane:
		return handleSimdExtractLane(vm, instruction, SimdF64x2ExtractLane)
	case F64x2ReplaceLane:
		return handleSimdReplaceLane(vm, instruction, vm.stack.PopFloat64, SimdF64x2ReplaceLane)
	case I8x16Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16Eq)
	case I8x16Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16Ne)
	case I8x16LtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16LtS)
	case I8x16LtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16LtU)
	case I8x16GtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16GtS)
	case I8x16GtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16GtU)
	case I8x16LeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16LeS)
	case I8x16LeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16LeU)
	case I8x16GeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16GeS)
	case I8x16GeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16GeU)
	case I16x8Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Eq)
	case I16x8Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Ne)
	case I16x8LtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8LtS)
	case I16x8LtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8LtU)
	case I16x8GtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8GtS)
	case I16x8GtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8GtU)
	case I16x8LeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8LeS)
	case I16x8LeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8LeU)
	case I16x8GeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8GeS)
	case I16x8GeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8GeU)
	case I32x4Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4Eq)
	case I32x4Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4Ne)
	case I32x4LtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4LtS)
	case I32x4LtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4LtU)
	case I32x4GtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4GtS)
	case I32x4GtU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4GtU)
	case I32x4LeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4LeS)
	case I32x4LeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4LeU)
	case I32x4GeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4GeS)
	case I32x4GeU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4GeU)
	case F32x4Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Eq)
	case F32x4Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Ne)
	case F32x4Lt:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Lt)
	case F32x4Gt:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Gt)
	case F32x4Le:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Le)
	case F32x4Ge:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Ge)
	case F64x2Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Eq)
	case F64x2Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Ne)
	case F64x2Lt:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Lt)
	case F64x2Gt:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Gt)
	case F64x2Le:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Le)
	case F64x2Ge:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Ge)
	case V128Not:
		return handleUnary(vm, vm.stack.PopV128, SimdV128Not)
	case V128And:
		return handleBinary(vm, vm.stack.PopV128, SimdV128And)
	case V128Andnot:
		return handleBinary(vm, vm.stack.PopV128, SimdV128Andnot)
	case V128Or:
		return handleBinary(vm, vm.stack.PopV128, SimdV128Or)
	case V128Xor:
		return handleBinary(vm, vm.stack.PopV128, SimdV128Xor)
	case V128Bitselect:
		return vm.handleSimdTernary(SimdV128Bitselect)
	case V128AnyTrue:
		return handleUnaryBool(vm, vm.stack.PopV128, SimdV128AnyTrue)
	case V128Load8Lane:
		return vm.handleSimdLoadLane(instruction, 8)
	case V128Load16Lane:
		return vm.handleSimdLoadLane(instruction, 16)
	case V128Load32Lane:
		return vm.handleSimdLoadLane(instruction, 32)
	case V128Load64Lane:
		return vm.handleSimdLoadLane(instruction, 64)
	case V128Store8Lane:
		return vm.handleSimdStoreLane(instruction, 8)
	case V128Store16Lane:
		return vm.handleSimdStoreLane(instruction, 16)
	case V128Store32Lane:
		return vm.handleSimdStoreLane(instruction, 32)
	case V128Store64Lane:
		return vm.handleSimdStoreLane(instruction, 64)
	case V128Load32Zero:
		return handleLoad(vm, instruction, SimdV128Load32Zero, 4)
	case V128Load64Zero:
		return handleLoad(vm, instruction, SimdV128Load64Zero, 8)
	case F32x4DemoteF64x2Zero:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4DemoteF64x2Zero)
	case F64x2PromoteLowF32x4:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2PromoteLowF32x4)
	case I8x16Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdI8x16Abs)
	case I8x16Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdI8x16Neg)
	case I8x16Popcnt:
		return handleUnary(vm, vm.stack.PopV128, SimdI8x16Popcnt)
	case I8x16AllTrue:
		return handleUnaryBool(vm, vm.stack.PopV128, SimdI8x16AllTrue)
	case I8x16Bitmask:
		return handleUnary(vm, vm.stack.PopV128, SimdI8x16Bitmask)
	case I8x16NarrowI16x8S:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16NarrowI16x8S)
	case I8x16NarrowI16x8U:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16NarrowI16x8U)
	case F32x4Ceil:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Ceil)
	case F32x4Floor:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Floor)
	case F32x4Trunc:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Trunc)
	case F32x4Nearest:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Nearest)
	case I8x16Shl:
		return vm.handleSimdShift(SimdI8x16Shl)
	case I8x16ShrU:
		return vm.handleSimdShift(SimdI8x16ShrU)
	case I8x16ShrS:
		return vm.handleSimdShift(SimdI8x16ShrS)
	case I8x16Add:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16Add)
	case I8x16AddSatS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16AddSatS)
	case I8x16AddSatU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16AddSatU)
	case I8x16Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16Sub)
	case I8x16SubSatS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16SubSatS)
	case I8x16SubSatU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16SubSatU)
	case F64x2Ceil:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Ceil)
	case F64x2Floor:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Floor)
	case I8x16MinS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16MinS)
	case I8x16MinU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16MinU)
	case I8x16MaxS:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16MaxS)
	case I8x16MaxU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16MaxU)
	case F64x2Trunc:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Trunc)
	case I8x16AvgrU:
		return handleBinary(vm, vm.stack.PopV128, SimdI8x16AvgrU)
	case I16x8ExtaddPairwiseI8x16S:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtaddPairwiseI8x16S)
	case I16x8ExtaddPairwiseI8x16U:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtaddPairwiseI8x16U)
	case I32x4ExtaddPairwiseI16x8S:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtaddPairwiseI16x8S)
	case I32x4ExtaddPairwiseI16x8U:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtaddPairwiseI16x8U)
	case I16x8Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8Abs)
	case I16x8Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8Neg)
	case I16x8Q15mulrSatS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Q15mulrSatS)
	case I16x8AllTrue:
		return handleUnaryBool(vm, vm.stack.PopV128, SimdI16x8AllTrue)
	case I16x8Bitmask:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8Bitmask)
	case I16x8NarrowI32x4S:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8NarrowI32x4S)
	case I16x8NarrowI32x4U:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8NarrowI32x4U)
	case I16x8ExtendLowI8x16S:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendLowI8x16S)
	case I16x8ExtendHighI8x16S:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendHighI8x16S)
	case I16x8ExtendLowI8x16U:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendLowI8x16U)
	case I16x8ExtendHighI8x16U:
		return handleUnary(vm, vm.stack.PopV128, SimdI16x8ExtendHighI8x16U)
	case I16x8Shl:
		return vm.handleSimdShift(SimdI16x8Shl)
	case I16x8ShrS:
		return vm.handleSimdShift(SimdI16x8ShrS)
	case I16x8ShrU:
		return vm.handleSimdShift(SimdI16x8ShrU)
	case I16x8Add:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Add)
	case I16x8AddSatS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8AddSatS)
	case I16x8AddSatU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8AddSatU)
	case I16x8Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Sub)
	case I16x8SubSatS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8SubSatS)
	case I16x8SubSatU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8SubSatU)
	case F64x2Nearest:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Nearest)
	case I16x8Mul:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8Mul)
	case I16x8MinS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8MinS)
	case I16x8MinU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8MinU)
	case I16x8MaxS:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8MaxS)
	case I16x8MaxU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8MaxU)
	case I16x8AvgrU:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8AvgrU)
	case I16x8ExtmulLowI8x16S:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulLowI8x16S)
	case I16x8ExtmulHighI8x16S:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulHighI8x16S)
	case I16x8ExtmulLowI8x16U:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulLowI8x16U)
	case I16x8ExtmulHighI8x16U:
		return handleBinary(vm, vm.stack.PopV128, SimdI16x8ExtmulHighI8x16U)
	case I32x4Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4Abs)
	case I32x4Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4Neg)
	case I32x4AllTrue:
		return handleUnaryBool(vm, vm.stack.PopV128, SimdI32x4AllTrue)
	case I32x4Bitmask:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4Bitmask)
	case I32x4ExtendLowI16x8S:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendLowI16x8S)
	case I32x4ExtendHighI16x8S:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendHighI16x8S)
	case I32x4ExtendLowI16x8U:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendLowI16x8U)
	case I32x4ExtendHighI16x8U:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4ExtendHighI16x8U)
	case I32x4Shl:
		return vm.handleSimdShift(SimdI32x4Shl)
	case I32x4ShrS:
		return vm.handleSimdShift(SimdI32x4ShrS)
	case I32x4ShrU:
		return vm.handleSimdShift(SimdI32x4ShrU)
	case I32x4Add:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4Add)
	case I32x4Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4Sub)
	case I32x4Mul:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4Mul)
	case I32x4MinS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4MinS)
	case I32x4MinU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4MinU)
	case I32x4MaxS:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4MaxS)
	case I32x4MaxU:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4MaxU)
	case I32x4DotI16x8S:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4DotI16x8S)
	case I32x4ExtmulLowI16x8S:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulLowI16x8S)
	case I32x4ExtmulHighI16x8S:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulHighI16x8S)
	case I32x4ExtmulLowI16x8U:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulLowI16x8U)
	case I32x4ExtmulHighI16x8U:
		return handleBinary(vm, vm.stack.PopV128, SimdI32x4ExtmulHighI16x8U)
	case I64x2Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2Abs)
	case I64x2Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2Neg)
	case I64x2AllTrue:
		return handleUnaryBool(vm, vm.stack.PopV128, SimdI64x2AllTrue)
	case I64x2Bitmask:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2Bitmask)
	case I64x2ExtendLowI32x4S:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendLowI32x4S)
	case I64x2ExtendHighI32x4S:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendHighI32x4S)
	case I64x2ExtendLowI32x4U:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendLowI32x4U)
	case I64x2ExtendHighI32x4U:
		return handleUnary(vm, vm.stack.PopV128, SimdI64x2ExtendHighI32x4U)
	case I64x2Shl:
		return vm.handleSimdShift(SimdI64x2Shl)
	case I64x2ShrS:
		return vm.handleSimdShift(SimdI64x2ShrS)
	case I64x2ShrU:
		return vm.handleSimdShift(SimdI64x2ShrU)
	case I64x2Add:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2Add)
	case I64x2Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2Sub)
	case I64x2Mul:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2Mul)
	case I64x2Eq:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2Eq)
	case I64x2Ne:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2Ne)
	case I64x2LtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2LtS)
	case I64x2GtS:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2GtS)
	case I64x2LeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2LeS)
	case I64x2GeS:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2GeS)
	case I64x2ExtmulLowI32x4S:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulLowI32x4S)
	case I64x2ExtmulHighI32x4S:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulHighI32x4S)
	case I64x2ExtmulLowI32x4U:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulLowI32x4U)
	case I64x2ExtmulHighI32x4U:
		return handleBinary(vm, vm.stack.PopV128, SimdI64x2ExtmulHighI32x4U)
	case F32x4Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Abs)
	case F32x4Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Neg)
	case F32x4Sqrt:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4Sqrt)
	case F32x4Add:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Add)
	case F32x4Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Sub)
	case F32x4Mul:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Mul)
	case F32x4Div:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Div)
	case F32x4Min:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Min)
	case F32x4Max:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Max)
	case F32x4Pmin:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Pmin)
	case F32x4Pmax:
		return handleBinary(vm, vm.stack.PopV128, SimdF32x4Pmax)
	case F64x2Abs:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Abs)
	case F64x2Neg:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Neg)
	case F64x2Sqrt:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2Sqrt)
	case F64x2Add:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Add)
	case F64x2Sub:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Sub)
	case F64x2Mul:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Mul)
	case F64x2Div:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Div)
	case F64x2Min:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Min)
	case F64x2Max:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Max)
	case F64x2Pmin:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Pmin)
	case F64x2Pmax:
		return handleBinary(vm, vm.stack.PopV128, SimdF64x2Pmax)
	case I32x4TruncSatF32x4S:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF32x4S)
	case I32x4TruncSatF32x4U:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF32x4U)
	case F32x4ConvertI32x4S:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4ConvertI32x4S)
	case F32x4ConvertI32x4U:
		return handleUnary(vm, vm.stack.PopV128, SimdF32x4ConvertI32x4U)
	case I32x4TruncSatF64x2SZero:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF64x2SZero)
	case I32x4TruncSatF64x2UZero:
		return handleUnary(vm, vm.stack.PopV128, SimdI32x4TruncSatF64x2UZero)
	case F64x2ConvertLowI32x4S:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2ConvertLowI32x4S)
	case F64x2ConvertLowI32x4U:
		return handleUnary(vm, vm.stack.PopV128, SimdF64x2ConvertLowI32x4U)
	default:
		return fmt.Errorf("unknown opcode %d", instruction.Opcode)
	}
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

	condition, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

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

func (vm *VM) handleElse() error {
	callFrame := vm.currentCallFrame()
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame, err := vm.popControlFrame()
	if err != nil {
		return err
	}

	callFrame.Decoder.Pc = ifFrame.ContinuationPc
	return nil
}

func (vm *VM) handleEnd() error {
	frame, err := vm.popControlFrame()
	if err != nil {
		return err
	}
	return vm.unwindStack(frame.StackHeight, frame.OutputCount)
}

func (vm *VM) handleBr(instruction Instruction) error {
	labelIndex := uint32(instruction.Immediates[0])
	return vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrIf(instruction Instruction) error {
	labelIndex := uint32(instruction.Immediates[0])
	val, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	if val == 0 {
		return nil
	}
	return vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrTable(instruction Instruction) error {
	immediates := instruction.Immediates
	table := immediates[:len(immediates)-1]
	defaultTarget := uint32(immediates[len(immediates)-1])
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	if index >= 0 && int(index) < len(table) {
		return vm.brToLabel(uint32(table[index]))
	} else {
		return vm.brToLabel(defaultTarget)
	}
}

func (vm *VM) brToLabel(labelIndex uint32) error {
	callFrame := vm.currentCallFrame()

	var targetFrame *ControlFrame
	var err error
	for range int(labelIndex) + 1 {
		if targetFrame, err = vm.popControlFrame(); err != nil {
			return err
		}
	}

	var arity uint
	if targetFrame.Opcode == Loop {
		arity = targetFrame.InputCount
	} else {
		arity = targetFrame.OutputCount
	}

	if err := vm.unwindStack(targetFrame.StackHeight, arity); err != nil {
		return err
	}

	if targetFrame.Opcode == Loop {
		vm.pushControlFrame(targetFrame)
	}

	callFrame.Decoder.Pc = targetFrame.ContinuationPc
	return nil
}

func (vm *VM) handleCall(instruction Instruction) error {
	localIndex := uint32(instruction.Immediates[0])
	function := vm.getFunction(localIndex)
	res, err := vm.invoke(function)
	if err != nil {
		return err
	}
	return vm.stack.PushAll(res)
}

func (vm *VM) handleCallIndirect(instruction Instruction) error {
	typeIndex := uint32(instruction.Immediates[0])
	tableIndex := uint32(instruction.Immediates[1])

	expectedType := vm.currentModuleInstance().Types[typeIndex]
	table := vm.getTable(tableIndex)

	elementIndex, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

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
	return vm.stack.PushAll(res)
}

func (vm *VM) handleSelect() error {
	condition, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	val2, err := vm.stack.Pop()
	if err != nil {
		return err
	}
	val1, err := vm.stack.Pop()
	if err != nil {
		return err
	}
	if condition != 0 {
		return vm.stack.Push(val1)
	}
	return vm.stack.Push(val2)
}

func (vm *VM) handleLocalGet(instruction Instruction) error {
	callFrame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	return vm.stack.Push(callFrame.Locals[localIndex])
}

func (vm *VM) handleLocalSet(instruction Instruction) error {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	valueType := getLocalValueType(frame.Function, localIndex)
	val, err := vm.stack.PopValueType(valueType)
	if err != nil {
		return err
	}

	frame.Locals[localIndex] = val
	return nil
}

func (vm *VM) handleLocalTee(instruction Instruction) error {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	valueType := getLocalValueType(frame.Function, localIndex)
	val, err := vm.stack.PopValueType(valueType)
	if err != nil {
		return err
	}

	frame.Locals[localIndex] = val
	return vm.stack.Push(val)
}

func (vm *VM) handleGlobalGet(instruction Instruction) error {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	return vm.stack.Push(global.Value)
}

func (vm *VM) handleGlobalSet(instruction Instruction) error {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	if !global.Mutable {
		return fmt.Errorf("global at index %d is not mutable", localIndex)
	}

	val, err := vm.stack.Pop()
	if err != nil {
		return err
	}

	global.Value = val
	return nil
}

func (vm *VM) handleTableGet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

	element, err := table.Get(index)
	if err != nil {
		return err
	}
	return vm.stack.Push(element)
}

func (vm *VM) handleTableSet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	reference, err := vm.stack.Pop()
	if err != nil {
		return err
	}

	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

	return table.Set(index, reference)
}

func (vm *VM) handleMemorySize(instruction Instruction) error {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	return vm.stack.Push(memory.Size())
}

func (vm *VM) handleMemoryGrow(instruction Instruction) error {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	pages, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	oldSize := memory.Grow(pages)
	return vm.stack.Push(oldSize)
}

func handleConst[T WasmNumber](
	vm *VM,
	instruction Instruction,
	getConst func(uint64) T,
) error {
	return vm.stack.Push(getConst(instruction.Immediates[0]))
}

func (vm *VM) handleRefFunc(instruction Instruction) error {
	funcIndex := uint32(instruction.Immediates[0])
	storeIndex := vm.currentModuleInstance().FuncAddrs[funcIndex]
	return vm.stack.Push(int32(storeIndex))
}

func (vm *VM) handleRefIsNull() error {
	top, err := vm.stack.Pop()
	if err != nil {
		return err
	}
	_, topIsNull := top.(Null)
	return vm.stack.Push(BoolToInt32(topIsNull))
}

func (vm *VM) handleMemoryInit(instruction Instruction) error {
	data := vm.getData(uint32(instruction.Immediates[0]))
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d, err := vm.stack.Pop3Int32()
	if err != nil {
		return err
	}
	return memory.Init(uint32(n), uint32(s), uint32(d), data.Content)
}

func (vm *VM) handleDataDrop(instruction Instruction) error {
	dataSegment := vm.getData(uint32(instruction.Immediates[0]))
	dataSegment.Content = nil
	return nil
}

func (vm *VM) handleMemoryCopy(instruction Instruction) error {
	destMemory := vm.getMemory(uint32(instruction.Immediates[0]))
	srcMemory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d, err := vm.stack.Pop3Int32()
	if err != nil {
		return err
	}
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *VM) handleMemoryFill(instruction Instruction) error {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	n, val, offset, err := vm.stack.Pop3Int32()
	if err != nil {
		return err
	}
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *VM) handleTableInit(instruction Instruction) error {
	element := vm.getElement(uint32(instruction.Immediates[0]))
	table := vm.getTable(uint32(instruction.Immediates[1]))
	n, s, d, err := vm.stack.Pop3Int32()
	if err != nil {
		return err
	}

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
		storeIndexes, err := toStoreFuncIndexes(moduleInstance, element.FuncIndexes)
		if err != nil {
			return err
		}
		return table.Init(n, d, s, storeIndexes)
	default:
		return ErrTableOutOfBounds
	}
}

func (vm *VM) handleElemDrop(instruction Instruction) error {
	element := vm.getElement(uint32(instruction.Immediates[0]))
	if element.Mode == DeclarativeElementMode {
		return nil
	}

	element.FuncIndexes = nil
	element.FuncIndexesExpressions = nil
	return nil
}

func (vm *VM) handleTableCopy(instruction Instruction) error {
	destTable := vm.getTable(uint32(instruction.Immediates[0]))
	srcTable := vm.getTable(uint32(instruction.Immediates[1]))
	n, s, d, err := vm.stack.Pop3Int32()
	if err != nil {
		return err
	}
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *VM) handleTableGrow(instruction Instruction) error {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	val, err := vm.stack.Pop()
	if err != nil {
		return err
	}
	return vm.stack.Push(table.Grow(n, val))
}

func (vm *VM) handleTableSize(instruction Instruction) error {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	return vm.stack.Push(table.Size())
}

func (vm *VM) handleTableFill(instruction Instruction) error {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	val, err := vm.stack.Pop()
	if err != nil {
		return err
	}
	i, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	return table.Fill(n, i, val)
}

func (vm *VM) handleI8x16Shuffle(instruction Instruction) error {
	v2, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	v1, err := vm.stack.PopV128()
	if err != nil {
		return err
	}

	var lanes [16]byte
	for i, imm := range instruction.Immediates {
		lanes[i] = byte(imm)
	}

	return vm.stack.Push(SimdI8x16Shuffle(v1, v2, lanes))
}

func handleBinary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() (T, error),
	op func(a, b T) R,
) error {
	b, err := pop()
	if err != nil {
		return err
	}
	a, err := pop()
	if err != nil {
		return err
	}
	return vm.stack.Push(op(a, b))
}

func handleBinarySafe[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() (T, error),
	op func(a, b T) (R, error),
) error {
	b, err := pop()
	if err != nil {
		return err
	}
	a, err := pop()
	if err != nil {
		return err
	}
	result, err := op(a, b)
	if err != nil {
		return err
	}
	return vm.stack.Push(result)
}

func handleBinaryBool[T WasmNumber](
	vm *VM,
	pop func() (T, error),
	op func(a, b T) bool,
) error {
	b, err := pop()
	if err != nil {
		return err
	}
	a, err := pop()
	if err != nil {
		return err
	}
	return vm.stack.Push(BoolToInt32(op(a, b)))
}

func handleUnary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() (T, error),
	op func(a T) R,
) error {
	a, err := pop()
	if err != nil {
		return err
	}
	return vm.stack.Push(op(a))
}

func handleUnarySafe[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() (T, error),
	op func(a T) (R, error),
) error {
	a, err := pop()
	if err != nil {
		return err
	}
	result, err := op(a)
	if err != nil {
		return err
	}
	return vm.stack.Push(result)
}

func handleUnaryBool[T WasmNumber | V128Value](
	vm *VM,
	pop func() (T, error),
	op func(a T) bool,
) error {
	a, err := pop()
	if err != nil {
		return err
	}
	return vm.stack.Push(BoolToInt32(op(a)))
}

func (vm *VM) handleSimdShift(
	op func(v V128Value, shift int32) V128Value,
) error {
	shift, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
	v, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	return vm.stack.Push(op(v, shift))
}

func (vm *VM) handleSimdTernary(op func(v1, v2, v3 V128Value) V128Value) error {
	v3, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	v2, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	v1, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	result := op(v1, v2, v3)
	return vm.stack.Push(result)
}

func handleSimdExtractLane[R WasmNumber](
	vm *VM,
	instruction Instruction,
	op func(v V128Value, laneIndex uint32) (R, error),
) error {
	laneIndex := uint32(instruction.Immediates[0])
	v, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	res, err := op(v, laneIndex)
	if err != nil {
		return err
	}
	return vm.stack.Push(res)
}

func handleStore[T WasmNumber | V128Value](
	vm *VM,
	instruction Instruction,
	pop func() (T, error), toBytes func(T) []byte,
	sizeBytes uint32,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	value, err := pop()
	if err != nil {
		return err
	}
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}
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
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

	data, err := memory.Get(offset, uint32(index), sizeBytes)
	if err != nil {
		return err
	}
	return vm.stack.Push(fromBytes(data))
}

func (vm *VM) handleSimdConst(instruction Instruction) error {
	v := V128Value{
		Low:  instruction.Immediates[0],
		High: instruction.Immediates[1],
	}
	return vm.stack.Push(v)
}

func (vm *VM) handleSimdLoadLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return nil
	}

	res, err := SetLane(v, laneIndex, laneValue)
	if err != nil {
		return err
	}
	return vm.stack.Push(res)
}

func (vm *VM) handleSimdStoreLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	index, err := vm.stack.PopInt32()
	if err != nil {
		return err
	}

	laneData, err := ExtractLane(v, laneSize, laneIndex)
	if err != nil {
		return err
	}
	return memory.Set(offset, uint32(index), laneData)
}

func handleSimdReplaceLane[T WasmNumber](
	vm *VM,
	instruction Instruction,
	pop func() (T, error),
	replaceLane func(V128Value, uint32, T) (V128Value, error),
) error {
	laneIndex := uint32(instruction.Immediates[0])
	laneValue, err := pop()
	if err != nil {
		return err
	}
	vector, err := vm.stack.PopV128()
	if err != nil {
		return err
	}
	result, err := replaceLane(vector, laneIndex, laneValue)
	if err != nil {
		return err
	}
	return vm.stack.Push(result)
}

func (vm *VM) unwindStack(targetHeight, arity uint) error {
	valuesToPreserve := vm.stack.PeekN(arity)
	vm.stack.Resize(targetHeight)
	return vm.stack.PushAll(valuesToPreserve)
}

func (vm *VM) getBlockInputOutputCount(blockType int32) (uint, uint) {
	if blockType == -0x40 { // empty block type.
		return 0, 0
	}

	if blockType > 0 { // type index.
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

func (vm *VM) popControlFrame() (*ControlFrame, error) {
	callFrame := vm.currentCallFrame()
	if len(callFrame.ControlStack) == 0 {
		return nil, errors.New("control stack is empty")
	}
	index := len(callFrame.ControlStack) - 1
	frame := callFrame.ControlStack[index]
	callFrame.ControlStack = callFrame.ControlStack[:index]
	return frame, nil
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
		if storeTableIndex >= uint32(len(vm.store.tables)) {
			return fmt.Errorf("unknown table")
		}

		table := vm.store.tables[storeTableIndex]
		if offset > int32(table.Size()) {
			return ErrTableOutOfBounds
		}

		if len(element.FuncIndexes) > 0 {
			indexes, err := toStoreFuncIndexes(moduleInstance, element.FuncIndexes)
			if err != nil {
				return err
			}
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

func resolveImports(module *Module, imports map[string]map[string]any) (
	[]FunctionInstance,
	[]*Table,
	[]*Memory,
	[]*Global,
	error) {
	functions := []FunctionInstance{}
	tables := []*Table{}
	memories := []*Memory{}
	globals := []*Global{}
	for _, imp := range module.Imports {
		importedModule, ok := imports[imp.ModuleName]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("missing module %s", imp.ModuleName)
		}

		importedObj, ok := importedModule[imp.Name]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf(
				"%s not in module %s", imp.Name, imp.ModuleName,
			)
		}

		switch t := imp.Type.(type) {
		case FunctionTypeIndex:
			switch f := importedObj.(type) {
			case func(...any) []any:
				hostFunction := &HostFunc{Type: module.Types[t], HostCode: f}
				functions = append(functions, hostFunction)
			case FunctionInstance:
				if !f.GetType().Equal(&module.Types[t]) {
					return nil, nil, nil, nil, fmt.Errorf(
						"type mismatch for import %s.%s", imp.ModuleName, imp.Name,
					)
				}
				functions = append(functions, f)
			default:
				return nil, nil, nil, nil, fmt.Errorf(
					"%s.%s not a function", imp.ModuleName, imp.Name,
				)
			}
		case GlobalType:
			switch v := importedObj.(type) {
			case int, int32, int64, float32, float64:
				global := &Global{Value: v, Mutable: t.IsMutable}
				globals = append(globals, global)
			case *Global:
				globals = append(globals, v)
			default:
				return nil, nil, nil, nil, fmt.Errorf(
					"%s.%s not a valid global", imp.ModuleName, imp.Name,
				)
			}
		case MemoryType:
			memory, ok := importedObj.(*Memory)
			if !ok {
				return nil, nil, nil, nil, fmt.Errorf(
					"%s.%s not a memory", imp.ModuleName, imp.Name,
				)
			}
			memories = append(memories, memory)
		case TableType:
			table, ok := importedObj.(*Table)
			if !ok {
				return nil, nil, nil, nil, fmt.Errorf(
					"%s.%s not a table", imp.ModuleName, imp.Name,
				)
			}
			tables = append(tables, table)
		}
	}
	return functions, tables, memories, globals, nil
}

func (vm *VM) resolveExports(
	module *Module,
	instance *ModuleInstance,
) ([]ExportInstance, error) {
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
		default:
			return nil, fmt.Errorf("unknown export index type %d", export.IndexType)
		}
		exports = append(exports, ExportInstance{Name: export.Name, Value: value})
	}
	return exports, nil
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

	args, err := vm.stack.PopValueTypes(fun.GetType().ParamTypes)
	if err != nil {
		return nil, err
	}
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
) ([]int32, error) {
	storeIndices := make([]int32, len(localIndexes))
	for i, localIndex := range localIndexes {
		if localIndex >= int32(len(moduleInstance.FuncAddrs)) {
			return nil, fmt.Errorf("invalid function index: %d", localIndex)
		}
		storeIndices[i] = int32(moduleInstance.FuncAddrs[localIndex])
	}
	return storeIndices, nil
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
