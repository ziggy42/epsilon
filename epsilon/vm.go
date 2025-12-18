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

const (
	maxCallStackDepth         = 1000
	controlStackCacheSlotSize = 14 // Control stack slot size per call frame.
	controlStackCacheSize     = maxCallStackDepth * controlStackCacheSlotSize
	localsCacheSlotSize       = 12 // Locals slot size per call frame.
	localsCacheSize           = maxCallStackDepth * localsCacheSlotSize
)

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
	code         []uint64
	pc           uint32
	controlStack []controlFrame
	locals       []value
	function     *wasmFunction
}

func (f *callFrame) next() uint64 {
	val := f.code[f.pc]
	f.pc++
	return val
}

// controlFrame represents a block of code that can be branched to.
type controlFrame struct {
	isLoop         bool
	blockType      int32
	continuationPc uint32 // The address to jump to when `br` targets this frame.
	stackHeight    uint32
}

// vm is the WebAssembly Virtual Machine.
type vm struct {
	store             *store
	stack             *valueStack
	callStack         []callFrame
	controlStackCache [controlStackCacheSize]controlFrame
	localsCache       [localsCacheSize]value
	features          ExperimentalFeatures
}

func newVm() *vm {
	return &vm{
		store:     &store{},
		stack:     newValueStack(),
		callStack: make([]callFrame, 0, maxCallStackDepth),
	}
}

func (vm *vm) instantiate(
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
		wasmFunc := &wasmFunction{
			functionType: funType,
			module:       moduleInstance,
			code:         function,
		}
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
			value:   val,
			Mutable: global.globalType.IsMutable,
			Type:    global.globalType.ValueType,
		})
	}

	// TODO: elements and data segments should at the very least be copied, but we
	// should probably have some runtime representation for them.
	for _, elem := range module.elementSegments {
		storeIndex := uint32(len(vm.store.elements))
		moduleInstance.elemAddrs = append(moduleInstance.elemAddrs, storeIndex)
		vm.store.elements = append(vm.store.elements, elem)
	}

	for _, data := range module.dataSegments {
		storeIndex := uint32(len(vm.store.datas))
		moduleInstance.dataAddrs = append(moduleInstance.dataAddrs, storeIndex)
		vm.store.datas = append(vm.store.datas, data)
	}

	if err := vm.initActiveElements(module, moduleInstance); err != nil {
		return nil, err
	}

	if err := vm.initActiveDatas(module, moduleInstance); err != nil {
		return nil, err
	}

	if module.startIndex != nil {
		storeFunctionIndex := moduleInstance.funcAddrs[*module.startIndex]
		function := vm.store.funcs[storeFunctionIndex]
		if err := vm.invokeFunction(function); err != nil {
			return nil, err
		}
	}

	moduleInstance.exports = vm.resolveExports(module, moduleInstance)
	return moduleInstance, nil
}

func (vm *vm) invoke(function FunctionInstance, args []any) ([]any, error) {
	vm.stack.pushAll(args)
	if err := vm.invokeFunction(function); err != nil {
		return nil, err
	}
	return vm.stack.popValueTypes(function.GetType().ResultTypes), nil
}

func (vm *vm) invokeFunction(function FunctionInstance) error {
	switch f := function.(type) {
	case *wasmFunction:
		return vm.invokeWasmFunction(f)
	case *hostFunction:
		return vm.invokeHostFunction(f)
	default:
		return fmt.Errorf("unknown function type")
	}
}

func (vm *vm) invokeWasmFunction(function *wasmFunction) error {
	if len(vm.callStack) >= maxCallStackDepth {
		return errCallStackExhausted
	}

	numParams := len(function.functionType.ParamTypes)
	numLocals := numParams + len(function.code.locals)
	var locals []value
	if numLocals <= localsCacheSlotSize {
		blockDepth := len(vm.callStack) * localsCacheSlotSize
		max := blockDepth + localsCacheSlotSize
		locals = vm.localsCache[blockDepth : blockDepth+numLocals : max]
		// Clear non-parameter locals to their zero values. WASM allows reading
		// uninitialized locals, so we must zero them to avoid reusing stale values
		// from previous invocations. Parameters are overwritten below.
		clear(locals[numParams:])
	} else {
		// The cache is not large enough to fit the amount of locals for the current
		// function, therefore we need a new allocation.
		locals = make([]value, numLocals)
	}

	// Copy params and shrink stack by operating on the underlying slice directly.
	newLen := len(vm.stack.data) - numParams
	copy(locals[:numParams], vm.stack.data[newLen:])
	vm.stack.data = vm.stack.data[:newLen]

	// Use part of the cache for the control stack to avoid allocations.
	blockDepth := len(vm.callStack) * controlStackCacheSlotSize
	// Slice cap to prevent appending into the next slot.
	max := blockDepth + controlStackCacheSlotSize
	controlStack := vm.controlStackCache[blockDepth:blockDepth:max]
	controlStack = append(controlStack, controlFrame{
		isLoop:         false,
		blockType:      int32(function.code.typeIndex),
		continuationPc: uint32(len(function.code.body)),
		stackHeight:    vm.stack.size(),
	})

	vm.callStack = append(vm.callStack, callFrame{
		code:         function.code.body,
		pc:           0,
		controlStack: controlStack,
		locals:       locals,
		function:     function,
	})

	for {
		// We must re-fetch the frame pointer on each iteration because nested calls
		// may append to vm.callStack, causing the slice to reallocate and
		// invalidate any previously held pointers. This pointer is safe to use
		// within a single instruction execution since no handler uses it after
		// invoking a nested call.
		frame := &vm.callStack[len(vm.callStack)-1]
		if frame.pc >= uint32(len(frame.code)) {
			break
		}
		if err := vm.executeInstruction(frame); err != nil {
			if errors.Is(err, errReturn) {
				break // A 'return' instruction was executed.
			}
			// Ensure we pop the stack frame even if executeInstruction fails.
			vm.callStack = vm.callStack[:len(vm.callStack)-1]
			return err
		}
	}

	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	return nil
}

func (vm *vm) executeInstruction(frame *callFrame) error {
	op := opcode(frame.next())
	var err error
	// Using a switch instead of a map of opcode -> Handler is significantly
	// faster.
	switch op {
	case unreachable:
		err = errUnreachable
	case nop:
		// Do nothing.
	case block, loop:
		vm.pushBlockFrame(op, int32(frame.next()))
	case ifOp:
		vm.handleIf(frame)
	case elseOp:
		vm.handleElse(frame)
	case end:
		vm.handleEnd()
	case br:
		vm.brToLabel(uint32(frame.next()))
	case brIf:
		vm.handleBrIf(frame)
	case brTable:
		vm.handleBrTable(frame)
	case returnOp:
		err = errReturn
	case call:
		err = vm.handleCall(frame)
	case callIndirect:
		err = vm.handleCallIndirect(frame)
	case drop:
		vm.stack.drop()
	case selectOp:
		vm.handleSelect()
	case selectT:
		count := frame.next()
		frame.pc += uint32(count)
		vm.handleSelect()
	case localGet:
		vm.stack.push(frame.locals[frame.next()])
	case localSet:
		frame.locals[frame.next()] = vm.stack.pop()
	case localTee:
		frame.locals[frame.next()] = vm.stack.data[len(vm.stack.data)-1]
	case globalGet:
		vm.handleGlobalGet(frame)
	case globalSet:
		vm.handleGlobalSet(frame)
	case tableGet:
		err = vm.handleTableGet(frame)
	case tableSet:
		err = vm.handleTableSet(frame)
	case i32Load:
		err = handleLoad(vm, frame, vm.stack.pushInt32, (*Memory).LoadUint32, uint32ToInt32)
	case i64Load:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadUint64, uint64ToInt64)
	case f32Load:
		err = handleLoad(vm, frame, vm.stack.pushFloat32, (*Memory).LoadUint32, math.Float32frombits)
	case f64Load:
		err = handleLoad(vm, frame, vm.stack.pushFloat64, (*Memory).LoadUint64, math.Float64frombits)
	case i32Load8S:
		err = handleLoad(vm, frame, vm.stack.pushInt32, (*Memory).LoadByte, signExtend8To32)
	case i32Load8U:
		err = handleLoad(vm, frame, vm.stack.pushInt32, (*Memory).LoadByte, zeroExtend8To32)
	case i32Load16S:
		err = handleLoad(vm, frame, vm.stack.pushInt32, (*Memory).LoadUint16, signExtend16To32)
	case i32Load16U:
		err = handleLoad(vm, frame, vm.stack.pushInt32, (*Memory).LoadUint16, zeroExtend16To32)
	case i64Load8S:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadByte, signExtend8To64)
	case i64Load8U:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadByte, zeroExtend8To64)
	case i64Load16S:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadUint16, signExtend16To64)
	case i64Load16U:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadUint16, zeroExtend16To64)
	case i64Load32S:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadUint32, signExtend32To64)
	case i64Load32U:
		err = handleLoad(vm, frame, vm.stack.pushInt64, (*Memory).LoadUint32, zeroExtend32To64)
	case i32Store:
		err = handleStore(vm, frame, uint32(vm.stack.popInt32()), (*Memory).StoreUint32)
	case i64Store:
		err = handleStore(vm, frame, uint64(vm.stack.popInt64()), (*Memory).StoreUint64)
	case f32Store:
		err = handleStore(vm, frame, math.Float32bits(vm.stack.popFloat32()), (*Memory).StoreUint32)
	case f64Store:
		err = handleStore(vm, frame, math.Float64bits(vm.stack.popFloat64()), (*Memory).StoreUint64)
	case i32Store8:
		err = handleStore(vm, frame, byte(vm.stack.popInt32()), (*Memory).StoreByte)
	case i32Store16:
		err = handleStore(vm, frame, uint16(vm.stack.popInt32()), (*Memory).StoreUint16)
	case i64Store8:
		err = handleStore(vm, frame, byte(vm.stack.popInt64()), (*Memory).StoreByte)
	case i64Store16:
		err = handleStore(vm, frame, uint16(vm.stack.popInt64()), (*Memory).StoreUint16)
	case i64Store32:
		err = handleStore(vm, frame, uint32(vm.stack.popInt64()), (*Memory).StoreUint32)
	case memorySize:
		vm.handleMemorySize(frame)
	case memoryGrow:
		vm.handleMemoryGrow(frame)
	case i32Const:
		vm.stack.pushInt32(int32(frame.next()))
	case i64Const:
		vm.stack.pushInt64(int64(frame.next()))
	case f32Const:
		vm.stack.pushFloat32(math.Float32frombits(uint32(frame.next())))
	case f64Const:
		vm.stack.pushFloat64(math.Float64frombits(frame.next()))
	case i32Eqz:
		vm.stack.pushInt32(boolToInt32(vm.stack.popInt32() == 0))
	case i32Eq:
		vm.handleBinaryBoolInt32(equal)
	case i32Ne:
		vm.handleBinaryBoolInt32(notEqual)
	case i32LtS:
		vm.handleBinaryBoolInt32(lessThan)
	case i32LtU:
		vm.handleBinaryBoolInt32(lessThanU32)
	case i32GtS:
		vm.handleBinaryBoolInt32(greaterThan)
	case i32GtU:
		vm.handleBinaryBoolInt32(greaterThanU32)
	case i32LeS:
		vm.handleBinaryBoolInt32(lessOrEqual)
	case i32LeU:
		vm.handleBinaryBoolInt32(lessOrEqualU32)
	case i32GeS:
		vm.handleBinaryBoolInt32(greaterOrEqual)
	case i32GeU:
		vm.handleBinaryBoolInt32(greaterOrEqualU32)
	case i64Eqz:
		vm.stack.pushInt32(boolToInt32(vm.stack.popInt64() == 0))
	case i64Eq:
		vm.handleBinaryBoolInt64(equal)
	case i64Ne:
		vm.handleBinaryBoolInt64(notEqual)
	case i64LtS:
		vm.handleBinaryBoolInt64(lessThan)
	case i64LtU:
		vm.handleBinaryBoolInt64(lessThanU64)
	case i64GtS:
		vm.handleBinaryBoolInt64(greaterThan)
	case i64GtU:
		vm.handleBinaryBoolInt64(greaterThanU64)
	case i64LeS:
		vm.handleBinaryBoolInt64(lessOrEqual)
	case i64LeU:
		vm.handleBinaryBoolInt64(lessOrEqualU64)
	case i64GeS:
		vm.handleBinaryBoolInt64(greaterOrEqual)
	case i64GeU:
		vm.handleBinaryBoolInt64(greaterOrEqualU64)
	case f32Eq:
		vm.handleBinaryBoolFloat32(equal)
	case f32Ne:
		vm.handleBinaryBoolFloat32(notEqual)
	case f32Lt:
		vm.handleBinaryBoolFloat32(lessThan)
	case f32Gt:
		vm.handleBinaryBoolFloat32(greaterThan)
	case f32Le:
		vm.handleBinaryBoolFloat32(lessOrEqual)
	case f32Ge:
		vm.handleBinaryBoolFloat32(greaterOrEqual)
	case f64Eq:
		vm.handleBinaryBoolFloat64(equal)
	case f64Ne:
		vm.handleBinaryBoolFloat64(notEqual)
	case f64Lt:
		vm.handleBinaryBoolFloat64(lessThan)
	case f64Gt:
		vm.handleBinaryBoolFloat64(greaterThan)
	case f64Le:
		vm.handleBinaryBoolFloat64(lessOrEqual)
	case f64Ge:
		vm.handleBinaryBoolFloat64(greaterOrEqual)
	case i32Clz:
		vm.stack.pushInt32(clz32(vm.stack.popInt32()))
	case i32Ctz:
		vm.stack.pushInt32(ctz32(vm.stack.popInt32()))
	case i32Popcnt:
		vm.stack.pushInt32(popcnt32(vm.stack.popInt32()))
	case i32Add:
		vm.handleBinaryInt32(add)
	case i32Sub:
		vm.handleBinaryInt32(sub)
	case i32Mul:
		vm.handleBinaryInt32(mul)
	case i32DivS:
		err = vm.handleBinarySafeInt32(divS32)
	case i32DivU:
		err = vm.handleBinarySafeInt32(divU32)
	case i32RemS:
		err = vm.handleBinarySafeInt32(remS32)
	case i32RemU:
		err = vm.handleBinarySafeInt32(remU32)
	case i32And:
		vm.handleBinaryInt32(and)
	case i32Or:
		vm.handleBinaryInt32(or)
	case i32Xor:
		vm.handleBinaryInt32(xor)
	case i32Shl:
		vm.handleBinaryInt32(shl32)
	case i32ShrS:
		vm.handleBinaryInt32(shrS32)
	case i32ShrU:
		vm.handleBinaryInt32(shrU32)
	case i32Rotl:
		vm.handleBinaryInt32(rotl32)
	case i32Rotr:
		vm.handleBinaryInt32(rotr32)
	case i64Clz:
		vm.stack.pushInt64(clz64(vm.stack.popInt64()))
	case i64Ctz:
		vm.stack.pushInt64(ctz64(vm.stack.popInt64()))
	case i64Popcnt:
		vm.stack.pushInt64(popcnt64(vm.stack.popInt64()))
	case i64Add:
		vm.handleBinaryInt64(add)
	case i64Sub:
		vm.handleBinaryInt64(sub)
	case i64Mul:
		vm.handleBinaryInt64(mul)
	case i64DivS:
		err = vm.handleBinarySafeInt64(divS64)
	case i64DivU:
		err = vm.handleBinarySafeInt64(divU64)
	case i64RemS:
		err = vm.handleBinarySafeInt64(remS64)
	case i64RemU:
		err = vm.handleBinarySafeInt64(remU64)
	case i64And:
		vm.handleBinaryInt64(and)
	case i64Or:
		vm.handleBinaryInt64(or)
	case i64Xor:
		vm.handleBinaryInt64(xor)
	case i64Shl:
		vm.handleBinaryInt64(shl64)
	case i64ShrS:
		vm.handleBinaryInt64(shrS64)
	case i64ShrU:
		vm.handleBinaryInt64(shrU64)
	case i64Rotl:
		vm.handleBinaryInt64(rotl64)
	case i64Rotr:
		vm.handleBinaryInt64(rotr64)
	case f32Abs:
		vm.stack.pushFloat32(abs(vm.stack.popFloat32()))
	case f32Neg:
		vm.stack.pushFloat32(-vm.stack.popFloat32())
	case f32Ceil:
		vm.stack.pushFloat32(ceil(vm.stack.popFloat32()))
	case f32Floor:
		vm.stack.pushFloat32(floor(vm.stack.popFloat32()))
	case f32Trunc:
		vm.stack.pushFloat32(trunc(vm.stack.popFloat32()))
	case f32Nearest:
		vm.stack.pushFloat32(nearest(vm.stack.popFloat32()))
	case f32Sqrt:
		vm.stack.pushFloat32(sqrt(vm.stack.popFloat32()))
	case f32Add:
		vm.handleBinaryFloat32(add[float32])
	case f32Sub:
		vm.handleBinaryFloat32(sub[float32])
	case f32Mul:
		vm.handleBinaryFloat32(mul[float32])
	case f32Div:
		vm.handleBinaryFloat32(div[float32])
	case f32Min:
		vm.handleBinaryFloat32(wasmMin[float32])
	case f32Max:
		vm.handleBinaryFloat32(wasmMax[float32])
	case f32Copysign:
		vm.handleBinaryFloat32(copysign[float32])
	case f64Abs:
		vm.stack.pushFloat64(abs(vm.stack.popFloat64()))
	case f64Neg:
		vm.stack.pushFloat64(-vm.stack.popFloat64())
	case f64Ceil:
		vm.stack.pushFloat64(ceil(vm.stack.popFloat64()))
	case f64Floor:
		vm.stack.pushFloat64(floor(vm.stack.popFloat64()))
	case f64Trunc:
		vm.stack.pushFloat64(trunc(vm.stack.popFloat64()))
	case f64Nearest:
		vm.stack.pushFloat64(nearest(vm.stack.popFloat64()))
	case f64Sqrt:
		vm.stack.pushFloat64(sqrt(vm.stack.popFloat64()))
	case f64Add:
		vm.handleBinaryFloat64(add[float64])
	case f64Sub:
		vm.handleBinaryFloat64(sub[float64])
	case f64Mul:
		vm.handleBinaryFloat64(mul[float64])
	case f64Div:
		vm.handleBinaryFloat64(div[float64])
	case f64Min:
		vm.handleBinaryFloat64(wasmMin[float64])
	case f64Max:
		vm.handleBinaryFloat64(wasmMax[float64])
	case f64Copysign:
		vm.handleBinaryFloat64(copysign[float64])
	case i32WrapI64:
		vm.stack.pushInt32(wrapI64ToI32(vm.stack.popInt64()))
	case i32TruncF32S:
		err = vm.handleUnarySafeFloat32(truncF32SToI32)
	case i32TruncF32U:
		err = vm.handleUnarySafeFloat32(truncF32UToI32)
	case i32TruncF64S:
		err = vm.handleUnarySafeFloat64(truncF64SToI32)
	case i32TruncF64U:
		err = vm.handleUnarySafeFloat64(truncF64UToI32)
	case i64ExtendI32S:
		vm.stack.pushInt64(extendI32SToI64(vm.stack.popInt32()))
	case i64ExtendI32U:
		vm.stack.pushInt64(extendI32UToI64(vm.stack.popInt32()))
	case i64TruncF32S:
		err = vm.handleTruncFloat32Int64(truncF32SToI64)
	case i64TruncF32U:
		err = vm.handleTruncFloat32Int64(truncF32UToI64)
	case i64TruncF64S:
		err = vm.handleTruncFloat64Int64(truncF64SToI64)
	case i64TruncF64U:
		err = vm.handleTruncFloat64Int64(truncF64UToI64)
	case f32ConvertI32S:
		vm.stack.pushFloat32(convertI32SToF32(vm.stack.popInt32()))
	case f32ConvertI32U:
		vm.stack.pushFloat32(convertI32UToF32(vm.stack.popInt32()))
	case f32ConvertI64S:
		vm.stack.pushFloat32(convertI64SToF32(vm.stack.popInt64()))
	case f32ConvertI64U:
		vm.stack.pushFloat32(convertI64UToF32(vm.stack.popInt64()))
	case f32DemoteF64:
		vm.stack.pushFloat32(demoteF64ToF32(vm.stack.popFloat64()))
	case f64ConvertI32S:
		vm.stack.pushFloat64(convertI32SToF64(vm.stack.popInt32()))
	case f64ConvertI32U:
		vm.stack.pushFloat64(convertI32UToF64(vm.stack.popInt32()))
	case f64ConvertI64S:
		vm.stack.pushFloat64(convertI64SToF64(vm.stack.popInt64()))
	case f64ConvertI64U:
		vm.stack.pushFloat64(convertI64UToF64(vm.stack.popInt64()))
	case f64PromoteF32:
		vm.stack.pushFloat64(promoteF32ToF64(vm.stack.popFloat32()))
	case i32ReinterpretF32:
		vm.stack.pushInt32(reinterpretF32ToI32(vm.stack.popFloat32()))
	case i64ReinterpretF64:
		vm.stack.pushInt64(reinterpretF64ToI64(vm.stack.popFloat64()))
	case f32ReinterpretI32:
		vm.stack.pushFloat32(reinterpretI32ToF32(vm.stack.popInt32()))
	case f64ReinterpretI64:
		vm.stack.pushFloat64(reinterpretI64ToF64(vm.stack.popInt64()))
	case i32Extend8S:
		vm.stack.pushInt32(extend8STo32(vm.stack.popInt32()))
	case i32Extend16S:
		vm.stack.pushInt32(extend16STo32(vm.stack.popInt32()))
	case i64Extend8S:
		vm.stack.pushInt64(extend8STo64(vm.stack.popInt64()))
	case i64Extend16S:
		vm.stack.pushInt64(extend16STo64(vm.stack.popInt64()))
	case i64Extend32S:
		vm.stack.pushInt64(extend32STo64(vm.stack.popInt64()))
	case refNull:
		frame.next() // type immediate
		vm.stack.pushInt32(NullReference)
	case refIsNull:
		vm.handleRefIsNull()
	case refFunc:
		vm.handleRefFunc(frame)
	case i32TruncSatF32S:
		vm.stack.pushInt32(truncSatF32SToI32(vm.stack.popFloat32()))
	case i32TruncSatF32U:
		vm.stack.pushInt32(truncSatF32UToI32(vm.stack.popFloat32()))
	case i32TruncSatF64S:
		vm.stack.pushInt32(truncSatF64SToI32(vm.stack.popFloat64()))
	case i32TruncSatF64U:
		vm.stack.pushInt32(truncSatF64UToI32(vm.stack.popFloat64()))
	case i64TruncSatF32S:
		vm.stack.pushInt64(truncSatF32SToI64(vm.stack.popFloat32()))
	case i64TruncSatF32U:
		vm.stack.pushInt64(truncSatF32UToI64(vm.stack.popFloat32()))
	case i64TruncSatF64S:
		vm.stack.pushInt64(truncSatF64SToI64(vm.stack.popFloat64()))
	case i64TruncSatF64U:
		vm.stack.pushInt64(truncSatF64UToI64(vm.stack.popFloat64()))
	case memoryInit:
		err = vm.handleMemoryInit(frame)
	case dataDrop:
		vm.handleDataDrop(frame)
	case memoryCopy:
		err = vm.handleMemoryCopy(frame)
	case memoryFill:
		err = vm.handleMemoryFill(frame)
	case tableInit:
		err = vm.handleTableInit(frame)
	case elemDrop:
		vm.handleElemDrop(frame)
	case tableCopy:
		err = vm.handleTableCopy(frame)
	case tableGrow:
		vm.handleTableGrow(frame)
	case tableSize:
		vm.handleTableSize(frame)
	case tableFill:
		err = vm.handleTableFill(frame)
	case v128Load:
		err = handleLoad(vm, frame, vm.stack.pushV128, (*Memory).LoadV128, identityV128)
	case v128Load8x8S:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load8x8S, 8)
	case v128Load8x8U:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load8x8U, 8)
	case v128Load16x4S:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load16x4S, 8)
	case v128Load16x4U:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load16x4U, 8)
	case v128Load32x2S:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load32x2S, 8)
	case v128Load32x2U:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load32x2U, 8)
	case v128Load8Splat:
		err = vm.handleLoadV128FromBytes(frame, simdI8x16SplatFromBytes, 1)
	case v128Load16Splat:
		err = vm.handleLoadV128FromBytes(frame, simdI16x8SplatFromBytes, 2)
	case v128Load32Splat:
		err = vm.handleLoadV128FromBytes(frame, simdI32x4SplatFromBytes, 4)
	case v128Load64Splat:
		err = vm.handleLoadV128FromBytes(frame, simdI64x2SplatFromBytes, 8)
	case v128Store:
		err = handleStore(vm, frame, vm.stack.popV128(), (*Memory).StoreV128)
	case v128Const:
		vm.stack.pushV128(V128Value{Low: frame.next(), High: frame.next()})
	case i8x16Shuffle:
		vm.handleI8x16Shuffle(frame)
	case i8x16Swizzle:
		vm.handleBinaryV128(simdI8x16Swizzle)
	case i8x16Splat:
		vm.stack.pushV128(simdI8x16Splat(vm.stack.popInt32()))
	case i16x8Splat:
		vm.stack.pushV128(simdI16x8Splat(vm.stack.popInt32()))
	case i32x4Splat:
		vm.stack.pushV128(simdI32x4Splat(vm.stack.popInt32()))
	case i64x2Splat:
		vm.stack.pushV128(simdI64x2Splat(vm.stack.popInt64()))
	case f32x4Splat:
		vm.stack.pushV128(simdF32x4Splat(vm.stack.popFloat32()))
	case f64x2Splat:
		vm.stack.pushV128(simdF64x2Splat(vm.stack.popFloat64()))
	case i8x16ExtractLaneS:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt32, simdI8x16ExtractLaneS)
	case i8x16ExtractLaneU:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt32, simdI8x16ExtractLaneU)
	case i8x16ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popInt32, simdI8x16ReplaceLane)
	case i16x8ExtractLaneS:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt32, simdI16x8ExtractLaneS)
	case i16x8ExtractLaneU:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt32, simdI16x8ExtractLaneU)
	case i16x8ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popInt32, simdI16x8ReplaceLane)
	case i32x4ExtractLane:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt32, simdI32x4ExtractLane)
	case i32x4ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popInt32, simdI32x4ReplaceLane)
	case i64x2ExtractLane:
		handleSimdExtractLane(vm, frame, vm.stack.pushInt64, simdI64x2ExtractLane)
	case i64x2ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popInt64, simdI64x2ReplaceLane)
	case f32x4ExtractLane:
		handleSimdExtractLane(vm, frame, vm.stack.pushFloat32, simdF32x4ExtractLane)
	case f32x4ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popFloat32, simdF32x4ReplaceLane)
	case f64x2ExtractLane:
		handleSimdExtractLane(vm, frame, vm.stack.pushFloat64, simdF64x2ExtractLane)
	case f64x2ReplaceLane:
		handleSimdReplaceLane(vm, frame, vm.stack.popFloat64, simdF64x2ReplaceLane)
	case i8x16Eq:
		vm.handleBinaryV128(simdI8x16Eq)
	case i8x16Ne:
		vm.handleBinaryV128(simdI8x16Ne)
	case i8x16LtS:
		vm.handleBinaryV128(simdI8x16LtS)
	case i8x16LtU:
		vm.handleBinaryV128(simdI8x16LtU)
	case i8x16GtS:
		vm.handleBinaryV128(simdI8x16GtS)
	case i8x16GtU:
		vm.handleBinaryV128(simdI8x16GtU)
	case i8x16LeS:
		vm.handleBinaryV128(simdI8x16LeS)
	case i8x16LeU:
		vm.handleBinaryV128(simdI8x16LeU)
	case i8x16GeS:
		vm.handleBinaryV128(simdI8x16GeS)
	case i8x16GeU:
		vm.handleBinaryV128(simdI8x16GeU)
	case i16x8Eq:
		vm.handleBinaryV128(simdI16x8Eq)
	case i16x8Ne:
		vm.handleBinaryV128(simdI16x8Ne)
	case i16x8LtS:
		vm.handleBinaryV128(simdI16x8LtS)
	case i16x8LtU:
		vm.handleBinaryV128(simdI16x8LtU)
	case i16x8GtS:
		vm.handleBinaryV128(simdI16x8GtS)
	case i16x8GtU:
		vm.handleBinaryV128(simdI16x8GtU)
	case i16x8LeS:
		vm.handleBinaryV128(simdI16x8LeS)
	case i16x8LeU:
		vm.handleBinaryV128(simdI16x8LeU)
	case i16x8GeS:
		vm.handleBinaryV128(simdI16x8GeS)
	case i16x8GeU:
		vm.handleBinaryV128(simdI16x8GeU)
	case i32x4Eq:
		vm.handleBinaryV128(simdI32x4Eq)
	case i32x4Ne:
		vm.handleBinaryV128(simdI32x4Ne)
	case i32x4LtS:
		vm.handleBinaryV128(simdI32x4LtS)
	case i32x4LtU:
		vm.handleBinaryV128(simdI32x4LtU)
	case i32x4GtS:
		vm.handleBinaryV128(simdI32x4GtS)
	case i32x4GtU:
		vm.handleBinaryV128(simdI32x4GtU)
	case i32x4LeS:
		vm.handleBinaryV128(simdI32x4LeS)
	case i32x4LeU:
		vm.handleBinaryV128(simdI32x4LeU)
	case i32x4GeS:
		vm.handleBinaryV128(simdI32x4GeS)
	case i32x4GeU:
		vm.handleBinaryV128(simdI32x4GeU)
	case f32x4Eq:
		vm.handleBinaryV128(simdF32x4Eq)
	case f32x4Ne:
		vm.handleBinaryV128(simdF32x4Ne)
	case f32x4Lt:
		vm.handleBinaryV128(simdF32x4Lt)
	case f32x4Gt:
		vm.handleBinaryV128(simdF32x4Gt)
	case f32x4Le:
		vm.handleBinaryV128(simdF32x4Le)
	case f32x4Ge:
		vm.handleBinaryV128(simdF32x4Ge)
	case f64x2Eq:
		vm.handleBinaryV128(simdF64x2Eq)
	case f64x2Ne:
		vm.handleBinaryV128(simdF64x2Ne)
	case f64x2Lt:
		vm.handleBinaryV128(simdF64x2Lt)
	case f64x2Gt:
		vm.handleBinaryV128(simdF64x2Gt)
	case f64x2Le:
		vm.handleBinaryV128(simdF64x2Le)
	case f64x2Ge:
		vm.handleBinaryV128(simdF64x2Ge)
	case v128Not:
		vm.stack.pushV128(simdV128Not(vm.stack.popV128()))
	case v128And:
		vm.handleBinaryV128(simdV128And)
	case v128Andnot:
		vm.handleBinaryV128(simdV128Andnot)
	case v128Or:
		vm.handleBinaryV128(simdV128Or)
	case v128Xor:
		vm.handleBinaryV128(simdV128Xor)
	case v128Bitselect:
		vm.handleSimdTernary(simdV128Bitselect)
	case v128AnyTrue:
		vm.stack.pushInt32(boolToInt32(simdV128AnyTrue(vm.stack.popV128())))
	case v128Load8Lane:
		err = vm.handleSimdLoadLane(frame, 8)
	case v128Load16Lane:
		err = vm.handleSimdLoadLane(frame, 16)
	case v128Load32Lane:
		err = vm.handleSimdLoadLane(frame, 32)
	case v128Load64Lane:
		err = vm.handleSimdLoadLane(frame, 64)
	case v128Store8Lane:
		err = vm.handleSimdStoreLane(frame, 8)
	case v128Store16Lane:
		err = vm.handleSimdStoreLane(frame, 16)
	case v128Store32Lane:
		err = vm.handleSimdStoreLane(frame, 32)
	case v128Store64Lane:
		err = vm.handleSimdStoreLane(frame, 64)
	case v128Load32Zero:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load32Zero, 4)
	case v128Load64Zero:
		err = vm.handleLoadV128FromBytes(frame, simdV128Load64Zero, 8)
	case f32x4DemoteF64x2Zero:
		vm.stack.pushV128(simdF32x4DemoteF64x2Zero(vm.stack.popV128()))
	case f64x2PromoteLowF32x4:
		vm.stack.pushV128(simdF64x2PromoteLowF32x4(vm.stack.popV128()))
	case i8x16Abs:
		vm.stack.pushV128(simdI8x16Abs(vm.stack.popV128()))
	case i8x16Neg:
		vm.stack.pushV128(simdI8x16Neg(vm.stack.popV128()))
	case i8x16Popcnt:
		vm.stack.pushV128(simdI8x16Popcnt(vm.stack.popV128()))
	case i8x16AllTrue:
		vm.stack.pushInt32(boolToInt32(simdI8x16AllTrue(vm.stack.popV128())))
	case i8x16Bitmask:
		vm.stack.pushInt32(simdI8x16Bitmask(vm.stack.popV128()))
	case i8x16NarrowI16x8S:
		vm.handleBinaryV128(simdI8x16NarrowI16x8S)
	case i8x16NarrowI16x8U:
		vm.handleBinaryV128(simdI8x16NarrowI16x8U)
	case f32x4Ceil:
		vm.stack.pushV128(simdF32x4Ceil(vm.stack.popV128()))
	case f32x4Floor:
		vm.stack.pushV128(simdF32x4Floor(vm.stack.popV128()))
	case f32x4Trunc:
		vm.stack.pushV128(simdF32x4Trunc(vm.stack.popV128()))
	case f32x4Nearest:
		vm.stack.pushV128(simdF32x4Nearest(vm.stack.popV128()))
	case i8x16Shl:
		vm.handleSimdShift(simdI8x16Shl)
	case i8x16ShrU:
		vm.handleSimdShift(simdI8x16ShrU)
	case i8x16ShrS:
		vm.handleSimdShift(simdI8x16ShrS)
	case i8x16Add:
		vm.handleBinaryV128(simdI8x16Add)
	case i8x16AddSatS:
		vm.handleBinaryV128(simdI8x16AddSatS)
	case i8x16AddSatU:
		vm.handleBinaryV128(simdI8x16AddSatU)
	case i8x16Sub:
		vm.handleBinaryV128(simdI8x16Sub)
	case i8x16SubSatS:
		vm.handleBinaryV128(simdI8x16SubSatS)
	case i8x16SubSatU:
		vm.handleBinaryV128(simdI8x16SubSatU)
	case f64x2Ceil:
		vm.stack.pushV128(simdF64x2Ceil(vm.stack.popV128()))
	case f64x2Floor:
		vm.stack.pushV128(simdF64x2Floor(vm.stack.popV128()))
	case i8x16MinS:
		vm.handleBinaryV128(simdI8x16MinS)
	case i8x16MinU:
		vm.handleBinaryV128(simdI8x16MinU)
	case i8x16MaxS:
		vm.handleBinaryV128(simdI8x16MaxS)
	case i8x16MaxU:
		vm.handleBinaryV128(simdI8x16MaxU)
	case f64x2Trunc:
		vm.stack.pushV128(simdF64x2Trunc(vm.stack.popV128()))
	case i8x16AvgrU:
		vm.handleBinaryV128(simdI8x16AvgrU)
	case i16x8ExtaddPairwiseI8x16S:
		vm.stack.pushV128(simdI16x8ExtaddPairwiseI8x16S(vm.stack.popV128()))
	case i16x8ExtaddPairwiseI8x16U:
		vm.stack.pushV128(simdI16x8ExtaddPairwiseI8x16U(vm.stack.popV128()))
	case i32x4ExtaddPairwiseI16x8S:
		vm.stack.pushV128(simdI32x4ExtaddPairwiseI16x8S(vm.stack.popV128()))
	case i32x4ExtaddPairwiseI16x8U:
		vm.stack.pushV128(simdI32x4ExtaddPairwiseI16x8U(vm.stack.popV128()))
	case i16x8Abs:
		vm.stack.pushV128(simdI16x8Abs(vm.stack.popV128()))
	case i16x8Neg:
		vm.stack.pushV128(simdI16x8Neg(vm.stack.popV128()))
	case i16x8Q15mulrSatS:
		vm.handleBinaryV128(simdI16x8Q15mulrSatS)
	case i16x8AllTrue:
		vm.stack.pushInt32(boolToInt32(simdI16x8AllTrue(vm.stack.popV128())))
	case i16x8Bitmask:
		vm.stack.pushInt32(simdI16x8Bitmask(vm.stack.popV128()))
	case i16x8NarrowI32x4S:
		vm.handleBinaryV128(simdI16x8NarrowI32x4S)
	case i16x8NarrowI32x4U:
		vm.handleBinaryV128(simdI16x8NarrowI32x4U)
	case i16x8ExtendLowI8x16S:
		vm.stack.pushV128(simdI16x8ExtendLowI8x16S(vm.stack.popV128()))
	case i16x8ExtendHighI8x16S:
		vm.stack.pushV128(simdI16x8ExtendHighI8x16S(vm.stack.popV128()))
	case i16x8ExtendLowI8x16U:
		vm.stack.pushV128(simdI16x8ExtendLowI8x16U(vm.stack.popV128()))
	case i16x8ExtendHighI8x16U:
		vm.stack.pushV128(simdI16x8ExtendHighI8x16U(vm.stack.popV128()))
	case i16x8Shl:
		vm.handleSimdShift(simdI16x8Shl)
	case i16x8ShrS:
		vm.handleSimdShift(simdI16x8ShrS)
	case i16x8ShrU:
		vm.handleSimdShift(simdI16x8ShrU)
	case i16x8Add:
		vm.handleBinaryV128(simdI16x8Add)
	case i16x8AddSatS:
		vm.handleBinaryV128(simdI16x8AddSatS)
	case i16x8AddSatU:
		vm.handleBinaryV128(simdI16x8AddSatU)
	case i16x8Sub:
		vm.handleBinaryV128(simdI16x8Sub)
	case i16x8SubSatS:
		vm.handleBinaryV128(simdI16x8SubSatS)
	case i16x8SubSatU:
		vm.handleBinaryV128(simdI16x8SubSatU)
	case f64x2Nearest:
		vm.stack.pushV128(simdF64x2Nearest(vm.stack.popV128()))
	case i16x8Mul:
		vm.handleBinaryV128(simdI16x8Mul)
	case i16x8MinS:
		vm.handleBinaryV128(simdI16x8MinS)
	case i16x8MinU:
		vm.handleBinaryV128(simdI16x8MinU)
	case i16x8MaxS:
		vm.handleBinaryV128(simdI16x8MaxS)
	case i16x8MaxU:
		vm.handleBinaryV128(simdI16x8MaxU)
	case i16x8AvgrU:
		vm.handleBinaryV128(simdI16x8AvgrU)
	case i16x8ExtmulLowI8x16S:
		vm.handleBinaryV128(simdI16x8ExtmulLowI8x16S)
	case i16x8ExtmulHighI8x16S:
		vm.handleBinaryV128(simdI16x8ExtmulHighI8x16S)
	case i16x8ExtmulLowI8x16U:
		vm.handleBinaryV128(simdI16x8ExtmulLowI8x16U)
	case i16x8ExtmulHighI8x16U:
		vm.handleBinaryV128(simdI16x8ExtmulHighI8x16U)
	case i32x4Abs:
		vm.stack.pushV128(simdI32x4Abs(vm.stack.popV128()))
	case i32x4Neg:
		vm.stack.pushV128(simdI32x4Neg(vm.stack.popV128()))
	case i32x4AllTrue:
		vm.stack.pushInt32(boolToInt32(simdI32x4AllTrue(vm.stack.popV128())))
	case i32x4Bitmask:
		vm.stack.pushInt32(simdI32x4Bitmask(vm.stack.popV128()))
	case i32x4ExtendLowI16x8S:
		vm.stack.pushV128(simdI32x4ExtendLowI16x8S(vm.stack.popV128()))
	case i32x4ExtendHighI16x8S:
		vm.stack.pushV128(simdI32x4ExtendHighI16x8S(vm.stack.popV128()))
	case i32x4ExtendLowI16x8U:
		vm.stack.pushV128(simdI32x4ExtendLowI16x8U(vm.stack.popV128()))
	case i32x4ExtendHighI16x8U:
		vm.stack.pushV128(simdI32x4ExtendHighI16x8U(vm.stack.popV128()))
	case i32x4Shl:
		vm.handleSimdShift(simdI32x4Shl)
	case i32x4ShrS:
		vm.handleSimdShift(simdI32x4ShrS)
	case i32x4ShrU:
		vm.handleSimdShift(simdI32x4ShrU)
	case i32x4Add:
		vm.handleBinaryV128(simdI32x4Add)
	case i32x4Sub:
		vm.handleBinaryV128(simdI32x4Sub)
	case i32x4Mul:
		vm.handleBinaryV128(simdI32x4Mul)
	case i32x4MinS:
		vm.handleBinaryV128(simdI32x4MinS)
	case i32x4MinU:
		vm.handleBinaryV128(simdI32x4MinU)
	case i32x4MaxS:
		vm.handleBinaryV128(simdI32x4MaxS)
	case i32x4MaxU:
		vm.handleBinaryV128(simdI32x4MaxU)
	case i32x4DotI16x8S:
		vm.handleBinaryV128(simdI32x4DotI16x8S)
	case i32x4ExtmulLowI16x8S:
		vm.handleBinaryV128(simdI32x4ExtmulLowI16x8S)
	case i32x4ExtmulHighI16x8S:
		vm.handleBinaryV128(simdI32x4ExtmulHighI16x8S)
	case i32x4ExtmulLowI16x8U:
		vm.handleBinaryV128(simdI32x4ExtmulLowI16x8U)
	case i32x4ExtmulHighI16x8U:
		vm.handleBinaryV128(simdI32x4ExtmulHighI16x8U)
	case i64x2Abs:
		vm.stack.pushV128(simdI64x2Abs(vm.stack.popV128()))
	case i64x2Neg:
		vm.stack.pushV128(simdI64x2Neg(vm.stack.popV128()))
	case i64x2AllTrue:
		vm.stack.pushInt32(boolToInt32(simdI64x2AllTrue(vm.stack.popV128())))
	case i64x2Bitmask:
		vm.stack.pushInt32(simdI64x2Bitmask(vm.stack.popV128()))
	case i64x2ExtendLowI32x4S:
		vm.stack.pushV128(simdI64x2ExtendLowI32x4S(vm.stack.popV128()))
	case i64x2ExtendHighI32x4S:
		vm.stack.pushV128(simdI64x2ExtendHighI32x4S(vm.stack.popV128()))
	case i64x2ExtendLowI32x4U:
		vm.stack.pushV128(simdI64x2ExtendLowI32x4U(vm.stack.popV128()))
	case i64x2ExtendHighI32x4U:
		vm.stack.pushV128(simdI64x2ExtendHighI32x4U(vm.stack.popV128()))
	case i64x2Shl:
		vm.handleSimdShift(simdI64x2Shl)
	case i64x2ShrS:
		vm.handleSimdShift(simdI64x2ShrS)
	case i64x2ShrU:
		vm.handleSimdShift(simdI64x2ShrU)
	case i64x2Add:
		vm.handleBinaryV128(simdI64x2Add)
	case i64x2Sub:
		vm.handleBinaryV128(simdI64x2Sub)
	case i64x2Mul:
		vm.handleBinaryV128(simdI64x2Mul)
	case i64x2Eq:
		vm.handleBinaryV128(simdI64x2Eq)
	case i64x2Ne:
		vm.handleBinaryV128(simdI64x2Ne)
	case i64x2LtS:
		vm.handleBinaryV128(simdI64x2LtS)
	case i64x2GtS:
		vm.handleBinaryV128(simdI64x2GtS)
	case i64x2LeS:
		vm.handleBinaryV128(simdI64x2LeS)
	case i64x2GeS:
		vm.handleBinaryV128(simdI64x2GeS)
	case i64x2ExtmulLowI32x4S:
		vm.handleBinaryV128(simdI64x2ExtmulLowI32x4S)
	case i64x2ExtmulHighI32x4S:
		vm.handleBinaryV128(simdI64x2ExtmulHighI32x4S)
	case i64x2ExtmulLowI32x4U:
		vm.handleBinaryV128(simdI64x2ExtmulLowI32x4U)
	case i64x2ExtmulHighI32x4U:
		vm.handleBinaryV128(simdI64x2ExtmulHighI32x4U)
	case f32x4Abs:
		vm.stack.pushV128(simdF32x4Abs(vm.stack.popV128()))
	case f32x4Neg:
		vm.stack.pushV128(simdF32x4Neg(vm.stack.popV128()))
	case f32x4Sqrt:
		vm.stack.pushV128(simdF32x4Sqrt(vm.stack.popV128()))
	case f32x4Add:
		vm.handleBinaryV128(simdF32x4Add)
	case f32x4Sub:
		vm.handleBinaryV128(simdF32x4Sub)
	case f32x4Mul:
		vm.handleBinaryV128(simdF32x4Mul)
	case f32x4Div:
		vm.handleBinaryV128(simdF32x4Div)
	case f32x4Min:
		vm.handleBinaryV128(simdF32x4Min)
	case f32x4Max:
		vm.handleBinaryV128(simdF32x4Max)
	case f32x4Pmin:
		vm.handleBinaryV128(simdF32x4Pmin)
	case f32x4Pmax:
		vm.handleBinaryV128(simdF32x4Pmax)
	case f64x2Abs:
		vm.stack.pushV128(simdF64x2Abs(vm.stack.popV128()))
	case f64x2Neg:
		vm.stack.pushV128(simdF64x2Neg(vm.stack.popV128()))
	case f64x2Sqrt:
		vm.stack.pushV128(simdF64x2Sqrt(vm.stack.popV128()))
	case f64x2Add:
		vm.handleBinaryV128(simdF64x2Add)
	case f64x2Sub:
		vm.handleBinaryV128(simdF64x2Sub)
	case f64x2Mul:
		vm.handleBinaryV128(simdF64x2Mul)
	case f64x2Div:
		vm.handleBinaryV128(simdF64x2Div)
	case f64x2Min:
		vm.handleBinaryV128(simdF64x2Min)
	case f64x2Max:
		vm.handleBinaryV128(simdF64x2Max)
	case f64x2Pmin:
		vm.handleBinaryV128(simdF64x2Pmin)
	case f64x2Pmax:
		vm.handleBinaryV128(simdF64x2Pmax)
	case i32x4TruncSatF32x4S:
		vm.stack.pushV128(simdI32x4TruncSatF32x4S(vm.stack.popV128()))
	case i32x4TruncSatF32x4U:
		vm.stack.pushV128(simdI32x4TruncSatF32x4U(vm.stack.popV128()))
	case f32x4ConvertI32x4S:
		vm.stack.pushV128(simdF32x4ConvertI32x4S(vm.stack.popV128()))
	case f32x4ConvertI32x4U:
		vm.stack.pushV128(simdF32x4ConvertI32x4U(vm.stack.popV128()))
	case i32x4TruncSatF64x2SZero:
		vm.stack.pushV128(simdI32x4TruncSatF64x2SZero(vm.stack.popV128()))
	case i32x4TruncSatF64x2UZero:
		vm.stack.pushV128(simdI32x4TruncSatF64x2UZero(vm.stack.popV128()))
	case f64x2ConvertLowI32x4S:
		vm.stack.pushV128(simdF64x2ConvertLowI32x4S(vm.stack.popV128()))
	case f64x2ConvertLowI32x4U:
		vm.stack.pushV128(simdF64x2ConvertLowI32x4U(vm.stack.popV128()))
	default:
		err = fmt.Errorf("unknown opcode %d", op)
	}
	return err
}

func (vm *vm) currentCallFrame() *callFrame {
	return &vm.callStack[len(vm.callStack)-1]
}

func (vm *vm) currentModuleInstance() *ModuleInstance {
	return vm.currentCallFrame().function.module
}

func (vm *vm) pushBlockFrame(opcode opcode, blockType int32) {
	callFrame := vm.currentCallFrame()
	// For loops, the continuation is a branch back to the start of the block.
	var continuationPc uint32
	if opcode == loop {
		continuationPc = callFrame.pc
	} else {
		continuationPc = callFrame.function.code.jumpCache[callFrame.pc]
	}

	vm.pushControlFrame(controlFrame{
		isLoop:         opcode == loop,
		blockType:      blockType,
		stackHeight:    vm.stack.size(),
		continuationPc: continuationPc,
	})
}

func (vm *vm) handleIf(frame *callFrame) {
	condition := vm.stack.popInt32()

	vm.pushBlockFrame(ifOp, int32(frame.next()))

	if condition != 0 {
		return
	}

	frame.pc = frame.function.code.jumpElseCache[frame.pc]
}

func (vm *vm) handleElse(frame *callFrame) {
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame := vm.popControlFrame()
	frame.pc = ifFrame.continuationPc
}

func (vm *vm) handleEnd() {
	frame := vm.popControlFrame()
	callFrame := vm.currentCallFrame()
	outputCount := vm.getOutputCount(callFrame.function.module, frame.blockType)
	vm.stack.unwind(frame.stackHeight, outputCount)
}

func (vm *vm) handleBrIf(frame *callFrame) {
	labelIndex := uint32(frame.next())
	val := vm.stack.popInt32()
	if val == 0 {
		return
	}
	vm.brToLabel(labelIndex)
}

func (vm *vm) handleBrTable(frame *callFrame) {
	size := frame.next()
	index := vm.stack.popInt32()
	var targetLabel uint32
	if index >= 0 && uint64(index) < size {
		targetLabel = uint32(frame.code[frame.pc+uint32(index)])
	} else {
		targetLabel = uint32(frame.code[frame.pc+uint32(size)])
	}
	frame.pc += uint32(size) + 1
	vm.brToLabel(targetLabel)
}

func (vm *vm) brToLabel(labelIndex uint32) {
	callFrame := vm.currentCallFrame()

	targetIndex := len(callFrame.controlStack) - int(labelIndex) - 1
	targetFrame := callFrame.controlStack[targetIndex]
	callFrame.controlStack = callFrame.controlStack[:targetIndex]

	var arity uint32
	if targetFrame.isLoop {
		arity = vm.getInputCount(callFrame.function.module, targetFrame.blockType)
	} else {
		arity = vm.getOutputCount(callFrame.function.module, targetFrame.blockType)
	}

	vm.stack.unwind(targetFrame.stackHeight, arity)
	if targetFrame.isLoop {
		vm.pushControlFrame(targetFrame)
	}

	callFrame.pc = targetFrame.continuationPc
}

func (vm *vm) handleCall(frame *callFrame) error {
	localIndex := uint32(frame.next())
	function := vm.getFunction(localIndex)
	return vm.invokeFunction(function)
}

func (vm *vm) handleCallIndirect(frame *callFrame) error {
	typeIndex := uint32(frame.next())
	tableIndex := uint32(frame.next())

	expectedType := vm.currentModuleInstance().types[typeIndex]
	table := vm.getTable(tableIndex)

	elementIndex := vm.stack.popInt32()

	tableElement, err := table.Get(elementIndex)
	if err != nil {
		return err
	}
	if tableElement == NullReference {
		return fmt.Errorf("uninitialized element %d", elementIndex)
	}

	function := vm.store.funcs[uint32(tableElement)]
	if !function.GetType().Equal(&expectedType) {
		return fmt.Errorf("indirect call type mismatch")
	}

	return vm.invokeFunction(function)
}

func (vm *vm) handleSelect() {
	data := vm.stack.data
	n := len(data)
	var top value
	if data[n-1].int32() != 0 {
		top = data[n-3]
	} else {
		top = data[n-2]
	}
	data[n-3] = top
	vm.stack.data = data[:n-2]
}

func (vm *vm) handleGlobalGet(frame *callFrame) {
	localIndex := uint32(frame.next())
	global := vm.getGlobal(localIndex)
	vm.stack.push(global.value)
}

func (vm *vm) handleGlobalSet(frame *callFrame) {
	localIndex := uint32(frame.next())
	global := vm.getGlobal(localIndex)
	global.value = vm.stack.pop()
}

func (vm *vm) handleTableGet(frame *callFrame) error {
	tableIndex := uint32(frame.next())
	table := vm.getTable(tableIndex)
	index := vm.stack.popInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}

	vm.stack.pushInt32(element)
	return nil
}

func (vm *vm) handleTableSet(frame *callFrame) error {
	tableIndex := uint32(frame.next())
	table := vm.getTable(tableIndex)
	reference := vm.stack.popInt32()
	index := vm.stack.popInt32()
	return table.Set(index, reference)
}

func (vm *vm) handleMemorySize(frame *callFrame) {
	memory := vm.getMemory(uint32(frame.next()))
	vm.stack.pushInt32(memory.Size())
}

func (vm *vm) handleMemoryGrow(frame *callFrame) {
	memory := vm.getMemory(uint32(frame.next()))
	pages := vm.stack.popInt32()
	oldSize := memory.Grow(pages)
	vm.stack.pushInt32(oldSize)
}

func (vm *vm) handleRefFunc(frame *callFrame) {
	funcIndex := uint32(frame.next())
	storeIndex := vm.currentModuleInstance().funcAddrs[funcIndex]
	vm.stack.pushInt32(int32(storeIndex))
}

func (vm *vm) handleRefIsNull() {
	top := vm.stack.popInt32()
	vm.stack.pushInt32(boolToInt32(top == NullReference))
}

func (vm *vm) handleMemoryInit(frame *callFrame) error {
	data := vm.getData(uint32(frame.next()))
	memory := vm.getMemory(uint32(frame.next()))
	n, s, d := vm.stack.pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.content)
}

func (vm *vm) handleDataDrop(frame *callFrame) {
	dataSegment := vm.getData(uint32(frame.next()))
	dataSegment.content = nil
}

func (vm *vm) handleMemoryCopy(frame *callFrame) error {
	destMemory := vm.getMemory(uint32(frame.next()))
	srcMemory := vm.getMemory(uint32(frame.next()))
	n, s, d := vm.stack.pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *vm) handleMemoryFill(frame *callFrame) error {
	memory := vm.getMemory(uint32(frame.next()))
	n, val, offset := vm.stack.pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *vm) handleTableInit(frame *callFrame) error {
	element := vm.getElement(uint32(frame.next()))
	table := vm.getTable(uint32(frame.next()))
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

func (vm *vm) handleElemDrop(frame *callFrame) {
	element := vm.getElement(uint32(frame.next()))
	element.functionIndexes = nil
	element.functionIndexesExpressions = nil
}

func (vm *vm) handleTableCopy(frame *callFrame) error {
	destTable := vm.getTable(uint32(frame.next()))
	srcTable := vm.getTable(uint32(frame.next()))
	n, s, d := vm.stack.pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *vm) handleTableGrow(frame *callFrame) {
	table := vm.getTable(uint32(frame.next()))
	n := vm.stack.popInt32()
	val := vm.stack.popInt32()
	vm.stack.pushInt32(table.Grow(n, val))
}

func (vm *vm) handleTableSize(frame *callFrame) {
	table := vm.getTable(uint32(frame.next()))
	vm.stack.pushInt32(table.Size())
}

func (vm *vm) handleTableFill(frame *callFrame) error {
	table := vm.getTable(uint32(frame.next()))
	n, val, i := vm.stack.pop3Int32()
	return table.Fill(n, i, val)
}

func (vm *vm) handleI8x16Shuffle(frame *callFrame) {
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()

	var lanes [16]byte
	for i := range 16 {
		lanes[i] = byte(frame.next())
	}

	vm.stack.pushV128(simdI8x16Shuffle(v1, v2, lanes))
}

func (vm *vm) handleBinaryInt32(op func(a, b int32) int32) {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	vm.stack.pushInt32(op(a, b))
}

func (vm *vm) handleBinaryInt64(op func(a, b int64) int64) {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	vm.stack.pushInt64(op(a, b))
}

func (vm *vm) handleBinaryFloat32(op func(a, b float32) float32) {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(op(a, b))
}

func (vm *vm) handleBinaryFloat64(op func(a, b float64) float64) {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(op(a, b))
}

func (vm *vm) handleBinaryV128(op func(a, b V128Value) V128Value) {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(op(a, b))
}

func (vm *vm) handleBinarySafeInt32(op func(a, b int32) (int32, error)) error {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	result, err := op(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(result)
	return nil
}

func (vm *vm) handleBinarySafeInt64(op func(a, b int64) (int64, error)) error {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	result, err := op(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(result)
	return nil
}

func (vm *vm) handleBinaryBoolInt32(op func(a, b int32) bool) {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	vm.stack.pushInt32(boolToInt32(op(a, b)))
}

func (vm *vm) handleBinaryBoolInt64(op func(a, b int64) bool) {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	vm.stack.pushInt32(boolToInt32(op(a, b)))
}

func (vm *vm) handleBinaryBoolFloat32(op func(a, b float32) bool) {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(op(a, b)))
}

func (vm *vm) handleBinaryBoolFloat64(op func(a, b float64) bool) {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(op(a, b)))
}

func (vm *vm) handleUnarySafeFloat32(op func(a float32) (int32, error)) error {
	a := vm.stack.popFloat32()
	result, err := op(a)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(result)
	return nil
}

func (vm *vm) handleUnarySafeFloat64(op func(a float64) (int32, error)) error {
	a := vm.stack.popFloat64()
	result, err := op(a)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(result)
	return nil
}

func (vm *vm) handleTruncFloat32Int64(op func(a float32) (int64, error)) error {
	a := vm.stack.popFloat32()
	result, err := op(a)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(result)
	return nil
}

func (vm *vm) handleTruncFloat64Int64(op func(a float64) (int64, error)) error {
	a := vm.stack.popFloat64()
	result, err := op(a)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(result)
	return nil
}

func (vm *vm) handleSimdShift(op func(v V128Value, shift int32) V128Value) {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(op(v, shift))
}

func (vm *vm) handleSimdTernary(op func(v1, v2, v3 V128Value) V128Value) {
	v3 := vm.stack.popV128()
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()
	vm.stack.pushV128(op(v1, v2, v3))
}

func handleSimdExtractLane[R wasmNumber](
	vm *vm,
	frame *callFrame,
	push func(R),
	op func(v V128Value, laneIndex uint32) R,
) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	push(op(v, laneIndex))
}

func handleStore[T any](
	vm *vm,
	frame *callFrame,
	val T,
	store func(*Memory, uint32, uint32, T) error,
) error {
	_ = frame.next() // align (unused)
	memory := vm.getMemory(uint32(frame.next()))
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return store(memory, offset, index, val)
}

func handleLoad[T any, R any](
	vm *vm,
	frame *callFrame,
	push func(R),
	load func(*Memory, uint32, uint32) (T, error),
	convert func(T) R,
) error {
	_ = frame.next() // align (unused)
	memory := vm.getMemory(uint32(frame.next()))
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := load(memory, offset, index)
	if err != nil {
		return err
	}
	push(convert(v))
	return nil
}

func (vm *vm) handleLoadV128FromBytes(
	frame *callFrame,
	fromBytes func(bytes []byte) V128Value,
	sizeBytes uint32,
) error {
	_ = frame.next() // align (unused)
	memory := vm.getMemory(uint32(frame.next()))
	offset := uint32(frame.next())
	index := vm.stack.popInt32()

	data, err := memory.Get(offset, uint32(index), sizeBytes)
	if err != nil {
		return err
	}
	vm.stack.pushV128(fromBytes(data))
	return nil
}

func (vm *vm) handleSimdLoadLane(
	frame *callFrame,
	laneSize uint32,
) error {
	_ = frame.next() // align (unused)
	memory := vm.getMemory(uint32(frame.next()))
	offset := uint32(frame.next())
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return err
	}

	vm.stack.pushV128(setLane(v, laneIndex, laneValue))
	return nil
}

func (vm *vm) handleSimdStoreLane(
	frame *callFrame,
	laneSize uint32,
) error {
	_ = frame.next() // align (unused)
	memory := vm.getMemory(uint32(frame.next()))
	offset := uint32(frame.next())
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneData := extractLane(v, laneSize, laneIndex)
	return memory.Set(offset, uint32(index), laneData)
}
func handleSimdReplaceLane[T wasmNumber](
	vm *vm,
	frame *callFrame,
	pop func() T,
	replaceLane func(V128Value, uint32, T) V128Value,
) {
	laneIndex := uint32(frame.next())
	laneValue := pop()
	vector := vm.stack.popV128()
	vm.stack.pushV128(replaceLane(vector, laneIndex, laneValue))
}

func (vm *vm) getInputCount(module *ModuleInstance, blockType int32) uint32 {
	if blockType == -0x40 { // empty block type.
		return 0
	}

	if blockType >= 0 { // type index.
		funcType := module.types[blockType]
		return uint32(len(funcType.ParamTypes))
	}

	return 0 // value type.
}

func (vm *vm) getOutputCount(module *ModuleInstance, blockType int32) uint32 {
	if blockType == -0x40 { // empty block type.
		return 0
	}

	if blockType >= 0 { // type index.
		funcType := module.types[blockType]
		return uint32(len(funcType.ResultTypes))
	}

	return 1 // value type.
}

func (vm *vm) pushControlFrame(frame controlFrame) {
	callFrame := vm.currentCallFrame()
	callFrame.controlStack = append(callFrame.controlStack, frame)
}

func (vm *vm) popControlFrame() controlFrame {
	callFrame := vm.currentCallFrame()
	// Validation guarantees the control stack is never empty.
	index := len(callFrame.controlStack) - 1
	frame := callFrame.controlStack[index]
	callFrame.controlStack = callFrame.controlStack[:index]
	return frame
}

func (vm *vm) initActiveElements(
	module *moduleDefinition,
	moduleInstance *ModuleInstance,
) error {
	for _, element := range module.elementSegments {
		if element.mode != activeElementMode {
			continue
		}

		expression := element.offsetExpression
		offsetVal, err := vm.invokeInitExpression(expression, I32, moduleInstance)
		if err != nil {
			return err
		}
		offset := offsetVal.int32()

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
			values := make([]int32, len(element.functionIndexesExpressions))
			for i, expr := range element.functionIndexesExpressions {
				refVal, err := vm.invokeInitExpression(
					expr,
					element.kind,
					moduleInstance,
				)
				if err != nil {
					return err
				}
				values[i] = refVal.int32()
			}

			if err := table.InitFromSlice(offset, values); err != nil {
				return err
			}
		}
	}
	return nil
}

func (vm *vm) initActiveDatas(
	module *moduleDefinition,
	moduleInstance *ModuleInstance,
) error {
	for _, segment := range module.dataSegments {
		if segment.mode != activeDataMode {
			continue
		}

		expression := segment.offsetExpression
		offsetVal, err := vm.invokeInitExpression(expression, I32, moduleInstance)
		if err != nil {
			return err
		}
		offset := offsetVal.int32()
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

func (vm *vm) invokeHostFunction(fun *hostFunction) (err error) {
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

	args := vm.stack.popValueTypes(fun.GetType().ParamTypes)
	res := fun.hostCode(args...)
	vm.stack.pushAll(res)
	return err
}

func (vm *vm) invokeInitExpression(
	expression []uint64,
	resultType ValueType,
	moduleInstance *ModuleInstance,
) (value, error) {
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
	if err := vm.invokeWasmFunction(&function); err != nil {
		return value{}, err
	}
	return vm.stack.pop(), nil
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
	elementIndex := vm.currentModuleInstance().elemAddrs[localIndex]
	return &vm.store.elements[elementIndex]
}

func (vm *vm) getData(localIndex uint32) *dataSegment {
	dataIndex := vm.currentModuleInstance().dataAddrs[localIndex]
	return &vm.store.datas[dataIndex]
}
