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
	"context"
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

// store represents all global state that can be manipulated by the vm. It
// consists of the runtime representation of all instances of functions,
// tables, memories, globals, element segments, and data segments that have
// been allocated during the vm life time.
type store struct {
	funcs    []FunctionInstance
	tables   []*Table
	memories []*Memory
	globals  []*Global
	elements []elementSegment
	datas    []dataSegment
}

type callFrame struct {
	decoder      *decoder
	controlStack []*controlFrame
	locals       []any
	function     *wasmFunction
}

// controlFrame represents a block of code that can be branched to.
type controlFrame struct {
	opcode         opcode // The opcode that created this control frame.
	continuationPc uint   // The address to jump to when `br` targets this frame.
	inputCount     uint   // Count of inputs this control instruction consumes.
	outputCount    uint   // Count of outputs this control instruction produces.
	stackHeight    uint
}

// vm is the WebAssembly Virtual Machine.
type vm struct {
	store          *store
	stack          *valueStack
	callStack      []*callFrame
	callStackDepth int
	features       ExperimentalFeatures
}

func newVm() *vm {
	return &vm{store: &store{}, stack: newValueStack()}
}

func (vm *vm) instantiate(
	ctx context.Context,
	module *moduleDefinition,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	validator := newValidator(vm.features)
	if err := validator.validateModule(module); err != nil {
		return nil, err
	}
	moduleInstance := &ModuleInstance{
		types: module.types,
		vm:    vm,
	}

	resolvedImports, err := resolveImports(module, imports)
	if err != nil {
		return nil, err
	}

	for _, functionInstance := range resolvedImports.functions {
		storeIndex := uint32(len(vm.store.funcs))
		moduleInstance.funcAddrs = append(moduleInstance.funcAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, functionInstance)
	}

	for _, function := range module.funcs {
		storeIndex := uint32(len(vm.store.funcs))
		funType := module.types[function.typeIndex]
		wasmFunc := newWasmFunction(funType, moduleInstance, function)
		moduleInstance.funcAddrs = append(moduleInstance.funcAddrs, storeIndex)
		vm.store.funcs = append(vm.store.funcs, wasmFunc)
	}

	for _, table := range resolvedImports.tables {
		storeIndex := uint32(len(vm.store.tables))
		moduleInstance.tableAddrs = append(moduleInstance.tableAddrs, storeIndex)
		vm.store.tables = append(vm.store.tables, table)
	}

	for _, tableType := range module.tables {
		storeIndex := uint32(len(vm.store.tables))
		table := NewTable(tableType)
		moduleInstance.tableAddrs = append(moduleInstance.tableAddrs, storeIndex)
		vm.store.tables = append(vm.store.tables, table)
	}

	for _, memory := range resolvedImports.memories {
		storeIndex := uint32(len(vm.store.memories))
		moduleInstance.memAddrs = append(moduleInstance.memAddrs, storeIndex)
		vm.store.memories = append(vm.store.memories, memory)
	}

	for _, memoryType := range module.memories {
		storeIndex := uint32(len(vm.store.memories))
		memory := NewMemory(memoryType)
		moduleInstance.memAddrs = append(moduleInstance.memAddrs, storeIndex)
		vm.store.memories = append(vm.store.memories, memory)
	}

	for _, global := range resolvedImports.globals {
		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.globalAddrs = append(moduleInstance.globalAddrs, storeIndex)
		vm.store.globals = append(vm.store.globals, global)
	}

	for _, global := range module.globalVariables {
		val, err := vm.invokeInitExpression(
			ctx,
			global.initExpression,
			global.globalType.ValueType,
			moduleInstance,
		)
		if err != nil {
			return nil, err
		}

		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.globalAddrs = append(moduleInstance.globalAddrs, storeIndex)
		vm.store.globals = append(vm.store.globals, &Global{
			Value:   val,
			Mutable: global.globalType.IsMutable,
			Type:    global.globalType.ValueType,
		})
	}

	// TODO: elements and data segments should at the very least be copied, but we
	// should probably have some runtime representation for them.
	for _, elem := range module.elementSegments {
		storeIndex := uint32(len(vm.store.elements))
		moduleInstance.dlemAddrs = append(moduleInstance.dlemAddrs, storeIndex)
		vm.store.elements = append(vm.store.elements, elem)
	}

	for _, data := range module.dataSegments {
		storeIndex := uint32(len(vm.store.datas))
		moduleInstance.dataAddrs = append(moduleInstance.dataAddrs, storeIndex)
		vm.store.datas = append(vm.store.datas, data)
	}

	if err := vm.initActiveElements(ctx, module, moduleInstance); err != nil {
		return nil, err
	}

	if err := vm.initActiveDatas(ctx, module, moduleInstance); err != nil {
		return nil, err
	}

	if module.startIndex != nil {
		storeFunctionIndex := moduleInstance.funcAddrs[*module.startIndex]
		function := vm.store.funcs[storeFunctionIndex]
		if _, err := vm.invokeFunction(ctx, function); err != nil {
			return nil, err
		}
	}

	moduleInstance.exports = vm.resolveExports(module, moduleInstance)
	return moduleInstance, nil
}

func (vm *vm) invoke(
	ctx context.Context,
	module *ModuleInstance,
	name string,
	args ...any,
) ([]any, error) {
	export, err := getExport(module, name, functionExportKind)
	if err != nil {
		return nil, err
	}

	vm.stack.pushAll(args)
	return vm.invokeFunction(ctx, export.(FunctionInstance))
}

func (vm *vm) invokeFunction(
	ctx context.Context,
	function FunctionInstance,
) ([]any, error) {
	switch f := function.(type) {
	case *wasmFunction:
		return vm.invokeWasmFunction(ctx, f)
	case *hostFunction:
		return vm.invokeHostFunction(ctx, f)
	default:
		return nil, fmt.Errorf("unknown function type")
	}
}

func (vm *vm) invokeWasmFunction(
	ctx context.Context,
	function *wasmFunction,
) ([]any, error) {
	if vm.callStackDepth >= maxCallStackDepth {
		return nil, errCallStackExhausted
	}
	vm.callStackDepth++
	defer func() { vm.callStackDepth-- }()

	locals := vm.stack.popN(len(function.functionType.ParamTypes))
	for _, local := range function.code.locals {
		locals = append(locals, defaultValue(local))
	}

	callFrame := &callFrame{
		decoder: newDecoder(function.code.body),
		controlStack: []*controlFrame{{
			opcode:         block,
			continuationPc: uint(len(function.code.body)),
			inputCount:     uint(len(function.functionType.ParamTypes)),
			outputCount:    uint(len(function.functionType.ResultTypes)),
			stackHeight:    vm.stack.size(),
		}},
		locals:   locals,
		function: function,
	}
	vm.callStack = append(vm.callStack, callFrame)

	// Check for cancellation every 1024 instructions
	const checkIntervalMask = 1023
	instructionCount := 0

	for callFrame.decoder.hasMore() {
		if instructionCount&checkIntervalMask == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		instructionCount++

		instruction, err := callFrame.decoder.decode()
		if err != nil {
			return nil, err
		}

		if err = vm.handleInstruction(ctx, instruction); err != nil {
			if errors.Is(err, errReturn) {
				break // A 'return' instruction was executed.
			}

			return nil, err
		}
	}

	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	values := vm.stack.popN(len(callFrame.function.functionType.ResultTypes))
	return values, nil
}

func (vm *vm) handleInstruction(
	ctx context.Context,
	instruction instruction,
) error {
	var err error
	// Using a switch instead of a map of opcode -> Handler is significantly
	// faster.
	switch instruction.opcode {
	case unreachable:
		err = errUnreachable
	case nop:
		// Do nothing.
	case block, loop:
		err = vm.handleStructured(instruction)
	case ifOp:
		err = vm.handleIf(instruction)
	case elseOp:
		vm.handleElse()
	case end:
		vm.handleEnd()
	case br:
		vm.handleBr(instruction)
	case brIf:
		vm.handleBrIf(instruction)
	case brTable:
		vm.handleBrTable(instruction)
	case returnOp:
		err = errReturn
	case call:
		err = vm.handleCall(ctx, instruction)
	case callIndirect:
		err = vm.handleCallIndirect(ctx, instruction)
	case drop:
		vm.stack.drop()
	case selectOp:
		vm.handleSelect()
	case selectT:
		vm.handleSelect()
	case localGet:
		vm.handleLocalGet(instruction)
	case localSet:
		vm.handleLocalSet(instruction)
	case localTee:
		vm.handleLocalTee(instruction)
	case globalGet:
		vm.handleGlobalGet(instruction)
	case globalSet:
		vm.handleGlobalSet(instruction)
	case tableGet:
		err = vm.handleTableGet(instruction)
	case tableSet:
		err = vm.handleTableSet(instruction)
	case i32Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, uint32ToInt32)
	case i64Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint64, uint64ToInt64)
	case f32Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, math.Float32frombits)
	case f64Load:
		err = handleLoad(vm, instruction, (*Memory).LoadUint64, math.Float64frombits)
	case i32Load8S:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, signExtend8To32)
	case i32Load8U:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, zeroExtend8To32)
	case i32Load16S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, signExtend16To32)
	case i32Load16U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, zeroExtend16To32)
	case i64Load8S:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, signExtend8To64)
	case i64Load8U:
		err = handleLoad(vm, instruction, (*Memory).LoadByte, zeroExtend8To64)
	case i64Load16S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, signExtend16To64)
	case i64Load16U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint16, zeroExtend16To64)
	case i64Load32S:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, signExtend32To64)
	case i64Load32U:
		err = handleLoad(vm, instruction, (*Memory).LoadUint32, zeroExtend32To64)
	case i32Store:
		err = handleStore(vm, instruction, uint32(vm.stack.popInt32()), (*Memory).StoreUint32)
	case i64Store:
		err = handleStore(vm, instruction, uint64(vm.stack.popInt64()), (*Memory).StoreUint64)
	case f32Store:
		err = handleStore(vm, instruction, math.Float32bits(vm.stack.popFloat32()), (*Memory).StoreUint32)
	case f64Store:
		err = handleStore(vm, instruction, math.Float64bits(vm.stack.popFloat64()), (*Memory).StoreUint64)
	case i32Store8:
		err = handleStore(vm, instruction, byte(vm.stack.popInt32()), (*Memory).StoreByte)
	case i32Store16:
		err = handleStore(vm, instruction, uint16(vm.stack.popInt32()), (*Memory).StoreUint16)
	case i64Store8:
		err = handleStore(vm, instruction, byte(vm.stack.popInt64()), (*Memory).StoreByte)
	case i64Store16:
		err = handleStore(vm, instruction, uint16(vm.stack.popInt64()), (*Memory).StoreUint16)
	case i64Store32:
		err = handleStore(vm, instruction, uint32(vm.stack.popInt64()), (*Memory).StoreUint32)
	case memorySize:
		vm.handleMemorySize(instruction)
	case memoryGrow:
		vm.handleMemoryGrow(instruction)
	case i32Const:
		vm.stack.push(int32(instruction.immediates[0]))
	case i64Const:
		vm.stack.push(int64(instruction.immediates[0]))
	case f32Const:
		vm.stack.push(math.Float32frombits(uint32(instruction.immediates[0])))
	case f64Const:
		vm.stack.push(math.Float64frombits(instruction.immediates[0]))
	case i32Eqz:
		handleUnaryBool(vm, vm.stack.popInt32, equalZero)
	case i32Eq:
		handleBinaryBool(vm, vm.stack.popInt32, equal)
	case i32Ne:
		handleBinaryBool(vm, vm.stack.popInt32, notEqual)
	case i32LtS:
		handleBinaryBool(vm, vm.stack.popInt32, lessThan)
	case i32LtU:
		handleBinaryBool(vm, vm.stack.popInt32, lessThanU32)
	case i32GtS:
		handleBinaryBool(vm, vm.stack.popInt32, greaterThan)
	case i32GtU:
		handleBinaryBool(vm, vm.stack.popInt32, greaterThanU32)
	case i32LeS:
		handleBinaryBool(vm, vm.stack.popInt32, lessOrEqual)
	case i32LeU:
		handleBinaryBool(vm, vm.stack.popInt32, lessOrEqualU32)
	case i32GeS:
		handleBinaryBool(vm, vm.stack.popInt32, greaterOrEqual)
	case i32GeU:
		handleBinaryBool(vm, vm.stack.popInt32, greaterOrEqualU32)
	case i64Eqz:
		handleUnaryBool(vm, vm.stack.popInt64, equalZero)
	case i64Eq:
		handleBinaryBool(vm, vm.stack.popInt64, equal)
	case i64Ne:
		handleBinaryBool(vm, vm.stack.popInt64, notEqual)
	case i64LtS:
		handleBinaryBool(vm, vm.stack.popInt64, lessThan)
	case i64LtU:
		handleBinaryBool(vm, vm.stack.popInt64, lessThanU64)
	case i64GtS:
		handleBinaryBool(vm, vm.stack.popInt64, greaterThan)
	case i64GtU:
		handleBinaryBool(vm, vm.stack.popInt64, greaterThanU64)
	case i64LeS:
		handleBinaryBool(vm, vm.stack.popInt64, lessOrEqual)
	case i64LeU:
		handleBinaryBool(vm, vm.stack.popInt64, lessOrEqualU64)
	case i64GeS:
		handleBinaryBool(vm, vm.stack.popInt64, greaterOrEqual)
	case i64GeU:
		handleBinaryBool(vm, vm.stack.popInt64, greaterOrEqualU64)
	case f32Eq:
		handleBinaryBool(vm, vm.stack.popFloat32, equal)
	case f32Ne:
		handleBinaryBool(vm, vm.stack.popFloat32, notEqual)
	case f32Lt:
		handleBinaryBool(vm, vm.stack.popFloat32, lessThan)
	case f32Gt:
		handleBinaryBool(vm, vm.stack.popFloat32, greaterThan)
	case f32Le:
		handleBinaryBool(vm, vm.stack.popFloat32, lessOrEqual)
	case f32Ge:
		handleBinaryBool(vm, vm.stack.popFloat32, greaterOrEqual)
	case f64Eq:
		handleBinaryBool(vm, vm.stack.popFloat64, equal)
	case f64Ne:
		handleBinaryBool(vm, vm.stack.popFloat64, notEqual)
	case f64Lt:
		handleBinaryBool(vm, vm.stack.popFloat64, lessThan)
	case f64Gt:
		handleBinaryBool(vm, vm.stack.popFloat64, greaterThan)
	case f64Le:
		handleBinaryBool(vm, vm.stack.popFloat64, lessOrEqual)
	case f64Ge:
		handleBinaryBool(vm, vm.stack.popFloat64, greaterOrEqual)
	case i32Clz:
		handleUnary(vm, vm.stack.popInt32, clz32)
	case i32Ctz:
		handleUnary(vm, vm.stack.popInt32, ctz32)
	case i32Popcnt:
		handleUnary(vm, vm.stack.popInt32, popcnt32)
	case i32Add:
		handleBinary(vm, vm.stack.popInt32, add)
	case i32Sub:
		handleBinary(vm, vm.stack.popInt32, sub)
	case i32Mul:
		handleBinary(vm, vm.stack.popInt32, mul)
	case i32DivS:
		err = handleBinarySafe(vm, vm.stack.popInt32, divS32)
	case i32DivU:
		err = handleBinarySafe(vm, vm.stack.popInt32, divU32)
	case i32RemS:
		err = handleBinarySafe(vm, vm.stack.popInt32, remS32)
	case i32RemU:
		err = handleBinarySafe(vm, vm.stack.popInt32, remU32)
	case i32And:
		handleBinary(vm, vm.stack.popInt32, and)
	case i32Or:
		handleBinary(vm, vm.stack.popInt32, or)
	case i32Xor:
		handleBinary(vm, vm.stack.popInt32, xor)
	case i32Shl:
		handleBinary(vm, vm.stack.popInt32, shl32)
	case i32ShrS:
		handleBinary(vm, vm.stack.popInt32, shrS32)
	case i32ShrU:
		handleBinary(vm, vm.stack.popInt32, shrU32)
	case i32Rotl:
		handleBinary(vm, vm.stack.popInt32, rotl32)
	case i32Rotr:
		handleBinary(vm, vm.stack.popInt32, rotr32)
	case i64Clz:
		handleUnary(vm, vm.stack.popInt64, clz64)
	case i64Ctz:
		handleUnary(vm, vm.stack.popInt64, ctz64)
	case i64Popcnt:
		handleUnary(vm, vm.stack.popInt64, popcnt64)
	case i64Add:
		handleBinary(vm, vm.stack.popInt64, add)
	case i64Sub:
		handleBinary(vm, vm.stack.popInt64, sub)
	case i64Mul:
		handleBinary(vm, vm.stack.popInt64, mul)
	case i64DivS:
		err = handleBinarySafe(vm, vm.stack.popInt64, divS64)
	case i64DivU:
		err = handleBinarySafe(vm, vm.stack.popInt64, divU64)
	case i64RemS:
		err = handleBinarySafe(vm, vm.stack.popInt64, remS64)
	case i64RemU:
		err = handleBinarySafe(vm, vm.stack.popInt64, remU64)
	case i64And:
		handleBinary(vm, vm.stack.popInt64, and)
	case i64Or:
		handleBinary(vm, vm.stack.popInt64, or)
	case i64Xor:
		handleBinary(vm, vm.stack.popInt64, xor)
	case i64Shl:
		handleBinary(vm, vm.stack.popInt64, shl64)
	case i64ShrS:
		handleBinary(vm, vm.stack.popInt64, shrS64)
	case i64ShrU:
		handleBinary(vm, vm.stack.popInt64, shrU64)
	case i64Rotl:
		handleBinary(vm, vm.stack.popInt64, rotl64)
	case i64Rotr:
		handleBinary(vm, vm.stack.popInt64, rotr64)
	case f32Abs:
		handleUnary(vm, vm.stack.popFloat32, abs[float32])
	case f32Neg:
		handleUnary(vm, vm.stack.popFloat32, neg[float32])
	case f32Ceil:
		handleUnary(vm, vm.stack.popFloat32, ceil[float32])
	case f32Floor:
		handleUnary(vm, vm.stack.popFloat32, floor[float32])
	case f32Trunc:
		handleUnary(vm, vm.stack.popFloat32, trunc[float32])
	case f32Nearest:
		handleUnary(vm, vm.stack.popFloat32, nearest[float32])
	case f32Sqrt:
		handleUnary(vm, vm.stack.popFloat32, sqrt[float32])
	case f32Add:
		handleBinary(vm, vm.stack.popFloat32, add[float32])
	case f32Sub:
		handleBinary(vm, vm.stack.popFloat32, sub[float32])
	case f32Mul:
		handleBinary(vm, vm.stack.popFloat32, mul[float32])
	case f32Div:
		handleBinary(vm, vm.stack.popFloat32, div[float32])
	case f32Min:
		handleBinary(vm, vm.stack.popFloat32, wasmMin[float32])
	case f32Max:
		handleBinary(vm, vm.stack.popFloat32, wasmMax[float32])
	case f32Copysign:
		handleBinary(vm, vm.stack.popFloat32, copysign[float32])
	case f64Abs:
		handleUnary(vm, vm.stack.popFloat64, abs[float64])
	case f64Neg:
		handleUnary(vm, vm.stack.popFloat64, neg[float64])
	case f64Ceil:
		handleUnary(vm, vm.stack.popFloat64, ceil[float64])
	case f64Floor:
		handleUnary(vm, vm.stack.popFloat64, floor[float64])
	case f64Trunc:
		handleUnary(vm, vm.stack.popFloat64, trunc[float64])
	case f64Nearest:
		handleUnary(vm, vm.stack.popFloat64, nearest[float64])
	case f64Sqrt:
		handleUnary(vm, vm.stack.popFloat64, sqrt[float64])
	case f64Add:
		handleBinary(vm, vm.stack.popFloat64, add[float64])
	case f64Sub:
		handleBinary(vm, vm.stack.popFloat64, sub[float64])
	case f64Mul:
		handleBinary(vm, vm.stack.popFloat64, mul[float64])
	case f64Div:
		handleBinary(vm, vm.stack.popFloat64, div[float64])
	case f64Min:
		handleBinary(vm, vm.stack.popFloat64, wasmMin[float64])
	case f64Max:
		handleBinary(vm, vm.stack.popFloat64, wasmMax[float64])
	case f64Copysign:
		handleBinary(vm, vm.stack.popFloat64, copysign[float64])
	case i32WrapI64:
		handleUnary(vm, vm.stack.popInt64, wrapI64ToI32)
	case i32TruncF32S:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32SToI32)
	case i32TruncF32U:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32UToI32)
	case i32TruncF64S:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64SToI32)
	case i32TruncF64U:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64UToI32)
	case i64ExtendI32S:
		handleUnary(vm, vm.stack.popInt32, extendI32SToI64)
	case i64ExtendI32U:
		handleUnary(vm, vm.stack.popInt32, extendI32UToI64)
	case i64TruncF32S:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32SToI64)
	case i64TruncF32U:
		err = handleUnarySafe(vm, vm.stack.popFloat32, truncF32UToI64)
	case i64TruncF64S:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64SToI64)
	case i64TruncF64U:
		err = handleUnarySafe(vm, vm.stack.popFloat64, truncF64UToI64)
	case f32ConvertI32S:
		handleUnary(vm, vm.stack.popInt32, convertI32SToF32)
	case f32ConvertI32U:
		handleUnary(vm, vm.stack.popInt32, convertI32UToF32)
	case f32ConvertI64S:
		handleUnary(vm, vm.stack.popInt64, convertI64SToF32)
	case f32ConvertI64U:
		handleUnary(vm, vm.stack.popInt64, convertI64UToF32)
	case f32DemoteF64:
		handleUnary(vm, vm.stack.popFloat64, demoteF64ToF32)
	case f64ConvertI32S:
		handleUnary(vm, vm.stack.popInt32, convertI32SToF64)
	case f64ConvertI32U:
		handleUnary(vm, vm.stack.popInt32, convertI32UToF64)
	case f64ConvertI64S:
		handleUnary(vm, vm.stack.popInt64, convertI64SToF64)
	case f64ConvertI64U:
		handleUnary(vm, vm.stack.popInt64, convertI64UToF64)
	case f64PromoteF32:
		handleUnary(vm, vm.stack.popFloat32, promoteF32ToF64)
	case i32ReinterpretF32:
		handleUnary(vm, vm.stack.popFloat32, reinterpretF32ToI32)
	case i64ReinterpretF64:
		handleUnary(vm, vm.stack.popFloat64, reinterpretF64ToI64)
	case f32ReinterpretI32:
		handleUnary(vm, vm.stack.popInt32, reinterpretI32ToF32)
	case f64ReinterpretI64:
		handleUnary(vm, vm.stack.popInt64, reinterpretI64ToF64)
	case i32Extend8S:
		handleUnary(vm, vm.stack.popInt32, extend8STo32)
	case i32Extend16S:
		handleUnary(vm, vm.stack.popInt32, extend16STo32)
	case i64Extend8S:
		handleUnary(vm, vm.stack.popInt64, extend8STo64)
	case i64Extend16S:
		handleUnary(vm, vm.stack.popInt64, extend16STo64)
	case i64Extend32S:
		handleUnary(vm, vm.stack.popInt64, extend32STo64)
	case refNull:
		vm.stack.push(NullVal)
	case refIsNull:
		vm.handleRefIsNull()
	case refFunc:
		vm.handleRefFunc(instruction)
	case i32TruncSatF32S:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32SToI32)
	case i32TruncSatF32U:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32UToI32)
	case i32TruncSatF64S:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64SToI32)
	case i32TruncSatF64U:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64UToI32)
	case i64TruncSatF32S:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32SToI64)
	case i64TruncSatF32U:
		handleUnary(vm, vm.stack.popFloat32, truncSatF32UToI64)
	case i64TruncSatF64S:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64SToI64)
	case i64TruncSatF64U:
		handleUnary(vm, vm.stack.popFloat64, truncSatF64UToI64)
	case memoryInit:
		err = vm.handleMemoryInit(instruction)
	case dataDrop:
		vm.handleDataDrop(instruction)
	case memoryCopy:
		err = vm.handleMemoryCopy(instruction)
	case memoryFill:
		err = vm.handleMemoryFill(instruction)
	case tableInit:
		err = vm.handleTableInit(instruction)
	case elemDrop:
		vm.handleElemDrop(instruction)
	case tableCopy:
		err = vm.handleTableCopy(instruction)
	case tableGrow:
		vm.handleTableGrow(instruction)
	case tableSize:
		vm.handleTableSize(instruction)
	case tableFill:
		err = vm.handleTableFill(instruction)
	case v128Load:
		err = handleLoad(vm, instruction, (*Memory).LoadV128, identityV128)
	case v128Load8x8S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load8x8S, 8)
	case v128Load8x8U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load8x8U, 8)
	case v128Load16x4S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load16x4S, 8)
	case v128Load16x4U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load16x4U, 8)
	case v128Load32x2S:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32x2S, 8)
	case v128Load32x2U:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32x2U, 8)
	case v128Load8Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI8x16SplatFromBytes, 1)
	case v128Load16Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI16x8SplatFromBytes, 2)
	case v128Load32Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI32x4SplatFromBytes, 4)
	case v128Load64Splat:
		err = vm.handleLoadV128FromBytes(instruction, simdI64x2SplatFromBytes, 8)
	case v128Store:
		err = handleStore(vm, instruction, vm.stack.popV128(), (*Memory).StoreV128)
	case v128Const:
		vm.handleSimdConst(instruction)
	case i8x16Shuffle:
		vm.handleI8x16Shuffle(instruction)
	case i8x16Swizzle:
		handleBinary(vm, vm.stack.popV128, simdI8x16Swizzle)
	case i8x16Splat:
		handleUnary(vm, vm.stack.popInt32, simdI8x16Splat)
	case i16x8Splat:
		handleUnary(vm, vm.stack.popInt32, simdI16x8Splat)
	case i32x4Splat:
		handleUnary(vm, vm.stack.popInt32, simdI32x4Splat)
	case i64x2Splat:
		handleUnary(vm, vm.stack.popInt64, simdI64x2Splat)
	case f32x4Splat:
		handleUnary(vm, vm.stack.popFloat32, simdF32x4Splat)
	case f64x2Splat:
		handleUnary(vm, vm.stack.popFloat64, simdF64x2Splat)
	case i8x16ExtractLaneS:
		handleSimdExtractLane(vm, instruction, simdI8x16ExtractLaneS)
	case i8x16ExtractLaneU:
		handleSimdExtractLane(vm, instruction, simdI8x16ExtractLaneU)
	case i8x16ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI8x16ReplaceLane)
	case i16x8ExtractLaneS:
		handleSimdExtractLane(vm, instruction, simdI16x8ExtractLaneS)
	case i16x8ExtractLaneU:
		handleSimdExtractLane(vm, instruction, simdI16x8ExtractLaneU)
	case i16x8ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI16x8ReplaceLane)
	case i32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, simdI32x4ExtractLane)
	case i32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt32, simdI32x4ReplaceLane)
	case i64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, simdI64x2ExtractLane)
	case i64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popInt64, simdI64x2ReplaceLane)
	case f32x4ExtractLane:
		handleSimdExtractLane(vm, instruction, simdF32x4ExtractLane)
	case f32x4ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popFloat32, simdF32x4ReplaceLane)
	case f64x2ExtractLane:
		handleSimdExtractLane(vm, instruction, simdF64x2ExtractLane)
	case f64x2ReplaceLane:
		handleSimdReplaceLane(vm, instruction, vm.stack.popFloat64, simdF64x2ReplaceLane)
	case i8x16Eq:
		handleBinary(vm, vm.stack.popV128, simdI8x16Eq)
	case i8x16Ne:
		handleBinary(vm, vm.stack.popV128, simdI8x16Ne)
	case i8x16LtS:
		handleBinary(vm, vm.stack.popV128, simdI8x16LtS)
	case i8x16LtU:
		handleBinary(vm, vm.stack.popV128, simdI8x16LtU)
	case i8x16GtS:
		handleBinary(vm, vm.stack.popV128, simdI8x16GtS)
	case i8x16GtU:
		handleBinary(vm, vm.stack.popV128, simdI8x16GtU)
	case i8x16LeS:
		handleBinary(vm, vm.stack.popV128, simdI8x16LeS)
	case i8x16LeU:
		handleBinary(vm, vm.stack.popV128, simdI8x16LeU)
	case i8x16GeS:
		handleBinary(vm, vm.stack.popV128, simdI8x16GeS)
	case i8x16GeU:
		handleBinary(vm, vm.stack.popV128, simdI8x16GeU)
	case i16x8Eq:
		handleBinary(vm, vm.stack.popV128, simdI16x8Eq)
	case i16x8Ne:
		handleBinary(vm, vm.stack.popV128, simdI16x8Ne)
	case i16x8LtS:
		handleBinary(vm, vm.stack.popV128, simdI16x8LtS)
	case i16x8LtU:
		handleBinary(vm, vm.stack.popV128, simdI16x8LtU)
	case i16x8GtS:
		handleBinary(vm, vm.stack.popV128, simdI16x8GtS)
	case i16x8GtU:
		handleBinary(vm, vm.stack.popV128, simdI16x8GtU)
	case i16x8LeS:
		handleBinary(vm, vm.stack.popV128, simdI16x8LeS)
	case i16x8LeU:
		handleBinary(vm, vm.stack.popV128, simdI16x8LeU)
	case i16x8GeS:
		handleBinary(vm, vm.stack.popV128, simdI16x8GeS)
	case i16x8GeU:
		handleBinary(vm, vm.stack.popV128, simdI16x8GeU)
	case i32x4Eq:
		handleBinary(vm, vm.stack.popV128, simdI32x4Eq)
	case i32x4Ne:
		handleBinary(vm, vm.stack.popV128, simdI32x4Ne)
	case i32x4LtS:
		handleBinary(vm, vm.stack.popV128, simdI32x4LtS)
	case i32x4LtU:
		handleBinary(vm, vm.stack.popV128, simdI32x4LtU)
	case i32x4GtS:
		handleBinary(vm, vm.stack.popV128, simdI32x4GtS)
	case i32x4GtU:
		handleBinary(vm, vm.stack.popV128, simdI32x4GtU)
	case i32x4LeS:
		handleBinary(vm, vm.stack.popV128, simdI32x4LeS)
	case i32x4LeU:
		handleBinary(vm, vm.stack.popV128, simdI32x4LeU)
	case i32x4GeS:
		handleBinary(vm, vm.stack.popV128, simdI32x4GeS)
	case i32x4GeU:
		handleBinary(vm, vm.stack.popV128, simdI32x4GeU)
	case f32x4Eq:
		handleBinary(vm, vm.stack.popV128, simdF32x4Eq)
	case f32x4Ne:
		handleBinary(vm, vm.stack.popV128, simdF32x4Ne)
	case f32x4Lt:
		handleBinary(vm, vm.stack.popV128, simdF32x4Lt)
	case f32x4Gt:
		handleBinary(vm, vm.stack.popV128, simdF32x4Gt)
	case f32x4Le:
		handleBinary(vm, vm.stack.popV128, simdF32x4Le)
	case f32x4Ge:
		handleBinary(vm, vm.stack.popV128, simdF32x4Ge)
	case f64x2Eq:
		handleBinary(vm, vm.stack.popV128, simdF64x2Eq)
	case f64x2Ne:
		handleBinary(vm, vm.stack.popV128, simdF64x2Ne)
	case f64x2Lt:
		handleBinary(vm, vm.stack.popV128, simdF64x2Lt)
	case f64x2Gt:
		handleBinary(vm, vm.stack.popV128, simdF64x2Gt)
	case f64x2Le:
		handleBinary(vm, vm.stack.popV128, simdF64x2Le)
	case f64x2Ge:
		handleBinary(vm, vm.stack.popV128, simdF64x2Ge)
	case v128Not:
		handleUnary(vm, vm.stack.popV128, simdV128Not)
	case v128And:
		handleBinary(vm, vm.stack.popV128, simdV128And)
	case v128Andnot:
		handleBinary(vm, vm.stack.popV128, simdV128Andnot)
	case v128Or:
		handleBinary(vm, vm.stack.popV128, simdV128Or)
	case v128Xor:
		handleBinary(vm, vm.stack.popV128, simdV128Xor)
	case v128Bitselect:
		vm.handleSimdTernary(simdV128Bitselect)
	case v128AnyTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdV128AnyTrue)
	case v128Load8Lane:
		err = vm.handleSimdLoadLane(instruction, 8)
	case v128Load16Lane:
		err = vm.handleSimdLoadLane(instruction, 16)
	case v128Load32Lane:
		err = vm.handleSimdLoadLane(instruction, 32)
	case v128Load64Lane:
		err = vm.handleSimdLoadLane(instruction, 64)
	case v128Store8Lane:
		err = vm.handleSimdStoreLane(instruction, 8)
	case v128Store16Lane:
		err = vm.handleSimdStoreLane(instruction, 16)
	case v128Store32Lane:
		err = vm.handleSimdStoreLane(instruction, 32)
	case v128Store64Lane:
		err = vm.handleSimdStoreLane(instruction, 64)
	case v128Load32Zero:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load32Zero, 4)
	case v128Load64Zero:
		err = vm.handleLoadV128FromBytes(instruction, simdV128Load64Zero, 8)
	case f32x4DemoteF64x2Zero:
		handleUnary(vm, vm.stack.popV128, simdF32x4DemoteF64x2Zero)
	case f64x2PromoteLowF32x4:
		handleUnary(vm, vm.stack.popV128, simdF64x2PromoteLowF32x4)
	case i8x16Abs:
		handleUnary(vm, vm.stack.popV128, simdI8x16Abs)
	case i8x16Neg:
		handleUnary(vm, vm.stack.popV128, simdI8x16Neg)
	case i8x16Popcnt:
		handleUnary(vm, vm.stack.popV128, simdI8x16Popcnt)
	case i8x16AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI8x16AllTrue)
	case i8x16Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI8x16Bitmask)
	case i8x16NarrowI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI8x16NarrowI16x8S)
	case i8x16NarrowI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI8x16NarrowI16x8U)
	case f32x4Ceil:
		handleUnary(vm, vm.stack.popV128, simdF32x4Ceil)
	case f32x4Floor:
		handleUnary(vm, vm.stack.popV128, simdF32x4Floor)
	case f32x4Trunc:
		handleUnary(vm, vm.stack.popV128, simdF32x4Trunc)
	case f32x4Nearest:
		handleUnary(vm, vm.stack.popV128, simdF32x4Nearest)
	case i8x16Shl:
		vm.handleSimdShift(simdI8x16Shl)
	case i8x16ShrU:
		vm.handleSimdShift(simdI8x16ShrU)
	case i8x16ShrS:
		vm.handleSimdShift(simdI8x16ShrS)
	case i8x16Add:
		handleBinary(vm, vm.stack.popV128, simdI8x16Add)
	case i8x16AddSatS:
		handleBinary(vm, vm.stack.popV128, simdI8x16AddSatS)
	case i8x16AddSatU:
		handleBinary(vm, vm.stack.popV128, simdI8x16AddSatU)
	case i8x16Sub:
		handleBinary(vm, vm.stack.popV128, simdI8x16Sub)
	case i8x16SubSatS:
		handleBinary(vm, vm.stack.popV128, simdI8x16SubSatS)
	case i8x16SubSatU:
		handleBinary(vm, vm.stack.popV128, simdI8x16SubSatU)
	case f64x2Ceil:
		handleUnary(vm, vm.stack.popV128, simdF64x2Ceil)
	case f64x2Floor:
		handleUnary(vm, vm.stack.popV128, simdF64x2Floor)
	case i8x16MinS:
		handleBinary(vm, vm.stack.popV128, simdI8x16MinS)
	case i8x16MinU:
		handleBinary(vm, vm.stack.popV128, simdI8x16MinU)
	case i8x16MaxS:
		handleBinary(vm, vm.stack.popV128, simdI8x16MaxS)
	case i8x16MaxU:
		handleBinary(vm, vm.stack.popV128, simdI8x16MaxU)
	case f64x2Trunc:
		handleUnary(vm, vm.stack.popV128, simdF64x2Trunc)
	case i8x16AvgrU:
		handleBinary(vm, vm.stack.popV128, simdI8x16AvgrU)
	case i16x8ExtaddPairwiseI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtaddPairwiseI8x16S)
	case i16x8ExtaddPairwiseI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtaddPairwiseI8x16U)
	case i32x4ExtaddPairwiseI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtaddPairwiseI16x8S)
	case i32x4ExtaddPairwiseI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtaddPairwiseI16x8U)
	case i16x8Abs:
		handleUnary(vm, vm.stack.popV128, simdI16x8Abs)
	case i16x8Neg:
		handleUnary(vm, vm.stack.popV128, simdI16x8Neg)
	case i16x8Q15mulrSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8Q15mulrSatS)
	case i16x8AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI16x8AllTrue)
	case i16x8Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI16x8Bitmask)
	case i16x8NarrowI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI16x8NarrowI32x4S)
	case i16x8NarrowI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI16x8NarrowI32x4U)
	case i16x8ExtendLowI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendLowI8x16S)
	case i16x8ExtendHighI8x16S:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendHighI8x16S)
	case i16x8ExtendLowI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendLowI8x16U)
	case i16x8ExtendHighI8x16U:
		handleUnary(vm, vm.stack.popV128, simdI16x8ExtendHighI8x16U)
	case i16x8Shl:
		vm.handleSimdShift(simdI16x8Shl)
	case i16x8ShrS:
		vm.handleSimdShift(simdI16x8ShrS)
	case i16x8ShrU:
		vm.handleSimdShift(simdI16x8ShrU)
	case i16x8Add:
		handleBinary(vm, vm.stack.popV128, simdI16x8Add)
	case i16x8AddSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8AddSatS)
	case i16x8AddSatU:
		handleBinary(vm, vm.stack.popV128, simdI16x8AddSatU)
	case i16x8Sub:
		handleBinary(vm, vm.stack.popV128, simdI16x8Sub)
	case i16x8SubSatS:
		handleBinary(vm, vm.stack.popV128, simdI16x8SubSatS)
	case i16x8SubSatU:
		handleBinary(vm, vm.stack.popV128, simdI16x8SubSatU)
	case f64x2Nearest:
		handleUnary(vm, vm.stack.popV128, simdF64x2Nearest)
	case i16x8Mul:
		handleBinary(vm, vm.stack.popV128, simdI16x8Mul)
	case i16x8MinS:
		handleBinary(vm, vm.stack.popV128, simdI16x8MinS)
	case i16x8MinU:
		handleBinary(vm, vm.stack.popV128, simdI16x8MinU)
	case i16x8MaxS:
		handleBinary(vm, vm.stack.popV128, simdI16x8MaxS)
	case i16x8MaxU:
		handleBinary(vm, vm.stack.popV128, simdI16x8MaxU)
	case i16x8AvgrU:
		handleBinary(vm, vm.stack.popV128, simdI16x8AvgrU)
	case i16x8ExtmulLowI8x16S:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulLowI8x16S)
	case i16x8ExtmulHighI8x16S:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulHighI8x16S)
	case i16x8ExtmulLowI8x16U:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulLowI8x16U)
	case i16x8ExtmulHighI8x16U:
		handleBinary(vm, vm.stack.popV128, simdI16x8ExtmulHighI8x16U)
	case i32x4Abs:
		handleUnary(vm, vm.stack.popV128, simdI32x4Abs)
	case i32x4Neg:
		handleUnary(vm, vm.stack.popV128, simdI32x4Neg)
	case i32x4AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI32x4AllTrue)
	case i32x4Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI32x4Bitmask)
	case i32x4ExtendLowI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendLowI16x8S)
	case i32x4ExtendHighI16x8S:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendHighI16x8S)
	case i32x4ExtendLowI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendLowI16x8U)
	case i32x4ExtendHighI16x8U:
		handleUnary(vm, vm.stack.popV128, simdI32x4ExtendHighI16x8U)
	case i32x4Shl:
		vm.handleSimdShift(simdI32x4Shl)
	case i32x4ShrS:
		vm.handleSimdShift(simdI32x4ShrS)
	case i32x4ShrU:
		vm.handleSimdShift(simdI32x4ShrU)
	case i32x4Add:
		handleBinary(vm, vm.stack.popV128, simdI32x4Add)
	case i32x4Sub:
		handleBinary(vm, vm.stack.popV128, simdI32x4Sub)
	case i32x4Mul:
		handleBinary(vm, vm.stack.popV128, simdI32x4Mul)
	case i32x4MinS:
		handleBinary(vm, vm.stack.popV128, simdI32x4MinS)
	case i32x4MinU:
		handleBinary(vm, vm.stack.popV128, simdI32x4MinU)
	case i32x4MaxS:
		handleBinary(vm, vm.stack.popV128, simdI32x4MaxS)
	case i32x4MaxU:
		handleBinary(vm, vm.stack.popV128, simdI32x4MaxU)
	case i32x4DotI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4DotI16x8S)
	case i32x4ExtmulLowI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulLowI16x8S)
	case i32x4ExtmulHighI16x8S:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulHighI16x8S)
	case i32x4ExtmulLowI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulLowI16x8U)
	case i32x4ExtmulHighI16x8U:
		handleBinary(vm, vm.stack.popV128, simdI32x4ExtmulHighI16x8U)
	case i64x2Abs:
		handleUnary(vm, vm.stack.popV128, simdI64x2Abs)
	case i64x2Neg:
		handleUnary(vm, vm.stack.popV128, simdI64x2Neg)
	case i64x2AllTrue:
		handleUnaryBool(vm, vm.stack.popV128, simdI64x2AllTrue)
	case i64x2Bitmask:
		handleUnary(vm, vm.stack.popV128, simdI64x2Bitmask)
	case i64x2ExtendLowI32x4S:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendLowI32x4S)
	case i64x2ExtendHighI32x4S:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendHighI32x4S)
	case i64x2ExtendLowI32x4U:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendLowI32x4U)
	case i64x2ExtendHighI32x4U:
		handleUnary(vm, vm.stack.popV128, simdI64x2ExtendHighI32x4U)
	case i64x2Shl:
		vm.handleSimdShift(simdI64x2Shl)
	case i64x2ShrS:
		vm.handleSimdShift(simdI64x2ShrS)
	case i64x2ShrU:
		vm.handleSimdShift(simdI64x2ShrU)
	case i64x2Add:
		handleBinary(vm, vm.stack.popV128, simdI64x2Add)
	case i64x2Sub:
		handleBinary(vm, vm.stack.popV128, simdI64x2Sub)
	case i64x2Mul:
		handleBinary(vm, vm.stack.popV128, simdI64x2Mul)
	case i64x2Eq:
		handleBinary(vm, vm.stack.popV128, simdI64x2Eq)
	case i64x2Ne:
		handleBinary(vm, vm.stack.popV128, simdI64x2Ne)
	case i64x2LtS:
		handleBinary(vm, vm.stack.popV128, simdI64x2LtS)
	case i64x2GtS:
		handleBinary(vm, vm.stack.popV128, simdI64x2GtS)
	case i64x2LeS:
		handleBinary(vm, vm.stack.popV128, simdI64x2LeS)
	case i64x2GeS:
		handleBinary(vm, vm.stack.popV128, simdI64x2GeS)
	case i64x2ExtmulLowI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulLowI32x4S)
	case i64x2ExtmulHighI32x4S:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulHighI32x4S)
	case i64x2ExtmulLowI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulLowI32x4U)
	case i64x2ExtmulHighI32x4U:
		handleBinary(vm, vm.stack.popV128, simdI64x2ExtmulHighI32x4U)
	case f32x4Abs:
		handleUnary(vm, vm.stack.popV128, simdF32x4Abs)
	case f32x4Neg:
		handleUnary(vm, vm.stack.popV128, simdF32x4Neg)
	case f32x4Sqrt:
		handleUnary(vm, vm.stack.popV128, simdF32x4Sqrt)
	case f32x4Add:
		handleBinary(vm, vm.stack.popV128, simdF32x4Add)
	case f32x4Sub:
		handleBinary(vm, vm.stack.popV128, simdF32x4Sub)
	case f32x4Mul:
		handleBinary(vm, vm.stack.popV128, simdF32x4Mul)
	case f32x4Div:
		handleBinary(vm, vm.stack.popV128, simdF32x4Div)
	case f32x4Min:
		handleBinary(vm, vm.stack.popV128, simdF32x4Min)
	case f32x4Max:
		handleBinary(vm, vm.stack.popV128, simdF32x4Max)
	case f32x4Pmin:
		handleBinary(vm, vm.stack.popV128, simdF32x4Pmin)
	case f32x4Pmax:
		handleBinary(vm, vm.stack.popV128, simdF32x4Pmax)
	case f64x2Abs:
		handleUnary(vm, vm.stack.popV128, simdF64x2Abs)
	case f64x2Neg:
		handleUnary(vm, vm.stack.popV128, simdF64x2Neg)
	case f64x2Sqrt:
		handleUnary(vm, vm.stack.popV128, simdF64x2Sqrt)
	case f64x2Add:
		handleBinary(vm, vm.stack.popV128, simdF64x2Add)
	case f64x2Sub:
		handleBinary(vm, vm.stack.popV128, simdF64x2Sub)
	case f64x2Mul:
		handleBinary(vm, vm.stack.popV128, simdF64x2Mul)
	case f64x2Div:
		handleBinary(vm, vm.stack.popV128, simdF64x2Div)
	case f64x2Min:
		handleBinary(vm, vm.stack.popV128, simdF64x2Min)
	case f64x2Max:
		handleBinary(vm, vm.stack.popV128, simdF64x2Max)
	case f64x2Pmin:
		handleBinary(vm, vm.stack.popV128, simdF64x2Pmin)
	case f64x2Pmax:
		handleBinary(vm, vm.stack.popV128, simdF64x2Pmax)
	case i32x4TruncSatF32x4S:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF32x4S)
	case i32x4TruncSatF32x4U:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF32x4U)
	case f32x4ConvertI32x4S:
		handleUnary(vm, vm.stack.popV128, simdF32x4ConvertI32x4S)
	case f32x4ConvertI32x4U:
		handleUnary(vm, vm.stack.popV128, simdF32x4ConvertI32x4U)
	case i32x4TruncSatF64x2SZero:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF64x2SZero)
	case i32x4TruncSatF64x2UZero:
		handleUnary(vm, vm.stack.popV128, simdI32x4TruncSatF64x2UZero)
	case f64x2ConvertLowI32x4S:
		handleUnary(vm, vm.stack.popV128, simdF64x2ConvertLowI32x4S)
	case f64x2ConvertLowI32x4U:
		handleUnary(vm, vm.stack.popV128, simdF64x2ConvertLowI32x4U)
	default:
		err = fmt.Errorf("unknown opcode %d", instruction.opcode)
	}
	return err
}

func (vm *vm) currentCallFrame() *callFrame {
	return vm.callStack[len(vm.callStack)-1]
}

func (vm *vm) currentModuleInstance() *ModuleInstance {
	return vm.currentCallFrame().function.module
}

func (vm *vm) pushBlockFrame(opcode opcode, blockType int32) error {
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
	if opcode == loop {
		frame.continuationPc = originalPc
	} else {
		if cachedPc, ok := callFrame.function.jumpCache[originalPc]; ok {
			frame.continuationPc = cachedPc
		} else {
			// Cache miss: we need to scan forward to find the matching 'end'.
			if err := callFrame.decoder.decodeUntilMatchingEnd(); err != nil {
				return err
			}

			callFrame.function.jumpCache[originalPc] = callFrame.decoder.pc
			frame.continuationPc = callFrame.decoder.pc
			callFrame.decoder.pc = originalPc
		}
	}

	vm.pushControlFrame(frame)
	return nil
}

func (vm *vm) handleStructured(instruction instruction) error {
	blockType := int32(instruction.immediates[0])
	return vm.pushBlockFrame(instruction.opcode, blockType)
}

func (vm *vm) handleIf(instruction instruction) error {
	frame := vm.currentCallFrame()
	blockType := int32(instruction.immediates[0])
	originalPc := frame.decoder.pc

	condition := vm.stack.popInt32()

	if err := vm.pushBlockFrame(ifOp, blockType); err != nil {
		return err
	}

	if condition != 0 {
		return nil
	}

	// We need to jump to the 'else' or 'end'.
	if elsePc, ok := frame.function.jumpElseCache[originalPc]; ok {
		frame.decoder.pc = elsePc
		return nil
	}

	// Cache miss, we need to find the matching Else or End.
	matchingOpcode, err := frame.decoder.decodeUntilMatchingElseOrEnd()
	if err != nil {
		return err
	}

	if matchingOpcode == elseOp {
		// We need to consume the Else instruction and jump to the next "actual"
		// instruction after it.
		if _, err := frame.decoder.decode(); err != nil {
			return err
		}
	}

	frame.function.jumpElseCache[originalPc] = frame.decoder.pc
	return nil
}

func (vm *vm) handleElse() {
	callFrame := vm.currentCallFrame()
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame := vm.popControlFrame()
	callFrame.decoder.pc = ifFrame.continuationPc
}

func (vm *vm) handleEnd() {
	frame := vm.popControlFrame()
	vm.stack.unwind(frame.stackHeight, frame.outputCount)
}

func (vm *vm) handleBr(instruction instruction) {
	labelIndex := uint32(instruction.immediates[0])
	vm.brToLabel(labelIndex)
}

func (vm *vm) handleBrIf(instruction instruction) {
	labelIndex := uint32(instruction.immediates[0])
	val := vm.stack.popInt32()
	if val == 0 {
		return
	}
	vm.brToLabel(labelIndex)
}

func (vm *vm) handleBrTable(instruction instruction) {
	immediates := instruction.immediates
	table := immediates[:len(immediates)-1]
	defaultTarget := uint32(immediates[len(immediates)-1])
	index := vm.stack.popInt32()
	if index >= 0 && int(index) < len(table) {
		vm.brToLabel(uint32(table[index]))
	} else {
		vm.brToLabel(defaultTarget)
	}
}

func (vm *vm) brToLabel(labelIndex uint32) {
	callFrame := vm.currentCallFrame()

	var targetFrame *controlFrame
	for range int(labelIndex) + 1 {
		targetFrame = vm.popControlFrame()
	}

	var arity uint
	if targetFrame.opcode == loop {
		arity = targetFrame.inputCount
	} else {
		arity = targetFrame.outputCount
	}

	vm.stack.unwind(targetFrame.stackHeight, arity)
	if targetFrame.opcode == loop {
		vm.pushControlFrame(targetFrame)
	}

	callFrame.decoder.pc = targetFrame.continuationPc
}

func (vm *vm) handleCall(
	ctx context.Context,
	instruction instruction,
) error {
	localIndex := uint32(instruction.immediates[0])
	function := vm.getFunction(localIndex)
	res, err := vm.invokeFunction(ctx, function)
	if err != nil {
		return err
	}
	vm.stack.pushAll(res)
	return nil
}

func (vm *vm) handleCallIndirect(
	ctx context.Context,
	instruction instruction,
) error {
	typeIndex := uint32(instruction.immediates[0])
	tableIndex := uint32(instruction.immediates[1])

	expectedType := vm.currentModuleInstance().types[typeIndex]
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

	res, err := vm.invokeFunction(ctx, function)
	if err != nil {
		return err
	}
	vm.stack.pushAll(res)
	return nil
}

func (vm *vm) handleSelect() {
	condition := vm.stack.popInt32()
	val2 := vm.stack.pop()
	val1 := vm.stack.pop()
	if condition != 0 {
		vm.stack.push(val1)
	} else {
		vm.stack.push(val2)
	}
}

func (vm *vm) handleLocalGet(instruction instruction) {
	callFrame := vm.currentCallFrame()
	localIndex := int32(instruction.immediates[0])
	vm.stack.push(callFrame.locals[localIndex])
}

func (vm *vm) handleLocalSet(instruction instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.immediates[0])
	// We know, due to validation, the top of the stack is always the right type.
	frame.locals[localIndex] = vm.stack.pop()
}

func (vm *vm) handleLocalTee(instruction instruction) {
	frame := vm.currentCallFrame()
	localIndex := int32(instruction.immediates[0])
	// We know, due to validation, the top of the stack is always the right type.
	val := vm.stack.pop()
	frame.locals[localIndex] = val
	vm.stack.push(val)
}

func (vm *vm) handleGlobalGet(instruction instruction) {
	localIndex := uint32(instruction.immediates[0])
	global := vm.getGlobal(localIndex)
	vm.stack.push(global.Value)
}

func (vm *vm) handleGlobalSet(instruction instruction) {
	localIndex := uint32(instruction.immediates[0])
	global := vm.getGlobal(localIndex)
	global.Value = vm.stack.pop()
}

func (vm *vm) handleTableGet(instruction instruction) error {
	tableIndex := uint32(instruction.immediates[0])
	table := vm.getTable(tableIndex)
	index := vm.stack.popInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}
	vm.stack.push(element)
	return nil
}

func (vm *vm) handleTableSet(instruction instruction) error {
	tableIndex := uint32(instruction.immediates[0])
	table := vm.getTable(tableIndex)
	reference := vm.stack.pop()
	index := vm.stack.popInt32()
	return table.Set(index, reference)
}

func (vm *vm) handleMemorySize(instruction instruction) {
	memory := vm.getMemory(uint32(instruction.immediates[0]))
	vm.stack.push(memory.Size())
}

func (vm *vm) handleMemoryGrow(instruction instruction) {
	memory := vm.getMemory(uint32(instruction.immediates[0]))
	pages := vm.stack.popInt32()
	oldSize := memory.Grow(pages)
	vm.stack.push(oldSize)
}

func (vm *vm) handleRefFunc(instruction instruction) {
	funcIndex := uint32(instruction.immediates[0])
	storeIndex := vm.currentModuleInstance().funcAddrs[funcIndex]
	vm.stack.push(int32(storeIndex))
}

func (vm *vm) handleRefIsNull() {
	top := vm.stack.pop()
	_, topIsNull := top.(Null)
	vm.stack.push(boolToInt32(topIsNull))
}

func (vm *vm) handleMemoryInit(instruction instruction) error {
	data := vm.getData(uint32(instruction.immediates[0]))
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	n, s, d := vm.stack.pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.content)
}

func (vm *vm) handleDataDrop(instruction instruction) {
	dataSegment := vm.getData(uint32(instruction.immediates[0]))
	dataSegment.content = nil
}

func (vm *vm) handleMemoryCopy(instruction instruction) error {
	destMemory := vm.getMemory(uint32(instruction.immediates[0]))
	srcMemory := vm.getMemory(uint32(instruction.immediates[1]))
	n, s, d := vm.stack.pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *vm) handleMemoryFill(instruction instruction) error {
	memory := vm.getMemory(uint32(instruction.immediates[0]))
	n, val, offset := vm.stack.pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *vm) handleTableInit(instruction instruction) error {
	element := vm.getElement(uint32(instruction.immediates[0]))
	table := vm.getTable(uint32(instruction.immediates[1]))
	n, s, d := vm.stack.pop3Int32()

	switch element.mode {
	case activeElementMode:
		// Trap if using an active, non-dropped element segment.
		// A dropped segment has its FuncIndexes slice set to nil.
		if element.functionIndexes != nil {
			return errTableOutOfBounds
		}
		return table.Init(n, d, s, element.functionIndexes)
	case passiveElementMode:
		moduleInstance := vm.currentModuleInstance()
		storeIndexes := toStoreFuncIndexes(moduleInstance, element.functionIndexes)
		return table.Init(n, d, s, storeIndexes)
	default:
		return errTableOutOfBounds
	}
}

func (vm *vm) handleElemDrop(instruction instruction) {
	element := vm.getElement(uint32(instruction.immediates[0]))
	element.functionIndexes = nil
	element.functionIndexesExpressions = nil
}

func (vm *vm) handleTableCopy(instruction instruction) error {
	destTable := vm.getTable(uint32(instruction.immediates[0]))
	srcTable := vm.getTable(uint32(instruction.immediates[1]))
	n, s, d := vm.stack.pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *vm) handleTableGrow(instruction instruction) {
	table := vm.getTable(uint32(instruction.immediates[0]))
	n := vm.stack.popInt32()
	val := vm.stack.pop()
	vm.stack.push(table.Grow(n, val))
}

func (vm *vm) handleTableSize(instruction instruction) {
	table := vm.getTable(uint32(instruction.immediates[0]))
	vm.stack.push(table.Size())
}

func (vm *vm) handleTableFill(instruction instruction) error {
	table := vm.getTable(uint32(instruction.immediates[0]))
	n := vm.stack.popInt32()
	val := vm.stack.pop()
	i := vm.stack.popInt32()
	return table.Fill(n, i, val)
}

func (vm *vm) handleI8x16Shuffle(instruction instruction) {
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()

	var lanes [16]byte
	for i, imm := range instruction.immediates {
		lanes[i] = byte(imm)
	}

	vm.stack.push(simdI8x16Shuffle(v1, v2, lanes))
}

func handleBinary[T wasmNumber | V128Value, R wasmNumber | V128Value](
	vm *vm,
	pop func() T,
	op func(a, b T) R,
) {
	b := pop()
	a := pop()
	vm.stack.push(op(a, b))
}

func handleBinarySafe[T wasmNumber | V128Value, R wasmNumber | V128Value](
	vm *vm,
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

func handleBinaryBool[T wasmNumber](
	vm *vm,
	pop func() T,
	op func(a, b T) bool,
) {
	b := pop()
	a := pop()
	vm.stack.push(boolToInt32(op(a, b)))
}

func handleUnary[T wasmNumber | V128Value, R wasmNumber | V128Value](
	vm *vm,
	pop func() T,
	op func(a T) R,
) {
	vm.stack.push(op(pop()))
}

func handleUnarySafe[T wasmNumber | V128Value, R wasmNumber | V128Value](
	vm *vm,
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

func handleUnaryBool[T wasmNumber | V128Value](
	vm *vm,
	pop func() T,
	op func(a T) bool,
) {
	vm.stack.push(boolToInt32(op(pop())))
}

func (vm *vm) handleSimdShift(op func(v V128Value, shift int32) V128Value) {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.push(op(v, shift))
}

func (vm *vm) handleSimdTernary(op func(v1, v2, v3 V128Value) V128Value) {
	v3 := vm.stack.popV128()
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()
	vm.stack.push(op(v1, v2, v3))
}

func handleSimdExtractLane[R wasmNumber](
	vm *vm,
	instruction instruction,
	op func(v V128Value, laneIndex uint32) R,
) {
	laneIndex := uint32(instruction.immediates[0])
	v := vm.stack.popV128()
	vm.stack.push(op(v, laneIndex))
}

func handleStore[T any](
	vm *vm,
	instruction instruction,
	val T,
	store func(*Memory, uint32, uint32, T) error,
) error {
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	offset := uint32(instruction.immediates[2])
	index := uint32(vm.stack.popInt32())
	return store(memory, offset, index, val)
}

func handleLoad[T any, R any](
	vm *vm,
	instruction instruction,
	load func(*Memory, uint32, uint32) (T, error),
	convert func(T) R,
) error {
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	offset := uint32(instruction.immediates[2])
	index := uint32(vm.stack.popInt32())
	v, err := load(memory, offset, index)
	if err != nil {
		return err
	}
	vm.stack.push(convert(v))
	return nil
}

func (vm *vm) handleLoadV128FromBytes(
	instruction instruction,
	fromBytes func(bytes []byte) V128Value,
	sizeBytes uint32,
) error {
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	offset := uint32(instruction.immediates[2])
	index := vm.stack.popInt32()

	data, err := memory.Get(offset, uint32(index), sizeBytes)
	if err != nil {
		return err
	}
	vm.stack.push(fromBytes(data))
	return nil
}

func (vm *vm) handleSimdConst(instruction instruction) {
	v := V128Value{
		Low:  instruction.immediates[0],
		High: instruction.immediates[1],
	}
	vm.stack.push(v)
}

func (vm *vm) handleSimdLoadLane(
	instruction instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.immediates[2])
	laneIndex := uint32(instruction.immediates[3])
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return nil
	}

	vm.stack.push(setLane(v, laneIndex, laneValue))
	return nil
}

func (vm *vm) handleSimdStoreLane(
	instruction instruction,
	laneSize uint32,
) error {
	offset := uint32(instruction.immediates[2])
	laneIndex := uint32(instruction.immediates[3])
	memory := vm.getMemory(uint32(instruction.immediates[1]))
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneData := extractLane(v, laneSize, laneIndex)
	return memory.Set(offset, uint32(index), laneData)
}

func handleSimdReplaceLane[T wasmNumber](
	vm *vm,
	instruction instruction,
	pop func() T,
	replaceLane func(V128Value, uint32, T) V128Value,
) {
	laneIndex := uint32(instruction.immediates[0])
	laneValue := pop()
	vector := vm.stack.popV128()
	vm.stack.push(replaceLane(vector, laneIndex, laneValue))
}

func (vm *vm) getBlockInputOutputCount(blockType int32) (uint, uint) {
	if blockType == -0x40 { // empty block type.
		return 0, 0
	}

	if blockType >= 0 { // type index.
		funcType := vm.currentModuleInstance().types[blockType]
		return uint(len(funcType.ParamTypes)), uint(len(funcType.ResultTypes))
	}

	return 0, 1 // value type.
}

func getExport(
	module *ModuleInstance,
	name string,
	indexType exportIndexKind,
) (any, error) {
	for _, export := range module.exports {
		if export.name != name {
			continue
		}

		switch indexType {
		case functionExportKind:
			function, ok := export.value.(FunctionInstance)
			if !ok {
				return nil, fmt.Errorf("export %s is not a function", name)
			}
			return function, nil
		case globalExportKind:
			global, ok := export.value.(*Global)
			if !ok {
				return nil, fmt.Errorf("export %s is not a global", name)
			}
			return global, nil
		case memoryExportKind:
			memory, ok := export.value.(*Memory)
			if !ok {
				return nil, fmt.Errorf("export %s is not a memory", name)
			}
			return memory, nil
		case tableExportKind:
			table, ok := export.value.(*Table)
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

func (vm *vm) pushControlFrame(frame *controlFrame) {
	callFrame := vm.currentCallFrame()
	callFrame.controlStack = append(callFrame.controlStack, frame)
}

func (vm *vm) popControlFrame() *controlFrame {
	callFrame := vm.currentCallFrame()
	// Validation guarantees the control stack is never empty.
	index := len(callFrame.controlStack) - 1
	frame := callFrame.controlStack[index]
	callFrame.controlStack = callFrame.controlStack[:index]
	return frame
}

func (vm *vm) initActiveElements(
	ctx context.Context,
	module *moduleDefinition,
	moduleInstance *ModuleInstance,
) error {
	for _, element := range module.elementSegments {
		if element.mode != activeElementMode {
			continue
		}

		expression := element.offsetExpression
		offsetAny, err := vm.invokeInitExpression(ctx, expression, I32, moduleInstance)
		if err != nil {
			return err
		}
		offset := offsetAny.(int32)

		storeTableIndex := moduleInstance.tableAddrs[element.tableIndex]
		table := vm.store.tables[storeTableIndex]
		if offset > int32(table.Size()) {
			return errTableOutOfBounds
		}

		if len(element.functionIndexes) > 0 {
			indexes := toStoreFuncIndexes(moduleInstance, element.functionIndexes)
			if err := table.InitFromSlice(offset, indexes); err != nil {
				return err
			}
		}

		if len(element.functionIndexesExpressions) > 0 {
			values := make([]any, len(element.functionIndexesExpressions))
			for i, expr := range element.functionIndexesExpressions {
				refAny, err := vm.invokeInitExpression(
					ctx,
					expr,
					element.kind,
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

func (vm *vm) initActiveDatas(
	ctx context.Context,
	module *moduleDefinition,
	moduleInstance *ModuleInstance,
) error {
	for _, segment := range module.dataSegments {
		if segment.mode != activeDataMode {
			continue
		}

		expression := segment.offsetExpression
		offsetAny, err := vm.invokeInitExpression(
			ctx,
			expression,
			I32,
			moduleInstance,
		)
		if err != nil {
			return err
		}
		offset := offsetAny.(int32)
		storeMemoryIndex := moduleInstance.memAddrs[segment.memoryIndex]
		memory := vm.store.memories[storeMemoryIndex]
		if err := memory.Set(uint32(offset), 0, segment.content); err != nil {
			return err
		}
	}

	return nil
}

func (vm *vm) resolveExports(
	module *moduleDefinition,
	instance *ModuleInstance,
) []exportInstance {
	exports := []exportInstance{}
	for _, export := range module.exports {
		var value any
		switch export.indexType {
		case functionExportKind:
			storeIndex := instance.funcAddrs[export.index]
			value = vm.store.funcs[storeIndex]
		case globalExportKind:
			storeIndex := instance.globalAddrs[export.index]
			value = vm.store.globals[storeIndex]
		case memoryExportKind:
			storeIndex := instance.memAddrs[export.index]
			value = vm.store.memories[storeIndex]
		case tableExportKind:
			storeIndex := instance.tableAddrs[export.index]
			value = vm.store.tables[storeIndex]
		}
		exports = append(exports, exportInstance{name: export.name, value: value})
	}
	return exports
}

func (vm *vm) invokeHostFunction(
	ctx context.Context,
	fun *hostFunction,
) (res []any, err error) {
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
	res = fun.hostCode(args...)
	return res, nil
}

func (vm *vm) invokeInitExpression(
	ctx context.Context,
	expression []byte,
	resultType ValueType,
	moduleInstance *ModuleInstance,
) (any, error) {
	// We create a fake function to execute the expression.
	// The expression is expected to return a single value.
	function := wasmFunction{
		functionType: FunctionType{
			ParamTypes:  []ValueType{},
			ResultTypes: []ValueType{resultType},
		},
		code:   function{body: expression},
		module: moduleInstance,
	}
	results, err := vm.invokeWasmFunction(ctx, &function)
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
		storeIndices[i] = int32(moduleInstance.funcAddrs[localIndex])
	}
	return storeIndices
}

func defaultValue(vt ValueType) any {
	switch vt {
	case I32:
		return int32(0)
	case I64:
		return int64(0)
	case F32:
		return float32(0)
	case F64:
		return float64(0)
	case V128:
		return V128Value{}
	case FuncRefType, ExternRefType:
		return NullVal
	default:
		// Should ideally not be reached with a valid module.
		return nil
	}
}

func (vm *vm) getFunction(localIndex uint32) FunctionInstance {
	functionIndex := vm.currentModuleInstance().funcAddrs[localIndex]
	return vm.store.funcs[functionIndex]
}

func (vm *vm) getTable(localIndex uint32) *Table {
	tableIndex := vm.currentModuleInstance().tableAddrs[localIndex]
	return vm.store.tables[tableIndex]
}

func (vm *vm) getMemory(localIndex uint32) *Memory {
	memoryIndex := vm.currentModuleInstance().memAddrs[localIndex]
	return vm.store.memories[memoryIndex]
}

func (vm *vm) getGlobal(localIndex uint32) *Global {
	globalIndex := vm.currentModuleInstance().globalAddrs[localIndex]
	return vm.store.globals[globalIndex]
}

func (vm *vm) getElement(localIndex uint32) *elementSegment {
	elementIndex := vm.currentModuleInstance().dlemAddrs[localIndex]
	return &vm.store.elements[elementIndex]
}

func (vm *vm) getData(localIndex uint32) *dataSegment {
	dataIndex := vm.currentModuleInstance().dataAddrs[localIndex]
	return &vm.store.datas[dataIndex]
}
