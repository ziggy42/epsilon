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
	errUnreachable        = errors.New("unreachable")
	errCallStackExhausted = errors.New("call stack exhausted")
	// Special error to signal a return instruction was hit.
	errReturn = errors.New("return instruction")
)

const maxCallStackDepth = 1000

type callFrame struct {
	decoder      *decoder
	controlStack []*controlFrame
	locals       []any
	function     *WasmFunction
}

// controlFrame represents a block of code that can be branched to.
type controlFrame struct {
	opcode         Opcode // The opcode that created this control frame.
	continuationPc uint   // The address to jump to when `br` targets this frame.
	inputCount     uint   // Count of inputs this control instruction consumes.
	outputCount    uint   // Count of outputs this control instruction produces.
	stackHeight    uint
}

// VM is the WebAssembly Virtual Machine.
type VM struct {
	store          *Store
	stack          *valueStack
	callStack      []*callFrame
	callStackDepth int
	features       ExperimentalFeatures
}

func NewVM() *VM {
	return &VM{store: NewStore(), stack: newValueStack()}
}

func (vm *VM) Instantiate(
	module *Module,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	validator := newValidator(vm.features)
	if err := validator.validateModule(module); err != nil {
		return nil, err
	}
	moduleInstance := &ModuleInstance{
		Types: module.Types,
		vm:    vm,
	}

	resolvedImports, err := resolveImports(module, imports)
	if err != nil {
		return nil, err
	}

	for _, functionInstance := range resolvedImports.functions {
		storeIndex := uint32(len(vm.store.funcs))
		moduleInstance.FuncAddrs = append(moduleInstance.FuncAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, functionInstance)
	}

	for _, function := range module.Funcs {
		storeIndex := uint32(len(vm.store.funcs))
		funType := module.Types[function.TypeIndex]
		wasmFunc := NewWasmFunction(funType, moduleInstance, function)
		moduleInstance.FuncAddrs = append(moduleInstance.FuncAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, wasmFunc)
	}

	for _, table := range resolvedImports.tables {
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

	for _, memory := range resolvedImports.memories {
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

	for _, global := range resolvedImports.globals {
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
	export, err := getExport(module, name, GlobalExportKind)
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
	export, err := getExport(module, name, FunctionExportKind)
	if err != nil {
		return nil, err
	}

	vm.stack.pushAll(args)
	return vm.invoke(export.(FunctionInstance))
}

func (vm *VM) invoke(function FunctionInstance) ([]any, error) {
	switch f := function.(type) {
	case *WasmFunction:
		return vm.invokeWasmFunction(f)
	case *HostFunction:
		return vm.invokeHostFunction(f)
	default:
		return nil, fmt.Errorf("unknown function type")
	}
}

func (vm *VM) invokeWasmFunction(function *WasmFunction) ([]any, error) {
	if vm.callStackDepth >= maxCallStackDepth {
		return nil, errCallStackExhausted
	}
	vm.callStackDepth++
	defer func() { vm.callStackDepth-- }()

	locals := vm.stack.popN(len(function.Type.ParamTypes))
	for _, local := range function.Code.Locals {
		locals = append(locals, DefaultValueForType(local))
	}

	callFrame := &callFrame{
		decoder: newDecoder(function.Code.Body),
		controlStack: []*controlFrame{{
			opcode:         Block,
			continuationPc: uint(len(function.Code.Body)),
			inputCount:     uint(len(function.Type.ParamTypes)),
			outputCount:    uint(len(function.Type.ResultTypes)),
			stackHeight:    vm.stack.size(),
		}},
		locals:   locals,
		function: function,
	}
	vm.callStack = append(vm.callStack, callFrame)

	for callFrame.decoder.hasMore() {
		instruction, err := callFrame.decoder.decode()
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
	values := vm.stack.popN(len(callFrame.function.Type.ResultTypes))
	return values, nil
}

func (vm *VM) handleInstruction(instruction Instruction) error {
	var err error
	// Using a switch instead of a map of Opcode -> Handler is significantly
	// faster.
	switch instruction.Opcode {
	case Unreachable:
		err = errUnreachable
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
		err = vm.handleCallIndirect(instruction)
	case Drop:
		vm.stack.drop()
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
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, uint32ToInt32)
	case I64Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint64, uint64ToInt64)
	case F32Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, math.Float32frombits)
	case F64Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint64, math.Float64frombits)
	case I32Load8S:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, signExtend8To32)
	case I32Load8U:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, zeroExtend8To32)
	case I32Load16S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, signExtend16To32)
	case I32Load16U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, zeroExtend16To32)
	case I64Load8S:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, signExtend8To64)
	case I64Load8U:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, zeroExtend8To64)
	case I64Load16S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, signExtend16To64)
	case I64Load16U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, zeroExtend16To64)
	case I64Load32S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, signExtend32To64)
	case I64Load32U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, zeroExtend32To64)
	case I32Store:
		err = handleStore(vm, instruction, uint32(vm.stack.popInt32()), (*Memory).StoreUint32)
	case I64Store:
		err = handleStore(vm, instruction, uint64(vm.stack.popInt64()), (*Memory).StoreUint64)
	case F32Store:
		err = handleStore(vm, instruction, math.Float32bits(vm.stack.popFloat32()), (*Memory).StoreUint32)
	case F64Store:
		err = handleStore(vm, instruction, math.Float64bits(vm.stack.popFloat64()), (*Memory).StoreUint64)
	case I32Store8:
		err = handleStore(vm, instruction, byte(vm.stack.popInt32()), (*Memory).StoreByte)
	case I32Store16:
		err = handleStore(vm, instruction, uint16(vm.stack.popInt32()), (*Memory).StoreUint16)
	case I64Store8:
		err = handleStore(vm, instruction, byte(vm.stack.popInt64()), (*Memory).StoreByte)
	case I64Store16:
		err = handleStore(vm, instruction, uint16(vm.stack.popInt64()), (*Memory).StoreUint16)
	case I64Store32:
		err = handleStore(vm, instruction, uint32(vm.stack.popInt64()), (*Memory).StoreUint32)
	case MemorySize:
		vm.handleMemorySize(instruction)
	case MemoryGrow:
		vm.handleMemoryGrow(instruction)
	case I32Const:
		vm.stack.push(int32(instruction.Immediates[0]))
	case I64Const:
		vm.stack.push(int64(instruction.Immediates[0]))
	case F32Const:
		vm.stack.push(math.Float32frombits(uint32(instruction.Immediates[0])))
	case F64Const:
		vm.stack.push(math.Float64frombits(instruction.Immediates[0]))
	case I32Eqz:
		handleUnaryBool(vm, vm.stack.popInt32, equalZero)
	case I32Eq:
		handleBinaryBool(vm, vm.stack.popInt32, equal)
	case I32Ne:
		handleBinaryBool(vm, vm.stack.popInt32, notEqual)
	case I32LtS:
		handleBinaryBool(vm, vm.stack.popInt32, lessThan)
	case I32LtU:
		handleBinaryBool(vm, vm.stack.popInt32, lessThanU32)
	case I32GtS:
		handleBinaryBool(vm, vm.stack.popInt32, greaterThan)
	case I32GtU:
		handleBinaryBool(vm, vm.stack.popInt32, greaterThanU32)
	case I32LeS:
		handleBinaryBool(vm, vm.stack.popInt32, lessOrEqual)
	case I32LeU:
		handleBinaryBool(vm, vm.stack.popInt32, lessOrEqualU32)
	case I32GeS:
		handleBinaryBool(vm, vm.stack.popInt32, greaterOrEqual)
	case I32GeU:
		handleBinaryBool(vm, vm.stack.popInt32, greaterOrEqualU32)
	case I64Eqz:
		handleUnaryBool(vm, vm.stack.popInt64, equalZero)
	case I64Eq:
		handleBinaryBool(vm, vm.stack.popInt64, equal)
	case I64Ne:
		handleBinaryBool(vm, vm.stack.popInt64, notEqual)
	case I64LtS:
		handleBinaryBool(vm, vm.stack.popInt64, lessThan)
	case I64LtU:
		handleBinaryBool(vm, vm.stack.popInt64, lessThanU64)
	case I64GtS:
		handleBinaryBool(vm, vm.stack.popInt64, greaterThan)
	case I64GtU:
		handleBinaryBool(vm, vm.stack.popInt64, greaterThanU64)
	case I64LeS:
		handleBinaryBool(vm, vm.stack.popInt64, lessOrEqual)
	case I64LeU:
		handleBinaryBool(vm, vm.stack.popInt64, lessOrEqualU64)
	case I64GeS:
		handleBinaryBool(vm, vm.stack.popInt64, greaterOrEqual)
	case I64GeU:
		handleBinaryBool(vm, vm.stack.popInt64, greaterOrEqualU64)
	case F32Eq:
		handleBinaryBool(vm, vm.stack.popFloat32, equal)
	case F32Ne:
		handleBinaryBool(vm, vm.stack.popFloat32, notEqual)
	case F32Lt:
		handleBinaryBool(vm, vm.stack.popFloat32, lessThan)
	case F32Gt:
		handleBinaryBool(vm, vm.stack.popFloat32, greaterThan)
	case F32Le:
		handleBinaryBool(vm, vm.stack.popFloat32, lessOrEqual)
	case F32Ge:
		handleBinaryBool(vm, vm.stack.popFloat32, greaterOrEqual)
	case F64Eq:
		handleBinaryBool(vm, vm.stack.popFloat64, equal)
	case F64Ne:
		handleBinaryBool(vm, vm.stack.popFloat64, notEqual)
	case F64Lt:
		handleBinaryBool(vm, vm.stack.popFloat64, lessThan)
	case F64Gt:
		handleBinaryBool(vm, vm.stack.popFloat64, greaterThan)
	case F64Le:
		handleBinaryBool(vm, vm.stack.popFloat64, lessOrEqual)
	case F64Ge:
		handleBinaryBool(vm, vm.stack.popFloat64, greaterOrEqual)
	case I32Clz:
		handleUnary(vm, vm.stack.popInt32, clz32)
	case I32Ctz:
		handleUnary(vm, vm.stack.popInt32, ctz32)
	case I32Popcnt:
		handleUnary(vm, vm.stack.popInt32, popcnt32)
	case I32Add:
		handleBinary(vm, vm.stack.popInt32, add)
	case I32Sub:
		handleBinary(vm, vm.stack.popInt32, sub)
	case I32Mul:
		handleBinary(vm, vm.stack.popInt32, mul)
	case I32DivS:
		err = handleBinarySafe(vm, vm.stack.popInt32, divS32)
	case I32DivU:
		err = handleBinarySafe(vm, vm.stack.popInt32, divU32)
	case I32RemS:
		err = handleBinarySafe(vm, vm.stack.popInt32, remS32)
	case I32RemU:
		err = handleBinarySafe(vm, vm.stack.popInt32, remU32)
	case I32And:
		handleBinary(vm, vm.stack.popInt32, and)
	case I32Or:
		handleBinary(vm, vm.stack.popInt32, or)
	case I32Xor:
		handleBinary(vm, vm.stack.popInt32, xor)
	case I32Shl:
		handleBinary(vm, vm.stack.popInt32, shl32)
	case I32ShrS:
		handleBinary(vm, vm.stack.popInt32, shrS32)
	case I32ShrU:
		handleBinary(vm, vm.stack.popInt32, shrU32)
	case I32Rotl:
		handleBinary(vm, vm.stack.popInt32, rotl32)
	case I32Rotr:
		handleBinary(vm, vm.stack.popInt32, rotr32)
	case I64Clz:
		handleUnary(vm, vm.stack.popInt64, clz64)
	case I64Ctz:
		handleUnary(vm, vm.stack.popInt64, ctz64)
	case I64Popcnt:
		handleUnary(vm, vm.stack.popInt64, popcnt64)
	case I64Add:
		handleBinary(vm, vm.stack.popInt64, add)
	case I64Sub:
		handleBinary(vm, vm.stack.popInt64, sub)
	case I64Mul:
		handleBinary(vm, vm.stack.popInt64, mul)
	case I64DivS:
		err = handleBinarySafe(vm, vm.stack.popInt64, divS64)
	case I64DivU:
		err = handleBinarySafe(vm, vm.stack.popInt64, divU64)
	case I64RemS:
		err = handleBinarySafe(vm, vm.stack.popInt64, remS64)
	case I64RemU:
		err = handleBinarySafe(vm, vm.stack.popInt64, remU64)
	case I64And:
		handleBinary(vm, vm.stack.popInt64, and)
	case I64Or:
		handleBinary(vm, vm.stack.popInt64, or)
	case I64Xor:
		handleBinary(vm, vm.stack.popInt64, xor)
	case I64Shl:
		handleBinary(vm, vm.stack.popInt64, shl64)
	case I64ShrS:
		handleBinary(vm, vm.stack.popInt64, shrS64)
	case I64ShrU:
		handleBinary(vm, vm.stack.popInt64, shrU64)
	case I64Rotl:
		handleBinary(vm, vm.stack.popInt64, rotl64)
	case I64Rotr:
		handleBinary(vm, vm.stack.popInt64, rotr64)
	case F32Abs:
		handleUnary(vm, vm.stack.popFloat32, abs[float32])
	case F32Neg:
		handleUnary(vm, vm.stack.popFloat32, neg[float32])
	case F32Ceil:
		handleUnary(vm, vm.stack.popFloat32, ceil[float32])
	case F32Floor:
		handleUnary(vm, vm.stack.popFloat32, floor[float32])
	case F32Trunc:
		handleUnary(vm, vm.stack.popFloat32, trunc[float32])
	case F32Nearest:
		handleUnary(vm, vm.stack.popFloat32, nearest[float32])
	case F32Sqrt:
		handleUnary(vm, vm.stack.popFloat32, sqrt[float32])
	case F32Add:
		handleBinary(vm, vm.stack.popFloat32, add[float32])
	case F32Sub:
		handleBinary(vm, vm.stack.popFloat32, sub[float32])
	case F32Mul:
		handleBinary(vm, vm.stack.popFloat32, mul[float32])
	case F32Div:
		handleBinary(vm, vm.stack.popFloat32, div[float32])
	case F32Min:
		handleBinary(vm, vm.stack.popFloat32, wasmMin[float32])
	case F32Max:
		handleBinary(vm, vm.stack.popFloat32, wasmMax[float32])
	case F32Copysign:
		handleBinary(vm, vm.stack.popFloat32, copysign[float32])
	case F64Abs:
		handleUnary(vm, vm.stack.popFloat64, abs[float64])
	case F64Neg:
		handleUnary(vm, vm.stack.popFloat64, neg[float64])
	case F64Ceil:
		handleUnary(vm, vm.stack.popFloat64, ceil[float64])
	case F64Floor:
		handleUnary(vm, vm.stack.popFloat64, floor[float64])
	case F64Trunc:
		handleUnary(vm, vm.stack.popFloat64, trunc[float64])
	case F64Nearest:
		handleUnary(vm, vm.stack.popFloat64, nearest[float64])
	case F64Sqrt:
		handleUnary(vm, vm.stack.popFloat64, sqrt[float64])
	case F64Add:
		handleBinary(vm, vm.stack.popFloat64, add[float64])
	case F64Sub:
		handleBinary(vm, vm.stack.popFloat64, sub[float64])
	case F64Mul:
		handleBinary(vm, vm.stack.popFloat64, mul[float64])
	case F64Div:
		handleBinary(vm, vm.stack.popFloat64, div[float64])
	case F64Min:
		handleBinary(vm, vm.stack.popFloat64, wasmMin[float64])
	case F64Max:
		handleBinary(vm, vm.stack.popFloat64, wasmMax[float64])
	case F64Copysign:
		handleBinary(vm, vm.stack.popFloat64, copysign[float64])
	case I32WrapI64:
		handleUnary(vm, vm.stack.popInt64, wrapI64ToI32)
	case I32TruncF32S:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32SToI32)
	case I32TruncF32U:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32UToI32)
	case I32TruncF64S:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64SToI32)
	case I32TruncF64U:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64UToI32)
	case I64ExtendI32S:
		handleUnary(vm, vm.stack.popInt32, extendI32SToI64)
	case I64ExtendI32U:
		handleUnary(vm, vm.stack.popInt32, extendI32UToI64)
	case I64TruncF32S:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32SToI64)
	case I64TruncF32U:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32UToI64)
	case I64TruncF64S:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64SToI64)
	case I64TruncF64U:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64UToI64)
	case F32ConvertI32S:
		handleUnary(vm, vm.stack.popInt32, convertI32SToF32)
	case F32ConvertI32U:
		handleUnary(vm, vm.stack.popInt32, convertI32UToF32)
	case F32ConvertI64S:
		handleUnary(vm, vm.stack.popInt64, convertI64SToF32)
	case F32ConvertI64U:
		handleUnary(vm, vm.stack.popInt64, convertI64UToF32)
	case F32DemoteF64:
		handleUnary(vm, vm.stack.popFloat64, demoteF64ToF32)
	case F64ConvertI32S:
		handleUnary(vm, vm.stack.popInt32, convertI32SToF64)
	case F64ConvertI32U:
		handleUnary(vm, vm.stack.popInt32, convertI32UToF64)
	case F64ConvertI64S:
		handleUnary(vm, vm.stack.popInt64, convertI64SToF64)
	case F64ConvertI64U:
		handleUnary(vm, vm.stack.popInt64, convertI64UToF64)
	case F64PromoteF32:
		handleUnary(vm, vm.stack.popFloat32, promoteF32ToF64)
	case I32ReinterpretF32:
		handleUnary(vm, vm.stack.popFloat32, reinterpretF32ToI32)
	case I64ReinterpretF64:
		handleUnary(vm, vm.stack.popFloat64, reinterpretF64ToI64)
	case F32ReinterpretI32:
		handleUnary(vm, vm.stack.popInt32, reinterpretI32ToF32)
	case F64ReinterpretI64:
		handleUnary(vm, vm.stack.popInt64, reinterpretI64ToF64)
	case I32Extend8S:
		handleUnary(vm, vm.stack.popInt32, extend8STo32)
	case I32Extend16S:
		handleUnary(vm, vm.stack.popInt32, extend16STo32)
	case I64Extend8S:
		handleUnary(vm, vm.stack.popInt64, extend8STo64)
	case I64Extend16S:
		handleUnary(vm, vm.stack.popInt64, extend16STo64)
	case I64Extend32S:
		handleUnary(vm, vm.stack.popInt64, extend32STo64)
	case RefNull:
		vm.stack.push(NullVal)
	case RefIsNull:
		vm.handleRefIsNull()
	case RefFunc:
		vm.handleRefFunc(instruction)
	case I32TruncSatF32S:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32SToI32)
	case I32TruncSatF32U:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32UToI32)
	case I32TruncSatF64S:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64SToI32)
	case I32TruncSatF64U:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64UToI32)
	case I64TruncSatF32S:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32SToI64)
	case I64TruncSatF32U:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32UToI64)
	case I64TruncSatF64S:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64SToI64)
	case I64TruncSatF64U:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64UToI64)
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
		err = handleLoad(vm, instruction, (*Memory).LoadV128, identityV128)
	case V128Load8x8S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load8x8S, 8)
	case V128Load8x8U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load8x8U, 8)
	case V128Load16x4S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load16x4S, 8)
	case V128Load16x4U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load16x4U, 8)
	case V128Load32x2S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32x2S, 8)
	case V128Load32x2U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32x2U, 8)
	case V128Load8Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI8x16SplatFromBytes, 1)
	case V128Load16Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI16x8SplatFromBytes, 2)
	case V128Load32Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI32x4SplatFromBytes, 4)
	case V128Load64Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI64x2SplatFromBytes, 8)
	case V128Store:
		err = handleStore(vm, instruction, vm.stack.popV128(), (*Memory).StoreV128)
	case V128Const:
		vm.handleSimdConst(instruction)
	case I8x16Shuffle:
		vm.handleI8x16Shuffle(instruction)
	case I8x16Swizzle:
		handleBinary(vm, vm.stack.popV128, simdI8x16Swizzle)
	case I8x16Splat:
		handleUnary(vm, vm.stack.popInt32, simdI8x16Splat)
	case I16x8Splat:
		handleUnary(vm, vm.stack.popInt32, simdI16x8Splat)
	case I32x4Splat:
		handleUnary(vm, vm.stack.popInt32, simdI32x4Splat)
	case I64x2Splat:
		handleUnary(vm, vm.stack.popInt64, simdI64x2Splat)
	case F32x4Splat:
		handleUnary(vm, vm.stack.popFloat32, simdF32x4Splat)
	case F64x2Splat:
		handleUnary(vm, vm.stack.popFloat64, simdF64x2Splat)
	case I8x16ExtractLaneS:
		handleSimdExtractLane(vm, instruction, simdI8x16ExtractLaneS)
	case I8x16ExtractLaneU:
		handleSimdExtractLane(vm, instruction, simdI8x16ExtractLaneU)
	case I8x16ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI8x16ReplaceLane)
	case I16x8ExtractLaneS:
		handleSimdExtractLane(vm, instruction, simdI16x8ExtractLaneS)
	case I16x8ExtractLaneU:
		handleSimdExtractLane(vm, instruction, simdI16x8ExtractLaneU)
	case I16x8ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI16x8ReplaceLane)
	case I32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, simdI32x4ExtractLane)
	case I32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI32x4ReplaceLane)
	case I64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, simdI64x2ExtractLane)
	case I64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt64, simdI64x2ReplaceLane)
	case F32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, simdF32x4ExtractLane)
	case F32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popFloat32, simdF32x4ReplaceLane)
	case F64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, simdF64x2ExtractLane)
	case F64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popFloat64, simdF64x2ReplaceLane)
	case I8x16Eq:
		handleBinary(vm, vm.stack.popV128, simdI8x16Eq)
	case I8x16Ne:
		handleBinary(vm, vm.stack.popV128, simdI8x16Ne)
	case I8x16LtS:
		handleBinary(vm, vm.stack.popV128, simdI8x16LtS)
	case I8x16LtU:
		handleBinary(vm, vm.stack.popV128, simdI8x16LtU)
	case I8x16GtS:
		handleBinary(vm, vm.stack.popV128, simdI8x16GtS)
	case I8x16GtU:
		handleBinary(vm, vm.stack.popV128, simdI8x16GtU)
	case I8x16LeS:
		handleBinary(vm, vm.stack.popV128, simdI8x16LeS)
	case I8x16LeU:
		handleBinary(vm, vm.stack.popV128, simdI8x16LeU)
	case I8x16GeS:
		handleBinary(vm, vm.stack.popV128, simdI8x16GeS)
	case I8x16GeU:
		handleBinary(vm, vm.stack.popV128, simdI8x16GeU)
	case I16x8Eq:
		handleBinary(vm, vm.stack.popV128, simdI16x8Eq)
	case I16x8Ne:
		handleBinary(vm, vm.stack.popV128, simdI16x8Ne)
	case I16x8LtS:
		handleBinary(vm, vm.stack.popV128, simdI16x8LtS)
	case I16x8LtU:
		handleBinary(vm, vm.stack.popV128, simdI16x8LtU)
	case I16x8GtS:
		handleBinary(vm, vm.stack.popV128, simdI16x8GtS)
	case I16x8GtU:
		handleBinary(vm, vm.stack.popV128, simdI16x8GtU)
	case I16x8LeS:
		handleBinary(vm, vm.stack.popV128, simdI16x8LeS)
	case I16x8LeU:
		handleBinary(vm, vm.stack.popV128, simdI16x8LeU)
	case I16x8GeS:
		handleBinary(vm, vm.stack.popV128, simdI16x8GeS)
	case I16x8GeU:
		handleBinary(vm, vm.stack.popV128, simdI16x8GeU)
	case I32x4Eq:
		handleBinary(vm, vm.stack.popV128, simdI32x4Eq)
	case I32x4Ne:
		handleBinary(vm, vm.stack.popV128, simdI32x4Ne)
	case I32x4LtS:
		handleBinary(vm, vm.stack.popV128, simdI32x4LtS)
	case I32x4LtU:
		handleBinary(vm, vm.stack.popV128, simdI32x4LtU)
	case I32x4GtS:
		handleBinary(vm, vm.stack.popV128, simdI32x4GtS)
	case I32x4GtU:
		handleBinary(vm, vm.stack.popV128, simdI32x4GtU)
	case I32x4LeS:
		handleBinary(vm, vm.stack.popV128, simdI32x4LeS)
	case I32x4LeU:
		handleBinary(vm, vm.stack.popV128, simdI32x4LeU)
	case I32x4GeS:
		handleBinary(vm, vm.stack.popV128, simdI32x4GeS)
	case I32x4GeU:
		handleBinary(vm, vm.stack.popV128, simdI32x4GeU)
	case F32x4Eq:
		handleBinary(vm, vm.stack.popV128, simdF32x4Eq)
	case F32x4Ne:
		handleBinary(vm, vm.stack.popV128, simdF32x4Ne)
	case F32x4Lt:
		handleBinary(vm, vm.stack.popV128, simdF32x4Lt)
	case F32x4Gt:
		handleBinary(vm, vm.stack.popV128, simdF32x4Gt)
	case F32x4Le:
		handleBinary(vm, vm.stack.popV128, simdF32x4Le)
	case F32x4Ge:
		handleBinary(vm, vm.stack.popV128, simdF32x4Ge)
	case F64x2Eq:
		handleBinary(vm, vm.stack.popV128, simdF64x2Eq)
	case F64x2Ne:
		handleBinary(vm, vm.stack.popV128, simdF64x2Ne)
	case F64x2Lt:
		handleBinary(vm, vm.stack.popV128, simdF64x2Lt)
	case F64x2Gt:
		handleBinary(vm, vm.stack.popV128, simdF64x2Gt)
	case F64x2Le:
		handleBinary(vm, vm.stack.popV128, simdF64x2Le)
	case F64x2Ge:
		handleBinary(vm, vm.stack.popV128, simdF64x2Ge)
	case V128Not:
		handleUnary(vm, vm.stack.popV128, simdV128Not)
	case V128And:
		handleBinary(vm, vm.stack.popV128, simdV128And)
	case V128Andnot:
		handleBinary(vm, vm.stack.popV128, simdV128Andnot)
	case V128Or:
		handleBinary(vm, vm.stack.popV128, simdV128Or)
	case V128Xor:
		handleBinary(vm, vm.stack.popV128, simdV128Xor)
	case V128Bitselect:
		vm.handleSimdTernary(simdV128Bitselect)
	case V128AnyTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdV128AnyTrue)
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
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32Zero, 4)
	case V128Load64Zero:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load64Zero, 8)
	case F32x4DemoteF64x2Zero:
		handleUnary(vm, vm.stack.popV128, simdF32x4DemoteF64x2Zero)
	case F64x2PromoteLowF32x4:
		handleUnary(vm, vm.stack.popV128, simdF64x2PromoteLowF32x4)
	case I8x16Abs:
		handleUnary(vm, vm.stack.popV128, simdI8x16Abs)
	case I8x16Neg:
		handleUnary(vm, vm.stack.popV128, simdI8x16Neg)
	case I8x16Popcnt:
		handleUnary(vm, vm.stack.popV128, simdI8x16Popcnt)
	case I8x16AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI8x16AllTrue)
	case I8x16Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI8x16Bitmask)
	case I8x16NarrowI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI8x16NarrowI16x8S)
	case I8x16NarrowI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI8x16NarrowI16x8U)
	case F32x4Ceil:
		handleUnary(vm, vm.stack.popV128, simdF32x4Ceil)
	case F32x4Floor:
		handleUnary(vm, vm.stack.popV128, simdF32x4Floor)
	case F32x4Trunc:
		handleUnary(vm, vm.stack.popV128, simdF32x4Trunc)
	case F32x4Nearest:
		handleUnary(vm, vm.stack.popV128, simdF32x4Nearest)
	case I8x16Shl:
		vm.handleSimdShift(simdI8x16Shl)
	case I8x16ShrU:
		vm.handleSimdShift(simdI8x16ShrU)
	case I8x16ShrS:
		vm.handleSimdShift(simdI8x16ShrS)
	case I8x16Add:
		handleBinary(vm, vm.stack.popV128, simdI8x16Add)
	case I8x16AddSatS:
		handleBinary(vm, vm.stack.popV128, simdI8x16AddSatS)
	case I8x16AddSatU:
		handleBinary(vm, vm.stack.popV128, simdI8x16AddSatU)
	case I8x16Sub:
		handleBinary(vm, vm.stack.popV128, simdI8x16Sub)
	case I8x16SubSatS:
		handleBinary(vm, vm.stack.popV128, simdI8x16SubSatS)
	case I8x16SubSatU:
		handleBinary(vm, vm.stack.popV128, simdI8x16SubSatU)
	case F64x2Ceil:
		handleUnary(vm, vm.stack.popV128, simdF64x2Ceil)
	case F64x2Floor:
		handleUnary(vm, vm.stack.popV128, simdF64x2Floor)
	case I8x16MinS:
		handleBinary(vm, vm.stack.popV128, simdI8x16MinS)
	case I8x16MinU:
		handleBinary(vm, vm.stack.popV128, simdI8x16MinU)
	case I8x16MaxS:
		handleBinary(vm, vm.stack.popV128, simdI8x16MaxS)
	case I8x16MaxU:
		handleBinary(vm, vm.stack.popV128, simdI8x16MaxU)
	case F64x2Trunc:
		handleUnary(vm, vm.stack.popV128, simdF64x2Trunc)
	case I8x16AvgrU:
		handleBinary(vm, vm.stack.popV128, simdI8x16AvgrU)
	case I16x8ExtaddPairwiseI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtaddPairwiseI8x16S)
	case I16x8ExtaddPairwiseI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtaddPairwiseI8x16U)
	case I32x4ExtaddPairwiseI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtaddPairwiseI16x8S)
	case I32x4ExtaddPairwiseI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtaddPairwiseI16x8U)
	case I16x8Abs:
		handleUnary(vm, vm.stack.popV128, simdI16x8Abs)
	case I16x8Neg:
		handleUnary(vm, vm.stack.popV128, simdI16x8Neg)
	case I16x8Q15mulrSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8Q15mulrSatS)
	case I16x8AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI16x8AllTrue)
	case I16x8Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI16x8Bitmask)
	case I16x8NarrowI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI16x8NarrowI32x4S)
	case I16x8NarrowI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI16x8NarrowI32x4U)
	case I16x8ExtendLowI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendLowI8x16S)
	case I16x8ExtendHighI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendHighI8x16S)
	case I16x8ExtendLowI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendLowI8x16U)
	case I16x8ExtendHighI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendHighI8x16U)
	case I16x8Shl:
		vm.handleSimdShift(simdI16x8Shl)
	case I16x8ShrS:
		vm.handleSimdShift(simdI16x8ShrS)
	case I16x8ShrU:
		vm.handleSimdShift(simdI16x8ShrU)
	case I16x8Add:
		handleBinary(vm, vm.stack.popV128, simdI16x8Add)
	case I16x8AddSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8AddSatS)
	case I16x8AddSatU:
		handleBinary(vm, vm.stack.popV128, simdI16x8AddSatU)
	case I16x8Sub:
		handleBinary(vm, vm.stack.popV128, simdI16x8Sub)
	case I16x8SubSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8SubSatS)
	case I16x8SubSatU:
		handleBinary(vm, vm.stack.popV128, simdI16x8SubSatU)
	case F64x2Nearest:
		handleUnary(vm, vm.stack.popV128, simdF64x2Nearest)
	case I16x8Mul:
		handleBinary(vm, vm.stack.popV128, simdI16x8Mul)
	case I16x8MinS:
		handleBinary(vm, vm.stack.popV128, simdI16x8MinS)
	case I16x8MinU:
		handleBinary(vm, vm.stack.popV128, simdI16x8MinU)
	case I16x8MaxS:
		handleBinary(vm, vm.stack.popV128, simdI16x8MaxS)
	case I16x8MaxU:
		handleBinary(vm, vm.stack.popV128, simdI16x8MaxU)
	case I16x8AvgrU:
		handleBinary(vm, vm.stack.popV128, simdI16x8AvgrU)
	case I16x8ExtmulLowI8x16S:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulLowI8x16S)
	case I16x8ExtmulHighI8x16S:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulHighI8x16S)
	case I16x8ExtmulLowI8x16U:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulLowI8x16U)
	case I16x8ExtmulHighI8x16U:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulHighI8x16U)
	case I32x4Abs:
		handleUnary(vm, vm.stack.popV128, simdI32x4Abs)
	case I32x4Neg:
		handleUnary(vm, vm.stack.popV128, simdI32x4Neg)
	case I32x4AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI32x4AllTrue)
	case I32x4Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI32x4Bitmask)
	case I32x4ExtendLowI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendLowI16x8S)
	case I32x4ExtendHighI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendHighI16x8S)
	case I32x4ExtendLowI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendLowI16x8U)
	case I32x4ExtendHighI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendHighI16x8U)
	case I32x4Shl:
		vm.handleSimdShift(simdI32x4Shl)
	case I32x4ShrS:
		vm.handleSimdShift(simdI32x4ShrS)
	case I32x4ShrU:
		vm.handleSimdShift(simdI32x4ShrU)
	case I32x4Add:
		handleBinary(vm, vm.stack.popV128, simdI32x4Add)
	case I32x4Sub:
		handleBinary(vm, vm.stack.popV128, simdI32x4Sub)
	case I32x4Mul:
		handleBinary(vm, vm.stack.popV128, simdI32x4Mul)
	case I32x4MinS:
		handleBinary(vm, vm.stack.popV128, simdI32x4MinS)
	case I32x4MinU:
		handleBinary(vm, vm.stack.popV128, simdI32x4MinU)
	case I32x4MaxS:
		handleBinary(vm, vm.stack.popV128, simdI32x4MaxS)
	case I32x4MaxU:
		handleBinary(vm, vm.stack.popV128, simdI32x4MaxU)
	case I32x4DotI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4DotI16x8S)
	case I32x4ExtmulLowI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulLowI16x8S)
	case I32x4ExtmulHighI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulHighI16x8S)
	case I32x4ExtmulLowI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulLowI16x8U)
	case I32x4ExtmulHighI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulHighI16x8U)
	case I64x2Abs:
		handleUnary(vm, vm.stack.popV128, simdI64x2Abs)
	case I64x2Neg:
		handleUnary(vm, vm.stack.popV128, simdI64x2Neg)
	case I64x2AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI64x2AllTrue)
	case I64x2Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI64x2Bitmask)
	case I64x2ExtendLowI32x4S:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendLowI32x4S)
	case I64x2ExtendHighI32x4S:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendHighI32x4S)
	case I64x2ExtendLowI32x4U:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendLowI32x4U)
	case I64x2ExtendHighI32x4U:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendHighI32x4U)
	case I64x2Shl:
		vm.handleSimdShift(simdI64x2Shl)
	case I64x2ShrS:
		vm.handleSimdShift(simdI64x2ShrS)
	case I64x2ShrU:
		vm.handleSimdShift(simdI64x2ShrU)
	case I64x2Add:
		handleBinary(vm, vm.stack.popV128, simdI64x2Add)
	case I64x2Sub:
		handleBinary(vm, vm.stack.popV128, simdI64x2Sub)
	case I64x2Mul:
		handleBinary(vm, vm.stack.popV128, simdI64x2Mul)
	case I64x2Eq:
		handleBinary(vm, vm.stack.popV128, simdI64x2Eq)
	case I64x2Ne:
		handleBinary(vm, vm.stack.popV128, simdI64x2Ne)
	case I64x2LtS:
		handleBinary(vm, vm.stack.popV128, simdI64x2LtS)
	case I64x2GtS:
		handleBinary(vm, vm.stack.popV128, simdI64x2GtS)
	case I64x2LeS:
		handleBinary(vm, vm.stack.popV128, simdI64x2LeS)
	case I64x2GeS:
		handleBinary(vm, vm.stack.popV128, simdI64x2GeS)
	case I64x2ExtmulLowI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulLowI32x4S)
	case I64x2ExtmulHighI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulHighI32x4S)
	case I64x2ExtmulLowI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulLowI32x4U)
	case I64x2ExtmulHighI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulHighI32x4U)
	case F32x4Abs:
		handleUnary(vm, vm.stack.popV128, simdF32x4Abs)
	case F32x4Neg:
		handleUnary(vm, vm.stack.popV128, simdF32x4Neg)
	case F32x4Sqrt:
		handleUnary(vm, vm.stack.popV128, simdF32x4Sqrt)
	case F32x4Add:
		handleBinary(vm, vm.stack.popV128, simdF32x4Add)
	case F32x4Sub:
		handleBinary(vm, vm.stack.popV128, simdF32x4Sub)
	case F32x4Mul:
		handleBinary(vm, vm.stack.popV128, simdF32x4Mul)
	case F32x4Div:
		handleBinary(vm, vm.stack.popV128, simdF32x4Div)
	case F32x4Min:
		handleBinary(vm, vm.stack.popV128, simdF32x4Min)
	case F32x4Max:
		handleBinary(vm, vm.stack.popV128, simdF32x4Max)
	case F32x4Pmin:
		handleBinary(vm, vm.stack.popV128, simdF32x4Pmin)
	case F32x4Pmax:
		handleBinary(vm, vm.stack.popV128, simdF32x4Pmax)
	case F64x2Abs:
		handleUnary(vm, vm.stack.popV128, simdF64x2Abs)
	case F64x2Neg:
		handleUnary(vm, vm.stack.popV128, simdF64x2Neg)
	case F64x2Sqrt:
		handleUnary(vm, vm.stack.popV128, simdF64x2Sqrt)
	case F64x2Add:
		handleBinary(vm, vm.stack.popV128, simdF64x2Add)
	case F64x2Sub:
		handleBinary(vm, vm.stack.popV128, simdF64x2Sub)
	case F64x2Mul:
		handleBinary(vm, vm.stack.popV128, simdF64x2Mul)
	case F64x2Div:
		handleBinary(vm, vm.stack.popV128, simdF64x2Div)
	case F64x2Min:
		handleBinary(vm, vm.stack.popV128, simdF64x2Min)
	case F64x2Max:
		handleBinary(vm, vm.stack.popV128, simdF64x2Max)
	case F64x2Pmin:
		handleBinary(vm, vm.stack.popV128, simdF64x2Pmin)
	case F64x2Pmax:
		handleBinary(vm, vm.stack.popV128, simdF64x2Pmax)
	case I32x4TruncSatF32x4S:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF32x4S)
	case I32x4TruncSatF32x4U:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF32x4U)
	case F32x4ConvertI32x4S:
		handleUnary(vm, vm.stack.popV128, simdF32x4ConvertI32x4S)
	case F32x4ConvertI32x4U:
		handleUnary(vm, vm.stack.popV128, simdF32x4ConvertI32x4U)
	case I32x4TruncSatF64x2SZero:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF64x2SZero)
	case I32x4TruncSatF64x2UZero:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF64x2UZero)
	case F64x2ConvertLowI32x4S:
		handleUnary(vm, vm.stack.popV128, simdF64x2ConvertLowI32x4S)
	case F64x2ConvertLowI32x4U:
		handleUnary(vm, vm.stack.popV128, simdF64x2ConvertLowI32x4U)
	default:
		err = fmt.Errorf("unknown opcode %d", instruction.Opcode)
	}
	return err
}

func (vm *VM) currentCallFrame() *callFrame {
	return vm.callStack[len(vm.callStack)-1]
}

func (vm *VM) currentModuleInstance() *ModuleInstance {
	return vm.currentCallFrame().function.Module
}

func (vm *VM) pushBlockFrame(opcode Opcode, blockType int32) error {
	callFrame := vm.currentCallFrame()
	originalPc := callFrame.decoder.pc
	inputCount, outputCount := vm.getBlockInputOutputCount(blockType)
	frame := &controlFrame{
		opcode:      opcode,
		inputCount:  inputCount,
		outputCount: outputCount,
		stackHeight: vm.stack.size(),
	}

	// For loops, the continuation is a branch back to the start of the block.
	if opcode == Loop {
		frame.continuationPc = originalPc
	} else {
		if cachedPc, ok := callFrame.function.JumpCache[originalPc]; ok {
			frame.continuationPc = cachedPc
		} else {
			// Cache miss: we need to scan forward to find the matching 'end'.
			if err := callFrame.decoder.decodeUntilMatchingEnd(); err != nil {
				return err
			}

			callFrame.function.JumpCache[originalPc] = callFrame.decoder.pc
			frame.continuationPc = callFrame.decoder.pc
			callFrame.decoder.pc = originalPc
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
	originalPc := frame.decoder.pc

	condition := vm.stack.popInt32()

	if err := vm.pushBlockFrame(If, blockType); err != nil {
		return err
	}

	if condition != 0 {
		return nil
	}

	// We need to jump to the 'else' or 'end'.
	if elsePc, ok := frame.function.JumpElseCache[originalPc]; ok {
		frame.decoder.pc = elsePc
		return nil
	}

	// Cache miss, we need to find the matching Else or End.
	matchingOpcode, err := frame.decoder.decodeUntilMatchingElseOrEnd()
	if err != nil {
		return err
	}

	if matchingOpcode == Else {
		// We need to consume the Else instruction and jump to the next "actual"
		// instruction after it.
		if _, err := frame.decoder.decode(); err != nil {
			return err
		}
	}

	frame.function.JumpElseCache[originalPc] = frame.decoder.pc
	return nil
}

func (vm *VM) handleElse() {
	callFrame := vm.currentCallFrame()
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame := vm.popControlFrame()
	callFrame.decoder.pc = ifFrame.continuationPc
}

func (vm *VM) handleEnd() {
	frame := vm.popControlFrame()
	vm.stack.unwind(frame.stackHeight, frame.outputCount)
}

func (vm *VM) handleBr(instruction Instruction) {
	labelIndex := uint32(instruction.Immediates[0])
	vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrIf(instruction Instruction) {
	labelIndex := uint32(instruction.Immediates[0])
	val := vm.stack.popInt32()
	if val == 0 {
		return
	}
	vm.brToLabel(labelIndex)
}

func (vm *VM) handleBrTable(instruction Instruction) {
	immediates := instruction.Immediates
	table := immediates[:len(immediates)-1]
	defaultTarget := uint32(immediates[len(immediates)-1])
	index := vm.stack.popInt32()
	if index >= 0 && int(index) < len(table) {
		vm.brToLabel(uint32(table[index]))
	} else {
		vm.brToLabel(defaultTarget)
	}
}

func (vm *VM) brToLabel(labelIndex uint32) {
	callFrame := vm.currentCallFrame()

	var targetFrame *controlFrame
	for range int(labelIndex) + 1 {
		targetFrame = vm.popControlFrame()
	}

	var arity uint
	if targetFrame.opcode == Loop {
		arity = targetFrame.inputCount
	} else {
		arity = targetFrame.outputCount
	}

	vm.stack.unwind(targetFrame.stackHeight, arity)
	if targetFrame.opcode == Loop {
		vm.pushControlFrame(targetFrame)
	}

	callFrame.decoder.pc = targetFrame.continuationPc
}

func (vm *VM) handleCall(instruction Instruction) error {
	localIndex := uint32(instruction.Immediates[0])
	function := vm.getFunction(localIndex)
	res, err := vm.invoke(function)
	if err != nil {
		return err
	}
	vm.stack.pushAll(res)
	return nil
}

func (vm *VM) handleCallIndirect(instruction Instruction) error {
	typeIndex := uint32(instruction.Immediates[0])
	tableIndex := uint32(instruction.Immediates[1])

	expectedType := vm.currentModuleInstance().Types[typeIndex]
	table := vm.getTable(tableIndex)

	elementIndex := vm.stack.popInt32()

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
	vm.stack.pushAll(res)
	return nil
}

func (vm *VM) handleSelect() {
	condition := vm.stack.popInt32()
	val2 := vm.stack.pop()
	val1 := vm.stack.pop()
	if condition != 0 {
		vm.stack.push(val1)
	} else {
		vm.stack.push(val2)
	}
}

func (vm *VM) handleLocalGet(instruction Instruction) {
	callFrame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	vm.stack.push(callFrame.locals[localIndex])
}

func (vm *VM) handleLocalSet(instruction Instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	// We know, due to validation, the top of the stack is always the right type.
	frame.locals[localIndex] = vm.stack.pop()
}

func (vm *VM) handleLocalTee(instruction Instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.Immediates[0])
	// We know, due to validation, the top of the stack is always the right type.
	val := vm.stack.pop()
	frame.locals[localIndex] = val
	vm.stack.push(val)
}

func (vm *VM) handleGlobalGet(instruction Instruction) {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	vm.stack.push(global.Value)
}

func (vm *VM) handleGlobalSet(instruction Instruction) {
	localIndex := uint32(instruction.Immediates[0])
	global := vm.getGlobal(localIndex)
	global.Value = vm.stack.pop()
}

func (vm *VM) handleTableGet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	index := vm.stack.popInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}
	vm.stack.push(element)
	return nil
}

func (vm *VM) handleTableSet(instruction Instruction) error {
	tableIndex := uint32(instruction.Immediates[0])
	table := vm.getTable(tableIndex)
	reference := vm.stack.pop()
	index := vm.stack.popInt32()
	return table.Set(index, reference)
}

func (vm *VM) handleMemorySize(instruction Instruction) {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	vm.stack.push(memory.Size())
}

func (vm *VM) handleMemoryGrow(instruction Instruction) {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	pages := vm.stack.popInt32()
	oldSize := memory.Grow(pages)
	vm.stack.push(oldSize)
}

func (vm *VM) handleRefFunc(instruction Instruction) {
	funcIndex := uint32(instruction.Immediates[0])
	storeIndex := vm.currentModuleInstance().FuncAddrs[funcIndex]
	vm.stack.push(int32(storeIndex))
}

func (vm *VM) handleRefIsNull() {
	top := vm.stack.pop()
	_, topIsNull := top.(Null)
	vm.stack.push(boolToInt32(topIsNull))
}

func (vm *VM) handleMemoryInit(instruction Instruction) error {
	data := vm.getData(uint32(instruction.Immediates[0]))
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.Content)
}

func (vm *VM) handleDataDrop(instruction Instruction) {
	dataSegment := vm.getData(uint32(instruction.Immediates[0]))
	dataSegment.Content = nil
}

func (vm *VM) handleMemoryCopy(instruction Instruction) error {
	destMemory := vm.getMemory(uint32(instruction.Immediates[0]))
	srcMemory := vm.getMemory(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *VM) handleMemoryFill(instruction Instruction) error {
	memory := vm.getMemory(uint32(instruction.Immediates[0]))
	n, val, offset := vm.stack.pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *VM) handleTableInit(instruction Instruction) error {
	element := vm.getElement(uint32(instruction.Immediates[0]))
	table := vm.getTable(uint32(instruction.Immediates[1]))
	n, s, d := vm.stack.pop3Int32()

	switch element.Mode {
	case ActiveElementMode:
		// Trap if using an active, non-dropped element segment.
		// A dropped segment has its FuncIndexes slice set to nil.
		if element.FuncIndexes != nil {
			return errTableOutOfBounds
		}
		return table.Init(n, d, s, element.FuncIndexes)
	case PassiveElementMode:
		moduleInstance := vm.currentModuleInstance()
		storeIndexes := toStoreFuncIndexes(moduleInstance, element.FuncIndexes)
		return table.Init(n, d, s, storeIndexes)
	default:
		return errTableOutOfBounds
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
	n, s, d := vm.stack.pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *VM) handleTableGrow(instruction Instruction) {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n := vm.stack.popInt32()
	val := vm.stack.pop()
	vm.stack.push(table.Grow(n, val))
}

func (vm *VM) handleTableSize(instruction Instruction) {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	vm.stack.push(table.Size())
}

func (vm *VM) handleTableFill(instruction Instruction) error {
	table := vm.getTable(uint32(instruction.Immediates[0]))
	n := vm.stack.popInt32()
	val := vm.stack.pop()
	i := vm.stack.popInt32()
	return table.Fill(n, i, val)
}

func (vm *VM) handleI8x16Shuffle(instruction Instruction) {
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()

	var lanes [16]byte
	for i, imm := range instruction.Immediates {
		lanes[i] = byte(imm)
	}

	vm.stack.push(simdI8x16Shuffle(v1, v2, lanes))
}

func handleBinary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a, b T) R,
) {
	b := pop()
	a := pop()
	vm.stack.push(op(a, b))
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
	vm.stack.push(result)
	return nil
}

func handleBinaryBool[T WasmNumber](
	vm *VM,
	pop func() T,
	op func(a, b T) bool,
) {
	b := pop()
	a := pop()
	vm.stack.push(boolToInt32(op(a, b)))
}

func handleUnary[T WasmNumber | V128Value, R WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a T) R,
) {
	vm.stack.push(op(pop()))
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
	vm.stack.push(result)
	return nil
}

func handleUnaryBool[T WasmNumber | V128Value](
	vm *VM,
	pop func() T,
	op func(a T) bool,
) {
	vm.stack.push(boolToInt32(op(pop())))
}

func (vm *VM) handleSimdShift(op func(v V128Value, shift int32) V128Value) {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.push(op(v, shift))
}

func (vm *VM) handleSimdTernary(op func(v1, v2, v3 V128Value) V128Value) {
	v3 := vm.stack.popV128()
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()
	vm.stack.push(op(v1, v2, v3))
}

func handleSimdExtractLane[R WasmNumber](
	vm *VM,
	instruction Instruction,
	op func(v V128Value, laneIndex uint32) R,
) {
	laneIndex := uint32(instruction.Immediates[0])
	v := vm.stack.popV128()
	vm.stack.push(op(v, laneIndex))
}

func handleStore[T any](
	vm *VM,
	instruction Instruction,
	val T,
	store func(*Memory, uint32, uint32, T) error,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	index := uint32(vm.stack.popInt32())
	return store(memory, offset, index, val)
}

func handleLoad[T any, R any](
	vm *VM,
	instruction Instruction,
	load func(*Memory, uint32, uint32) (T, error),
	convert func(T) R,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	index := uint32(vm.stack.popInt32())
	v, err := load(memory, offset, index)
	if err != nil {
		return err
	}
	vm.stack.push(convert(v))
	return nil
}

func (vm *VM) handleLoadV128FromBytes(
	instruction Instruction,
	fromBytes func(bytes []byte) V128Value,
	sizeBytes uint32,
) error {
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	offset := uint32(instruction.Immediates[2])
	index := vm.stack.popInt32()

	data, err := memory.Get(offset, uint32(index), sizeBytes)
	if err != nil {
		return err
	}
	vm.stack.push(fromBytes(data))
	return nil
}

func (vm *VM) handleSimdConst(instruction Instruction) {
	v := V128Value{
		Low:  instruction.Immediates[0],
		High: instruction.Immediates[1],
	}
	vm.stack.push(v)
}

func (vm *VM) handleSimdLoadLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return nil
	}

	vm.stack.push(setLane(v, laneIndex, laneValue))
	return nil
}

func (vm *VM) handleSimdStoreLane(
	instruction Instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.Immediates[2])
	laneIndex := uint32(instruction.Immediates[3])
	memory := vm.getMemory(uint32(instruction.Immediates[1]))
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneData := extractLane(v, laneSize, laneIndex)
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
	vector := vm.stack.popV128()
	vm.stack.push(replaceLane(vector, laneIndex, laneValue))
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

func getExport(
	module *ModuleInstance,
	name string,
	indexType ExportIndexKind,
) (any, error) {
	for _, export := range module.Exports {
		if export.Name != name {
			continue
		}

		switch indexType {
		case FunctionExportKind:
			function, ok := export.Value.(FunctionInstance)
			if !ok {
				return nil, fmt.Errorf("export %s is not a function", name)
			}
			return function, nil
		case GlobalExportKind:
			global, ok := export.Value.(*Global)
			if !ok {
				return nil, fmt.Errorf("export %s is not a global", name)
			}
			return global, nil
		case MemoryExportKind:
			memory, ok := export.Value.(*Memory)
			if !ok {
				return nil, fmt.Errorf("export %s is not a memory", name)
			}
			return memory, nil
		case TableExportKind:
			table, ok := export.Value.(*Table)
			if !ok {
				return nil, fmt.Errorf("export %s is not a table", name)
			}
			return table, nil
		default:
			return nil, fmt.Errorf("unsupported indexType %d", indexType)
		}
	}

	return nil, fmt.Errorf("failed to find export with name: %s", name)
}

func (vm *VM) pushControlFrame(frame *controlFrame) {
	callFrame := vm.currentCallFrame()
	callFrame.controlStack = append(callFrame.controlStack, frame)
}

func (vm *VM) popControlFrame() *controlFrame {
	callFrame := vm.currentCallFrame()
	// Validation guarantees the control stack is never empty.
	index := len(callFrame.controlStack) - 1
	frame := callFrame.controlStack[index]
	callFrame.controlStack = callFrame.controlStack[:index]
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
			return errTableOutOfBounds
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
		case FunctionExportKind:
			storeIndex := instance.FuncAddrs[export.Index]
			value = vm.store.funcs[storeIndex]
		case GlobalExportKind:
			storeIndex := instance.GlobalAddrs[export.Index]
			value = vm.store.globals[storeIndex]
		case MemoryExportKind:
			storeIndex := instance.MemAddrs[export.Index]
			value = vm.store.memories[storeIndex]
		case TableExportKind:
			storeIndex := instance.TableAddrs[export.Index]
			value = vm.store.tables[storeIndex]
		}
		exports = append(exports, ExportInstance{Name: export.Name, Value: value})
	}
	return exports
}

func (vm *VM) invokeHostFunction(fun *HostFunction) (res []any, err error) {
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

	args := vm.stack.popN(len(fun.GetType().ParamTypes))
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
	function := WasmFunction{
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
