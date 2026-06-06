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
// consists of the runtime representation of all instances of functions,
// tables, memories, globals, element segments, and data segments that have
// been allocated during the vm life time.
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
	locals []value
	module *ModuleInstance
}

// controlFrame represents a block of code that can be branched to.
type controlFrame struct {
	isLoop      bool
	targetIp    int32
	stackHeight uint32
	arity       uint32
}

// A frame returns the next instruction index, or one of these sentinels.
const (
	advance = -1 // continue to the next instruction
	trap    = -2 // stop; frameCtx.trap holds the error
)

type frame func(c *frameCtx) int

type frameCtx struct {
	vm     *vm
	locals []value
	module *ModuleInstance // for stateful ops: memory / global / table / call
	ctrl   []controlFrame
	trap   error
}

// brToLabel branches out labelIndex enclosing blocks, unwinding the value stack
// to the target's height (keeping its arity values) and returning the target
// instruction index.
func (c *frameCtx) brToLabel(labelIndex int) int {
	targetIndex := len(c.ctrl) - labelIndex - 1
	target := c.ctrl[targetIndex]
	c.ctrl = c.ctrl[:targetIndex]
	c.vm.stack.unwind(target.stackHeight, target.arity)
	if target.isLoop {
		c.ctrl = append(c.ctrl, target)
	}
	return int(target.targetIp)
}

func (c *frameCtx) handleBrTable(labels []uint32, defaultLabel uint32) int {
	index := uint32(c.vm.stack.popInt32())
	if index < uint32(len(labels)) {
		return c.brToLabel(int(labels[index]))
	}
	return c.brToLabel(int(defaultLabel))
}

// vm is the WebAssembly Virtual Machine.
type vm struct {
	store             *store
	stack             *valueStack
	callStack         []callFrame
	controlStackCache []controlFrame
	frameCtxCache     []frameCtx
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
		frameCtxCache:     make([]frameCtx, config.CallStackPreallocationSize),
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
		if err := vm.compile(wasmFunc); err != nil {
			return nil, err
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

	// Allocate runtime data instances. The byte slice is shared with the
	// parsed dataSegment: the runtime only ever reads it (memory.init copies
	// out) and data.drop nils the instance's slice header, never the backing
	// array, so the moduleDefinition is left untouched and can be reinstantiated.
	for _, segment := range module.dataSegments {
		storeIndex := uint32(len(vm.store.datas))
		moduleInstance.dataAddrs = append(moduleInstance.dataAddrs, storeIndex)
		data := dataInstance{content: segment.content}
		vm.store.datas = append(vm.store.datas, data)
	}

	// Apply active data segments to their target memories, then drop them. After
	// this, memory.init against an active segment sees an empty content and
	// traps for any non-zero size.
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

	vm.callStack = append(vm.callStack, callFrame{
		locals: locals,
		module: function.module,
	})

	// To avoid the performance penalty of checking the fuel limit on every
	// instruction when fuel is disabled, we provide two separate loop
	// implementations.
	frameCtx := vm.newFrameCtx(function)
	var err error
	if vm.config.EnableFuel {
		err = vm.runLoopWithFuel(frameCtx, function.frames)
	} else {
		err = vm.runLoop(frameCtx, function.frames)
	}
	// The run loop pops this frame before returning, so its locals slots are free
	// to reuse. Rewinding the cursor here is a no-op for the heap case.
	vm.localsTop = localsMark
	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	return err
}

func (vm *vm) runLoop(frameCtx *frameCtx, frames []frame) error {
	ip := 0
	for ip < len(frames) {
		switch n := frames[ip](frameCtx); n {
		case advance:
			ip++
		case trap:
			return frameCtx.trap
		default:
			ip = n
		}
	}
	return nil
}

func (vm *vm) runLoopWithFuel(frameCtx *frameCtx, frames []frame) error {
	ip := 0
	for ip < len(frames) {
		if vm.fuel == 0 {
			return errFuelExhausted
		}
		vm.fuel--
		switch n := frames[ip](frameCtx); n {
		case advance:
			ip++
		case trap:
			return frameCtx.trap
		default:
			ip = n
		}
	}
	return nil
}

// operandWordCount returns the number of operand words after the opcode at
// body[operandStart-1], matching the encoding produced by parser.readCode.
func operandWordCount(op opcode, body []uint64, operandStart int) int {
	switch op {
	case block, loop, ifOp, i32Const, i64Const, f32Const, f64Const,
		br, brIf, call, localGet, localSet, localTee, globalGet, globalSet,
		tableGet, tableSet, memoryFill, dataDrop, elemDrop, tableGrow, tableSize,
		tableFill, refNull, refFunc, memorySize, memoryGrow,
		i8x16ExtractLaneS, i8x16ExtractLaneU, i16x8ExtractLaneS, i16x8ExtractLaneU,
		i32x4ExtractLane, i64x2ExtractLane, f32x4ExtractLane, f64x2ExtractLane,
		i8x16ReplaceLane, i16x8ReplaceLane, i32x4ReplaceLane, i64x2ReplaceLane,
		f32x4ReplaceLane, f64x2ReplaceLane:
		return 1
	case callIndirect, memoryInit, memoryCopy, tableInit, tableCopy, v128Const:
		return 2
	case i32Load, i64Load, f32Load, f64Load, i32Load8S, i32Load8U, i32Load16S,
		i32Load16U, i64Load8S, i64Load8U, i64Load16S, i64Load16U, i64Load32S,
		i64Load32U, i32Store, i64Store, f32Store, f64Store, i32Store8, i32Store16,
		i64Store8, i64Store16, i64Store32,
		v128Load, v128Load32Zero, v128Load64Zero, v128Load8Splat, v128Load16Splat,
		v128Load32Splat, v128Load64Splat, v128Load8x8S, v128Load8x8U,
		v128Load16x4S, v128Load16x4U, v128Load32x2S, v128Load32x2U, v128Store:
		return 3
	case v128Load8Lane, v128Load16Lane, v128Load32Lane, v128Load64Lane,
		v128Store8Lane, v128Store16Lane, v128Store32Lane, v128Store64Lane:
		return 4
	case i8x16Shuffle:
		return 16
	case brTable:
		// 1 count word + N label words + 1 default word.
		return int(body[operandStart]) + 2
	case selectT:
		// 1 count word + N type words.
		return int(body[operandStart]) + 1
	default:
		return 0
	}
}

// newFrameCtx returns the execution context for function.
func (vm *vm) newFrameCtx(function *wasmFunction) *frameCtx {
	callDepth := len(vm.callStack) - 1
	frame := &vm.callStack[callDepth]

	var c *frameCtx
	var ctrl []controlFrame
	if callDepth < vm.config.CallStackPreallocationSize {
		c = &vm.frameCtxCache[callDepth]
		base := callDepth * controlStackCacheSlotSize
		ctrl = vm.controlStackCache[base : base : base+controlStackCacheSlotSize]
	} else {
		c = &frameCtx{}
	}
	*c = frameCtx{vm: vm, locals: frame.locals, module: frame.module}
	c.ctrl = append(ctrl, controlFrame{
		targetIp:    int32(len(function.frames)),
		arity:       uint32(len(function.functionType.ResultTypes)),
		stackHeight: vm.stack.size(),
	})
	return c
}

// compile lowers a function to its frames; it runs once before the function is
// invoked. An unhandled opcode returns an error: validation has already
// accepted the module, so that signals a gap in the compiler, not bad input.
func (vm *vm) compile(fn *wasmFunction) error {
	frames, err := vm.compileClosures(&fn.code, fn.module)
	if err != nil {
		return err
	}
	fn.frames = frames
	// Frames are the only runtime representation now; release the compile inputs.
	fn.code.body = nil
	fn.code.jumpCache = nil
	fn.code.jumpElseCache = nil
	return nil
}

// compileClosures lowers a function body to its frames, returning an error on
// an opcode it does not handle.
func (vm *vm) compileClosures(fn *function, module *ModuleInstance) ([]frame, error) {
	body := fn.body

	// First pass: assign an instruction index to every bytecode boundary so
	// branch targets (which the parser recorded as bytecode pcs) can be
	// translated into instruction indices.
	pcToIp := make(map[uint32]int, len(body))
	starts := make([]int, 0, len(body))
	for i := 0; i < len(body); {
		pcToIp[uint32(i)] = len(starts)
		starts = append(starts, i)
		op := opcode(body[i])
		i += 1 + operandWordCount(op, body, i+1)
	}
	pcToIp[uint32(len(body))] = len(starts)

	// Second pass: emit one closure per instruction.
	code := make([]frame, 0, len(starts))
	for _, pc := range starts {
		instr, err := vm.compileInstr(fn, module, pc, pcToIp)
		if err != nil {
			return nil, err
		}
		code = append(code, instr)
	}
	return code, nil
}

// compileInstr builds the closure for the instruction at bytecode index pc.
func (vm *vm) compileInstr(
	fn *function, module *ModuleInstance, pc int, pcToIp map[uint32]int,
) (frame, error) {
	body := fn.body
	op := opcode(body[pc])

	switch op {
	case unreachable:
		return func(c *frameCtx) int { c.trap = errUnreachable; return trap }, nil
	case nop:
		return func(c *frameCtx) int { return advance }, nil
	case block:
		blockType := int32(body[pc+1])
		afterEndIp := pcToIp[fn.jumpCache[uint32(pc+2)]]
		inputCount := vm.getInputCount(module, blockType)
		outputCount := vm.getOutputCount(module, blockType)
		return func(c *frameCtx) int {
			c.ctrl = append(c.ctrl, controlFrame{
				targetIp:    int32(afterEndIp),
				arity:       outputCount,
				stackHeight: c.vm.stack.size() - inputCount,
			})
			return advance
		}, nil
	case loop:
		blockType := int32(body[pc+1])
		bodyIp := pcToIp[uint32(pc+2)]
		inputCount := vm.getInputCount(module, blockType)
		return func(c *frameCtx) int {
			c.ctrl = append(c.ctrl, controlFrame{
				isLoop:      true,
				targetIp:    int32(bodyIp),
				arity:       inputCount,
				stackHeight: c.vm.stack.size() - inputCount,
			})
			return advance
		}, nil
	case ifOp:
		blockType := int32(body[pc+1])
		afterEndIp := pcToIp[fn.jumpCache[uint32(pc+2)]]
		elseIp := pcToIp[fn.jumpElseCache[uint32(pc+2)]]
		inputCount := vm.getInputCount(module, blockType)
		outputCount := vm.getOutputCount(module, blockType)
		return func(c *frameCtx) int {
			condition := c.vm.stack.popInt32()
			c.ctrl = append(c.ctrl, controlFrame{
				targetIp:    int32(afterEndIp),
				arity:       outputCount,
				stackHeight: c.vm.stack.size() - inputCount,
			})
			if condition == 0 {
				return elseIp
			}
			return advance
		}, nil
	case elseOp:
		return func(c *frameCtx) int {
			target := c.ctrl[len(c.ctrl)-1].targetIp
			c.ctrl = c.ctrl[:len(c.ctrl)-1]
			return int(target)
		}, nil
	case end:
		return func(c *frameCtx) int {
			if len(c.ctrl) > 0 {
				c.ctrl = c.ctrl[:len(c.ctrl)-1]
			}
			return advance
		}, nil
	case br:
		label := int(body[pc+1])
		return func(c *frameCtx) int { return c.brToLabel(label) }, nil
	case brIf:
		label := int(body[pc+1])
		return func(c *frameCtx) int {
			if c.vm.stack.popInt32() != 0 {
				return c.brToLabel(label)
			}
			return advance
		}, nil
	case brTable:
		count := uint32(body[pc+1])
		labels := make([]uint32, count)
		for i := range labels {
			labels[i] = uint32(body[pc+2+i])
		}
		defaultLabel := uint32(body[pc+2+int(count)])
		return func(c *frameCtx) int { return c.handleBrTable(labels, defaultLabel) }, nil
	case returnOp:
		return func(c *frameCtx) int { return c.brToLabel(len(c.ctrl) - 1) }, nil
	case call:
		funcIndex := body[pc+1]
		return safe(func(c *frameCtx) error { return c.vm.handleCall(c.module, funcIndex) }), nil
	case callIndirect:
		typeIndex, tableIndex := body[pc+1], body[pc+2]
		return safe(func(c *frameCtx) error {
			return c.vm.handleCallIndirect(c.module, typeIndex, tableIndex)
		}), nil
	case drop:
		return func(c *frameCtx) int { c.vm.stack.drop(); return advance }, nil
	case selectOp:
		return func(c *frameCtx) int { c.vm.handleSelect(); return advance }, nil
	case selectT:
		// The type-vector operand is for validation only; semantics match select.
		return func(c *frameCtx) int { c.vm.handleSelect(); return advance }, nil
	case localGet:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.stack.push(c.locals[idx]); return advance }, nil
	case localSet:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.locals[idx] = c.vm.stack.pop(); return advance }, nil
	case localTee:
		idx := body[pc+1]
		return func(c *frameCtx) int {
			c.locals[idx] = c.vm.stack.data[len(c.vm.stack.data)-1]
			return advance
		}, nil
	case globalGet:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleGlobalGet(c.module, idx); return advance }, nil
	case globalSet:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleGlobalSet(c.module, idx); return advance }, nil
	case tableGet:
		idx := body[pc+1]
		return safe(func(c *frameCtx) error { return c.vm.handleTableGet(c.module, idx) }), nil
	case tableSet:
		idx := body[pc+1]
		return safe(func(c *frameCtx) error { return c.vm.handleTableSet(c.module, idx) }), nil
	case i32Load:
		return handleLoad(body, pc, vm.stack.pushInt32, (*Memory).LoadUint32, uint32ToInt32), nil
	case i64Load:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadUint64, uint64ToInt64), nil
	case f32Load:
		return handleLoad(body, pc, vm.stack.pushFloat32, (*Memory).LoadUint32, math.Float32frombits), nil
	case f64Load:
		return handleLoad(body, pc, vm.stack.pushFloat64, (*Memory).LoadUint64, math.Float64frombits), nil
	case i32Load8S:
		return handleLoad(body, pc, vm.stack.pushInt32, (*Memory).LoadByte, signExtend8To32), nil
	case i32Load8U:
		return handleLoad(body, pc, vm.stack.pushInt32, (*Memory).LoadByte, zeroExtend8To32), nil
	case i32Load16S:
		return handleLoad(body, pc, vm.stack.pushInt32, (*Memory).LoadUint16, signExtend16To32), nil
	case i32Load16U:
		return handleLoad(body, pc, vm.stack.pushInt32, (*Memory).LoadUint16, zeroExtend16To32), nil
	case i64Load8S:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadByte, signExtend8To64), nil
	case i64Load8U:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadByte, zeroExtend8To64), nil
	case i64Load16S:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadUint16, signExtend16To64), nil
	case i64Load16U:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadUint16, zeroExtend16To64), nil
	case i64Load32S:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadUint32, signExtend32To64), nil
	case i64Load32U:
		return handleLoad(body, pc, vm.stack.pushInt64, (*Memory).LoadUint32, zeroExtend32To64), nil
	case i32Store:
		return handleStore(body, pc, vm.stack.popInt32, func(m *Memory, o, i uint32, v int32) error { return m.StoreUint32(o, i, uint32(v)) }), nil
	case i64Store:
		return handleStore(body, pc, vm.stack.popInt64, func(m *Memory, o, i uint32, v int64) error { return m.StoreUint64(o, i, uint64(v)) }), nil
	case f32Store:
		return handleStore(body, pc, vm.stack.popFloat32, func(m *Memory, o, i uint32, v float32) error { return m.StoreUint32(o, i, math.Float32bits(v)) }), nil
	case f64Store:
		return handleStore(body, pc, vm.stack.popFloat64, func(m *Memory, o, i uint32, v float64) error { return m.StoreUint64(o, i, math.Float64bits(v)) }), nil
	case i32Store8:
		return handleStore(body, pc, vm.stack.popInt32, func(m *Memory, o, i uint32, v int32) error { return m.StoreByte(o, i, byte(v)) }), nil
	case i32Store16:
		return handleStore(body, pc, vm.stack.popInt32, func(m *Memory, o, i uint32, v int32) error { return m.StoreUint16(o, i, uint16(v)) }), nil
	case i64Store8:
		return handleStore(body, pc, vm.stack.popInt64, func(m *Memory, o, i uint32, v int64) error { return m.StoreByte(o, i, byte(v)) }), nil
	case i64Store16:
		return handleStore(body, pc, vm.stack.popInt64, func(m *Memory, o, i uint32, v int64) error { return m.StoreUint16(o, i, uint16(v)) }), nil
	case i64Store32:
		return handleStore(body, pc, vm.stack.popInt64, func(m *Memory, o, i uint32, v int64) error { return m.StoreUint32(o, i, uint32(v)) }), nil
	case memorySize:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleMemorySize(c.module, idx); return advance }, nil
	case memoryGrow:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleMemoryGrow(c.module, idx); return advance }, nil
	case i32Const:
		v := int32(body[pc+1])
		return func(c *frameCtx) int { c.vm.stack.pushInt32(v); return advance }, nil
	case i64Const:
		v := int64(body[pc+1])
		return func(c *frameCtx) int { c.vm.stack.pushInt64(v); return advance }, nil
	case f32Const:
		v := math.Float32frombits(uint32(body[pc+1]))
		return func(c *frameCtx) int { c.vm.stack.pushFloat32(v); return advance }, nil
	case f64Const:
		v := math.Float64frombits(body[pc+1])
		return func(c *frameCtx) int { c.vm.stack.pushFloat64(v); return advance }, nil
	case i32Eqz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(c.vm.stack.popInt32() == 0)) }), nil
	case i32Eq:
		return cmp32(equal[int32]), nil
	case i32Ne:
		return cmp32(notEqual[int32]), nil
	case i32LtS:
		return cmp32(lessThan[int32]), nil
	case i32LtU:
		return cmp32(lessThanU32), nil
	case i32GtS:
		return cmp32(greaterThan[int32]), nil
	case i32GtU:
		return cmp32(greaterThanU32), nil
	case i32LeS:
		return cmp32(lessOrEqual[int32]), nil
	case i32LeU:
		return cmp32(lessOrEqualU32), nil
	case i32GeS:
		return cmp32(greaterOrEqual[int32]), nil
	case i32GeU:
		return cmp32(greaterOrEqualU32), nil
	case i64Eqz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(c.vm.stack.popInt64() == 0)) }), nil
	case i64Eq:
		return cmp64(equal[int64]), nil
	case i64Ne:
		return cmp64(notEqual[int64]), nil
	case i64LtS:
		return cmp64(lessThan[int64]), nil
	case i64LtU:
		return cmp64(lessThanU64), nil
	case i64GtS:
		return cmp64(greaterThan[int64]), nil
	case i64GtU:
		return cmp64(greaterThanU64), nil
	case i64LeS:
		return cmp64(lessOrEqual[int64]), nil
	case i64LeU:
		return cmp64(lessOrEqualU64), nil
	case i64GeS:
		return cmp64(greaterOrEqual[int64]), nil
	case i64GeU:
		return cmp64(greaterOrEqualU64), nil
	case f32Eq:
		return cmpf32(equal[float32]), nil
	case f32Ne:
		return cmpf32(notEqual[float32]), nil
	case f32Lt:
		return cmpf32(lessThan[float32]), nil
	case f32Gt:
		return cmpf32(greaterThan[float32]), nil
	case f32Le:
		return cmpf32(lessOrEqual[float32]), nil
	case f32Ge:
		return cmpf32(greaterOrEqual[float32]), nil
	case f64Eq:
		return cmpf64(equal[float64]), nil
	case f64Ne:
		return cmpf64(notEqual[float64]), nil
	case f64Lt:
		return cmpf64(lessThan[float64]), nil
	case f64Gt:
		return cmpf64(greaterThan[float64]), nil
	case f64Le:
		return cmpf64(lessOrEqual[float64]), nil
	case f64Ge:
		return cmpf64(greaterOrEqual[float64]), nil
	case i32Clz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(clz32(c.vm.stack.popInt32())) }), nil
	case i32Ctz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(ctz32(c.vm.stack.popInt32())) }), nil
	case i32Popcnt:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(popcnt32(c.vm.stack.popInt32())) }), nil
	case i32Add:
		return alu32(func(a, b int32) int32 { return a + b }), nil
	case i32Sub:
		return alu32(func(a, b int32) int32 { return a - b }), nil
	case i32Mul:
		return alu32(func(a, b int32) int32 { return a * b }), nil
	case i32DivS:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt32(divS32) }), nil
	case i32DivU:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt32(divU32) }), nil
	case i32RemS:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt32(remS32) }), nil
	case i32RemU:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt32(remU32) }), nil
	case i32And:
		return alu32(func(a, b int32) int32 { return a & b }), nil
	case i32Or:
		return alu32(func(a, b int32) int32 { return a | b }), nil
	case i32Xor:
		return alu32(func(a, b int32) int32 { return a ^ b }), nil
	case i32Shl:
		return alu32(func(a, b int32) int32 { return a << (uint32(b) % 32) }), nil
	case i32ShrS:
		return alu32(func(a, b int32) int32 { return a >> (uint32(b) % 32) }), nil
	case i32ShrU:
		return alu32(shrU32), nil
	case i32Rotl:
		return alu32(rotl32), nil
	case i32Rotr:
		return alu32(rotr32), nil
	case i64Clz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(clz64(c.vm.stack.popInt64())) }), nil
	case i64Ctz:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(ctz64(c.vm.stack.popInt64())) }), nil
	case i64Popcnt:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(popcnt64(c.vm.stack.popInt64())) }), nil
	case i64Add:
		return alu64(func(a, b int64) int64 { return a + b }), nil
	case i64Sub:
		return alu64(func(a, b int64) int64 { return a - b }), nil
	case i64Mul:
		return alu64(func(a, b int64) int64 { return a * b }), nil
	case i64DivS:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt64(divS64) }), nil
	case i64DivU:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt64(divU64) }), nil
	case i64RemS:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt64(remS64) }), nil
	case i64RemU:
		return safe(func(c *frameCtx) error { return c.vm.handleBinarySafeInt64(remU64) }), nil
	case i64And:
		return alu64(func(a, b int64) int64 { return a & b }), nil
	case i64Or:
		return alu64(func(a, b int64) int64 { return a | b }), nil
	case i64Xor:
		return alu64(func(a, b int64) int64 { return a ^ b }), nil
	case i64Shl:
		return alu64(shl64), nil
	case i64ShrS:
		return alu64(shrS64), nil
	case i64ShrU:
		return alu64(shrU64), nil
	case i64Rotl:
		return alu64(rotl64), nil
	case i64Rotr:
		return alu64(rotr64), nil
	case f32Abs:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(abs(c.vm.stack.popFloat32())) }), nil
	case f32Neg:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(-c.vm.stack.popFloat32()) }), nil
	case f32Ceil:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(ceil(c.vm.stack.popFloat32())) }), nil
	case f32Floor:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(floor(c.vm.stack.popFloat32())) }), nil
	case f32Trunc:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(trunc(c.vm.stack.popFloat32())) }), nil
	case f32Nearest:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(nearest(c.vm.stack.popFloat32())) }), nil
	case f32Sqrt:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(sqrt(c.vm.stack.popFloat32())) }), nil
	case f32Add:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(add[float32]) }), nil
	case f32Sub:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(sub[float32]) }), nil
	case f32Mul:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(mul[float32]) }), nil
	case f32Div:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(div[float32]) }), nil
	case f32Min:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(wasmMin[float32]) }), nil
	case f32Max:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(wasmMax[float32]) }), nil
	case f32Copysign:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat32(copysign[float32]) }), nil
	case f64Abs:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(abs(c.vm.stack.popFloat64())) }), nil
	case f64Neg:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(-c.vm.stack.popFloat64()) }), nil
	case f64Ceil:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(ceil(c.vm.stack.popFloat64())) }), nil
	case f64Floor:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(floor(c.vm.stack.popFloat64())) }), nil
	case f64Trunc:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(trunc(c.vm.stack.popFloat64())) }), nil
	case f64Nearest:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(nearest(c.vm.stack.popFloat64())) }), nil
	case f64Sqrt:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(sqrt(c.vm.stack.popFloat64())) }), nil
	case f64Add:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(add[float64]) }), nil
	case f64Sub:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(sub[float64]) }), nil
	case f64Mul:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(mul[float64]) }), nil
	case f64Div:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(div[float64]) }), nil
	case f64Min:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(wasmMin[float64]) }), nil
	case f64Max:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(wasmMax[float64]) }), nil
	case f64Copysign:
		return simple(func(c *frameCtx) { c.vm.handleBinaryFloat64(copysign[float64]) }), nil
	case i32WrapI64:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(wrapI64ToI32(c.vm.stack.popInt64())) }), nil
	case i32TruncF32S:
		return safe(func(c *frameCtx) error { return c.vm.handleUnarySafeFloat32(truncF32SToI32) }), nil
	case i32TruncF32U:
		return safe(func(c *frameCtx) error { return c.vm.handleUnarySafeFloat32(truncF32UToI32) }), nil
	case i32TruncF64S:
		return safe(func(c *frameCtx) error { return c.vm.handleUnarySafeFloat64(truncF64SToI32) }), nil
	case i32TruncF64U:
		return safe(func(c *frameCtx) error { return c.vm.handleUnarySafeFloat64(truncF64UToI32) }), nil
	case i64ExtendI32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(extendI32SToI64(c.vm.stack.popInt32())) }), nil
	case i64ExtendI32U:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(extendI32UToI64(c.vm.stack.popInt32())) }), nil
	case i64TruncF32S:
		return safe(func(c *frameCtx) error { return c.vm.handleTruncFloat32Int64(truncF32SToI64) }), nil
	case i64TruncF32U:
		return safe(func(c *frameCtx) error { return c.vm.handleTruncFloat32Int64(truncF32UToI64) }), nil
	case i64TruncF64S:
		return safe(func(c *frameCtx) error { return c.vm.handleTruncFloat64Int64(truncF64SToI64) }), nil
	case i64TruncF64U:
		return safe(func(c *frameCtx) error { return c.vm.handleTruncFloat64Int64(truncF64UToI64) }), nil
	case f32ConvertI32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(convertI32SToF32(c.vm.stack.popInt32())) }), nil
	case f32ConvertI32U:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(convertI32UToF32(c.vm.stack.popInt32())) }), nil
	case f32ConvertI64S:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(convertI64SToF32(c.vm.stack.popInt64())) }), nil
	case f32ConvertI64U:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(convertI64UToF32(c.vm.stack.popInt64())) }), nil
	case f32DemoteF64:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(demoteF64ToF32(c.vm.stack.popFloat64())) }), nil
	case f64ConvertI32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(convertI32SToF64(c.vm.stack.popInt32())) }), nil
	case f64ConvertI32U:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(convertI32UToF64(c.vm.stack.popInt32())) }), nil
	case f64ConvertI64S:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(convertI64SToF64(c.vm.stack.popInt64())) }), nil
	case f64ConvertI64U:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(convertI64UToF64(c.vm.stack.popInt64())) }), nil
	case f64PromoteF32:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(promoteF32ToF64(c.vm.stack.popFloat32())) }), nil
	case i32ReinterpretF32:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(reinterpretF32ToI32(c.vm.stack.popFloat32())) }), nil
	case i64ReinterpretF64:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(reinterpretF64ToI64(c.vm.stack.popFloat64())) }), nil
	case f32ReinterpretI32:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat32(reinterpretI32ToF32(c.vm.stack.popInt32())) }), nil
	case f64ReinterpretI64:
		return simple(func(c *frameCtx) { c.vm.stack.pushFloat64(reinterpretI64ToF64(c.vm.stack.popInt64())) }), nil
	case i32Extend8S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(extend8STo32(c.vm.stack.popInt32())) }), nil
	case i32Extend16S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(extend16STo32(c.vm.stack.popInt32())) }), nil
	case i64Extend8S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(extend8STo64(c.vm.stack.popInt64())) }), nil
	case i64Extend16S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(extend16STo64(c.vm.stack.popInt64())) }), nil
	case i64Extend32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(extend32STo64(c.vm.stack.popInt64())) }), nil
	case refNull:
		return func(c *frameCtx) int { c.vm.stack.pushInt32(NullReference); return advance }, nil
	case refIsNull:
		return func(c *frameCtx) int { c.vm.handleRefIsNull(); return advance }, nil
	case refFunc:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleRefFunc(c.module, idx); return advance }, nil
	case i32TruncSatF32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(truncSatF32SToI32(c.vm.stack.popFloat32())) }), nil
	case i32TruncSatF32U:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(truncSatF32UToI32(c.vm.stack.popFloat32())) }), nil
	case i32TruncSatF64S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(truncSatF64SToI32(c.vm.stack.popFloat64())) }), nil
	case i32TruncSatF64U:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(truncSatF64UToI32(c.vm.stack.popFloat64())) }), nil
	case i64TruncSatF32S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(truncSatF32SToI64(c.vm.stack.popFloat32())) }), nil
	case i64TruncSatF32U:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(truncSatF32UToI64(c.vm.stack.popFloat32())) }), nil
	case i64TruncSatF64S:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(truncSatF64SToI64(c.vm.stack.popFloat64())) }), nil
	case i64TruncSatF64U:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt64(truncSatF64UToI64(c.vm.stack.popFloat64())) }), nil
	case memoryInit:
		dataIdx, memIdx := body[pc+1], body[pc+2]
		return safe(func(c *frameCtx) error { return c.vm.handleMemoryInit(c.module, dataIdx, memIdx) }), nil
	case dataDrop:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleDataDrop(c.module, idx); return advance }, nil
	case memoryCopy:
		destIdx, srcIdx := body[pc+1], body[pc+2]
		return safe(func(c *frameCtx) error { return c.vm.handleMemoryCopy(c.module, destIdx, srcIdx) }), nil
	case memoryFill:
		idx := body[pc+1]
		return safe(func(c *frameCtx) error { return c.vm.handleMemoryFill(c.module, idx) }), nil
	case tableInit:
		elemIdx, tableIdx := body[pc+1], body[pc+2]
		return safe(func(c *frameCtx) error { return c.vm.handleTableInit(c.module, elemIdx, tableIdx) }), nil
	case elemDrop:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleElemDrop(c.module, idx); return advance }, nil
	case tableCopy:
		destIdx, srcIdx := body[pc+1], body[pc+2]
		return safe(func(c *frameCtx) error { return c.vm.handleTableCopy(c.module, destIdx, srcIdx) }), nil
	case tableGrow:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleTableGrow(c.module, idx); return advance }, nil
	case tableSize:
		idx := body[pc+1]
		return func(c *frameCtx) int { c.vm.handleTableSize(c.module, idx); return advance }, nil
	case tableFill:
		idx := body[pc+1]
		return safe(func(c *frameCtx) error { return c.vm.handleTableFill(c.module, idx) }), nil
	case v128Load:
		return handleLoad(body, pc, vm.stack.pushV128, (*Memory).LoadV128, identityV128), nil
	case v128Load8x8S:
		return handleLoadV128FromBytes(body, pc, simdV128Load8x8S, 8), nil
	case v128Load8x8U:
		return handleLoadV128FromBytes(body, pc, simdV128Load8x8U, 8), nil
	case v128Load16x4S:
		return handleLoadV128FromBytes(body, pc, simdV128Load16x4S, 8), nil
	case v128Load16x4U:
		return handleLoadV128FromBytes(body, pc, simdV128Load16x4U, 8), nil
	case v128Load32x2S:
		return handleLoadV128FromBytes(body, pc, simdV128Load32x2S, 8), nil
	case v128Load32x2U:
		return handleLoadV128FromBytes(body, pc, simdV128Load32x2U, 8), nil
	case v128Load8Splat:
		return handleLoadV128FromBytes(body, pc, simdI8x16SplatFromBytes, 1), nil
	case v128Load16Splat:
		return handleLoadV128FromBytes(body, pc, simdI16x8SplatFromBytes, 2), nil
	case v128Load32Splat:
		return handleLoadV128FromBytes(body, pc, simdI32x4SplatFromBytes, 4), nil
	case v128Load64Splat:
		return handleLoadV128FromBytes(body, pc, simdI64x2SplatFromBytes, 8), nil
	case v128Store:
		return handleStore(body, pc, vm.stack.popV128, (*Memory).StoreV128), nil
	case v128Const:
		v := V128Value{Low: body[pc+1], High: body[pc+2]}
		return func(c *frameCtx) int { c.vm.stack.pushV128(v); return advance }, nil
	case i8x16Shuffle:
		var lanes [16]byte
		for i := range lanes {
			lanes[i] = byte(body[pc+1+i])
		}
		return func(c *frameCtx) int { c.vm.handleI8x16Shuffle(lanes); return advance }, nil
	case i8x16Swizzle:
		return v128Binary(simdI8x16Swizzle), nil
	case i8x16Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdI8x16Splat(c.vm.stack.popInt32())) }), nil
	case i16x8Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdI16x8Splat(c.vm.stack.popInt32())) }), nil
	case i32x4Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdI32x4Splat(c.vm.stack.popInt32())) }), nil
	case i64x2Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdI64x2Splat(c.vm.stack.popInt64())) }), nil
	case f32x4Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdF32x4Splat(c.vm.stack.popFloat32())) }), nil
	case f64x2Splat:
		return simple(func(c *frameCtx) { c.vm.stack.pushV128(simdF64x2Splat(c.vm.stack.popFloat64())) }), nil
	case i8x16ExtractLaneS:
		return handleExtractLane(body, pc, vm.stack.pushInt32, simdI8x16ExtractLaneS), nil
	case i8x16ExtractLaneU:
		return handleExtractLane(body, pc, vm.stack.pushInt32, simdI8x16ExtractLaneU), nil
	case i8x16ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popInt32, simdI8x16ReplaceLane), nil
	case i16x8ExtractLaneS:
		return handleExtractLane(body, pc, vm.stack.pushInt32, simdI16x8ExtractLaneS), nil
	case i16x8ExtractLaneU:
		return handleExtractLane(body, pc, vm.stack.pushInt32, simdI16x8ExtractLaneU), nil
	case i16x8ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popInt32, simdI16x8ReplaceLane), nil
	case i32x4ExtractLane:
		return handleExtractLane(body, pc, vm.stack.pushInt32, simdI32x4ExtractLane), nil
	case i32x4ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popInt32, simdI32x4ReplaceLane), nil
	case i64x2ExtractLane:
		return handleExtractLane(body, pc, vm.stack.pushInt64, simdI64x2ExtractLane), nil
	case i64x2ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popInt64, simdI64x2ReplaceLane), nil
	case f32x4ExtractLane:
		return handleExtractLane(body, pc, vm.stack.pushFloat32, simdF32x4ExtractLane), nil
	case f32x4ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popFloat32, simdF32x4ReplaceLane), nil
	case f64x2ExtractLane:
		return handleExtractLane(body, pc, vm.stack.pushFloat64, simdF64x2ExtractLane), nil
	case f64x2ReplaceLane:
		return handleReplaceLane(body, pc, vm.stack.popFloat64, simdF64x2ReplaceLane), nil
	case i8x16Eq:
		return v128Binary(simdI8x16Eq), nil
	case i8x16Ne:
		return v128Binary(simdI8x16Ne), nil
	case i8x16LtS:
		return v128Binary(simdI8x16LtS), nil
	case i8x16LtU:
		return v128Binary(simdI8x16LtU), nil
	case i8x16GtS:
		return v128Binary(simdI8x16GtS), nil
	case i8x16GtU:
		return v128Binary(simdI8x16GtU), nil
	case i8x16LeS:
		return v128Binary(simdI8x16LeS), nil
	case i8x16LeU:
		return v128Binary(simdI8x16LeU), nil
	case i8x16GeS:
		return v128Binary(simdI8x16GeS), nil
	case i8x16GeU:
		return v128Binary(simdI8x16GeU), nil
	case i16x8Eq:
		return v128Binary(simdI16x8Eq), nil
	case i16x8Ne:
		return v128Binary(simdI16x8Ne), nil
	case i16x8LtS:
		return v128Binary(simdI16x8LtS), nil
	case i16x8LtU:
		return v128Binary(simdI16x8LtU), nil
	case i16x8GtS:
		return v128Binary(simdI16x8GtS), nil
	case i16x8GtU:
		return v128Binary(simdI16x8GtU), nil
	case i16x8LeS:
		return v128Binary(simdI16x8LeS), nil
	case i16x8LeU:
		return v128Binary(simdI16x8LeU), nil
	case i16x8GeS:
		return v128Binary(simdI16x8GeS), nil
	case i16x8GeU:
		return v128Binary(simdI16x8GeU), nil
	case i32x4Eq:
		return v128Binary(simdI32x4Eq), nil
	case i32x4Ne:
		return v128Binary(simdI32x4Ne), nil
	case i32x4LtS:
		return v128Binary(simdI32x4LtS), nil
	case i32x4LtU:
		return v128Binary(simdI32x4LtU), nil
	case i32x4GtS:
		return v128Binary(simdI32x4GtS), nil
	case i32x4GtU:
		return v128Binary(simdI32x4GtU), nil
	case i32x4LeS:
		return v128Binary(simdI32x4LeS), nil
	case i32x4LeU:
		return v128Binary(simdI32x4LeU), nil
	case i32x4GeS:
		return v128Binary(simdI32x4GeS), nil
	case i32x4GeU:
		return v128Binary(simdI32x4GeU), nil
	case f32x4Eq:
		return v128Binary(simdF32x4Eq), nil
	case f32x4Ne:
		return v128Binary(simdF32x4Ne), nil
	case f32x4Lt:
		return v128Binary(simdF32x4Lt), nil
	case f32x4Gt:
		return v128Binary(simdF32x4Gt), nil
	case f32x4Le:
		return v128Binary(simdF32x4Le), nil
	case f32x4Ge:
		return v128Binary(simdF32x4Ge), nil
	case f64x2Eq:
		return v128Binary(simdF64x2Eq), nil
	case f64x2Ne:
		return v128Binary(simdF64x2Ne), nil
	case f64x2Lt:
		return v128Binary(simdF64x2Lt), nil
	case f64x2Gt:
		return v128Binary(simdF64x2Gt), nil
	case f64x2Le:
		return v128Binary(simdF64x2Le), nil
	case f64x2Ge:
		return v128Binary(simdF64x2Ge), nil
	case v128Not:
		return v128Unary(simdV128Not), nil
	case v128And:
		return v128Binary(simdV128And), nil
	case v128Andnot:
		return v128Binary(simdV128Andnot), nil
	case v128Or:
		return v128Binary(simdV128Or), nil
	case v128Xor:
		return v128Binary(simdV128Xor), nil
	case v128Bitselect:
		return simple(func(c *frameCtx) { c.vm.handleSimdTernary(simdV128Bitselect) }), nil
	case v128AnyTrue:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(simdV128AnyTrue(c.vm.stack.popV128()))) }), nil
	case v128Load8Lane:
		return handleSimdLoadLane(body, pc, 8), nil
	case v128Load16Lane:
		return handleSimdLoadLane(body, pc, 16), nil
	case v128Load32Lane:
		return handleSimdLoadLane(body, pc, 32), nil
	case v128Load64Lane:
		return handleSimdLoadLane(body, pc, 64), nil
	case v128Store8Lane:
		return handleSimdStoreLane(body, pc, 8), nil
	case v128Store16Lane:
		return handleSimdStoreLane(body, pc, 16), nil
	case v128Store32Lane:
		return handleSimdStoreLane(body, pc, 32), nil
	case v128Store64Lane:
		return handleSimdStoreLane(body, pc, 64), nil
	case v128Load32Zero:
		return handleLoadV128FromBytes(body, pc, simdV128Load32Zero, 4), nil
	case v128Load64Zero:
		return handleLoadV128FromBytes(body, pc, simdV128Load64Zero, 8), nil
	case f32x4DemoteF64x2Zero:
		return v128Unary(simdF32x4DemoteF64x2Zero), nil
	case f64x2PromoteLowF32x4:
		return v128Unary(simdF64x2PromoteLowF32x4), nil
	case i8x16Abs:
		return v128Unary(simdI8x16Abs), nil
	case i8x16Neg:
		return v128Unary(simdI8x16Neg), nil
	case i8x16Popcnt:
		return v128Unary(simdI8x16Popcnt), nil
	case i8x16AllTrue:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(simdI8x16AllTrue(c.vm.stack.popV128()))) }), nil
	case i8x16Bitmask:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(simdI8x16Bitmask(c.vm.stack.popV128())) }), nil
	case i8x16NarrowI16x8S:
		return v128Binary(simdI8x16NarrowI16x8S), nil
	case i8x16NarrowI16x8U:
		return v128Binary(simdI8x16NarrowI16x8U), nil
	case f32x4Ceil:
		return v128Unary(simdF32x4Ceil), nil
	case f32x4Floor:
		return v128Unary(simdF32x4Floor), nil
	case f32x4Trunc:
		return v128Unary(simdF32x4Trunc), nil
	case f32x4Nearest:
		return v128Unary(simdF32x4Nearest), nil
	case i8x16Shl:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI8x16Shl) }), nil
	case i8x16ShrU:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI8x16ShrU) }), nil
	case i8x16ShrS:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI8x16ShrS) }), nil
	case i8x16Add:
		return v128Binary(simdI8x16Add), nil
	case i8x16AddSatS:
		return v128Binary(simdI8x16AddSatS), nil
	case i8x16AddSatU:
		return v128Binary(simdI8x16AddSatU), nil
	case i8x16Sub:
		return v128Binary(simdI8x16Sub), nil
	case i8x16SubSatS:
		return v128Binary(simdI8x16SubSatS), nil
	case i8x16SubSatU:
		return v128Binary(simdI8x16SubSatU), nil
	case f64x2Ceil:
		return v128Unary(simdF64x2Ceil), nil
	case f64x2Floor:
		return v128Unary(simdF64x2Floor), nil
	case i8x16MinS:
		return v128Binary(simdI8x16MinS), nil
	case i8x16MinU:
		return v128Binary(simdI8x16MinU), nil
	case i8x16MaxS:
		return v128Binary(simdI8x16MaxS), nil
	case i8x16MaxU:
		return v128Binary(simdI8x16MaxU), nil
	case f64x2Trunc:
		return v128Unary(simdF64x2Trunc), nil
	case i8x16AvgrU:
		return v128Binary(simdI8x16AvgrU), nil
	case i16x8ExtaddPairwiseI8x16S:
		return v128Unary(simdI16x8ExtaddPairwiseI8x16S), nil
	case i16x8ExtaddPairwiseI8x16U:
		return v128Unary(simdI16x8ExtaddPairwiseI8x16U), nil
	case i32x4ExtaddPairwiseI16x8S:
		return v128Unary(simdI32x4ExtaddPairwiseI16x8S), nil
	case i32x4ExtaddPairwiseI16x8U:
		return v128Unary(simdI32x4ExtaddPairwiseI16x8U), nil
	case i16x8Abs:
		return v128Unary(simdI16x8Abs), nil
	case i16x8Neg:
		return v128Unary(simdI16x8Neg), nil
	case i16x8Q15mulrSatS:
		return v128Binary(simdI16x8Q15mulrSatS), nil
	case i16x8AllTrue:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(simdI16x8AllTrue(c.vm.stack.popV128()))) }), nil
	case i16x8Bitmask:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(simdI16x8Bitmask(c.vm.stack.popV128())) }), nil
	case i16x8NarrowI32x4S:
		return v128Binary(simdI16x8NarrowI32x4S), nil
	case i16x8NarrowI32x4U:
		return v128Binary(simdI16x8NarrowI32x4U), nil
	case i16x8ExtendLowI8x16S:
		return v128Unary(simdI16x8ExtendLowI8x16S), nil
	case i16x8ExtendHighI8x16S:
		return v128Unary(simdI16x8ExtendHighI8x16S), nil
	case i16x8ExtendLowI8x16U:
		return v128Unary(simdI16x8ExtendLowI8x16U), nil
	case i16x8ExtendHighI8x16U:
		return v128Unary(simdI16x8ExtendHighI8x16U), nil
	case i16x8Shl:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI16x8Shl) }), nil
	case i16x8ShrS:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI16x8ShrS) }), nil
	case i16x8ShrU:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI16x8ShrU) }), nil
	case i16x8Add:
		return v128Binary(simdI16x8Add), nil
	case i16x8AddSatS:
		return v128Binary(simdI16x8AddSatS), nil
	case i16x8AddSatU:
		return v128Binary(simdI16x8AddSatU), nil
	case i16x8Sub:
		return v128Binary(simdI16x8Sub), nil
	case i16x8SubSatS:
		return v128Binary(simdI16x8SubSatS), nil
	case i16x8SubSatU:
		return v128Binary(simdI16x8SubSatU), nil
	case f64x2Nearest:
		return v128Unary(simdF64x2Nearest), nil
	case i16x8Mul:
		return v128Binary(simdI16x8Mul), nil
	case i16x8MinS:
		return v128Binary(simdI16x8MinS), nil
	case i16x8MinU:
		return v128Binary(simdI16x8MinU), nil
	case i16x8MaxS:
		return v128Binary(simdI16x8MaxS), nil
	case i16x8MaxU:
		return v128Binary(simdI16x8MaxU), nil
	case i16x8AvgrU:
		return v128Binary(simdI16x8AvgrU), nil
	case i16x8ExtmulLowI8x16S:
		return v128Binary(simdI16x8ExtmulLowI8x16S), nil
	case i16x8ExtmulHighI8x16S:
		return v128Binary(simdI16x8ExtmulHighI8x16S), nil
	case i16x8ExtmulLowI8x16U:
		return v128Binary(simdI16x8ExtmulLowI8x16U), nil
	case i16x8ExtmulHighI8x16U:
		return v128Binary(simdI16x8ExtmulHighI8x16U), nil
	case i32x4Abs:
		return v128Unary(simdI32x4Abs), nil
	case i32x4Neg:
		return v128Unary(simdI32x4Neg), nil
	case i32x4AllTrue:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(simdI32x4AllTrue(c.vm.stack.popV128()))) }), nil
	case i32x4Bitmask:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(simdI32x4Bitmask(c.vm.stack.popV128())) }), nil
	case i32x4ExtendLowI16x8S:
		return v128Unary(simdI32x4ExtendLowI16x8S), nil
	case i32x4ExtendHighI16x8S:
		return v128Unary(simdI32x4ExtendHighI16x8S), nil
	case i32x4ExtendLowI16x8U:
		return v128Unary(simdI32x4ExtendLowI16x8U), nil
	case i32x4ExtendHighI16x8U:
		return v128Unary(simdI32x4ExtendHighI16x8U), nil
	case i32x4Shl:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI32x4Shl) }), nil
	case i32x4ShrS:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI32x4ShrS) }), nil
	case i32x4ShrU:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI32x4ShrU) }), nil
	case i32x4Add:
		return v128Binary(simdI32x4Add), nil
	case i32x4Sub:
		return v128Binary(simdI32x4Sub), nil
	case i32x4Mul:
		return v128Binary(simdI32x4Mul), nil
	case i32x4MinS:
		return v128Binary(simdI32x4MinS), nil
	case i32x4MinU:
		return v128Binary(simdI32x4MinU), nil
	case i32x4MaxS:
		return v128Binary(simdI32x4MaxS), nil
	case i32x4MaxU:
		return v128Binary(simdI32x4MaxU), nil
	case i32x4DotI16x8S:
		return v128Binary(simdI32x4DotI16x8S), nil
	case i32x4ExtmulLowI16x8S:
		return v128Binary(simdI32x4ExtmulLowI16x8S), nil
	case i32x4ExtmulHighI16x8S:
		return v128Binary(simdI32x4ExtmulHighI16x8S), nil
	case i32x4ExtmulLowI16x8U:
		return v128Binary(simdI32x4ExtmulLowI16x8U), nil
	case i32x4ExtmulHighI16x8U:
		return v128Binary(simdI32x4ExtmulHighI16x8U), nil
	case i64x2Abs:
		return v128Unary(simdI64x2Abs), nil
	case i64x2Neg:
		return v128Unary(simdI64x2Neg), nil
	case i64x2AllTrue:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(boolToInt32(simdI64x2AllTrue(c.vm.stack.popV128()))) }), nil
	case i64x2Bitmask:
		return simple(func(c *frameCtx) { c.vm.stack.pushInt32(simdI64x2Bitmask(c.vm.stack.popV128())) }), nil
	case i64x2ExtendLowI32x4S:
		return v128Unary(simdI64x2ExtendLowI32x4S), nil
	case i64x2ExtendHighI32x4S:
		return v128Unary(simdI64x2ExtendHighI32x4S), nil
	case i64x2ExtendLowI32x4U:
		return v128Unary(simdI64x2ExtendLowI32x4U), nil
	case i64x2ExtendHighI32x4U:
		return v128Unary(simdI64x2ExtendHighI32x4U), nil
	case i64x2Shl:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI64x2Shl) }), nil
	case i64x2ShrS:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI64x2ShrS) }), nil
	case i64x2ShrU:
		return simple(func(c *frameCtx) { c.vm.handleSimdShift(simdI64x2ShrU) }), nil
	case i64x2Add:
		return v128Binary(simdI64x2Add), nil
	case i64x2Sub:
		return v128Binary(simdI64x2Sub), nil
	case i64x2Mul:
		return v128Binary(simdI64x2Mul), nil
	case i64x2Eq:
		return v128Binary(simdI64x2Eq), nil
	case i64x2Ne:
		return v128Binary(simdI64x2Ne), nil
	case i64x2LtS:
		return v128Binary(simdI64x2LtS), nil
	case i64x2GtS:
		return v128Binary(simdI64x2GtS), nil
	case i64x2LeS:
		return v128Binary(simdI64x2LeS), nil
	case i64x2GeS:
		return v128Binary(simdI64x2GeS), nil
	case i64x2ExtmulLowI32x4S:
		return v128Binary(simdI64x2ExtmulLowI32x4S), nil
	case i64x2ExtmulHighI32x4S:
		return v128Binary(simdI64x2ExtmulHighI32x4S), nil
	case i64x2ExtmulLowI32x4U:
		return v128Binary(simdI64x2ExtmulLowI32x4U), nil
	case i64x2ExtmulHighI32x4U:
		return v128Binary(simdI64x2ExtmulHighI32x4U), nil
	case f32x4Abs:
		return v128Unary(simdF32x4Abs), nil
	case f32x4Neg:
		return v128Unary(simdF32x4Neg), nil
	case f32x4Sqrt:
		return v128Unary(simdF32x4Sqrt), nil
	case f32x4Add:
		return v128Binary(simdF32x4Add), nil
	case f32x4Sub:
		return v128Binary(simdF32x4Sub), nil
	case f32x4Mul:
		return v128Binary(simdF32x4Mul), nil
	case f32x4Div:
		return v128Binary(simdF32x4Div), nil
	case f32x4Min:
		return v128Binary(simdF32x4Min), nil
	case f32x4Max:
		return v128Binary(simdF32x4Max), nil
	case f32x4Pmin:
		return v128Binary(simdF32x4Pmin), nil
	case f32x4Pmax:
		return v128Binary(simdF32x4Pmax), nil
	case f64x2Abs:
		return v128Unary(simdF64x2Abs), nil
	case f64x2Neg:
		return v128Unary(simdF64x2Neg), nil
	case f64x2Sqrt:
		return v128Unary(simdF64x2Sqrt), nil
	case f64x2Add:
		return v128Binary(simdF64x2Add), nil
	case f64x2Sub:
		return v128Binary(simdF64x2Sub), nil
	case f64x2Mul:
		return v128Binary(simdF64x2Mul), nil
	case f64x2Div:
		return v128Binary(simdF64x2Div), nil
	case f64x2Min:
		return v128Binary(simdF64x2Min), nil
	case f64x2Max:
		return v128Binary(simdF64x2Max), nil
	case f64x2Pmin:
		return v128Binary(simdF64x2Pmin), nil
	case f64x2Pmax:
		return v128Binary(simdF64x2Pmax), nil
	case i32x4TruncSatF32x4S:
		return v128Unary(simdI32x4TruncSatF32x4S), nil
	case i32x4TruncSatF32x4U:
		return v128Unary(simdI32x4TruncSatF32x4U), nil
	case f32x4ConvertI32x4S:
		return v128Unary(simdF32x4ConvertI32x4S), nil
	case f32x4ConvertI32x4U:
		return v128Unary(simdF32x4ConvertI32x4U), nil
	case i32x4TruncSatF64x2SZero:
		return v128Unary(simdI32x4TruncSatF64x2SZero), nil
	case i32x4TruncSatF64x2UZero:
		return v128Unary(simdI32x4TruncSatF64x2UZero), nil
	case f64x2ConvertLowI32x4S:
		return v128Unary(simdF64x2ConvertLowI32x4S), nil
	case f64x2ConvertLowI32x4U:
		return v128Unary(simdF64x2ConvertLowI32x4U), nil
	default:
		return nil, fmt.Errorf("unknown opcode %d", op)
	}
}

// simple wraps a body that always advances to the next instruction.
func simple(body func(c *frameCtx)) frame {
	return func(c *frameCtx) int { body(c); return advance }
}

// safe wraps a body that may trap; a non-nil error halts with the trap set.
func safe(body func(c *frameCtx) error) frame {
	return func(c *frameCtx) int {
		if err := body(c); err != nil {
			c.trap = err
			return trap
		}
		return advance
	}
}

// v128Unary builds a closure for an operand-free V128 -> V128 instruction.
func v128Unary(op func(V128Value) V128Value) frame {
	return simple(func(c *frameCtx) { c.vm.stack.pushV128(op(c.vm.stack.popV128())) })
}

// v128Binary builds a closure for an operand-free (V128, V128) -> V128 instruction.
func v128Binary(op func(a, b V128Value) V128Value) frame {
	return simple(func(c *frameCtx) { c.vm.handleBinaryV128(op) })
}

func cmp32(op func(a, b int32) bool) frame {
	return simple(func(c *frameCtx) { c.vm.handleBinaryBoolInt32(op) })
}

func cmp64(op func(a, b int64) bool) frame {
	return simple(func(c *frameCtx) { c.vm.handleBinaryBoolInt64(op) })
}

func cmpf32(op func(a, b float32) bool) frame {
	return simple(func(c *frameCtx) { c.vm.handleBinaryBoolFloat32(op) })
}

func cmpf64(op func(a, b float64) bool) frame {
	return simple(func(c *frameCtx) { c.vm.handleBinaryBoolFloat64(op) })
}

func alu64(op func(a, b int64) int64) frame {
	return simple(func(c *frameCtx) {
		b := c.vm.stack.popInt64()
		data := c.vm.stack.data
		last := len(data) - 1
		data[last] = i64(op(data[last].int64(), b))
	})
}

func alu32(op func(a, b int32) int32) frame {
	return func(c *frameCtx) int {
		b := c.vm.stack.popInt32()
		data := c.vm.stack.data
		last := len(data) - 1
		data[last] = i32(op(data[last].int32(), b))
		return advance
	}
}

func (vm *vm) handleCall(module *ModuleInstance, funcIndex uint64) error {
	functionIndex := module.funcAddrs[funcIndex]
	function := vm.store.funcs[functionIndex]
	return vm.invokeFunction(function)
}

func (vm *vm) handleCallIndirect(module *ModuleInstance, typeIndex, tableIndex uint64) error {
	expectedType := module.types[typeIndex]
	table := vm.getTable(module, tableIndex)

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

func (vm *vm) handleGlobalGet(module *ModuleInstance, idx uint64) {
	global := vm.getGlobal(module, idx)
	vm.stack.push(global.value)
}

func (vm *vm) handleGlobalSet(module *ModuleInstance, idx uint64) {
	global := vm.getGlobal(module, idx)
	global.value = vm.stack.pop()
}

func (vm *vm) handleTableGet(module *ModuleInstance, idx uint64) error {
	table := vm.getTable(module, idx)
	index := vm.stack.popInt32()

	element, err := table.Get(index)
	if err != nil {
		return err
	}

	vm.stack.pushInt32(element)
	return nil
}

func (vm *vm) handleTableSet(module *ModuleInstance, idx uint64) error {
	table := vm.getTable(module, idx)
	reference := vm.stack.popInt32()
	index := vm.stack.popInt32()
	return table.Set(index, reference)
}

func (vm *vm) handleMemorySize(module *ModuleInstance, idx uint64) {
	memory := vm.getMemory(module, idx)
	vm.stack.pushInt32(memory.Size())
}

func (vm *vm) handleMemoryGrow(module *ModuleInstance, idx uint64) {
	memory := vm.getMemory(module, idx)
	pages := vm.stack.popInt32()
	oldSize := memory.Grow(pages)
	vm.stack.pushInt32(oldSize)
}

func (vm *vm) handleRefFunc(module *ModuleInstance, idx uint64) {
	storeIndex := module.funcAddrs[idx]
	vm.stack.pushInt32(int32(storeIndex))
}

func (vm *vm) handleRefIsNull() {
	top := vm.stack.popInt32()
	vm.stack.pushInt32(boolToInt32(top == NullReference))
}

func (vm *vm) handleMemoryInit(module *ModuleInstance, dataIdx, memIdx uint64) error {
	data := vm.getData(module, dataIdx)
	memory := vm.getMemory(module, memIdx)
	n, s, d := vm.stack.pop3Int32()
	return memory.Init(uint32(n), uint32(s), uint32(d), data.content)
}

func (vm *vm) handleDataDrop(module *ModuleInstance, dataIdx uint64) {
	dataSegment := vm.getData(module, dataIdx)
	dataSegment.content = nil
}

func (vm *vm) handleMemoryCopy(module *ModuleInstance, destIdx, srcIdx uint64) error {
	destMemory := vm.getMemory(module, destIdx)
	srcMemory := vm.getMemory(module, srcIdx)
	n, s, d := vm.stack.pop3Int32()
	return srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d))
}

func (vm *vm) handleMemoryFill(module *ModuleInstance, memIdx uint64) error {
	memory := vm.getMemory(module, memIdx)
	n, val, offset := vm.stack.pop3Int32()
	return memory.Fill(uint32(n), uint32(offset), byte(val))
}

func (vm *vm) handleTableInit(module *ModuleInstance, elemIdx, tableIdx uint64) error {
	element := vm.getElement(module, elemIdx)
	table := vm.getTable(module, tableIdx)
	n, s, d := vm.stack.pop3Int32()
	// element.functionIndexes is nil for dropped, active, and declarative
	// segments; only passive segments retain their entries until elem.drop.
	return table.Init(n, d, s, element.functionIndexes)
}

func (vm *vm) handleElemDrop(module *ModuleInstance, elemIdx uint64) {
	element := vm.getElement(module, elemIdx)
	element.functionIndexes = nil
}

func (vm *vm) handleTableCopy(module *ModuleInstance, destIdx, srcIdx uint64) error {
	destTable := vm.getTable(module, destIdx)
	srcTable := vm.getTable(module, srcIdx)
	n, s, d := vm.stack.pop3Int32()
	return srcTable.Copy(destTable, n, s, d)
}

func (vm *vm) handleTableGrow(module *ModuleInstance, tableIdx uint64) {
	table := vm.getTable(module, tableIdx)
	n := vm.stack.popInt32()
	val := vm.stack.popInt32()
	vm.stack.pushInt32(table.Grow(n, val))
}

func (vm *vm) handleTableSize(module *ModuleInstance, tableIdx uint64) {
	table := vm.getTable(module, tableIdx)
	vm.stack.pushInt32(int32(table.Size()))
}

func (vm *vm) handleTableFill(module *ModuleInstance, tableIdx uint64) error {
	table := vm.getTable(module, tableIdx)
	n, val, i := vm.stack.pop3Int32()
	return table.Fill(n, i, val)
}

func (vm *vm) handleI8x16Shuffle(lanes [16]byte) {
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Shuffle(v1, v2,
		lanes[0], lanes[1], lanes[2], lanes[3], lanes[4], lanes[5], lanes[6], lanes[7],
		lanes[8], lanes[9], lanes[10], lanes[11], lanes[12], lanes[13], lanes[14], lanes[15]))
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

// handleLoad builds a memory-load closure, capturing the memory index and
// offset from the memarg at body[pc+1..pc+3] (align is unused).
func handleLoad[T any, R any](
	body []uint64, pc int,
	push func(R),
	load func(*Memory, uint32, uint32) (T, error),
	convert func(T) R,
) frame {
	memIdx := body[pc+2]
	offset := uint32(body[pc+3])
	return func(c *frameCtx) int {
		memory := c.vm.getMemory(c.module, memIdx)
		index := uint32(c.vm.stack.popInt32())
		v, err := load(memory, offset, index)
		if err != nil {
			c.trap = err
			return trap
		}
		push(convert(v))
		return advance
	}
}

// handleStore builds a memory-store closure. The value is popped before the
// address index.
func handleStore[T any](
	body []uint64, pc int,
	pop func() T,
	store func(*Memory, uint32, uint32, T) error,
) frame {
	memIdx := body[pc+2]
	offset := uint32(body[pc+3])
	return func(c *frameCtx) int {
		val := pop()
		memory := c.vm.getMemory(c.module, memIdx)
		index := uint32(c.vm.stack.popInt32())
		if err := store(memory, offset, index, val); err != nil {
			c.trap = err
			return trap
		}
		return advance
	}
}

// handleLoadV128FromBytes builds a v128 load that converts sizeBytes of memory
// via fromBytes.
func handleLoadV128FromBytes(
	body []uint64, pc int, fromBytes func([]byte) V128Value, sizeBytes uint32,
) frame {
	memIdx := body[pc+2]
	offset := uint32(body[pc+3])
	return func(c *frameCtx) int {
		memory := c.vm.getMemory(c.module, memIdx)
		index := c.vm.stack.popInt32()
		data, err := memory.Get(offset, uint32(index), sizeBytes)
		if err != nil {
			c.trap = err
			return trap
		}
		c.vm.stack.pushV128(fromBytes(data))
		return advance
	}
}

// handleExtractLane builds a lane-extract closure; the lane index is at pc+1.
func handleExtractLane[R wasmNumber](
	body []uint64, pc int, push func(R), op func(V128Value, uint32) R,
) frame {
	laneIndex := uint32(body[pc+1])
	return func(c *frameCtx) int {
		push(op(c.vm.stack.popV128(), laneIndex))
		return advance
	}
}

// handleReplaceLane builds a lane-replace closure; the lane index is at pc+1.
func handleReplaceLane[T wasmNumber](
	body []uint64, pc int, pop func() T, op func(V128Value, uint32, T) V128Value,
) frame {
	laneIndex := uint32(body[pc+1])
	return func(c *frameCtx) int {
		laneValue := pop()
		vector := c.vm.stack.popV128()
		c.vm.stack.pushV128(op(vector, laneIndex, laneValue))
		return advance
	}
}

// handleSimdLoadLane builds a v128 load-lane closure for a laneSize-bit lane.
func handleSimdLoadLane(body []uint64, pc int, laneSize uint32) frame {
	memIdx := body[pc+2]
	offset := uint32(body[pc+3])
	laneIndex := uint32(body[pc+4])
	return func(c *frameCtx) int {
		memory := c.vm.getMemory(c.module, memIdx)
		v := c.vm.stack.popV128()
		index := c.vm.stack.popInt32()
		laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
		if err != nil {
			c.trap = err
			return trap
		}
		c.vm.stack.pushV128(simdLoadLane(v, laneIndex, laneValue))
		return advance
	}
}

// handleSimdStoreLane builds a v128 store-lane closure for a laneSize-bit lane.
func handleSimdStoreLane(body []uint64, pc int, laneSize uint32) frame {
	memIdx := body[pc+2]
	offset := uint32(body[pc+3])
	laneIndex := uint32(body[pc+4])
	return func(c *frameCtx) int {
		memory := c.vm.getMemory(c.module, memIdx)
		v := c.vm.stack.popV128()
		index := c.vm.stack.popInt32()

		lanesPerUint64 := 64 / laneSize
		shift := (laneIndex % lanesPerUint64) * laneSize
		var val uint64
		if laneIndex < lanesPerUint64 {
			val = v.Low >> shift
		} else {
			val = v.High >> shift
		}

		var err error
		switch laneSize {
		case 8:
			err = memory.StoreByte(offset, uint32(index), byte(val))
		case 16:
			err = memory.StoreUint16(offset, uint32(index), uint16(val))
		case 32:
			err = memory.StoreUint32(offset, uint32(index), uint32(val))
		case 64:
			err = memory.StoreUint64(offset, uint32(index), val)
		}
		if err != nil {
			c.trap = err
			return trap
		}
		return advance
	}
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
	// We create a fake function to execute the expression.
	// The expression is expected to return a single value.
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
	if err := vm.compile(&function); err != nil {
		return value{}, err
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

func (vm *vm) getTable(module *ModuleInstance, localIndex uint64) *Table {
	tableIndex := module.tableAddrs[localIndex]
	return vm.store.tables[tableIndex]
}

func (vm *vm) getMemory(module *ModuleInstance, localIndex uint64) *Memory {
	memoryIndex := module.memAddrs[localIndex]
	return vm.store.memories[memoryIndex]
}

func (vm *vm) getGlobal(module *ModuleInstance, localIndex uint64) *Global {
	globalIndex := module.globalAddrs[localIndex]
	return vm.store.globals[globalIndex]
}

func (vm *vm) getElement(module *ModuleInstance, localIndex uint64) *elementInstance {
	elementIndex := module.elemAddrs[localIndex]
	return &vm.store.elements[elementIndex]
}

func (vm *vm) getData(module *ModuleInstance, localIndex uint64) *dataInstance {
	dataIndex := module.dataAddrs[localIndex]
	return &vm.store.datas[dataIndex]
}
