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
	"math/bits"
)

var (
	errUnreachable              = errors.New("unreachable")
	errCallStackExhausted       = errors.New("call stack exhausted")
	errFuelExhausted            = errors.New("fuel exhausted")
	errUnknownFunctionType      = errors.New("unknown function type")
	errIndirectCallTypeMismatch = errors.New("indirect call type mismatch")
	errHostResultCountMismatch  = errors.New("host func result count mismatch")
)

const (
	controlStackCacheSlotSize = 14 // Control stack slot size per call frame.
	localsCacheSlotSize       = 12 // Locals slot size per call frame.
)

// store represents all global state that can be manipulated by the vm. It
// consists of the runtime representation of all instances of functions, tables,
// memories, globals, element segments, and data segments that have been
// allocated during the vm life time.
type store struct {
	funcs    []FunctionInstance
	tables   []*Table
	memories []*Memory
	globals  []*Global
	elements []elementInstance
	datas    []dataInstance
}

// elementInstance is the runtime representation of an element segment.
// https://webassembly.github.io/spec/core/exec/runtime.html#element-instances.
// functionIndexes holds store-resolved function references; it is nil when the
// segment has been dropped (by elem.drop, or implicitly for active/declarative
// segments after instantiation).
type elementInstance struct {
	functionIndexes []int32
}

// dataInstance is the runtime representation of a data segment.
// https://webassembly.github.io/spec/core/exec/runtime.html#data-instances.
// content is nil when the segment has been dropped (by data.drop, or implicitly
// for active segments after instantiation).
type dataInstance struct {
	content []byte
}

type callFrame struct {
	pc           uint32
	controlStack []controlFrame
	locals       []value
	function     *function
	module       *ModuleInstance
}

func (f *callFrame) next() uint64 {
	val := f.function.body[f.pc]
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
	controlStackCache []controlFrame
	localsCache       []value
	// localsTop is the bump-allocation cursor into localsCache: each wasm call
	// carves its locals starting here, advances the cursor, and rewinds it when
	// the call returns. This lets a frame with many locals use the shared cache
	// rather than allocating them on the heap.
	localsTop int
	config    Config
	fuel      uint64
}

func newVm(config Config) *vm {
	ctrlCacheSize := config.CallStackPreallocationSize * controlStackCacheSlotSize
	localsCacheSize := config.CallStackPreallocationSize * localsCacheSlotSize
	return &vm{
		store:             &store{},
		stack:             newValueStack(),
		callStack:         make([]callFrame, 0, config.CallStackPreallocationSize),
		controlStackCache: make([]controlFrame, ctrlCacheSize),
		localsCache:       make([]value, localsCacheSize),
		config:            config,
		fuel:              config.Fuel,
	}
}

func (vm *vm) instantiate(
	module *moduleDefinition,
	imports map[string]map[string]any,
) (*ModuleInstance, error) {
	validator := newValidator(vm.config)
	if err := validator.validateModule(module); err != nil {
		return nil, err
	}
	moduleInstance := &ModuleInstance{types: module.types, vm: vm}

	resolvedImports, err := resolveImports(module, moduleInstance, imports)
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
		table := newTable(vm, tableType)
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
		memory := newMemory(vm, memoryType)
		moduleInstance.memAddrs = append(moduleInstance.memAddrs, storeIndex)
		vm.store.memories = append(vm.store.memories, memory)
	}

	for _, global := range resolvedImports.globals {
		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.globalAddrs = append(moduleInstance.globalAddrs, storeIndex)
		vm.store.globals = append(vm.store.globals, global)
	}

	for _, variable := range module.globalVariables {
		valueType := variable.globalType.ValueType
		initExpression := variable.initExpression
		val, err := vm.invokeExpression(initExpression, valueType, moduleInstance)
		if err != nil {
			return nil, err
		}

		storeIndex := uint32(len(vm.store.globals))
		moduleInstance.globalAddrs = append(moduleInstance.globalAddrs, storeIndex)
		global := newGlobal(vm, val, variable.globalType.IsMutable, valueType)
		vm.store.globals = append(vm.store.globals, global)
	}

	for _, segment := range module.elementSegments {
		instance, err := vm.newElementInstance(segment, moduleInstance)
		if err != nil {
			return nil, err
		}
		storeIndex := uint32(len(vm.store.elements))
		moduleInstance.elemAddrs = append(moduleInstance.elemAddrs, storeIndex)
		vm.store.elements = append(vm.store.elements, instance)
	}

	// Apply active element segments to their target tables, then drop them.
	// Declarative segments are also dropped immediately.
	for i, segment := range module.elementSegments {
		if segment.mode == passiveElementMode {
			continue
		}
		elem := &vm.store.elements[moduleInstance.elemAddrs[i]]
		if segment.mode == activeElementMode {
			err := vm.applyActiveElementSegment(segment, elem, moduleInstance)
			if err != nil {
				return nil, err
			}
		}
		elem.functionIndexes = nil
	}

	// Allocate runtime data instances. The byte slice is shared with the parsed
	// dataSegment: the runtime only ever reads it (memory.init copies out) and
	// data.drop nils the instance's slice header, never the backing array, so the
	// moduleDefinition is left untouched and can be reinstantiated.
	for _, segment := range module.dataSegments {
		storeIndex := uint32(len(vm.store.datas))
		moduleInstance.dataAddrs = append(moduleInstance.dataAddrs, storeIndex)
		data := dataInstance{content: segment.content}
		vm.store.datas = append(vm.store.datas, data)
	}

	// Apply active data segments to their target memories, then drop them. After
	// this, memory.init against an active segment sees an empty content and traps
	// for any non-zero size.
	for i, segment := range module.dataSegments {
		if segment.mode != activeDataMode {
			continue
		}
		data := &vm.store.datas[moduleInstance.dataAddrs[i]]
		err := vm.applyActiveDataSegment(segment, data, moduleInstance)
		if err != nil {
			return nil, err
		}
		data.content = nil
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
		return errUnknownFunctionType
	}
}

func (vm *vm) invokeWasmFunction(function *wasmFunction) error {
	if len(vm.callStack) >= vm.config.MaxCallStackDepth {
		return errCallStackExhausted
	}

	callDepth := len(vm.callStack)
	// Unlike locals, the control stack uses a fixed per-depth slot; only depths
	// within the preallocated range are cached, and deeper frames use the heap.
	useControlStackCache := callDepth < vm.config.CallStackPreallocationSize

	numParams := len(function.functionType.ParamTypes)
	numLocals := numParams + len(function.code.locals)

	// Carve this frame's locals at the cursor; nested calls bump past them, so
	// frames never overlap.
	localsMark := vm.localsTop
	var locals []value
	if end := localsMark + numLocals; end <= len(vm.localsCache) {
		locals = vm.localsCache[localsMark:end:end]
		vm.localsTop = end
		// Cache slots may hold stale values from earlier calls, and WASM allows
		// reading uninitialized locals, so zero the non-parameter locals. The
		// parameter slots are filled from the operand stack afterward.
		if function.code.defaultLocals != nil {
			copy(locals[numParams:], function.code.defaultLocals)
		} else {
			clear(locals[numParams:])
		}
	} else {
		// Not enough cache room: heap allocate (make already zeroes the locals).
		locals = make([]value, numLocals)
		if function.code.defaultLocals != nil {
			copy(locals[numParams:], function.code.defaultLocals)
		}
	}

	// Copy params and shrink stack by operating on the underlying slice directly.
	newLen := len(vm.stack.data) - numParams
	copy(locals[:numParams], vm.stack.data[newLen:])
	vm.stack.data = vm.stack.data[:newLen]

	var controlStack []controlFrame
	if useControlStackCache {
		// Use part of the cache for the control stack to avoid allocations.
		blockDepth := callDepth * controlStackCacheSlotSize
		// Slice cap to prevent appending into the next slot.
		max := blockDepth + controlStackCacheSlotSize
		controlStack = vm.controlStackCache[blockDepth:blockDepth:max]
	}
	controlStack = append(controlStack, controlFrame{
		isLoop:         false,
		blockType:      int32(function.code.typeIndex),
		continuationPc: uint32(len(function.code.body)),
		stackHeight:    vm.stack.size(),
	})

	vm.callStack = append(vm.callStack, callFrame{
		pc:           0,
		controlStack: controlStack,
		locals:       locals,
		function:     &function.code,
		module:       function.module,
	})

	err := vm.runLoop()
	// The run loop pops this frame before returning, so its locals slots are free
	// to reuse. Rewinding the cursor here is a no-op for the heap case.
	vm.localsTop = localsMark
	return err
}

func (vm *vm) runLoop() error {
	// The frame pointer is invalidated whenever a nested call grows
	// vm.callStack: only the call and call_indirect cases can do that, and
	// they re-derive it after the callee returns. bodyLen is loop-invariant
	// because nested calls always restore this frame to the top of the stack
	// before control returns here.
	frame := &vm.callStack[len(vm.callStack)-1]
	bodyLen := uint32(len(frame.function.body))
	// Checking a cached flag per instruction is cheaper than the duplicated
	// dispatch loop it would take to hoist it: the branch predicts perfectly.
	fuelOn := vm.config.EnableFuel
	for frame.pc < bodyLen {
		if fuelOn {
			if vm.fuel == 0 {
				vm.callStack = vm.callStack[:len(vm.callStack)-1]
				return errFuelExhausted
			}
			vm.fuel--
		}
		op := opcode(frame.next())
		var err error
		// Using a switch instead of a map of opcode -> Handler is
		// significantly faster.
		switch op {
		case unreachable:
			err = errUnreachable
		case nop:
			// Do nothing.
		case block, loop:
			vm.pushBlockFrame(frame, op)
		case ifOp:
			vm.handleIf(frame)
		case elseOp:
			vm.handleElse(frame)
		case end:
			vm.handleEnd(frame)
		case br:
			vm.brToLabel(frame, uint32(frame.next()))
		case brIf:
			vm.handleBrIf(frame)
		case brTable:
			vm.handleBrTable(frame)
		case returnOp:
			// Semantically, a return is equivalent to a branch to the outermost block
			// of the function. By branching to label N-1 (where N is the depth of the
			// control stack), we trigger a stack unwind to the function's base height
			// and jump to the 'end' instruction at the end of the function body.
			vm.brToLabel(frame, uint32(len(frame.controlStack)-1))
		case call:
			err = vm.handleCall(frame)
			// The callee (or a re-entrant host function) may have grown
			// vm.callStack and reallocated it, invalidating frame.
			frame = &vm.callStack[len(vm.callStack)-1]
		case callIndirect:
			err = vm.handleCallIndirect(frame)
			// The callee (or a re-entrant host function) may have grown
			// vm.callStack and reallocated it, invalidating frame.
			frame = &vm.callStack[len(vm.callStack)-1]
		case drop:
			vm.stack.drop()
		case selectOp:
			vm.handleSelect()
		case selectT:
			frame.pc += uint32(frame.next())
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
			err = vm.handleI32Load(frame)
		case i64Load:
			err = vm.handleI64Load(frame)
		case f32Load:
			err = vm.handleF32Load(frame)
		case f64Load:
			err = vm.handleF64Load(frame)
		case i32Load8S:
			err = vm.handleI32Load8S(frame)
		case i32Load8U:
			err = vm.handleI32Load8U(frame)
		case i32Load16S:
			err = vm.handleI32Load16S(frame)
		case i32Load16U:
			err = vm.handleI32Load16U(frame)
		case i64Load8S:
			err = vm.handleI64Load8S(frame)
		case i64Load8U:
			err = vm.handleI64Load8U(frame)
		case i64Load16S:
			err = vm.handleI64Load16S(frame)
		case i64Load16U:
			err = vm.handleI64Load16U(frame)
		case i64Load32S:
			err = vm.handleI64Load32S(frame)
		case i64Load32U:
			err = vm.handleI64Load32U(frame)
		case i32Store:
			err = vm.handleI32Store(frame)
		case i64Store:
			err = vm.handleI64Store(frame)
		case f32Store:
			err = vm.handleF32Store(frame)
		case f64Store:
			err = vm.handleF64Store(frame)
		case i32Store8:
			err = vm.handleI32Store8(frame)
		case i32Store16:
			err = vm.handleI32Store16(frame)
		case i64Store8:
			err = vm.handleI64Store8(frame)
		case i64Store16:
			err = vm.handleI64Store16(frame)
		case i64Store32:
			err = vm.handleI64Store32(frame)
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
			vm.handleI32Eq()
		case i32Ne:
			vm.handleI32Ne()
		case i32LtS:
			vm.handleI32LtS()
		case i32LtU:
			vm.handleI32LtU()
		case i32GtS:
			vm.handleI32GtS()
		case i32GtU:
			vm.handleI32GtU()
		case i32LeS:
			vm.handleI32LeS()
		case i32LeU:
			vm.handleI32LeU()
		case i32GeS:
			vm.handleI32GeS()
		case i32GeU:
			vm.handleI32GeU()
		case i64Eqz:
			vm.stack.pushInt32(boolToInt32(vm.stack.popInt64() == 0))
		case i64Eq:
			vm.handleI64Eq()
		case i64Ne:
			vm.handleI64Ne()
		case i64LtS:
			vm.handleI64LtS()
		case i64LtU:
			vm.handleI64LtU()
		case i64GtS:
			vm.handleI64GtS()
		case i64GtU:
			vm.handleI64GtU()
		case i64LeS:
			vm.handleI64LeS()
		case i64LeU:
			vm.handleI64LeU()
		case i64GeS:
			vm.handleI64GeS()
		case i64GeU:
			vm.handleI64GeU()
		case f32Eq:
			vm.handleF32Eq()
		case f32Ne:
			vm.handleF32Ne()
		case f32Lt:
			vm.handleF32Lt()
		case f32Gt:
			vm.handleF32Gt()
		case f32Le:
			vm.handleF32Le()
		case f32Ge:
			vm.handleF32Ge()
		case f64Eq:
			vm.handleF64Eq()
		case f64Ne:
			vm.handleF64Ne()
		case f64Lt:
			vm.handleF64Lt()
		case f64Gt:
			vm.handleF64Gt()
		case f64Le:
			vm.handleF64Le()
		case f64Ge:
			vm.handleF64Ge()
		case i32Clz:
			a := vm.stack.popInt32()
			vm.stack.pushInt32(int32(bits.LeadingZeros32(uint32(a))))
		case i32Ctz:
			a := vm.stack.popInt32()
			vm.stack.pushInt32(int32(bits.TrailingZeros32(uint32(a))))
		case i32Popcnt:
			a := vm.stack.popInt32()
			vm.stack.pushInt32(int32(bits.OnesCount32(uint32(a))))
		case i32Add:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() + b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Sub:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() - b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Mul:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() * b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32DivS:
			err = vm.handleI32DivS()
		case i32DivU:
			err = vm.handleI32DivU()
		case i32RemS:
			err = vm.handleI32RemS()
		case i32RemU:
			err = vm.handleI32RemU()
		case i32And:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() & b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Or:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() | b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Xor:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() ^ b
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Shl:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() << (uint32(b) % 32)
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32ShrS:
			b := vm.stack.popInt32()
			res := vm.stack.data[len(vm.stack.data)-1].int32() >> (uint32(b) % 32)
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32ShrU:
			b := vm.stack.popInt32()
			a := vm.stack.data[len(vm.stack.data)-1].int32()
			res := int32(uint32(a) >> (uint32(b) % 32))
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Rotl:
			b := vm.stack.popInt32()
			a := vm.stack.data[len(vm.stack.data)-1].int32()
			res := int32(bits.RotateLeft32(uint32(a), int(b)))
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i32Rotr:
			b := vm.stack.popInt32()
			a := vm.stack.data[len(vm.stack.data)-1].int32()
			res := int32(bits.RotateLeft32(uint32(a), -int(b)))
			vm.stack.data[len(vm.stack.data)-1] = i32(res)
		case i64Clz:
			a := vm.stack.popInt64()
			vm.stack.pushInt64(int64(bits.LeadingZeros64(uint64(a))))
		case i64Ctz:
			a := vm.stack.popInt64()
			vm.stack.pushInt64(int64(bits.TrailingZeros64(uint64(a))))
		case i64Popcnt:
			a := vm.stack.popInt64()
			vm.stack.pushInt64(int64(bits.OnesCount64(uint64(a))))
		case i64Add:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() + b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Sub:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() - b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Mul:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() * b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64DivS:
			err = vm.handleI64DivS()
		case i64DivU:
			err = vm.handleI64DivU()
		case i64RemS:
			err = vm.handleI64RemS()
		case i64RemU:
			err = vm.handleI64RemU()
		case i64And:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() & b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Or:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() | b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Xor:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() ^ b
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Shl:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() << (uint64(b) % 64)
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64ShrS:
			b := vm.stack.popInt64()
			res := vm.stack.data[len(vm.stack.data)-1].int64() >> (uint64(b) % 64)
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64ShrU:
			b := vm.stack.popInt64()
			a := vm.stack.data[len(vm.stack.data)-1].int64()
			res := int64(uint64(a) >> (uint64(b) % 64))
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Rotl:
			b := vm.stack.popInt64()
			a := vm.stack.data[len(vm.stack.data)-1].int64()
			res := int64(bits.RotateLeft64(uint64(a), int(b)))
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
		case i64Rotr:
			b := vm.stack.popInt64()
			a := vm.stack.data[len(vm.stack.data)-1].int64()
			res := int64(bits.RotateLeft64(uint64(a), -int(b)))
			vm.stack.data[len(vm.stack.data)-1] = i64(res)
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
			vm.handleF32Add()
		case f32Sub:
			vm.handleF32Sub()
		case f32Mul:
			vm.handleF32Mul()
		case f32Div:
			vm.handleF32Div()
		case f32Min:
			vm.handleF32Min()
		case f32Max:
			vm.handleF32Max()
		case f32Copysign:
			vm.handleF32Copysign()
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
			vm.handleF64Add()
		case f64Sub:
			vm.handleF64Sub()
		case f64Mul:
			vm.handleF64Mul()
		case f64Div:
			vm.handleF64Div()
		case f64Min:
			vm.handleF64Min()
		case f64Max:
			vm.handleF64Max()
		case f64Copysign:
			vm.handleF64Copysign()
		case i32WrapI64:
			vm.stack.pushInt32(int32(vm.stack.popInt64()))
		case i32TruncF32S:
			err = vm.handleI32TruncF32S()
		case i32TruncF32U:
			err = vm.handleI32TruncF32U()
		case i32TruncF64S:
			err = vm.handleI32TruncF64S()
		case i32TruncF64U:
			err = vm.handleI32TruncF64U()
		case i64ExtendI32S:
			vm.stack.pushInt64(int64(vm.stack.popInt32()))
		case i64ExtendI32U:
			vm.stack.pushInt64(int64(uint32(vm.stack.popInt32())))
		case i64TruncF32S:
			err = vm.handleI64TruncF32S()
		case i64TruncF32U:
			err = vm.handleI64TruncF32U()
		case i64TruncF64S:
			err = vm.handleI64TruncF64S()
		case i64TruncF64U:
			err = vm.handleI64TruncF64U()
		case f32ConvertI32S:
			vm.stack.pushFloat32(float32(vm.stack.popInt32()))
		case f32ConvertI32U:
			vm.stack.pushFloat32(float32(uint32(vm.stack.popInt32())))
		case f32ConvertI64S:
			vm.stack.pushFloat32(float32(vm.stack.popInt64()))
		case f32ConvertI64U:
			vm.stack.pushFloat32(float32(uint64(vm.stack.popInt64())))
		case f32DemoteF64:
			vm.stack.pushFloat32(float32(vm.stack.popFloat64()))
		case f64ConvertI32S:
			vm.stack.pushFloat64(float64(vm.stack.popInt32()))
		case f64ConvertI32U:
			vm.stack.pushFloat64(float64(uint32(vm.stack.popInt32())))
		case f64ConvertI64S:
			vm.stack.pushFloat64(float64(vm.stack.popInt64()))
		case f64ConvertI64U:
			vm.stack.pushFloat64(float64(uint64(vm.stack.popInt64())))
		case f64PromoteF32:
			vm.stack.pushFloat64(float64(vm.stack.popFloat32()))
		case i32ReinterpretF32:
			vm.stack.pushInt32(int32(math.Float32bits(vm.stack.popFloat32())))
		case i64ReinterpretF64:
			vm.stack.pushInt64(int64(math.Float64bits(vm.stack.popFloat64())))
		case f32ReinterpretI32:
			vm.stack.pushFloat32(math.Float32frombits(uint32(vm.stack.popInt32())))
		case f64ReinterpretI64:
			vm.stack.pushFloat64(math.Float64frombits(uint64(vm.stack.popInt64())))
		case i32Extend8S:
			vm.stack.pushInt32(int32(int8(vm.stack.popInt32())))
		case i32Extend16S:
			vm.stack.pushInt32(int32(int16(vm.stack.popInt32())))
		case i64Extend8S:
			vm.stack.pushInt64(int64(int8(vm.stack.popInt64())))
		case i64Extend16S:
			vm.stack.pushInt64(int64(int16(vm.stack.popInt64())))
		case i64Extend32S:
			vm.stack.pushInt64(int64(int32(vm.stack.popInt64())))
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
			err = vm.handleV128Load(frame)
		case v128Load8x8S:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load8x8S(data))
			}
		case v128Load8x8U:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load8x8U(data))
			}
		case v128Load16x4S:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load16x4S(data))
			}
		case v128Load16x4U:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load16x4U(data))
			}
		case v128Load32x2S:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load32x2S(data))
			}
		case v128Load32x2U:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load32x2U(data))
			}
		case v128Load8Splat:
			var data []byte
			if data, err = vm.memGet(frame, 1); err == nil {
				vm.stack.pushV128(simdI8x16SplatFromBytes(data))
			}
		case v128Load16Splat:
			var data []byte
			if data, err = vm.memGet(frame, 2); err == nil {
				vm.stack.pushV128(simdI16x8SplatFromBytes(data))
			}
		case v128Load32Splat:
			var data []byte
			if data, err = vm.memGet(frame, 4); err == nil {
				vm.stack.pushV128(simdI32x4SplatFromBytes(data))
			}
		case v128Load64Splat:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdI64x2SplatFromBytes(data))
			}
		case v128Store:
			err = vm.handleV128Store(frame)
		case v128Const:
			vm.stack.pushV128(V128Value{Low: frame.next(), High: frame.next()})
		case i8x16Shuffle:
			vm.handleI8x16Shuffle(frame)
		case i8x16Swizzle:
			vm.handleI8x16Swizzle()
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
			vm.handleI8x16ExtractLaneS(frame)
		case i8x16ExtractLaneU:
			vm.handleI8x16ExtractLaneU(frame)
		case i8x16ReplaceLane:
			vm.handleI8x16ReplaceLane(frame)
		case i16x8ExtractLaneS:
			vm.handleI16x8ExtractLaneS(frame)
		case i16x8ExtractLaneU:
			vm.handleI16x8ExtractLaneU(frame)
		case i16x8ReplaceLane:
			vm.handleI16x8ReplaceLane(frame)
		case i32x4ExtractLane:
			vm.handleI32x4ExtractLane(frame)
		case i32x4ReplaceLane:
			vm.handleI32x4ReplaceLane(frame)
		case i64x2ExtractLane:
			vm.handleI64x2ExtractLane(frame)
		case i64x2ReplaceLane:
			vm.handleI64x2ReplaceLane(frame)
		case f32x4ExtractLane:
			vm.handleF32x4ExtractLane(frame)
		case f32x4ReplaceLane:
			vm.handleF32x4ReplaceLane(frame)
		case f64x2ExtractLane:
			vm.handleF64x2ExtractLane(frame)
		case f64x2ReplaceLane:
			vm.handleF64x2ReplaceLane(frame)
		case i8x16Eq:
			vm.handleI8x16Eq()
		case i8x16Ne:
			vm.handleI8x16Ne()
		case i8x16LtS:
			vm.handleI8x16LtS()
		case i8x16LtU:
			vm.handleI8x16LtU()
		case i8x16GtS:
			vm.handleI8x16GtS()
		case i8x16GtU:
			vm.handleI8x16GtU()
		case i8x16LeS:
			vm.handleI8x16LeS()
		case i8x16LeU:
			vm.handleI8x16LeU()
		case i8x16GeS:
			vm.handleI8x16GeS()
		case i8x16GeU:
			vm.handleI8x16GeU()
		case i16x8Eq:
			vm.handleI16x8Eq()
		case i16x8Ne:
			vm.handleI16x8Ne()
		case i16x8LtS:
			vm.handleI16x8LtS()
		case i16x8LtU:
			vm.handleI16x8LtU()
		case i16x8GtS:
			vm.handleI16x8GtS()
		case i16x8GtU:
			vm.handleI16x8GtU()
		case i16x8LeS:
			vm.handleI16x8LeS()
		case i16x8LeU:
			vm.handleI16x8LeU()
		case i16x8GeS:
			vm.handleI16x8GeS()
		case i16x8GeU:
			vm.handleI16x8GeU()
		case i32x4Eq:
			vm.handleI32x4Eq()
		case i32x4Ne:
			vm.handleI32x4Ne()
		case i32x4LtS:
			vm.handleI32x4LtS()
		case i32x4LtU:
			vm.handleI32x4LtU()
		case i32x4GtS:
			vm.handleI32x4GtS()
		case i32x4GtU:
			vm.handleI32x4GtU()
		case i32x4LeS:
			vm.handleI32x4LeS()
		case i32x4LeU:
			vm.handleI32x4LeU()
		case i32x4GeS:
			vm.handleI32x4GeS()
		case i32x4GeU:
			vm.handleI32x4GeU()
		case f32x4Eq:
			vm.handleF32x4Eq()
		case f32x4Ne:
			vm.handleF32x4Ne()
		case f32x4Lt:
			vm.handleF32x4Lt()
		case f32x4Gt:
			vm.handleF32x4Gt()
		case f32x4Le:
			vm.handleF32x4Le()
		case f32x4Ge:
			vm.handleF32x4Ge()
		case f64x2Eq:
			vm.handleF64x2Eq()
		case f64x2Ne:
			vm.handleF64x2Ne()
		case f64x2Lt:
			vm.handleF64x2Lt()
		case f64x2Gt:
			vm.handleF64x2Gt()
		case f64x2Le:
			vm.handleF64x2Le()
		case f64x2Ge:
			vm.handleF64x2Ge()
		case v128Not:
			vm.stack.pushV128(simdV128Not(vm.stack.popV128()))
		case v128And:
			vm.handleV128And()
		case v128Andnot:
			vm.handleV128Andnot()
		case v128Or:
			vm.handleV128Or()
		case v128Xor:
			vm.handleV128Xor()
		case v128Bitselect:
			vm.handleV128Bitselect()
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
			var data []byte
			if data, err = vm.memGet(frame, 4); err == nil {
				vm.stack.pushV128(simdV128Load32Zero(data))
			}
		case v128Load64Zero:
			var data []byte
			if data, err = vm.memGet(frame, 8); err == nil {
				vm.stack.pushV128(simdV128Load64Zero(data))
			}
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
			vm.handleI8x16NarrowI16x8S()
		case i8x16NarrowI16x8U:
			vm.handleI8x16NarrowI16x8U()
		case f32x4Ceil:
			vm.stack.pushV128(simdF32x4Ceil(vm.stack.popV128()))
		case f32x4Floor:
			vm.stack.pushV128(simdF32x4Floor(vm.stack.popV128()))
		case f32x4Trunc:
			vm.stack.pushV128(simdF32x4Trunc(vm.stack.popV128()))
		case f32x4Nearest:
			vm.stack.pushV128(simdF32x4Nearest(vm.stack.popV128()))
		case i8x16Shl:
			vm.handleI8x16Shl()
		case i8x16ShrU:
			vm.handleI8x16ShrU()
		case i8x16ShrS:
			vm.handleI8x16ShrS()
		case i8x16Add:
			vm.handleI8x16Add()
		case i8x16AddSatS:
			vm.handleI8x16AddSatS()
		case i8x16AddSatU:
			vm.handleI8x16AddSatU()
		case i8x16Sub:
			vm.handleI8x16Sub()
		case i8x16SubSatS:
			vm.handleI8x16SubSatS()
		case i8x16SubSatU:
			vm.handleI8x16SubSatU()
		case f64x2Ceil:
			vm.stack.pushV128(simdF64x2Ceil(vm.stack.popV128()))
		case f64x2Floor:
			vm.stack.pushV128(simdF64x2Floor(vm.stack.popV128()))
		case i8x16MinS:
			vm.handleI8x16MinS()
		case i8x16MinU:
			vm.handleI8x16MinU()
		case i8x16MaxS:
			vm.handleI8x16MaxS()
		case i8x16MaxU:
			vm.handleI8x16MaxU()
		case f64x2Trunc:
			vm.stack.pushV128(simdF64x2Trunc(vm.stack.popV128()))
		case i8x16AvgrU:
			vm.handleI8x16AvgrU()
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
			vm.handleI16x8Q15mulrSatS()
		case i16x8AllTrue:
			vm.stack.pushInt32(boolToInt32(simdI16x8AllTrue(vm.stack.popV128())))
		case i16x8Bitmask:
			vm.stack.pushInt32(simdI16x8Bitmask(vm.stack.popV128()))
		case i16x8NarrowI32x4S:
			vm.handleI16x8NarrowI32x4S()
		case i16x8NarrowI32x4U:
			vm.handleI16x8NarrowI32x4U()
		case i16x8ExtendLowI8x16S:
			vm.stack.pushV128(simdI16x8ExtendLowI8x16S(vm.stack.popV128()))
		case i16x8ExtendHighI8x16S:
			vm.stack.pushV128(simdI16x8ExtendHighI8x16S(vm.stack.popV128()))
		case i16x8ExtendLowI8x16U:
			vm.stack.pushV128(simdI16x8ExtendLowI8x16U(vm.stack.popV128()))
		case i16x8ExtendHighI8x16U:
			vm.stack.pushV128(simdI16x8ExtendHighI8x16U(vm.stack.popV128()))
		case i16x8Shl:
			vm.handleI16x8Shl()
		case i16x8ShrS:
			vm.handleI16x8ShrS()
		case i16x8ShrU:
			vm.handleI16x8ShrU()
		case i16x8Add:
			vm.handleI16x8Add()
		case i16x8AddSatS:
			vm.handleI16x8AddSatS()
		case i16x8AddSatU:
			vm.handleI16x8AddSatU()
		case i16x8Sub:
			vm.handleI16x8Sub()
		case i16x8SubSatS:
			vm.handleI16x8SubSatS()
		case i16x8SubSatU:
			vm.handleI16x8SubSatU()
		case f64x2Nearest:
			vm.stack.pushV128(simdF64x2Nearest(vm.stack.popV128()))
		case i16x8Mul:
			vm.handleI16x8Mul()
		case i16x8MinS:
			vm.handleI16x8MinS()
		case i16x8MinU:
			vm.handleI16x8MinU()
		case i16x8MaxS:
			vm.handleI16x8MaxS()
		case i16x8MaxU:
			vm.handleI16x8MaxU()
		case i16x8AvgrU:
			vm.handleI16x8AvgrU()
		case i16x8ExtmulLowI8x16S:
			vm.handleI16x8ExtmulLowI8x16S()
		case i16x8ExtmulHighI8x16S:
			vm.handleI16x8ExtmulHighI8x16S()
		case i16x8ExtmulLowI8x16U:
			vm.handleI16x8ExtmulLowI8x16U()
		case i16x8ExtmulHighI8x16U:
			vm.handleI16x8ExtmulHighI8x16U()
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
			vm.handleI32x4Shl()
		case i32x4ShrS:
			vm.handleI32x4ShrS()
		case i32x4ShrU:
			vm.handleI32x4ShrU()
		case i32x4Add:
			vm.handleI32x4Add()
		case i32x4Sub:
			vm.handleI32x4Sub()
		case i32x4Mul:
			vm.handleI32x4Mul()
		case i32x4MinS:
			vm.handleI32x4MinS()
		case i32x4MinU:
			vm.handleI32x4MinU()
		case i32x4MaxS:
			vm.handleI32x4MaxS()
		case i32x4MaxU:
			vm.handleI32x4MaxU()
		case i32x4DotI16x8S:
			vm.handleI32x4DotI16x8S()
		case i32x4ExtmulLowI16x8S:
			vm.handleI32x4ExtmulLowI16x8S()
		case i32x4ExtmulHighI16x8S:
			vm.handleI32x4ExtmulHighI16x8S()
		case i32x4ExtmulLowI16x8U:
			vm.handleI32x4ExtmulLowI16x8U()
		case i32x4ExtmulHighI16x8U:
			vm.handleI32x4ExtmulHighI16x8U()
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
			vm.handleI64x2Shl()
		case i64x2ShrS:
			vm.handleI64x2ShrS()
		case i64x2ShrU:
			vm.handleI64x2ShrU()
		case i64x2Add:
			vm.handleI64x2Add()
		case i64x2Sub:
			vm.handleI64x2Sub()
		case i64x2Mul:
			vm.handleI64x2Mul()
		case i64x2Eq:
			vm.handleI64x2Eq()
		case i64x2Ne:
			vm.handleI64x2Ne()
		case i64x2LtS:
			vm.handleI64x2LtS()
		case i64x2GtS:
			vm.handleI64x2GtS()
		case i64x2LeS:
			vm.handleI64x2LeS()
		case i64x2GeS:
			vm.handleI64x2GeS()
		case i64x2ExtmulLowI32x4S:
			vm.handleI64x2ExtmulLowI32x4S()
		case i64x2ExtmulHighI32x4S:
			vm.handleI64x2ExtmulHighI32x4S()
		case i64x2ExtmulLowI32x4U:
			vm.handleI64x2ExtmulLowI32x4U()
		case i64x2ExtmulHighI32x4U:
			vm.handleI64x2ExtmulHighI32x4U()
		case f32x4Abs:
			vm.stack.pushV128(simdF32x4Abs(vm.stack.popV128()))
		case f32x4Neg:
			vm.stack.pushV128(simdF32x4Neg(vm.stack.popV128()))
		case f32x4Sqrt:
			vm.stack.pushV128(simdF32x4Sqrt(vm.stack.popV128()))
		case f32x4Add:
			vm.handleF32x4Add()
		case f32x4Sub:
			vm.handleF32x4Sub()
		case f32x4Mul:
			vm.handleF32x4Mul()
		case f32x4Div:
			vm.handleF32x4Div()
		case f32x4Min:
			vm.handleF32x4Min()
		case f32x4Max:
			vm.handleF32x4Max()
		case f32x4Pmin:
			vm.handleF32x4Pmin()
		case f32x4Pmax:
			vm.handleF32x4Pmax()
		case f64x2Abs:
			vm.stack.pushV128(simdF64x2Abs(vm.stack.popV128()))
		case f64x2Neg:
			vm.stack.pushV128(simdF64x2Neg(vm.stack.popV128()))
		case f64x2Sqrt:
			vm.stack.pushV128(simdF64x2Sqrt(vm.stack.popV128()))
		case f64x2Add:
			vm.handleF64x2Add()
		case f64x2Sub:
			vm.handleF64x2Sub()
		case f64x2Mul:
			vm.handleF64x2Mul()
		case f64x2Div:
			vm.handleF64x2Div()
		case f64x2Min:
			vm.handleF64x2Min()
		case f64x2Max:
			vm.handleF64x2Max()
		case f64x2Pmin:
			vm.handleF64x2Pmin()
		case f64x2Pmax:
			vm.handleF64x2Pmax()
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
		if err != nil {
			vm.callStack = vm.callStack[:len(vm.callStack)-1]
			return err
		}
	}
	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	return nil
}

func (vm *vm) pushBlockFrame(frame *callFrame, opcode opcode) {
	blockType := int32(frame.next())
	// For loops, the continuation is a branch back to the start of the block.
	var continuationPc uint32
	if opcode == loop {
		continuationPc = frame.pc
	} else {
		continuationPc = frame.function.jumpCache[frame.pc]
	}

	vm.pushControlFrame(frame, controlFrame{
		isLoop:         opcode == loop,
		blockType:      blockType,
		stackHeight:    vm.stack.size() - vm.getInputCount(frame.module, blockType),
		continuationPc: continuationPc,
	})
}

func (vm *vm) handleIf(frame *callFrame) {
	condition := vm.stack.popInt32()

	vm.pushBlockFrame(frame, ifOp)

	if condition != 0 {
		return
	}

	frame.pc = frame.function.jumpElseCache[frame.pc]
}

func (vm *vm) handleElse(frame *callFrame) {
	// When we encounter an 'else' instruction, it means we have just finished
	// executing the 'then' block of an 'if' statement. We need to jump to the
	// 'end' of the 'if' block, skipping the 'else' block.
	ifFrame := vm.popControlFrame(frame)
	frame.pc = ifFrame.continuationPc
}

func (vm *vm) handleEnd(frame *callFrame) {
	frame.controlStack = frame.controlStack[:len(frame.controlStack)-1]
}

func (vm *vm) handleBrIf(frame *callFrame) {
	labelIndex := uint32(frame.next())
	val := vm.stack.popInt32()
	if val == 0 {
		return
	}
	vm.brToLabel(frame, labelIndex)
}

func (vm *vm) handleBrTable(frame *callFrame) {
	size := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	var targetLabel uint32
	if index < size {
		targetLabel = uint32(frame.function.body[frame.pc+index])
	} else {
		targetLabel = uint32(frame.function.body[frame.pc+size])
	}
	frame.pc += size + 1
	vm.brToLabel(frame, targetLabel)
}

func (vm *vm) brToLabel(frame *callFrame, labelIndex uint32) {
	targetIndex := len(frame.controlStack) - int(labelIndex) - 1
	targetFrame := frame.controlStack[targetIndex]
	frame.controlStack = frame.controlStack[:targetIndex]

	var arity uint32
	if targetFrame.isLoop {
		arity = vm.getInputCount(frame.module, targetFrame.blockType)
	} else {
		arity = vm.getOutputCount(frame.module, targetFrame.blockType)
	}

	vm.stack.unwind(targetFrame.stackHeight, arity)
	if targetFrame.isLoop {
		vm.pushControlFrame(frame, targetFrame)
	}

	frame.pc = targetFrame.continuationPc
}

func (vm *vm) handleCall(frame *callFrame) error {
	functionIndex := frame.module.funcAddrs[frame.next()]
	function := vm.store.funcs[functionIndex]
	return vm.invokeFunction(function)
}

func (vm *vm) handleCallIndirect(frame *callFrame) error {
	expectedType := frame.module.types[frame.next()]
	table := vm.getTable(frame, frame.next())

	elementIndex := vm.stack.popInt32()

	tableElement, err := table.Get(elementIndex)
	if err != nil {
		return err
	}
	if tableElement == NullReference {
		return fmt.Errorf("uninitialized element %d", elementIndex)
	}

	function := vm.store.funcs[tableElement]
	if !function.GetType().Equal(expectedType) {
		return errIndirectCallTypeMismatch
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
	global := vm.getGlobal(frame, frame.next())
	vm.stack.push(global.value)
}

func (vm *vm) handleGlobalSet(frame *callFrame) {
	global := vm.getGlobal(frame, frame.next())
	global.value = vm.stack.pop()
}

func (vm *vm) handleTableGet(frame *callFrame) error {
	table := vm.getTable(frame, frame.next())
	index := vm.stack.popInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}

	vm.stack.pushInt32(element)
	return nil
}

func (vm *vm) handleTableSet(frame *callFrame) error {
	table := vm.getTable(frame, frame.next())
	reference := vm.stack.popInt32()
	index := vm.stack.popInt32()
	return table.Set(index, reference)
}

func (vm *vm) handleMemorySize(frame *callFrame) {
	memory := vm.getMemory(frame, frame.next())
	vm.stack.pushInt32(memory.Size())
}

func (vm *vm) handleMemoryGrow(frame *callFrame) {
	memory := vm.getMemory(frame, frame.next())
	pages := vm.stack.popInt32()
	oldSize := memory.Grow(pages)
	vm.stack.pushInt32(oldSize)
}

func (vm *vm) handleRefFunc(frame *callFrame) {
	storeIndex := frame.module.funcAddrs[frame.next()]
	vm.stack.pushInt32(int32(storeIndex))
}

func (vm *vm) handleRefIsNull() {
	top := vm.stack.popInt32()
	vm.stack.pushInt32(boolToInt32(top == NullReference))
}

func (vm *vm) handleMemoryInit(frame *callFrame) error {
	data := vm.getData(frame, frame.next())
	memory := vm.getMemory(frame, frame.next())
	n, s, d := vm.stack.pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.content)
}

func (vm *vm) handleDataDrop(frame *callFrame) {
	dataSegment := vm.getData(frame, frame.next())
	dataSegment.content = nil
}

func (vm *vm) handleMemoryCopy(frame *callFrame) error {
	destMemory := vm.getMemory(frame, frame.next())
	srcMemory := vm.getMemory(frame, frame.next())
	n, s, d := vm.stack.pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *vm) handleMemoryFill(frame *callFrame) error {
	memory := vm.getMemory(frame, frame.next())
	n, val, offset := vm.stack.pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *vm) handleTableInit(frame *callFrame) error {
	element := vm.getElement(frame, frame.next())
	table := vm.getTable(frame, frame.next())
	n, s, d := vm.stack.pop3Int32()
	// element.functionIndexes is nil for dropped, active, and declarative
	// segments; only passive segments retain their entries until elem.drop.
	return table.Init(n, d, s, element.functionIndexes)
}

func (vm *vm) handleElemDrop(frame *callFrame) {
	element := vm.getElement(frame, frame.next())
	element.functionIndexes = nil
}

func (vm *vm) handleTableCopy(frame *callFrame) error {
	destTable := vm.getTable(frame, frame.next())
	srcTable := vm.getTable(frame, frame.next())
	n, s, d := vm.stack.pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *vm) handleTableGrow(frame *callFrame) {
	table := vm.getTable(frame, frame.next())
	n := vm.stack.popInt32()
	val := vm.stack.popInt32()
	vm.stack.pushInt32(table.Grow(n, val))
}

func (vm *vm) handleTableSize(frame *callFrame) {
	table := vm.getTable(frame, frame.next())
	vm.stack.pushInt32(int32(table.Size()))
}

func (vm *vm) handleTableFill(frame *callFrame) error {
	table := vm.getTable(frame, frame.next())
	n, val, i := vm.stack.pop3Int32()
	return table.Fill(n, i, val)
}

func (vm *vm) getInputCount(module *ModuleInstance, blockType int32) uint32 {
	// Empty (-0x40) and value-type block types both consume no inputs.
	if blockType < 0 {
		return 0
	}
	return uint32(len(module.types[blockType].ParamTypes))
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

func (vm *vm) pushControlFrame(frame *callFrame, controlFrame controlFrame) {
	frame.controlStack = append(frame.controlStack, controlFrame)
}

func (vm *vm) popControlFrame(frame *callFrame) controlFrame {
	// Validation guarantees the control stack is never empty.
	index := len(frame.controlStack) - 1
	controlFrame := frame.controlStack[index]
	frame.controlStack = frame.controlStack[:index]
	return controlFrame
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

	// popValueTypes returns a fresh slice per call, so the host may retain args.
	args := vm.stack.popValueTypes(fun.GetType().ParamTypes)
	res := fun.hostCode(fun.module, args...)
	if len(res) != len(fun.GetType().ResultTypes) {
		return errHostResultCountMismatch
	}

	vm.stack.pushAll(res)
	return err
}

func (vm *vm) invokeExpression(
	expression []uint64,
	resultType ValueType,
	moduleInstance *ModuleInstance,
) (value, error) {
	// We create a fake function to execute the expression. The expression is
	// expected to return a single value.
	function := wasmFunction{
		functionType: FunctionType{
			ParamTypes:  []ValueType{},
			ResultTypes: []ValueType{resultType},
		},
		code: function{
			body:      expression,
			typeIndex: 0xffffffff, // Represents -1. See getOutputCount.
		},
		module: moduleInstance,
	}
	if err := vm.invokeWasmFunction(&function); err != nil {
		return value{}, err
	}
	return vm.stack.pop(), nil
}

func (vm *vm) applyActiveElementSegment(
	segment elementSegment,
	elem *elementInstance,
	moduleInstance *ModuleInstance,
) error {
	offsetExpression := segment.offsetExpression
	offsetVal, err := vm.invokeExpression(offsetExpression, I32, moduleInstance)
	if err != nil {
		return err
	}
	table := vm.store.tables[moduleInstance.tableAddrs[segment.tableIndex]]
	return table.InitFromSlice(offsetVal.int32(), elem.functionIndexes)
}

func (vm *vm) applyActiveDataSegment(
	segment dataSegment,
	data *dataInstance,
	moduleInstance *ModuleInstance,
) error {
	offsetExpression := segment.offsetExpression
	offsetVal, err := vm.invokeExpression(offsetExpression, I32, moduleInstance)
	if err != nil {
		return err
	}
	memory := vm.store.memories[moduleInstance.memAddrs[segment.memoryIndex]]
	return memory.Set(uint32(offsetVal.int32()), 0, data.content)
}

func (vm *vm) newElementInstance(
	segment elementSegment,
	moduleInstance *ModuleInstance,
) (elementInstance, error) {
	var indexes []int32
	switch {
	case len(segment.functionIndexes) > 0:
		indexes = toStoreFuncIndexes(moduleInstance, segment.functionIndexes)
	case len(segment.functionIndexesExpressions) > 0:
		indexes = make([]int32, len(segment.functionIndexesExpressions))
		for i, expr := range segment.functionIndexesExpressions {
			refVal, err := vm.invokeExpression(expr, segment.kind, moduleInstance)
			if err != nil {
				return elementInstance{}, err
			}
			indexes[i] = refVal.int32()
		}
	}
	return elementInstance{functionIndexes: indexes}, nil
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

func (vm *vm) getTable(frame *callFrame, localIndex uint64) *Table {
	tableIndex := frame.module.tableAddrs[localIndex]
	return vm.store.tables[tableIndex]
}

func (vm *vm) getMemory(frame *callFrame, localIndex uint64) *Memory {
	memoryIndex := frame.module.memAddrs[localIndex]
	return vm.store.memories[memoryIndex]
}

func (vm *vm) getGlobal(frame *callFrame, localIndex uint64) *Global {
	globalIndex := frame.module.globalAddrs[localIndex]
	return vm.store.globals[globalIndex]
}

func (vm *vm) getElement(frame *callFrame, localIndex uint64) *elementInstance {
	elementIndex := frame.module.elemAddrs[localIndex]
	return &vm.store.elements[elementIndex]
}

func (vm *vm) getData(frame *callFrame, localIndex uint64) *dataInstance {
	dataIndex := frame.module.dataAddrs[localIndex]
	return &vm.store.datas[dataIndex]
}
