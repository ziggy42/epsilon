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
	vm           *vm
	controlStack []controlFrame
	locals       []value
	module       *ModuleInstance
	trap         error
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
	trap    = -2 // stop; callFrame.trap holds the error
)

// instr is a decoded instruction in the threaded run loop: a shared handler
// plus its inline operands. The handler reads operands from the instr rather
// than from a captured closure, so no per-instruction closure is allocated.
type instr struct {
	fn   handler
	a, b uint64
}

type handler func(c *callFrame, in *instr) int

// brToLabel branches out labelIndex enclosing blocks, unwinding the value stack
// to the target's height (keeping its arity values) and returning the target
// instruction index.
func (c *callFrame) brToLabel(labelIndex int) int {
	targetIndex := len(c.controlStack) - labelIndex - 1
	target := c.controlStack[targetIndex]
	c.controlStack = c.controlStack[:targetIndex]
	c.vm.stack.unwind(target.stackHeight, target.arity)
	if target.isLoop {
		c.controlStack = append(c.controlStack, target)
	}
	return int(target.targetIp)
}

// vm is the WebAssembly Virtual Machine.
type vm struct {
	store             *store
	stack             *valueStack
	callStackCache    []callFrame
	callDepth         int
	controlStackCache []controlFrame
	localsCache       []value
	// brTables holds the label vectors of every br_table in the store, indexed
	// by the instr operand. They are variable length, so they live here rather
	// than inline in the fixed-size instr.
	brTables [][]uint32
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
		callStackCache:    make([]callFrame, config.CallStackPreallocationSize),
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
	if vm.callDepth >= vm.config.MaxCallStackDepth {
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

	// Call frames within the preallocated range use a zero-allocation cache
	// slot, while deeper frames fallback to the heap. Either way, the frame's
	// memory address remains stable for its entire lifetime, allowing the run
	// loop to safely hold a *callFrame pointer across nested invocations.
	var call *callFrame
	var controlStack []controlFrame
	if vm.callDepth < vm.config.CallStackPreallocationSize {
		// Cache hit: fetch a pointer from the preallocated stack.
		call = &vm.callStackCache[vm.callDepth]

		// Carve out a zero-length slice with a strict capacity bound from the
		// control stack cache. The 3-index slice prevents appends from leaking
		// into the next slot's memory.
		blockDepth := vm.callDepth * controlStackCacheSlotSize
		max := blockDepth + controlStackCacheSlotSize
		controlStack = vm.controlStackCache[blockDepth:blockDepth:max]
	} else {
		// Cache miss: heap allocate a new frame.
		call = &callFrame{}
	}
	// Initialize the control stack with the function's entry block frame.
	controlStack = append(controlStack, controlFrame{
		targetIp:    int32(len(function.frames)),
		arity:       uint32(len(function.functionType.ResultTypes)),
		stackHeight: vm.stack.size(),
	})
	// Populate the frame. If cached, this overwrites stale data from past calls.
	*call = callFrame{
		vm:           vm,
		controlStack: controlStack,
		locals:       locals,
		module:       function.module,
		trap:         nil,
	}
	vm.callDepth++

	// To avoid the performance penalty of checking the fuel limit on every
	// instruction when fuel is disabled, we provide two separate loop
	// implementations.
	var err error
	if vm.config.EnableFuel {
		err = vm.runLoopWithFuel(call, function.frames)
	} else {
		err = vm.runLoop(call, function.frames)
	}
	// The run loop is done with this frame, so its locals slots are free to
	// reuse. Rewinding the cursor here is a no-op for the heap case.
	vm.localsTop = localsMark
	vm.callDepth--
	return err
}

func (vm *vm) runLoop(c *callFrame, frames []instr) error {
	ip := 0
	for ip < len(frames) {
		in := &frames[ip]
		switch n := in.fn(c, in); n {
		case advance:
			ip++
		case trap:
			return c.trap
		default:
			ip = n
		}
	}
	return nil
}

func (vm *vm) runLoopWithFuel(c *callFrame, frames []instr) error {
	ip := 0
	for ip < len(frames) {
		if vm.fuel == 0 {
			return errFuelExhausted
		}
		vm.fuel--
		in := &frames[ip]
		switch n := in.fn(c, in); n {
		case advance:
			ip++
		case trap:
			return c.trap
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
func (vm *vm) compileClosures(fn *function, module *ModuleInstance) ([]instr, error) {
	body := fn.body

	// First pass: assign an instruction index to every bytecode boundary so
	// branch targets (which the parser recorded as bytecode pcs) can be
	// translated into instruction indices. pcToIp is a dense pc-indexed slice
	// rather than a map: branch targets are always instruction-start pcs, whose
	// entries are filled below; the gaps for operand words are never read.
	pcToIp := make([]int, len(body)+1)
	starts := make([]int, 0, len(body))
	for i := 0; i < len(body); {
		pcToIp[i] = len(starts)
		starts = append(starts, i)
		op := opcode(body[i])
		i += 1 + operandWordCount(op, body, i+1)
	}
	pcToIp[len(body)] = len(starts)

	// Second pass: emit one instr per instruction. Opcodes converted to a shared
	// threaded handler are built by compileThreaded; the rest fall back to a
	// per-instruction closure adapted via runLegacy.
	code := make([]instr, 0, len(starts))
	for _, pc := range starts {
		in, ok := vm.compileThreaded(fn, module, pc, pcToIp)
		if !ok {
			return nil, fmt.Errorf("unhandled opcode %d", opcode(fn.body[pc]))
		}
		code = append(code, in)
	}
	return code, nil
}

// compileThreaded builds the threaded instr for opcodes that have a shared
// handler. It returns ok=false for opcodes still served by the closure path.
func (vm *vm) compileThreaded(fn *function, module *ModuleInstance, pc int, pcToIp []int) (instr, bool) {
	body := fn.body
	switch opcode(body[pc]) {
	case block:
		blockType := int32(body[pc+1])
		afterEndIp := pcToIp[fn.jumpCache[uint32(pc+2)]]
		inputCount := vm.getInputCount(module, blockType)
		outputCount := vm.getOutputCount(module, blockType)
		return instr{fn: opBlock, a: uint64(uint32(afterEndIp)), b: uint64(outputCount)<<32 | uint64(inputCount)}, true
	case loop:
		blockType := int32(body[pc+1])
		bodyIp := pcToIp[uint32(pc+2)]
		inputCount := vm.getInputCount(module, blockType)
		return instr{fn: opLoop, a: uint64(uint32(bodyIp)), b: uint64(inputCount)}, true
	case ifOp:
		blockType := int32(body[pc+1])
		afterEndIp := pcToIp[fn.jumpCache[uint32(pc+2)]]
		elseIp := pcToIp[fn.jumpElseCache[uint32(pc+2)]]
		inputCount := vm.getInputCount(module, blockType)
		outputCount := vm.getOutputCount(module, blockType)
		return instr{fn: opIf, a: uint64(uint32(afterEndIp))<<32 | uint64(uint32(elseIp)), b: uint64(outputCount)<<32 | uint64(inputCount)}, true
	case elseOp:
		return instr{fn: opElse}, true
	case end:
		return instr{fn: opEnd}, true
	case br:
		return instr{fn: opBr, a: body[pc+1]}, true
	case brIf:
		return instr{fn: opBrIf, a: body[pc+1]}, true
	case returnOp:
		return instr{fn: opReturn}, true
	case call:
		return instr{fn: opCall, a: body[pc+1]}, true
	case callIndirect:
		return instr{fn: opCallIndirect, a: body[pc+1], b: body[pc+2]}, true
	case selectOp:
		return instr{fn: opSelect}, true
	case selectT:
		return instr{fn: opSelect}, true
	case globalGet:
		return instr{fn: opGlobalGet, a: body[pc+1]}, true
	case globalSet:
		return instr{fn: opGlobalSet, a: body[pc+1]}, true
	case memorySize:
		return instr{fn: opMemorySize, a: body[pc+1]}, true
	case memoryGrow:
		return instr{fn: opMemoryGrow, a: body[pc+1]}, true
	case refIsNull:
		return instr{fn: opRefIsNull}, true
	case refNull:
		return instr{fn: opRefNull}, true
	case refFunc:
		return instr{fn: opRefFunc, a: body[pc+1]}, true
	case brTable:
		count := int(body[pc+1])
		labels := make([]uint32, count)
		for i := range labels {
			labels[i] = uint32(body[pc+2+i])
		}
		idx := len(vm.brTables)
		vm.brTables = append(vm.brTables, labels)
		return instr{fn: opBrTable, a: uint64(idx), b: body[pc+2+count]}, true
	case nop:
		return instr{fn: opNop}, true
	case unreachable:
		return instr{fn: opUnreachable}, true
	case drop:
		return instr{fn: opDrop}, true
	case i32Const:
		return instr{fn: opI32Const, a: body[pc+1]}, true
	case i64Const:
		return instr{fn: opI64Const, a: body[pc+1]}, true
	case f32Const:
		return instr{fn: opF32Const, a: body[pc+1]}, true
	case f64Const:
		return instr{fn: opF64Const, a: body[pc+1]}, true
	case localGet:
		return instr{fn: opLocalGet, a: body[pc+1]}, true
	case localSet:
		return instr{fn: opLocalSet, a: body[pc+1]}, true
	case localTee:
		return instr{fn: opLocalTee, a: body[pc+1]}, true
	case i32Add:
		return instr{fn: opBinI32[addI32]}, true
	case i32Sub:
		return instr{fn: opBinI32[subI32]}, true
	case i32Mul:
		return instr{fn: opBinI32[mulI32]}, true
	case i32And:
		return instr{fn: opBinI32[andI32]}, true
	case i32Or:
		return instr{fn: opBinI32[orI32]}, true
	case i32Xor:
		return instr{fn: opBinI32[xorI32]}, true
	case i32Shl:
		return instr{fn: opBinI32[shlI32]}, true
	case i32ShrS:
		return instr{fn: opBinI32[shrSI32]}, true
	case i32ShrU:
		return instr{fn: opBinI32[shrUI32]}, true
	case i32Rotl:
		return instr{fn: opBinI32[rotlI32]}, true
	case i32Rotr:
		return instr{fn: opBinI32[rotrI32]}, true
	case i64Add:
		return instr{fn: opBinI64[addI64]}, true
	case i64Sub:
		return instr{fn: opBinI64[subI64]}, true
	case i64Mul:
		return instr{fn: opBinI64[mulI64]}, true
	case i64And:
		return instr{fn: opBinI64[andI64]}, true
	case i64Or:
		return instr{fn: opBinI64[orI64]}, true
	case i64Xor:
		return instr{fn: opBinI64[xorI64]}, true
	case i64Shl:
		return instr{fn: opBinI64[shlI64]}, true
	case i64ShrS:
		return instr{fn: opBinI64[shrSI64]}, true
	case i64ShrU:
		return instr{fn: opBinI64[shrUI64]}, true
	case i64Rotl:
		return instr{fn: opBinI64[rotlI64]}, true
	case i64Rotr:
		return instr{fn: opBinI64[rotrI64]}, true
	case i32Eq:
		return instr{fn: opCmpI32[eqI32]}, true
	case i32Ne:
		return instr{fn: opCmpI32[neI32]}, true
	case i32LtS:
		return instr{fn: opCmpI32[ltSI32]}, true
	case i32LtU:
		return instr{fn: opCmpI32[ltUI32]}, true
	case i32GtS:
		return instr{fn: opCmpI32[gtSI32]}, true
	case i32GtU:
		return instr{fn: opCmpI32[gtUI32]}, true
	case i32LeS:
		return instr{fn: opCmpI32[leSI32]}, true
	case i32LeU:
		return instr{fn: opCmpI32[leUI32]}, true
	case i32GeS:
		return instr{fn: opCmpI32[geSI32]}, true
	case i32GeU:
		return instr{fn: opCmpI32[geUI32]}, true
	case i64Eq:
		return instr{fn: opCmpI64[eqI64]}, true
	case i64Ne:
		return instr{fn: opCmpI64[neI64]}, true
	case i64LtS:
		return instr{fn: opCmpI64[ltSI64]}, true
	case i64LtU:
		return instr{fn: opCmpI64[ltUI64]}, true
	case i64GtS:
		return instr{fn: opCmpI64[gtSI64]}, true
	case i64GtU:
		return instr{fn: opCmpI64[gtUI64]}, true
	case i64LeS:
		return instr{fn: opCmpI64[leSI64]}, true
	case i64LeU:
		return instr{fn: opCmpI64[leUI64]}, true
	case i64GeS:
		return instr{fn: opCmpI64[geSI64]}, true
	case i64GeU:
		return instr{fn: opCmpI64[geUI64]}, true
	case f32Eq:
		return instr{fn: opCmpF32[f32EqOp]}, true
	case f32Ne:
		return instr{fn: opCmpF32[f32NeOp]}, true
	case f32Lt:
		return instr{fn: opCmpF32[f32LtOp]}, true
	case f32Gt:
		return instr{fn: opCmpF32[f32GtOp]}, true
	case f32Le:
		return instr{fn: opCmpF32[f32LeOp]}, true
	case f32Ge:
		return instr{fn: opCmpF32[f32GeOp]}, true
	case f64Eq:
		return instr{fn: opCmpF64[f64EqOp]}, true
	case f64Ne:
		return instr{fn: opCmpF64[f64NeOp]}, true
	case f64Lt:
		return instr{fn: opCmpF64[f64LtOp]}, true
	case f64Gt:
		return instr{fn: opCmpF64[f64GtOp]}, true
	case f64Le:
		return instr{fn: opCmpF64[f64LeOp]}, true
	case f64Ge:
		return instr{fn: opCmpF64[f64GeOp]}, true
	case i32DivS:
		return instr{fn: opSafeBinI32[i32DivSOp]}, true
	case i32DivU:
		return instr{fn: opSafeBinI32[i32DivUOp]}, true
	case i32RemS:
		return instr{fn: opSafeBinI32[i32RemSOp]}, true
	case i32RemU:
		return instr{fn: opSafeBinI32[i32RemUOp]}, true
	case i64DivS:
		return instr{fn: opSafeBinI64[i64DivSOp]}, true
	case i64DivU:
		return instr{fn: opSafeBinI64[i64DivUOp]}, true
	case i64RemS:
		return instr{fn: opSafeBinI64[i64RemSOp]}, true
	case i64RemU:
		return instr{fn: opSafeBinI64[i64RemUOp]}, true
	case f32Add:
		return instr{fn: opBinF32[f32AddOp]}, true
	case f32Sub:
		return instr{fn: opBinF32[f32SubOp]}, true
	case f32Mul:
		return instr{fn: opBinF32[f32MulOp]}, true
	case f32Div:
		return instr{fn: opBinF32[f32DivOp]}, true
	case f32Min:
		return instr{fn: opBinF32[f32MinOp]}, true
	case f32Max:
		return instr{fn: opBinF32[f32MaxOp]}, true
	case f32Copysign:
		return instr{fn: opBinF32[f32CopysignOp]}, true
	case f64Add:
		return instr{fn: opBinF64[f64AddOp]}, true
	case f64Sub:
		return instr{fn: opBinF64[f64SubOp]}, true
	case f64Mul:
		return instr{fn: opBinF64[f64MulOp]}, true
	case f64Div:
		return instr{fn: opBinF64[f64DivOp]}, true
	case f64Min:
		return instr{fn: opBinF64[f64MinOp]}, true
	case f64Max:
		return instr{fn: opBinF64[f64MaxOp]}, true
	case f64Copysign:
		return instr{fn: opBinF64[f64CopysignOp]}, true
	case i32TruncF32S:
		return instr{fn: opUnarySafeF32[i32TruncF32SOp]}, true
	case i32TruncF32U:
		return instr{fn: opUnarySafeF32[i32TruncF32UOp]}, true
	case i32TruncF64S:
		return instr{fn: opUnarySafeF64[i32TruncF64SOp]}, true
	case i32TruncF64U:
		return instr{fn: opUnarySafeF64[i32TruncF64UOp]}, true
	case i64TruncF32S:
		return instr{fn: opTruncF32I64[i64TruncF32SOp]}, true
	case i64TruncF32U:
		return instr{fn: opTruncF32I64[i64TruncF32UOp]}, true
	case i64TruncF64S:
		return instr{fn: opTruncF64I64[i64TruncF64SOp]}, true
	case i64TruncF64U:
		return instr{fn: opTruncF64I64[i64TruncF64UOp]}, true
	case i8x16Swizzle:
		return instr{fn: opBinV128[i8x16SwizzleOp]}, true
	case i8x16Eq:
		return instr{fn: opBinV128[i8x16EqOp]}, true
	case i8x16Ne:
		return instr{fn: opBinV128[i8x16NeOp]}, true
	case i8x16LtS:
		return instr{fn: opBinV128[i8x16LtSOp]}, true
	case i8x16LtU:
		return instr{fn: opBinV128[i8x16LtUOp]}, true
	case i8x16GtS:
		return instr{fn: opBinV128[i8x16GtSOp]}, true
	case i8x16GtU:
		return instr{fn: opBinV128[i8x16GtUOp]}, true
	case i8x16LeS:
		return instr{fn: opBinV128[i8x16LeSOp]}, true
	case i8x16LeU:
		return instr{fn: opBinV128[i8x16LeUOp]}, true
	case i8x16GeS:
		return instr{fn: opBinV128[i8x16GeSOp]}, true
	case i8x16GeU:
		return instr{fn: opBinV128[i8x16GeUOp]}, true
	case i16x8Eq:
		return instr{fn: opBinV128[i16x8EqOp]}, true
	case i16x8Ne:
		return instr{fn: opBinV128[i16x8NeOp]}, true
	case i16x8LtS:
		return instr{fn: opBinV128[i16x8LtSOp]}, true
	case i16x8LtU:
		return instr{fn: opBinV128[i16x8LtUOp]}, true
	case i16x8GtS:
		return instr{fn: opBinV128[i16x8GtSOp]}, true
	case i16x8GtU:
		return instr{fn: opBinV128[i16x8GtUOp]}, true
	case i16x8LeS:
		return instr{fn: opBinV128[i16x8LeSOp]}, true
	case i16x8LeU:
		return instr{fn: opBinV128[i16x8LeUOp]}, true
	case i16x8GeS:
		return instr{fn: opBinV128[i16x8GeSOp]}, true
	case i16x8GeU:
		return instr{fn: opBinV128[i16x8GeUOp]}, true
	case i32x4Eq:
		return instr{fn: opBinV128[i32x4EqOp]}, true
	case i32x4Ne:
		return instr{fn: opBinV128[i32x4NeOp]}, true
	case i32x4LtS:
		return instr{fn: opBinV128[i32x4LtSOp]}, true
	case i32x4LtU:
		return instr{fn: opBinV128[i32x4LtUOp]}, true
	case i32x4GtS:
		return instr{fn: opBinV128[i32x4GtSOp]}, true
	case i32x4GtU:
		return instr{fn: opBinV128[i32x4GtUOp]}, true
	case i32x4LeS:
		return instr{fn: opBinV128[i32x4LeSOp]}, true
	case i32x4LeU:
		return instr{fn: opBinV128[i32x4LeUOp]}, true
	case i32x4GeS:
		return instr{fn: opBinV128[i32x4GeSOp]}, true
	case i32x4GeU:
		return instr{fn: opBinV128[i32x4GeUOp]}, true
	case f32x4Eq:
		return instr{fn: opBinV128[f32x4EqOp]}, true
	case f32x4Ne:
		return instr{fn: opBinV128[f32x4NeOp]}, true
	case f32x4Lt:
		return instr{fn: opBinV128[f32x4LtOp]}, true
	case f32x4Gt:
		return instr{fn: opBinV128[f32x4GtOp]}, true
	case f32x4Le:
		return instr{fn: opBinV128[f32x4LeOp]}, true
	case f32x4Ge:
		return instr{fn: opBinV128[f32x4GeOp]}, true
	case f64x2Eq:
		return instr{fn: opBinV128[f64x2EqOp]}, true
	case f64x2Ne:
		return instr{fn: opBinV128[f64x2NeOp]}, true
	case f64x2Lt:
		return instr{fn: opBinV128[f64x2LtOp]}, true
	case f64x2Gt:
		return instr{fn: opBinV128[f64x2GtOp]}, true
	case f64x2Le:
		return instr{fn: opBinV128[f64x2LeOp]}, true
	case f64x2Ge:
		return instr{fn: opBinV128[f64x2GeOp]}, true
	case v128Not:
		return instr{fn: opUnaryV128G[v128NotOp]}, true
	case v128And:
		return instr{fn: opBinV128[v128AndOp]}, true
	case v128Andnot:
		return instr{fn: opBinV128[v128AndnotOp]}, true
	case v128Or:
		return instr{fn: opBinV128[v128OrOp]}, true
	case v128Xor:
		return instr{fn: opBinV128[v128XorOp]}, true
	case f32x4DemoteF64x2Zero:
		return instr{fn: opUnaryV128G[f32x4DemoteF64x2ZeroOp]}, true
	case f64x2PromoteLowF32x4:
		return instr{fn: opUnaryV128G[f64x2PromoteLowF32x4Op]}, true
	case i8x16Abs:
		return instr{fn: opUnaryV128G[i8x16AbsOp]}, true
	case i8x16Neg:
		return instr{fn: opUnaryV128G[i8x16NegOp]}, true
	case i8x16Popcnt:
		return instr{fn: opUnaryV128G[i8x16PopcntOp]}, true
	case i8x16NarrowI16x8S:
		return instr{fn: opBinV128[i8x16NarrowI16x8SOp]}, true
	case i8x16NarrowI16x8U:
		return instr{fn: opBinV128[i8x16NarrowI16x8UOp]}, true
	case f32x4Ceil:
		return instr{fn: opUnaryV128G[f32x4CeilOp]}, true
	case f32x4Floor:
		return instr{fn: opUnaryV128G[f32x4FloorOp]}, true
	case f32x4Trunc:
		return instr{fn: opUnaryV128G[f32x4TruncOp]}, true
	case f32x4Nearest:
		return instr{fn: opUnaryV128G[f32x4NearestOp]}, true
	case i8x16Shl:
		return instr{fn: opShiftV128[i8x16ShlOp]}, true
	case i8x16ShrU:
		return instr{fn: opShiftV128[i8x16ShrUOp]}, true
	case i8x16ShrS:
		return instr{fn: opShiftV128[i8x16ShrSOp]}, true
	case i8x16Add:
		return instr{fn: opBinV128[i8x16AddOp]}, true
	case i8x16AddSatS:
		return instr{fn: opBinV128[i8x16AddSatSOp]}, true
	case i8x16AddSatU:
		return instr{fn: opBinV128[i8x16AddSatUOp]}, true
	case i8x16Sub:
		return instr{fn: opBinV128[i8x16SubOp]}, true
	case i8x16SubSatS:
		return instr{fn: opBinV128[i8x16SubSatSOp]}, true
	case i8x16SubSatU:
		return instr{fn: opBinV128[i8x16SubSatUOp]}, true
	case f64x2Ceil:
		return instr{fn: opUnaryV128G[f64x2CeilOp]}, true
	case f64x2Floor:
		return instr{fn: opUnaryV128G[f64x2FloorOp]}, true
	case i8x16MinS:
		return instr{fn: opBinV128[i8x16MinSOp]}, true
	case i8x16MinU:
		return instr{fn: opBinV128[i8x16MinUOp]}, true
	case i8x16MaxS:
		return instr{fn: opBinV128[i8x16MaxSOp]}, true
	case i8x16MaxU:
		return instr{fn: opBinV128[i8x16MaxUOp]}, true
	case f64x2Trunc:
		return instr{fn: opUnaryV128G[f64x2TruncOp]}, true
	case i8x16AvgrU:
		return instr{fn: opBinV128[i8x16AvgrUOp]}, true
	case i16x8ExtaddPairwiseI8x16S:
		return instr{fn: opUnaryV128G[i16x8ExtaddPairwiseI8x16SOp]}, true
	case i16x8ExtaddPairwiseI8x16U:
		return instr{fn: opUnaryV128G[i16x8ExtaddPairwiseI8x16UOp]}, true
	case i32x4ExtaddPairwiseI16x8S:
		return instr{fn: opUnaryV128G[i32x4ExtaddPairwiseI16x8SOp]}, true
	case i32x4ExtaddPairwiseI16x8U:
		return instr{fn: opUnaryV128G[i32x4ExtaddPairwiseI16x8UOp]}, true
	case i16x8Abs:
		return instr{fn: opUnaryV128G[i16x8AbsOp]}, true
	case i16x8Neg:
		return instr{fn: opUnaryV128G[i16x8NegOp]}, true
	case i16x8Q15mulrSatS:
		return instr{fn: opBinV128[i16x8Q15mulrSatSOp]}, true
	case i16x8NarrowI32x4S:
		return instr{fn: opBinV128[i16x8NarrowI32x4SOp]}, true
	case i16x8NarrowI32x4U:
		return instr{fn: opBinV128[i16x8NarrowI32x4UOp]}, true
	case i16x8ExtendLowI8x16S:
		return instr{fn: opUnaryV128G[i16x8ExtendLowI8x16SOp]}, true
	case i16x8ExtendHighI8x16S:
		return instr{fn: opUnaryV128G[i16x8ExtendHighI8x16SOp]}, true
	case i16x8ExtendLowI8x16U:
		return instr{fn: opUnaryV128G[i16x8ExtendLowI8x16UOp]}, true
	case i16x8ExtendHighI8x16U:
		return instr{fn: opUnaryV128G[i16x8ExtendHighI8x16UOp]}, true
	case i16x8Shl:
		return instr{fn: opShiftV128[i16x8ShlOp]}, true
	case i16x8ShrS:
		return instr{fn: opShiftV128[i16x8ShrSOp]}, true
	case i16x8ShrU:
		return instr{fn: opShiftV128[i16x8ShrUOp]}, true
	case i16x8Add:
		return instr{fn: opBinV128[i16x8AddOp]}, true
	case i16x8AddSatS:
		return instr{fn: opBinV128[i16x8AddSatSOp]}, true
	case i16x8AddSatU:
		return instr{fn: opBinV128[i16x8AddSatUOp]}, true
	case i16x8Sub:
		return instr{fn: opBinV128[i16x8SubOp]}, true
	case i16x8SubSatS:
		return instr{fn: opBinV128[i16x8SubSatSOp]}, true
	case i16x8SubSatU:
		return instr{fn: opBinV128[i16x8SubSatUOp]}, true
	case f64x2Nearest:
		return instr{fn: opUnaryV128G[f64x2NearestOp]}, true
	case i16x8Mul:
		return instr{fn: opBinV128[i16x8MulOp]}, true
	case i16x8MinS:
		return instr{fn: opBinV128[i16x8MinSOp]}, true
	case i16x8MinU:
		return instr{fn: opBinV128[i16x8MinUOp]}, true
	case i16x8MaxS:
		return instr{fn: opBinV128[i16x8MaxSOp]}, true
	case i16x8MaxU:
		return instr{fn: opBinV128[i16x8MaxUOp]}, true
	case i16x8AvgrU:
		return instr{fn: opBinV128[i16x8AvgrUOp]}, true
	case i16x8ExtmulLowI8x16S:
		return instr{fn: opBinV128[i16x8ExtmulLowI8x16SOp]}, true
	case i16x8ExtmulHighI8x16S:
		return instr{fn: opBinV128[i16x8ExtmulHighI8x16SOp]}, true
	case i16x8ExtmulLowI8x16U:
		return instr{fn: opBinV128[i16x8ExtmulLowI8x16UOp]}, true
	case i16x8ExtmulHighI8x16U:
		return instr{fn: opBinV128[i16x8ExtmulHighI8x16UOp]}, true
	case i32x4Abs:
		return instr{fn: opUnaryV128G[i32x4AbsOp]}, true
	case i32x4Neg:
		return instr{fn: opUnaryV128G[i32x4NegOp]}, true
	case i32x4ExtendLowI16x8S:
		return instr{fn: opUnaryV128G[i32x4ExtendLowI16x8SOp]}, true
	case i32x4ExtendHighI16x8S:
		return instr{fn: opUnaryV128G[i32x4ExtendHighI16x8SOp]}, true
	case i32x4ExtendLowI16x8U:
		return instr{fn: opUnaryV128G[i32x4ExtendLowI16x8UOp]}, true
	case i32x4ExtendHighI16x8U:
		return instr{fn: opUnaryV128G[i32x4ExtendHighI16x8UOp]}, true
	case i32x4Shl:
		return instr{fn: opShiftV128[i32x4ShlOp]}, true
	case i32x4ShrS:
		return instr{fn: opShiftV128[i32x4ShrSOp]}, true
	case i32x4ShrU:
		return instr{fn: opShiftV128[i32x4ShrUOp]}, true
	case i32x4Add:
		return instr{fn: opBinV128[i32x4AddOp]}, true
	case i32x4Sub:
		return instr{fn: opBinV128[i32x4SubOp]}, true
	case i32x4Mul:
		return instr{fn: opBinV128[i32x4MulOp]}, true
	case i32x4MinS:
		return instr{fn: opBinV128[i32x4MinSOp]}, true
	case i32x4MinU:
		return instr{fn: opBinV128[i32x4MinUOp]}, true
	case i32x4MaxS:
		return instr{fn: opBinV128[i32x4MaxSOp]}, true
	case i32x4MaxU:
		return instr{fn: opBinV128[i32x4MaxUOp]}, true
	case i32x4DotI16x8S:
		return instr{fn: opBinV128[i32x4DotI16x8SOp]}, true
	case i32x4ExtmulLowI16x8S:
		return instr{fn: opBinV128[i32x4ExtmulLowI16x8SOp]}, true
	case i32x4ExtmulHighI16x8S:
		return instr{fn: opBinV128[i32x4ExtmulHighI16x8SOp]}, true
	case i32x4ExtmulLowI16x8U:
		return instr{fn: opBinV128[i32x4ExtmulLowI16x8UOp]}, true
	case i32x4ExtmulHighI16x8U:
		return instr{fn: opBinV128[i32x4ExtmulHighI16x8UOp]}, true
	case i64x2Abs:
		return instr{fn: opUnaryV128G[i64x2AbsOp]}, true
	case i64x2Neg:
		return instr{fn: opUnaryV128G[i64x2NegOp]}, true
	case i64x2ExtendLowI32x4S:
		return instr{fn: opUnaryV128G[i64x2ExtendLowI32x4SOp]}, true
	case i64x2ExtendHighI32x4S:
		return instr{fn: opUnaryV128G[i64x2ExtendHighI32x4SOp]}, true
	case i64x2ExtendLowI32x4U:
		return instr{fn: opUnaryV128G[i64x2ExtendLowI32x4UOp]}, true
	case i64x2ExtendHighI32x4U:
		return instr{fn: opUnaryV128G[i64x2ExtendHighI32x4UOp]}, true
	case i64x2Shl:
		return instr{fn: opShiftV128[i64x2ShlOp]}, true
	case i64x2ShrS:
		return instr{fn: opShiftV128[i64x2ShrSOp]}, true
	case i64x2ShrU:
		return instr{fn: opShiftV128[i64x2ShrUOp]}, true
	case i64x2Add:
		return instr{fn: opBinV128[i64x2AddOp]}, true
	case i64x2Sub:
		return instr{fn: opBinV128[i64x2SubOp]}, true
	case i64x2Mul:
		return instr{fn: opBinV128[i64x2MulOp]}, true
	case i64x2Eq:
		return instr{fn: opBinV128[i64x2EqOp]}, true
	case i64x2Ne:
		return instr{fn: opBinV128[i64x2NeOp]}, true
	case i64x2LtS:
		return instr{fn: opBinV128[i64x2LtSOp]}, true
	case i64x2GtS:
		return instr{fn: opBinV128[i64x2GtSOp]}, true
	case i64x2LeS:
		return instr{fn: opBinV128[i64x2LeSOp]}, true
	case i64x2GeS:
		return instr{fn: opBinV128[i64x2GeSOp]}, true
	case i64x2ExtmulLowI32x4S:
		return instr{fn: opBinV128[i64x2ExtmulLowI32x4SOp]}, true
	case i64x2ExtmulHighI32x4S:
		return instr{fn: opBinV128[i64x2ExtmulHighI32x4SOp]}, true
	case i64x2ExtmulLowI32x4U:
		return instr{fn: opBinV128[i64x2ExtmulLowI32x4UOp]}, true
	case i64x2ExtmulHighI32x4U:
		return instr{fn: opBinV128[i64x2ExtmulHighI32x4UOp]}, true
	case f32x4Abs:
		return instr{fn: opUnaryV128G[f32x4AbsOp]}, true
	case f32x4Neg:
		return instr{fn: opUnaryV128G[f32x4NegOp]}, true
	case f32x4Sqrt:
		return instr{fn: opUnaryV128G[f32x4SqrtOp]}, true
	case f32x4Add:
		return instr{fn: opBinV128[f32x4AddOp]}, true
	case f32x4Sub:
		return instr{fn: opBinV128[f32x4SubOp]}, true
	case f32x4Mul:
		return instr{fn: opBinV128[f32x4MulOp]}, true
	case f32x4Div:
		return instr{fn: opBinV128[f32x4DivOp]}, true
	case f32x4Min:
		return instr{fn: opBinV128[f32x4MinOp]}, true
	case f32x4Max:
		return instr{fn: opBinV128[f32x4MaxOp]}, true
	case f32x4Pmin:
		return instr{fn: opBinV128[f32x4PminOp]}, true
	case f32x4Pmax:
		return instr{fn: opBinV128[f32x4PmaxOp]}, true
	case f64x2Abs:
		return instr{fn: opUnaryV128G[f64x2AbsOp]}, true
	case f64x2Neg:
		return instr{fn: opUnaryV128G[f64x2NegOp]}, true
	case f64x2Sqrt:
		return instr{fn: opUnaryV128G[f64x2SqrtOp]}, true
	case f64x2Add:
		return instr{fn: opBinV128[f64x2AddOp]}, true
	case f64x2Sub:
		return instr{fn: opBinV128[f64x2SubOp]}, true
	case f64x2Mul:
		return instr{fn: opBinV128[f64x2MulOp]}, true
	case f64x2Div:
		return instr{fn: opBinV128[f64x2DivOp]}, true
	case f64x2Min:
		return instr{fn: opBinV128[f64x2MinOp]}, true
	case f64x2Max:
		return instr{fn: opBinV128[f64x2MaxOp]}, true
	case f64x2Pmin:
		return instr{fn: opBinV128[f64x2PminOp]}, true
	case f64x2Pmax:
		return instr{fn: opBinV128[f64x2PmaxOp]}, true
	case i32x4TruncSatF32x4S:
		return instr{fn: opUnaryV128G[i32x4TruncSatF32x4SOp]}, true
	case i32x4TruncSatF32x4U:
		return instr{fn: opUnaryV128G[i32x4TruncSatF32x4UOp]}, true
	case f32x4ConvertI32x4S:
		return instr{fn: opUnaryV128G[f32x4ConvertI32x4SOp]}, true
	case f32x4ConvertI32x4U:
		return instr{fn: opUnaryV128G[f32x4ConvertI32x4UOp]}, true
	case i32x4TruncSatF64x2SZero:
		return instr{fn: opUnaryV128G[i32x4TruncSatF64x2SZeroOp]}, true
	case i32x4TruncSatF64x2UZero:
		return instr{fn: opUnaryV128G[i32x4TruncSatF64x2UZeroOp]}, true
	case f64x2ConvertLowI32x4S:
		return instr{fn: opUnaryV128G[f64x2ConvertLowI32x4SOp]}, true
	case f64x2ConvertLowI32x4U:
		return instr{fn: opUnaryV128G[f64x2ConvertLowI32x4UOp]}, true
	case i32Load:
		return instr{fn: opI32Load, a: body[pc+2], b: body[pc+3]}, true
	case i64Load:
		return instr{fn: opI64Load, a: body[pc+2], b: body[pc+3]}, true
	case f32Load:
		return instr{fn: opF32Load, a: body[pc+2], b: body[pc+3]}, true
	case f64Load:
		return instr{fn: opF64Load, a: body[pc+2], b: body[pc+3]}, true
	case i32Load8S:
		return instr{fn: opI32Load8S, a: body[pc+2], b: body[pc+3]}, true
	case i32Load8U:
		return instr{fn: opI32Load8U, a: body[pc+2], b: body[pc+3]}, true
	case i32Load16S:
		return instr{fn: opI32Load16S, a: body[pc+2], b: body[pc+3]}, true
	case i32Load16U:
		return instr{fn: opI32Load16U, a: body[pc+2], b: body[pc+3]}, true
	case i64Load8S:
		return instr{fn: opI64Load8S, a: body[pc+2], b: body[pc+3]}, true
	case i64Load8U:
		return instr{fn: opI64Load8U, a: body[pc+2], b: body[pc+3]}, true
	case i64Load16S:
		return instr{fn: opI64Load16S, a: body[pc+2], b: body[pc+3]}, true
	case i64Load16U:
		return instr{fn: opI64Load16U, a: body[pc+2], b: body[pc+3]}, true
	case i64Load32S:
		return instr{fn: opI64Load32S, a: body[pc+2], b: body[pc+3]}, true
	case i64Load32U:
		return instr{fn: opI64Load32U, a: body[pc+2], b: body[pc+3]}, true
	case i32Store:
		return instr{fn: opI32Store, a: body[pc+2], b: body[pc+3]}, true
	case i64Store:
		return instr{fn: opI64Store, a: body[pc+2], b: body[pc+3]}, true
	case f32Store:
		return instr{fn: opF32Store, a: body[pc+2], b: body[pc+3]}, true
	case f64Store:
		return instr{fn: opF64Store, a: body[pc+2], b: body[pc+3]}, true
	case i32Store8:
		return instr{fn: opI32Store8, a: body[pc+2], b: body[pc+3]}, true
	case i32Store16:
		return instr{fn: opI32Store16, a: body[pc+2], b: body[pc+3]}, true
	case i64Store8:
		return instr{fn: opI64Store8, a: body[pc+2], b: body[pc+3]}, true
	case i64Store16:
		return instr{fn: opI64Store16, a: body[pc+2], b: body[pc+3]}, true
	case i64Store32:
		return instr{fn: opI64Store32, a: body[pc+2], b: body[pc+3]}, true
	case i32Clz:
		return instr{fn: opI32Clz}, true
	case i32Ctz:
		return instr{fn: opI32Ctz}, true
	case i32Popcnt:
		return instr{fn: opI32Popcnt}, true
	case i32Eqz:
		return instr{fn: opI32Eqz}, true
	case i64Clz:
		return instr{fn: opI64Clz}, true
	case i64Ctz:
		return instr{fn: opI64Ctz}, true
	case i64Popcnt:
		return instr{fn: opI64Popcnt}, true
	case i64Eqz:
		return instr{fn: opI64Eqz}, true
	case f32Abs:
		return instr{fn: opF32Abs}, true
	case f32Neg:
		return instr{fn: opF32Neg}, true
	case f32Ceil:
		return instr{fn: opF32Ceil}, true
	case f32Floor:
		return instr{fn: opF32Floor}, true
	case f32Trunc:
		return instr{fn: opF32Trunc}, true
	case f32Nearest:
		return instr{fn: opF32Nearest}, true
	case f32Sqrt:
		return instr{fn: opF32Sqrt}, true
	case f64Abs:
		return instr{fn: opF64Abs}, true
	case f64Neg:
		return instr{fn: opF64Neg}, true
	case f64Ceil:
		return instr{fn: opF64Ceil}, true
	case f64Floor:
		return instr{fn: opF64Floor}, true
	case f64Trunc:
		return instr{fn: opF64Trunc}, true
	case f64Nearest:
		return instr{fn: opF64Nearest}, true
	case f64Sqrt:
		return instr{fn: opF64Sqrt}, true
	case i32WrapI64:
		return instr{fn: opI32WrapI64}, true
	case i64ExtendI32S:
		return instr{fn: opI64ExtendI32S}, true
	case i64ExtendI32U:
		return instr{fn: opI64ExtendI32U}, true
	case i32Extend8S:
		return instr{fn: opI32Extend8S}, true
	case i32Extend16S:
		return instr{fn: opI32Extend16S}, true
	case i64Extend8S:
		return instr{fn: opI64Extend8S}, true
	case i64Extend16S:
		return instr{fn: opI64Extend16S}, true
	case i64Extend32S:
		return instr{fn: opI64Extend32S}, true
	case f32ConvertI32S:
		return instr{fn: opF32ConvertI32S}, true
	case f32ConvertI32U:
		return instr{fn: opF32ConvertI32U}, true
	case f32ConvertI64S:
		return instr{fn: opF32ConvertI64S}, true
	case f32ConvertI64U:
		return instr{fn: opF32ConvertI64U}, true
	case f32DemoteF64:
		return instr{fn: opF32DemoteF64}, true
	case f64ConvertI32S:
		return instr{fn: opF64ConvertI32S}, true
	case f64ConvertI32U:
		return instr{fn: opF64ConvertI32U}, true
	case f64ConvertI64S:
		return instr{fn: opF64ConvertI64S}, true
	case f64ConvertI64U:
		return instr{fn: opF64ConvertI64U}, true
	case f64PromoteF32:
		return instr{fn: opF64PromoteF32}, true
	case i32ReinterpretF32:
		return instr{fn: opI32ReinterpretF32}, true
	case i64ReinterpretF64:
		return instr{fn: opI64ReinterpretF64}, true
	case f32ReinterpretI32:
		return instr{fn: opF32ReinterpretI32}, true
	case f64ReinterpretI64:
		return instr{fn: opF64ReinterpretI64}, true
	case i32TruncSatF32S:
		return instr{fn: opI32TruncSatF32S}, true
	case i32TruncSatF32U:
		return instr{fn: opI32TruncSatF32U}, true
	case i32TruncSatF64S:
		return instr{fn: opI32TruncSatF64S}, true
	case i32TruncSatF64U:
		return instr{fn: opI32TruncSatF64U}, true
	case i64TruncSatF32S:
		return instr{fn: opI64TruncSatF32S}, true
	case i64TruncSatF32U:
		return instr{fn: opI64TruncSatF32U}, true
	case i64TruncSatF64S:
		return instr{fn: opI64TruncSatF64S}, true
	case i64TruncSatF64U:
		return instr{fn: opI64TruncSatF64U}, true
	case i8x16Splat:
		return instr{fn: opI8x16Splat}, true
	case i16x8Splat:
		return instr{fn: opI16x8Splat}, true
	case i32x4Splat:
		return instr{fn: opI32x4Splat}, true
	case i64x2Splat:
		return instr{fn: opI64x2Splat}, true
	case f32x4Splat:
		return instr{fn: opF32x4Splat}, true
	case f64x2Splat:
		return instr{fn: opF64x2Splat}, true
	case v128AnyTrue:
		return instr{fn: opV128AnyTrue}, true
	case i8x16AllTrue:
		return instr{fn: opI8x16AllTrue}, true
	case i8x16Bitmask:
		return instr{fn: opI8x16Bitmask}, true
	case i16x8AllTrue:
		return instr{fn: opI16x8AllTrue}, true
	case i16x8Bitmask:
		return instr{fn: opI16x8Bitmask}, true
	case i32x4AllTrue:
		return instr{fn: opI32x4AllTrue}, true
	case i32x4Bitmask:
		return instr{fn: opI32x4Bitmask}, true
	case i64x2AllTrue:
		return instr{fn: opI64x2AllTrue}, true
	case i64x2Bitmask:
		return instr{fn: opI64x2Bitmask}, true
	case tableGet:
		return instr{fn: opTableGet, a: body[pc+1]}, true
	case tableSet:
		return instr{fn: opTableSet, a: body[pc+1]}, true
	case memoryInit:
		return instr{fn: opMemoryInit, a: body[pc+1], b: body[pc+2]}, true
	case memoryCopy:
		return instr{fn: opMemoryCopy, a: body[pc+1], b: body[pc+2]}, true
	case memoryFill:
		return instr{fn: opMemoryFill, a: body[pc+1]}, true
	case dataDrop:
		return instr{fn: opDataDrop, a: body[pc+1]}, true
	case tableInit:
		return instr{fn: opTableInit, a: body[pc+1], b: body[pc+2]}, true
	case elemDrop:
		return instr{fn: opElemDrop, a: body[pc+1]}, true
	case tableCopy:
		return instr{fn: opTableCopy, a: body[pc+1], b: body[pc+2]}, true
	case tableGrow:
		return instr{fn: opTableGrow, a: body[pc+1]}, true
	case tableSize:
		return instr{fn: opTableSize, a: body[pc+1]}, true
	case tableFill:
		return instr{fn: opTableFill, a: body[pc+1]}, true
	case i8x16ExtractLaneS:
		return instr{fn: opI8x16ExtractLaneS, a: body[pc+1]}, true
	case i8x16ExtractLaneU:
		return instr{fn: opI8x16ExtractLaneU, a: body[pc+1]}, true
	case i16x8ExtractLaneS:
		return instr{fn: opI16x8ExtractLaneS, a: body[pc+1]}, true
	case i16x8ExtractLaneU:
		return instr{fn: opI16x8ExtractLaneU, a: body[pc+1]}, true
	case i32x4ExtractLane:
		return instr{fn: opI32x4ExtractLane, a: body[pc+1]}, true
	case i64x2ExtractLane:
		return instr{fn: opI64x2ExtractLane, a: body[pc+1]}, true
	case f32x4ExtractLane:
		return instr{fn: opF32x4ExtractLane, a: body[pc+1]}, true
	case f64x2ExtractLane:
		return instr{fn: opF64x2ExtractLane, a: body[pc+1]}, true
	case i8x16ReplaceLane:
		return instr{fn: opI8x16ReplaceLane, a: body[pc+1]}, true
	case i16x8ReplaceLane:
		return instr{fn: opI16x8ReplaceLane, a: body[pc+1]}, true
	case i32x4ReplaceLane:
		return instr{fn: opI32x4ReplaceLane, a: body[pc+1]}, true
	case i64x2ReplaceLane:
		return instr{fn: opI64x2ReplaceLane, a: body[pc+1]}, true
	case f32x4ReplaceLane:
		return instr{fn: opF32x4ReplaceLane, a: body[pc+1]}, true
	case f64x2ReplaceLane:
		return instr{fn: opF64x2ReplaceLane, a: body[pc+1]}, true
	case v128Load8x8S:
		return instr{fn: opV128Load8x8S, a: body[pc+2], b: body[pc+3]}, true
	case v128Load8x8U:
		return instr{fn: opV128Load8x8U, a: body[pc+2], b: body[pc+3]}, true
	case v128Load16x4S:
		return instr{fn: opV128Load16x4S, a: body[pc+2], b: body[pc+3]}, true
	case v128Load16x4U:
		return instr{fn: opV128Load16x4U, a: body[pc+2], b: body[pc+3]}, true
	case v128Load32x2S:
		return instr{fn: opV128Load32x2S, a: body[pc+2], b: body[pc+3]}, true
	case v128Load32x2U:
		return instr{fn: opV128Load32x2U, a: body[pc+2], b: body[pc+3]}, true
	case v128Load8Splat:
		return instr{fn: opV128Load8Splat, a: body[pc+2], b: body[pc+3]}, true
	case v128Load16Splat:
		return instr{fn: opV128Load16Splat, a: body[pc+2], b: body[pc+3]}, true
	case v128Load32Splat:
		return instr{fn: opV128Load32Splat, a: body[pc+2], b: body[pc+3]}, true
	case v128Load64Splat:
		return instr{fn: opV128Load64Splat, a: body[pc+2], b: body[pc+3]}, true
	case v128Load32Zero:
		return instr{fn: opV128Load32Zero, a: body[pc+2], b: body[pc+3]}, true
	case v128Load64Zero:
		return instr{fn: opV128Load64Zero, a: body[pc+2], b: body[pc+3]}, true
	case v128Load8Lane:
		return instr{fn: opV128Load8Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Load16Lane:
		return instr{fn: opV128Load16Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Load32Lane:
		return instr{fn: opV128Load32Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Load64Lane:
		return instr{fn: opV128Load64Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Store8Lane:
		return instr{fn: opV128Store8Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Store16Lane:
		return instr{fn: opV128Store16Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Store32Lane:
		return instr{fn: opV128Store32Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Store64Lane:
		return instr{fn: opV128Store64Lane, a: body[pc+2], b: body[pc+3]<<32 | body[pc+4]}, true
	case v128Const:
		return instr{fn: opV128Const, a: body[pc+1], b: body[pc+2]}, true
	case v128Load:
		return instr{fn: opV128Load, a: body[pc+2], b: body[pc+3]}, true
	case v128Store:
		return instr{fn: opV128Store, a: body[pc+2], b: body[pc+3]}, true
	case v128Bitselect:
		return instr{fn: opTernaryV128[v128BitselectOp]}, true
	case i8x16Shuffle:
		var a, b uint64
		for i := 0; i < 8; i++ {
			a |= body[pc+1+i] << (8 * i)
			b |= body[pc+1+8+i] << (8 * i)
		}
		return instr{fn: opI8x16Shuffle, a: a, b: b}, true
	default:
		return instr{}, false
	}
}

func opNop(c *callFrame, in *instr) int         { return advance }
func opUnreachable(c *callFrame, in *instr) int { c.trap = errUnreachable; return trap }
func opDrop(c *callFrame, in *instr) int        { c.vm.stack.drop(); return advance }

func opI32Const(c *callFrame, in *instr) int { c.vm.stack.pushInt32(int32(in.a)); return advance }
func opI64Const(c *callFrame, in *instr) int { c.vm.stack.pushInt64(int64(in.a)); return advance }
func opF32Const(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(math.Float32frombits(uint32(in.a)))
	return advance
}
func opF64Const(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(math.Float64frombits(in.a))
	return advance
}

func opLocalGet(c *callFrame, in *instr) int { c.vm.stack.push(c.locals[in.a]); return advance }
func opLocalSet(c *callFrame, in *instr) int { c.locals[in.a] = c.vm.stack.pop(); return advance }
func opLocalTee(c *callFrame, in *instr) int {
	c.locals[in.a] = c.vm.stack.data[len(c.vm.stack.data)-1]
	return advance
}

// Binary integer arithmetic: pop b, apply op to (top, b) in place. The op is a
// zero-size type parameter, so opBinI32[addI32] is a shared allocation-free
// handler.
type binOpI32 interface{ apply(a, b int32) int32 }

func opBinI32[O binOpI32](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt32()
	data := c.vm.stack.data
	last := len(data) - 1
	data[last] = i32(o.apply(data[last].int32(), b))
	return advance
}

type binOpI64 interface{ apply(a, b int64) int64 }

func opBinI64[O binOpI64](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt64()
	data := c.vm.stack.data
	last := len(data) - 1
	data[last] = i64(o.apply(data[last].int64(), b))
	return advance
}

type cmpOpI32 interface{ apply(a, b int32) bool }

func opCmpI32[O cmpOpI32](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt32()
	a := c.vm.stack.popInt32()
	c.vm.stack.pushInt32(boolToInt32(o.apply(a, b)))
	return advance
}

type cmpOpI64 interface{ apply(a, b int64) bool }

func opCmpI64[O cmpOpI64](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt64()
	a := c.vm.stack.popInt64()
	c.vm.stack.pushInt32(boolToInt32(o.apply(a, b)))
	return advance
}

type addI32 struct{}
type subI32 struct{}
type mulI32 struct{}
type andI32 struct{}
type orI32 struct{}
type xorI32 struct{}
type shlI32 struct{}
type shrSI32 struct{}
type shrUI32 struct{}
type rotlI32 struct{}
type rotrI32 struct{}

func (addI32) apply(a, b int32) int32  { return a + b }
func (subI32) apply(a, b int32) int32  { return a - b }
func (mulI32) apply(a, b int32) int32  { return a * b }
func (andI32) apply(a, b int32) int32  { return a & b }
func (orI32) apply(a, b int32) int32   { return a | b }
func (xorI32) apply(a, b int32) int32  { return a ^ b }
func (shlI32) apply(a, b int32) int32  { return a << (uint32(b) % 32) }
func (shrSI32) apply(a, b int32) int32 { return a >> (uint32(b) % 32) }
func (shrUI32) apply(a, b int32) int32 { return shrU32(a, b) }
func (rotlI32) apply(a, b int32) int32 { return rotl32(a, b) }
func (rotrI32) apply(a, b int32) int32 { return rotr32(a, b) }

type addI64 struct{}
type subI64 struct{}
type mulI64 struct{}
type andI64 struct{}
type orI64 struct{}
type xorI64 struct{}
type shlI64 struct{}
type shrSI64 struct{}
type shrUI64 struct{}
type rotlI64 struct{}
type rotrI64 struct{}

func (addI64) apply(a, b int64) int64  { return a + b }
func (subI64) apply(a, b int64) int64  { return a - b }
func (mulI64) apply(a, b int64) int64  { return a * b }
func (andI64) apply(a, b int64) int64  { return a & b }
func (orI64) apply(a, b int64) int64   { return a | b }
func (xorI64) apply(a, b int64) int64  { return a ^ b }
func (shlI64) apply(a, b int64) int64  { return shl64(a, b) }
func (shrSI64) apply(a, b int64) int64 { return shrS64(a, b) }
func (shrUI64) apply(a, b int64) int64 { return shrU64(a, b) }
func (rotlI64) apply(a, b int64) int64 { return rotl64(a, b) }
func (rotrI64) apply(a, b int64) int64 { return rotr64(a, b) }

type eqI32 struct{}
type neI32 struct{}
type ltSI32 struct{}
type ltUI32 struct{}
type gtSI32 struct{}
type gtUI32 struct{}
type leSI32 struct{}
type leUI32 struct{}
type geSI32 struct{}
type geUI32 struct{}

func (eqI32) apply(a, b int32) bool  { return equal(a, b) }
func (neI32) apply(a, b int32) bool  { return notEqual(a, b) }
func (ltSI32) apply(a, b int32) bool { return lessThan(a, b) }
func (ltUI32) apply(a, b int32) bool { return lessThanU32(a, b) }
func (gtSI32) apply(a, b int32) bool { return greaterThan(a, b) }
func (gtUI32) apply(a, b int32) bool { return greaterThanU32(a, b) }
func (leSI32) apply(a, b int32) bool { return lessOrEqual(a, b) }
func (leUI32) apply(a, b int32) bool { return lessOrEqualU32(a, b) }
func (geSI32) apply(a, b int32) bool { return greaterOrEqual(a, b) }
func (geUI32) apply(a, b int32) bool { return greaterOrEqualU32(a, b) }

type eqI64 struct{}
type neI64 struct{}
type ltSI64 struct{}
type ltUI64 struct{}
type gtSI64 struct{}
type gtUI64 struct{}
type leSI64 struct{}
type leUI64 struct{}
type geSI64 struct{}
type geUI64 struct{}

func (eqI64) apply(a, b int64) bool  { return equal(a, b) }
func (neI64) apply(a, b int64) bool  { return notEqual(a, b) }
func (ltSI64) apply(a, b int64) bool { return lessThan(a, b) }
func (ltUI64) apply(a, b int64) bool { return lessThanU64(a, b) }
func (gtSI64) apply(a, b int64) bool { return greaterThan(a, b) }
func (gtUI64) apply(a, b int64) bool { return greaterThanU64(a, b) }
func (leSI64) apply(a, b int64) bool { return lessOrEqual(a, b) }
func (leUI64) apply(a, b int64) bool { return lessOrEqualU64(a, b) }
func (geSI64) apply(a, b int64) bool { return greaterOrEqual(a, b) }
func (geUI64) apply(a, b int64) bool { return greaterOrEqualU64(a, b) }

// Generic handlers for the remaining operand-free families. Each is shared by
// every opcode in the family; the operation is the zero-size type parameter.

type binOpF32 interface{ apply(a, b float32) float32 }

func opBinF32[O binOpF32](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popFloat32()
	a := c.vm.stack.popFloat32()
	c.vm.stack.pushFloat32(o.apply(a, b))
	return advance
}

type binOpF64 interface{ apply(a, b float64) float64 }

func opBinF64[O binOpF64](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popFloat64()
	a := c.vm.stack.popFloat64()
	c.vm.stack.pushFloat64(o.apply(a, b))
	return advance
}

type cmpOpF32 interface{ apply(a, b float32) bool }

func opCmpF32[O cmpOpF32](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popFloat32()
	a := c.vm.stack.popFloat32()
	c.vm.stack.pushInt32(boolToInt32(o.apply(a, b)))
	return advance
}

type cmpOpF64 interface{ apply(a, b float64) bool }

func opCmpF64[O cmpOpF64](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popFloat64()
	a := c.vm.stack.popFloat64()
	c.vm.stack.pushInt32(boolToInt32(o.apply(a, b)))
	return advance
}

type safeOpI32 interface {
	apply(a, b int32) (int32, error)
}

func opSafeBinI32[O safeOpI32](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt32()
	a := c.vm.stack.popInt32()
	r, err := o.apply(a, b)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(r)
	return advance
}

type safeOpI64 interface {
	apply(a, b int64) (int64, error)
}

func opSafeBinI64[O safeOpI64](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popInt64()
	a := c.vm.stack.popInt64()
	r, err := o.apply(a, b)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(r)
	return advance
}

type usafeOpF32 interface {
	apply(a float32) (int32, error)
}

func opUnarySafeF32[O usafeOpF32](c *callFrame, in *instr) int {
	var o O
	r, err := o.apply(c.vm.stack.popFloat32())
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(r)
	return advance
}

type usafeOpF64 interface {
	apply(a float64) (int32, error)
}

func opUnarySafeF64[O usafeOpF64](c *callFrame, in *instr) int {
	var o O
	r, err := o.apply(c.vm.stack.popFloat64())
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(r)
	return advance
}

type truncOpF32 interface {
	apply(a float32) (int64, error)
}

func opTruncF32I64[O truncOpF32](c *callFrame, in *instr) int {
	var o O
	r, err := o.apply(c.vm.stack.popFloat32())
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(r)
	return advance
}

type truncOpF64 interface {
	apply(a float64) (int64, error)
}

func opTruncF64I64[O truncOpF64](c *callFrame, in *instr) int {
	var o O
	r, err := o.apply(c.vm.stack.popFloat64())
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(r)
	return advance
}

type binOpV128 interface {
	apply(a, b V128Value) V128Value
}

func opBinV128[O binOpV128](c *callFrame, in *instr) int {
	var o O
	b := c.vm.stack.popV128()
	a := c.vm.stack.popV128()
	c.vm.stack.pushV128(o.apply(a, b))
	return advance
}

type unaryOpV128 interface {
	apply(a V128Value) V128Value
}

func opUnaryV128G[O unaryOpV128](c *callFrame, in *instr) int {
	var o O
	c.vm.stack.pushV128(o.apply(c.vm.stack.popV128()))
	return advance
}

type shiftOpV128 interface {
	apply(v V128Value, shift int32) V128Value
}

func opShiftV128[O shiftOpV128](c *callFrame, in *instr) int {
	var o O
	shift := c.vm.stack.popInt32()
	v := c.vm.stack.popV128()
	c.vm.stack.pushV128(o.apply(v, shift))
	return advance
}

func opV128Const(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(V128Value{Low: in.a, High: in.b})
	return advance
}

func opV128Load(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadV128(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(identityV128(v))
	return advance
}

func opV128Store(c *callFrame, in *instr) int {
	val := c.vm.stack.popV128()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreV128(uint32(in.b), index, val); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI8x16Shuffle(c *callFrame, in *instr) int {
	a, b := in.a, in.b
	v2 := c.vm.stack.popV128()
	v1 := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdI8x16Shuffle(v1, v2,
		byte(a), byte(a>>8), byte(a>>16), byte(a>>24), byte(a>>32), byte(a>>40), byte(a>>48), byte(a>>56),
		byte(b), byte(b>>8), byte(b>>16), byte(b>>24), byte(b>>32), byte(b>>40), byte(b>>48), byte(b>>56)))
	return advance
}

type ternaryOpV128 interface {
	apply(v1, v2, v3 V128Value) V128Value
}

func opTernaryV128[O ternaryOpV128](c *callFrame, in *instr) int {
	var o O
	v3 := c.vm.stack.popV128()
	v2 := c.vm.stack.popV128()
	v1 := c.vm.stack.popV128()
	c.vm.stack.pushV128(o.apply(v1, v2, v3))
	return advance
}

type v128BitselectOp struct{}

func (v128BitselectOp) apply(v1, v2, v3 V128Value) V128Value { return simdV128Bitselect(v1, v2, v3) }

func opI8x16ExtractLaneS(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI8x16ExtractLaneS(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI8x16ExtractLaneU(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI8x16ExtractLaneU(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI16x8ExtractLaneS(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI16x8ExtractLaneS(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI16x8ExtractLaneU(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI16x8ExtractLaneU(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI32x4ExtractLane(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI32x4ExtractLane(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI64x2ExtractLane(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(simdI64x2ExtractLane(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opF32x4ExtractLane(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(simdF32x4ExtractLane(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opF64x2ExtractLane(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(simdF64x2ExtractLane(c.vm.stack.popV128(), uint32(in.a)))
	return advance
}

func opI8x16ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popInt32()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdI8x16ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opI16x8ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popInt32()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdI16x8ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opI32x4ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popInt32()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdI32x4ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opI64x2ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popInt64()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdI64x2ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opF32x4ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popFloat32()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdF32x4ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opF64x2ReplaceLane(c *callFrame, in *instr) int {
	laneValue := c.vm.stack.popFloat64()
	vector := c.vm.stack.popV128()
	c.vm.stack.pushV128(simdF64x2ReplaceLane(vector, uint32(in.a), laneValue))
	return advance
}

func opV128Load8x8S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load8x8S(data))
	return advance
}

func opV128Load8x8U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load8x8U(data))
	return advance
}

func opV128Load16x4S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load16x4S(data))
	return advance
}

func opV128Load16x4U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load16x4U(data))
	return advance
}

func opV128Load32x2S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load32x2S(data))
	return advance
}

func opV128Load32x2U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load32x2U(data))
	return advance
}

func opV128Load8Splat(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 1)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdI8x16SplatFromBytes(data))
	return advance
}

func opV128Load16Splat(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 2)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdI16x8SplatFromBytes(data))
	return advance
}

func opV128Load32Splat(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 4)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdI32x4SplatFromBytes(data))
	return advance
}

func opV128Load64Splat(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdI64x2SplatFromBytes(data))
	return advance
}

func opV128Load32Zero(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 4)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load32Zero(data))
	return advance
}

func opV128Load64Zero(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := c.vm.stack.popInt32()
	data, err := memory.Get(uint32(in.b), uint32(index), 8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdV128Load64Zero(data))
	return advance
}

func opV128Load8Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneValue, err := memory.Get(uint32(in.b>>32), uint32(index), 8/8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdLoadLane(v, uint32(in.b), laneValue))
	return advance
}

func opV128Load16Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneValue, err := memory.Get(uint32(in.b>>32), uint32(index), 16/8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdLoadLane(v, uint32(in.b), laneValue))
	return advance
}

func opV128Load32Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneValue, err := memory.Get(uint32(in.b>>32), uint32(index), 32/8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdLoadLane(v, uint32(in.b), laneValue))
	return advance
}

func opV128Load64Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneValue, err := memory.Get(uint32(in.b>>32), uint32(index), 64/8)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushV128(simdLoadLane(v, uint32(in.b), laneValue))
	return advance
}

func opV128Store8Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneIndex := uint32(in.b)
	lanesPerUint64 := uint32(64 / 8)
	shift := (laneIndex % lanesPerUint64) * 8
	var val uint64
	if laneIndex < lanesPerUint64 {
		val = v.Low >> shift
	} else {
		val = v.High >> shift
	}
	if err := memory.StoreByte(uint32(in.b>>32), uint32(index), byte(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opV128Store16Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneIndex := uint32(in.b)
	lanesPerUint64 := uint32(64 / 16)
	shift := (laneIndex % lanesPerUint64) * 16
	var val uint64
	if laneIndex < lanesPerUint64 {
		val = v.Low >> shift
	} else {
		val = v.High >> shift
	}
	if err := memory.StoreUint16(uint32(in.b>>32), uint32(index), uint16(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opV128Store32Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneIndex := uint32(in.b)
	lanesPerUint64 := uint32(64 / 32)
	shift := (laneIndex % lanesPerUint64) * 32
	var val uint64
	if laneIndex < lanesPerUint64 {
		val = v.Low >> shift
	} else {
		val = v.High >> shift
	}
	if err := memory.StoreUint32(uint32(in.b>>32), uint32(index), uint32(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opV128Store64Lane(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	v := c.vm.stack.popV128()
	index := c.vm.stack.popInt32()
	laneIndex := uint32(in.b)
	lanesPerUint64 := uint32(64 / 64)
	shift := (laneIndex % lanesPerUint64) * 64
	var val uint64
	if laneIndex < lanesPerUint64 {
		val = v.Low >> shift
	} else {
		val = v.High >> shift
	}
	if err := memory.StoreUint64(uint32(in.b>>32), uint32(index), val); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opTableGet(c *callFrame, in *instr) int {
	table := c.vm.getTable(c, in.a)
	element, err := table.Get(c.vm.stack.popInt32())
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(element)
	return advance
}

func opTableSet(c *callFrame, in *instr) int {
	table := c.vm.getTable(c, in.a)
	reference := c.vm.stack.popInt32()
	index := c.vm.stack.popInt32()
	if err := table.Set(index, reference); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opMemoryInit(c *callFrame, in *instr) int {
	data := c.vm.getData(c, in.a)
	memory := c.vm.getMemory(c, in.b)
	n, s, d := c.vm.stack.pop3Int32()
	if err := memory.Init(uint32(n), uint32(s), uint32(d), data.content); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opMemoryCopy(c *callFrame, in *instr) int {
	destMemory := c.vm.getMemory(c, in.a)
	srcMemory := c.vm.getMemory(c, in.b)
	n, s, d := c.vm.stack.pop3Int32()
	if err := srcMemory.Copy(destMemory, uint32(n), uint32(s), uint32(d)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opMemoryFill(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	n, val, offset := c.vm.stack.pop3Int32()
	if err := memory.Fill(uint32(n), uint32(offset), byte(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opDataDrop(c *callFrame, in *instr) int {
	c.vm.getData(c, in.a).content = nil
	return advance
}

func opTableInit(c *callFrame, in *instr) int {
	element := c.vm.getElement(c, in.a)
	table := c.vm.getTable(c, in.b)
	n, s, d := c.vm.stack.pop3Int32()
	if err := table.Init(n, d, s, element.functionIndexes); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opElemDrop(c *callFrame, in *instr) int {
	c.vm.getElement(c, in.a).functionIndexes = nil
	return advance
}

func opTableCopy(c *callFrame, in *instr) int {
	destTable := c.vm.getTable(c, in.a)
	srcTable := c.vm.getTable(c, in.b)
	n, s, d := c.vm.stack.pop3Int32()
	if err := srcTable.Copy(destTable, n, s, d); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opTableGrow(c *callFrame, in *instr) int {
	table := c.vm.getTable(c, in.a)
	n := c.vm.stack.popInt32()
	val := c.vm.stack.popInt32()
	c.vm.stack.pushInt32(table.Grow(n, val))
	return advance
}

func opTableSize(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(int32(c.vm.getTable(c, in.a).Size()))
	return advance
}

func opTableFill(c *callFrame, in *instr) int {
	table := c.vm.getTable(c, in.a)
	n, val, i := c.vm.stack.pop3Int32()
	if err := table.Fill(n, i, val); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI32Clz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(clz32(c.vm.stack.popInt32()))
	return advance
}

func opI32Ctz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(ctz32(c.vm.stack.popInt32()))
	return advance
}

func opI32Popcnt(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(popcnt32(c.vm.stack.popInt32()))
	return advance
}

func opI32Eqz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(c.vm.stack.popInt32() == 0))
	return advance
}

func opI64Clz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(clz64(c.vm.stack.popInt64()))
	return advance
}

func opI64Ctz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(ctz64(c.vm.stack.popInt64()))
	return advance
}

func opI64Popcnt(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(popcnt64(c.vm.stack.popInt64()))
	return advance
}

func opI64Eqz(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(c.vm.stack.popInt64() == 0))
	return advance
}

func opF32Abs(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(abs(c.vm.stack.popFloat32()))
	return advance
}

func opF32Neg(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(-c.vm.stack.popFloat32())
	return advance
}

func opF32Ceil(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(ceil(c.vm.stack.popFloat32()))
	return advance
}

func opF32Floor(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(floor(c.vm.stack.popFloat32()))
	return advance
}

func opF32Trunc(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(trunc(c.vm.stack.popFloat32()))
	return advance
}

func opF32Nearest(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(nearest(c.vm.stack.popFloat32()))
	return advance
}

func opF32Sqrt(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(sqrt(c.vm.stack.popFloat32()))
	return advance
}

func opF64Abs(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(abs(c.vm.stack.popFloat64()))
	return advance
}

func opF64Neg(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(-c.vm.stack.popFloat64())
	return advance
}

func opF64Ceil(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(ceil(c.vm.stack.popFloat64()))
	return advance
}

func opF64Floor(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(floor(c.vm.stack.popFloat64()))
	return advance
}

func opF64Trunc(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(trunc(c.vm.stack.popFloat64()))
	return advance
}

func opF64Nearest(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(nearest(c.vm.stack.popFloat64()))
	return advance
}

func opF64Sqrt(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(sqrt(c.vm.stack.popFloat64()))
	return advance
}

func opI32WrapI64(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(wrapI64ToI32(c.vm.stack.popInt64()))
	return advance
}

func opI64ExtendI32S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(extendI32SToI64(c.vm.stack.popInt32()))
	return advance
}

func opI64ExtendI32U(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(extendI32UToI64(c.vm.stack.popInt32()))
	return advance
}

func opI32Extend8S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(extend8STo32(c.vm.stack.popInt32()))
	return advance
}

func opI32Extend16S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(extend16STo32(c.vm.stack.popInt32()))
	return advance
}

func opI64Extend8S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(extend8STo64(c.vm.stack.popInt64()))
	return advance
}

func opI64Extend16S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(extend16STo64(c.vm.stack.popInt64()))
	return advance
}

func opI64Extend32S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(extend32STo64(c.vm.stack.popInt64()))
	return advance
}

func opF32ConvertI32S(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(convertI32SToF32(c.vm.stack.popInt32()))
	return advance
}

func opF32ConvertI32U(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(convertI32UToF32(c.vm.stack.popInt32()))
	return advance
}

func opF32ConvertI64S(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(convertI64SToF32(c.vm.stack.popInt64()))
	return advance
}

func opF32ConvertI64U(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(convertI64UToF32(c.vm.stack.popInt64()))
	return advance
}

func opF32DemoteF64(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(demoteF64ToF32(c.vm.stack.popFloat64()))
	return advance
}

func opF64ConvertI32S(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(convertI32SToF64(c.vm.stack.popInt32()))
	return advance
}

func opF64ConvertI32U(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(convertI32UToF64(c.vm.stack.popInt32()))
	return advance
}

func opF64ConvertI64S(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(convertI64SToF64(c.vm.stack.popInt64()))
	return advance
}

func opF64ConvertI64U(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(convertI64UToF64(c.vm.stack.popInt64()))
	return advance
}

func opF64PromoteF32(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(promoteF32ToF64(c.vm.stack.popFloat32()))
	return advance
}

func opI32ReinterpretF32(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(reinterpretF32ToI32(c.vm.stack.popFloat32()))
	return advance
}

func opI64ReinterpretF64(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(reinterpretF64ToI64(c.vm.stack.popFloat64()))
	return advance
}

func opF32ReinterpretI32(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat32(reinterpretI32ToF32(c.vm.stack.popInt32()))
	return advance
}

func opF64ReinterpretI64(c *callFrame, in *instr) int {
	c.vm.stack.pushFloat64(reinterpretI64ToF64(c.vm.stack.popInt64()))
	return advance
}

func opI32TruncSatF32S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(truncSatF32SToI32(c.vm.stack.popFloat32()))
	return advance
}

func opI32TruncSatF32U(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(truncSatF32UToI32(c.vm.stack.popFloat32()))
	return advance
}

func opI32TruncSatF64S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(truncSatF64SToI32(c.vm.stack.popFloat64()))
	return advance
}

func opI32TruncSatF64U(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(truncSatF64UToI32(c.vm.stack.popFloat64()))
	return advance
}

func opI64TruncSatF32S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(truncSatF32SToI64(c.vm.stack.popFloat32()))
	return advance
}

func opI64TruncSatF32U(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(truncSatF32UToI64(c.vm.stack.popFloat32()))
	return advance
}

func opI64TruncSatF64S(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(truncSatF64SToI64(c.vm.stack.popFloat64()))
	return advance
}

func opI64TruncSatF64U(c *callFrame, in *instr) int {
	c.vm.stack.pushInt64(truncSatF64UToI64(c.vm.stack.popFloat64()))
	return advance
}

func opI8x16Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdI8x16Splat(c.vm.stack.popInt32()))
	return advance
}

func opI16x8Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdI16x8Splat(c.vm.stack.popInt32()))
	return advance
}

func opI32x4Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdI32x4Splat(c.vm.stack.popInt32()))
	return advance
}

func opI64x2Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdI64x2Splat(c.vm.stack.popInt64()))
	return advance
}

func opF32x4Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdF32x4Splat(c.vm.stack.popFloat32()))
	return advance
}

func opF64x2Splat(c *callFrame, in *instr) int {
	c.vm.stack.pushV128(simdF64x2Splat(c.vm.stack.popFloat64()))
	return advance
}

func opV128AnyTrue(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(simdV128AnyTrue(c.vm.stack.popV128())))
	return advance
}

func opI8x16AllTrue(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(simdI8x16AllTrue(c.vm.stack.popV128())))
	return advance
}

func opI8x16Bitmask(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI8x16Bitmask(c.vm.stack.popV128()))
	return advance
}

func opI16x8AllTrue(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(simdI16x8AllTrue(c.vm.stack.popV128())))
	return advance
}

func opI16x8Bitmask(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI16x8Bitmask(c.vm.stack.popV128()))
	return advance
}

func opI32x4AllTrue(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(simdI32x4AllTrue(c.vm.stack.popV128())))
	return advance
}

func opI32x4Bitmask(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI32x4Bitmask(c.vm.stack.popV128()))
	return advance
}

func opI64x2AllTrue(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(simdI64x2AllTrue(c.vm.stack.popV128())))
	return advance
}

func opI64x2Bitmask(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(simdI64x2Bitmask(c.vm.stack.popV128()))
	return advance
}

func opBlock(c *callFrame, in *instr) int {
	c.controlStack = append(c.controlStack, controlFrame{
		targetIp:    int32(in.a),
		arity:       uint32(in.b >> 32),
		stackHeight: c.vm.stack.size() - uint32(in.b),
	})
	return advance
}

func opLoop(c *callFrame, in *instr) int {
	c.controlStack = append(c.controlStack, controlFrame{
		isLoop:      true,
		targetIp:    int32(in.a),
		arity:       uint32(in.b),
		stackHeight: c.vm.stack.size() - uint32(in.b),
	})
	return advance
}

func opIf(c *callFrame, in *instr) int {
	condition := c.vm.stack.popInt32()
	c.controlStack = append(c.controlStack, controlFrame{
		targetIp:    int32(in.a >> 32),
		arity:       uint32(in.b >> 32),
		stackHeight: c.vm.stack.size() - uint32(in.b),
	})
	if condition == 0 {
		return int(int32(in.a))
	}
	return advance
}

func opElse(c *callFrame, in *instr) int {
	target := c.controlStack[len(c.controlStack)-1].targetIp
	c.controlStack = c.controlStack[:len(c.controlStack)-1]
	return int(target)
}

func opEnd(c *callFrame, in *instr) int {
	if len(c.controlStack) > 0 {
		c.controlStack = c.controlStack[:len(c.controlStack)-1]
	}
	return advance
}

func opBr(c *callFrame, in *instr) int { return c.brToLabel(int(in.a)) }

func opBrTable(c *callFrame, in *instr) int {
	labels := c.vm.brTables[in.a]
	index := uint32(c.vm.stack.popInt32())
	if index < uint32(len(labels)) {
		return c.brToLabel(int(labels[index]))
	}
	return c.brToLabel(int(in.b))
}

func opBrIf(c *callFrame, in *instr) int {
	if c.vm.stack.popInt32() != 0 {
		return c.brToLabel(int(in.a))
	}
	return advance
}

func opReturn(c *callFrame, in *instr) int { return c.brToLabel(len(c.controlStack) - 1) }

func opCall(c *callFrame, in *instr) int {
	function := c.vm.store.funcs[c.module.funcAddrs[in.a]]
	if err := c.vm.invokeFunction(function); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opCallIndirect(c *callFrame, in *instr) int {
	expectedType := c.module.types[in.a]
	table := c.vm.getTable(c, in.b)
	elementIndex := c.vm.stack.popInt32()
	tableElement, err := table.Get(elementIndex)
	if err != nil {
		c.trap = err
		return trap
	}
	if tableElement == NullReference {
		c.trap = fmt.Errorf("uninitialized element %d", elementIndex)
		return trap
	}
	function := c.vm.store.funcs[tableElement]
	if !function.GetType().Equal(expectedType) {
		c.trap = errIndirectCallTypeMismatch
		return trap
	}
	if err := c.vm.invokeFunction(function); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opSelect(c *callFrame, in *instr) int {
	data := c.vm.stack.data
	n := len(data)
	var top value
	if data[n-1].int32() != 0 {
		top = data[n-3]
	} else {
		top = data[n-2]
	}
	data[n-3] = top
	c.vm.stack.data = data[:n-2]
	return advance
}

func opGlobalGet(c *callFrame, in *instr) int {
	c.vm.stack.push(c.vm.getGlobal(c, in.a).value)
	return advance
}

func opGlobalSet(c *callFrame, in *instr) int {
	c.vm.getGlobal(c, in.a).value = c.vm.stack.pop()
	return advance
}

func opMemorySize(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(c.vm.getMemory(c, in.a).Size())
	return advance
}

func opMemoryGrow(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	c.vm.stack.pushInt32(memory.Grow(c.vm.stack.popInt32()))
	return advance
}

func opRefIsNull(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(boolToInt32(c.vm.stack.popInt32() == NullReference))
	return advance
}

func opRefNull(c *callFrame, in *instr) int { c.vm.stack.pushInt32(NullReference); return advance }

func opRefFunc(c *callFrame, in *instr) int {
	c.vm.stack.pushInt32(int32(c.module.funcAddrs[in.a]))
	return advance
}

func opI32Load(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint32(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(uint32ToInt32(v))
	return advance
}

func opI64Load(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint64(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(uint64ToInt64(v))
	return advance
}

func opF32Load(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint32(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushFloat32(math.Float32frombits(v))
	return advance
}

func opF64Load(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint64(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushFloat64(math.Float64frombits(v))
	return advance
}

func opI32Load8S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadByte(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(signExtend8To32(v))
	return advance
}

func opI32Load8U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadByte(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(zeroExtend8To32(v))
	return advance
}

func opI32Load16S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint16(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(signExtend16To32(v))
	return advance
}

func opI32Load16U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint16(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt32(zeroExtend16To32(v))
	return advance
}

func opI64Load8S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadByte(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(signExtend8To64(v))
	return advance
}

func opI64Load8U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadByte(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(zeroExtend8To64(v))
	return advance
}

func opI64Load16S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint16(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(signExtend16To64(v))
	return advance
}

func opI64Load16U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint16(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(zeroExtend16To64(v))
	return advance
}

func opI64Load32S(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint32(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(signExtend32To64(v))
	return advance
}

func opI64Load32U(c *callFrame, in *instr) int {
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	v, err := memory.LoadUint32(uint32(in.b), index)
	if err != nil {
		c.trap = err
		return trap
	}
	c.vm.stack.pushInt64(zeroExtend32To64(v))
	return advance
}

func opI32Store(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt32()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint32(uint32(in.b), index, uint32(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI64Store(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt64()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint64(uint32(in.b), index, uint64(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opF32Store(c *callFrame, in *instr) int {
	val := c.vm.stack.popFloat32()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint32(uint32(in.b), index, math.Float32bits(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opF64Store(c *callFrame, in *instr) int {
	val := c.vm.stack.popFloat64()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint64(uint32(in.b), index, math.Float64bits(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI32Store8(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt32()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreByte(uint32(in.b), index, byte(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI32Store16(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt32()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint16(uint32(in.b), index, uint16(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI64Store8(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt64()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreByte(uint32(in.b), index, byte(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI64Store16(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt64()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint16(uint32(in.b), index, uint16(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

func opI64Store32(c *callFrame, in *instr) int {
	val := c.vm.stack.popInt64()
	memory := c.vm.getMemory(c, in.a)
	index := uint32(c.vm.stack.popInt32())
	if err := memory.StoreUint32(uint32(in.b), index, uint32(val)); err != nil {
		c.trap = err
		return trap
	}
	return advance
}

type f32EqOp struct{}

func (f32EqOp) apply(a, b float32) bool { return equal(a, b) }

type f32NeOp struct{}

func (f32NeOp) apply(a, b float32) bool { return notEqual(a, b) }

type f32LtOp struct{}

func (f32LtOp) apply(a, b float32) bool { return lessThan(a, b) }

type f32GtOp struct{}

func (f32GtOp) apply(a, b float32) bool { return greaterThan(a, b) }

type f32LeOp struct{}

func (f32LeOp) apply(a, b float32) bool { return lessOrEqual(a, b) }

type f32GeOp struct{}

func (f32GeOp) apply(a, b float32) bool { return greaterOrEqual(a, b) }

type f64EqOp struct{}

func (f64EqOp) apply(a, b float64) bool { return equal(a, b) }

type f64NeOp struct{}

func (f64NeOp) apply(a, b float64) bool { return notEqual(a, b) }

type f64LtOp struct{}

func (f64LtOp) apply(a, b float64) bool { return lessThan(a, b) }

type f64GtOp struct{}

func (f64GtOp) apply(a, b float64) bool { return greaterThan(a, b) }

type f64LeOp struct{}

func (f64LeOp) apply(a, b float64) bool { return lessOrEqual(a, b) }

type f64GeOp struct{}

func (f64GeOp) apply(a, b float64) bool { return greaterOrEqual(a, b) }

type i32DivSOp struct{}

func (i32DivSOp) apply(a, b int32) (int32, error) { return divS32(a, b) }

type i32DivUOp struct{}

func (i32DivUOp) apply(a, b int32) (int32, error) { return divU32(a, b) }

type i32RemSOp struct{}

func (i32RemSOp) apply(a, b int32) (int32, error) { return remS32(a, b) }

type i32RemUOp struct{}

func (i32RemUOp) apply(a, b int32) (int32, error) { return remU32(a, b) }

type i64DivSOp struct{}

func (i64DivSOp) apply(a, b int64) (int64, error) { return divS64(a, b) }

type i64DivUOp struct{}

func (i64DivUOp) apply(a, b int64) (int64, error) { return divU64(a, b) }

type i64RemSOp struct{}

func (i64RemSOp) apply(a, b int64) (int64, error) { return remS64(a, b) }

type i64RemUOp struct{}

func (i64RemUOp) apply(a, b int64) (int64, error) { return remU64(a, b) }

type f32AddOp struct{}

func (f32AddOp) apply(a, b float32) float32 { return add(a, b) }

type f32SubOp struct{}

func (f32SubOp) apply(a, b float32) float32 { return sub(a, b) }

type f32MulOp struct{}

func (f32MulOp) apply(a, b float32) float32 { return mul(a, b) }

type f32DivOp struct{}

func (f32DivOp) apply(a, b float32) float32 { return div(a, b) }

type f32MinOp struct{}

func (f32MinOp) apply(a, b float32) float32 { return wasmMin(a, b) }

type f32MaxOp struct{}

func (f32MaxOp) apply(a, b float32) float32 { return wasmMax(a, b) }

type f32CopysignOp struct{}

func (f32CopysignOp) apply(a, b float32) float32 { return copysign(a, b) }

type f64AddOp struct{}

func (f64AddOp) apply(a, b float64) float64 { return add(a, b) }

type f64SubOp struct{}

func (f64SubOp) apply(a, b float64) float64 { return sub(a, b) }

type f64MulOp struct{}

func (f64MulOp) apply(a, b float64) float64 { return mul(a, b) }

type f64DivOp struct{}

func (f64DivOp) apply(a, b float64) float64 { return div(a, b) }

type f64MinOp struct{}

func (f64MinOp) apply(a, b float64) float64 { return wasmMin(a, b) }

type f64MaxOp struct{}

func (f64MaxOp) apply(a, b float64) float64 { return wasmMax(a, b) }

type f64CopysignOp struct{}

func (f64CopysignOp) apply(a, b float64) float64 { return copysign(a, b) }

type i32TruncF32SOp struct{}

func (i32TruncF32SOp) apply(a float32) (int32, error) { return truncF32SToI32(a) }

type i32TruncF32UOp struct{}

func (i32TruncF32UOp) apply(a float32) (int32, error) { return truncF32UToI32(a) }

type i32TruncF64SOp struct{}

func (i32TruncF64SOp) apply(a float64) (int32, error) { return truncF64SToI32(a) }

type i32TruncF64UOp struct{}

func (i32TruncF64UOp) apply(a float64) (int32, error) { return truncF64UToI32(a) }

type i64TruncF32SOp struct{}

func (i64TruncF32SOp) apply(a float32) (int64, error) { return truncF32SToI64(a) }

type i64TruncF32UOp struct{}

func (i64TruncF32UOp) apply(a float32) (int64, error) { return truncF32UToI64(a) }

type i64TruncF64SOp struct{}

func (i64TruncF64SOp) apply(a float64) (int64, error) { return truncF64SToI64(a) }

type i64TruncF64UOp struct{}

func (i64TruncF64UOp) apply(a float64) (int64, error) { return truncF64UToI64(a) }

type i8x16SwizzleOp struct{}

func (i8x16SwizzleOp) apply(a, b V128Value) V128Value { return simdI8x16Swizzle(a, b) }

type i8x16EqOp struct{}

func (i8x16EqOp) apply(a, b V128Value) V128Value { return simdI8x16Eq(a, b) }

type i8x16NeOp struct{}

func (i8x16NeOp) apply(a, b V128Value) V128Value { return simdI8x16Ne(a, b) }

type i8x16LtSOp struct{}

func (i8x16LtSOp) apply(a, b V128Value) V128Value { return simdI8x16LtS(a, b) }

type i8x16LtUOp struct{}

func (i8x16LtUOp) apply(a, b V128Value) V128Value { return simdI8x16LtU(a, b) }

type i8x16GtSOp struct{}

func (i8x16GtSOp) apply(a, b V128Value) V128Value { return simdI8x16GtS(a, b) }

type i8x16GtUOp struct{}

func (i8x16GtUOp) apply(a, b V128Value) V128Value { return simdI8x16GtU(a, b) }

type i8x16LeSOp struct{}

func (i8x16LeSOp) apply(a, b V128Value) V128Value { return simdI8x16LeS(a, b) }

type i8x16LeUOp struct{}

func (i8x16LeUOp) apply(a, b V128Value) V128Value { return simdI8x16LeU(a, b) }

type i8x16GeSOp struct{}

func (i8x16GeSOp) apply(a, b V128Value) V128Value { return simdI8x16GeS(a, b) }

type i8x16GeUOp struct{}

func (i8x16GeUOp) apply(a, b V128Value) V128Value { return simdI8x16GeU(a, b) }

type i16x8EqOp struct{}

func (i16x8EqOp) apply(a, b V128Value) V128Value { return simdI16x8Eq(a, b) }

type i16x8NeOp struct{}

func (i16x8NeOp) apply(a, b V128Value) V128Value { return simdI16x8Ne(a, b) }

type i16x8LtSOp struct{}

func (i16x8LtSOp) apply(a, b V128Value) V128Value { return simdI16x8LtS(a, b) }

type i16x8LtUOp struct{}

func (i16x8LtUOp) apply(a, b V128Value) V128Value { return simdI16x8LtU(a, b) }

type i16x8GtSOp struct{}

func (i16x8GtSOp) apply(a, b V128Value) V128Value { return simdI16x8GtS(a, b) }

type i16x8GtUOp struct{}

func (i16x8GtUOp) apply(a, b V128Value) V128Value { return simdI16x8GtU(a, b) }

type i16x8LeSOp struct{}

func (i16x8LeSOp) apply(a, b V128Value) V128Value { return simdI16x8LeS(a, b) }

type i16x8LeUOp struct{}

func (i16x8LeUOp) apply(a, b V128Value) V128Value { return simdI16x8LeU(a, b) }

type i16x8GeSOp struct{}

func (i16x8GeSOp) apply(a, b V128Value) V128Value { return simdI16x8GeS(a, b) }

type i16x8GeUOp struct{}

func (i16x8GeUOp) apply(a, b V128Value) V128Value { return simdI16x8GeU(a, b) }

type i32x4EqOp struct{}

func (i32x4EqOp) apply(a, b V128Value) V128Value { return simdI32x4Eq(a, b) }

type i32x4NeOp struct{}

func (i32x4NeOp) apply(a, b V128Value) V128Value { return simdI32x4Ne(a, b) }

type i32x4LtSOp struct{}

func (i32x4LtSOp) apply(a, b V128Value) V128Value { return simdI32x4LtS(a, b) }

type i32x4LtUOp struct{}

func (i32x4LtUOp) apply(a, b V128Value) V128Value { return simdI32x4LtU(a, b) }

type i32x4GtSOp struct{}

func (i32x4GtSOp) apply(a, b V128Value) V128Value { return simdI32x4GtS(a, b) }

type i32x4GtUOp struct{}

func (i32x4GtUOp) apply(a, b V128Value) V128Value { return simdI32x4GtU(a, b) }

type i32x4LeSOp struct{}

func (i32x4LeSOp) apply(a, b V128Value) V128Value { return simdI32x4LeS(a, b) }

type i32x4LeUOp struct{}

func (i32x4LeUOp) apply(a, b V128Value) V128Value { return simdI32x4LeU(a, b) }

type i32x4GeSOp struct{}

func (i32x4GeSOp) apply(a, b V128Value) V128Value { return simdI32x4GeS(a, b) }

type i32x4GeUOp struct{}

func (i32x4GeUOp) apply(a, b V128Value) V128Value { return simdI32x4GeU(a, b) }

type f32x4EqOp struct{}

func (f32x4EqOp) apply(a, b V128Value) V128Value { return simdF32x4Eq(a, b) }

type f32x4NeOp struct{}

func (f32x4NeOp) apply(a, b V128Value) V128Value { return simdF32x4Ne(a, b) }

type f32x4LtOp struct{}

func (f32x4LtOp) apply(a, b V128Value) V128Value { return simdF32x4Lt(a, b) }

type f32x4GtOp struct{}

func (f32x4GtOp) apply(a, b V128Value) V128Value { return simdF32x4Gt(a, b) }

type f32x4LeOp struct{}

func (f32x4LeOp) apply(a, b V128Value) V128Value { return simdF32x4Le(a, b) }

type f32x4GeOp struct{}

func (f32x4GeOp) apply(a, b V128Value) V128Value { return simdF32x4Ge(a, b) }

type f64x2EqOp struct{}

func (f64x2EqOp) apply(a, b V128Value) V128Value { return simdF64x2Eq(a, b) }

type f64x2NeOp struct{}

func (f64x2NeOp) apply(a, b V128Value) V128Value { return simdF64x2Ne(a, b) }

type f64x2LtOp struct{}

func (f64x2LtOp) apply(a, b V128Value) V128Value { return simdF64x2Lt(a, b) }

type f64x2GtOp struct{}

func (f64x2GtOp) apply(a, b V128Value) V128Value { return simdF64x2Gt(a, b) }

type f64x2LeOp struct{}

func (f64x2LeOp) apply(a, b V128Value) V128Value { return simdF64x2Le(a, b) }

type f64x2GeOp struct{}

func (f64x2GeOp) apply(a, b V128Value) V128Value { return simdF64x2Ge(a, b) }

type v128NotOp struct{}

func (v128NotOp) apply(a V128Value) V128Value { return simdV128Not(a) }

type v128AndOp struct{}

func (v128AndOp) apply(a, b V128Value) V128Value { return simdV128And(a, b) }

type v128AndnotOp struct{}

func (v128AndnotOp) apply(a, b V128Value) V128Value { return simdV128Andnot(a, b) }

type v128OrOp struct{}

func (v128OrOp) apply(a, b V128Value) V128Value { return simdV128Or(a, b) }

type v128XorOp struct{}

func (v128XorOp) apply(a, b V128Value) V128Value { return simdV128Xor(a, b) }

type f32x4DemoteF64x2ZeroOp struct{}

func (f32x4DemoteF64x2ZeroOp) apply(a V128Value) V128Value { return simdF32x4DemoteF64x2Zero(a) }

type f64x2PromoteLowF32x4Op struct{}

func (f64x2PromoteLowF32x4Op) apply(a V128Value) V128Value { return simdF64x2PromoteLowF32x4(a) }

type i8x16AbsOp struct{}

func (i8x16AbsOp) apply(a V128Value) V128Value { return simdI8x16Abs(a) }

type i8x16NegOp struct{}

func (i8x16NegOp) apply(a V128Value) V128Value { return simdI8x16Neg(a) }

type i8x16PopcntOp struct{}

func (i8x16PopcntOp) apply(a V128Value) V128Value { return simdI8x16Popcnt(a) }

type i8x16NarrowI16x8SOp struct{}

func (i8x16NarrowI16x8SOp) apply(a, b V128Value) V128Value { return simdI8x16NarrowI16x8S(a, b) }

type i8x16NarrowI16x8UOp struct{}

func (i8x16NarrowI16x8UOp) apply(a, b V128Value) V128Value { return simdI8x16NarrowI16x8U(a, b) }

type f32x4CeilOp struct{}

func (f32x4CeilOp) apply(a V128Value) V128Value { return simdF32x4Ceil(a) }

type f32x4FloorOp struct{}

func (f32x4FloorOp) apply(a V128Value) V128Value { return simdF32x4Floor(a) }

type f32x4TruncOp struct{}

func (f32x4TruncOp) apply(a V128Value) V128Value { return simdF32x4Trunc(a) }

type f32x4NearestOp struct{}

func (f32x4NearestOp) apply(a V128Value) V128Value { return simdF32x4Nearest(a) }

type i8x16ShlOp struct{}

func (i8x16ShlOp) apply(v V128Value, shift int32) V128Value { return simdI8x16Shl(v, shift) }

type i8x16ShrUOp struct{}

func (i8x16ShrUOp) apply(v V128Value, shift int32) V128Value { return simdI8x16ShrU(v, shift) }

type i8x16ShrSOp struct{}

func (i8x16ShrSOp) apply(v V128Value, shift int32) V128Value { return simdI8x16ShrS(v, shift) }

type i8x16AddOp struct{}

func (i8x16AddOp) apply(a, b V128Value) V128Value { return simdI8x16Add(a, b) }

type i8x16AddSatSOp struct{}

func (i8x16AddSatSOp) apply(a, b V128Value) V128Value { return simdI8x16AddSatS(a, b) }

type i8x16AddSatUOp struct{}

func (i8x16AddSatUOp) apply(a, b V128Value) V128Value { return simdI8x16AddSatU(a, b) }

type i8x16SubOp struct{}

func (i8x16SubOp) apply(a, b V128Value) V128Value { return simdI8x16Sub(a, b) }

type i8x16SubSatSOp struct{}

func (i8x16SubSatSOp) apply(a, b V128Value) V128Value { return simdI8x16SubSatS(a, b) }

type i8x16SubSatUOp struct{}

func (i8x16SubSatUOp) apply(a, b V128Value) V128Value { return simdI8x16SubSatU(a, b) }

type f64x2CeilOp struct{}

func (f64x2CeilOp) apply(a V128Value) V128Value { return simdF64x2Ceil(a) }

type f64x2FloorOp struct{}

func (f64x2FloorOp) apply(a V128Value) V128Value { return simdF64x2Floor(a) }

type i8x16MinSOp struct{}

func (i8x16MinSOp) apply(a, b V128Value) V128Value { return simdI8x16MinS(a, b) }

type i8x16MinUOp struct{}

func (i8x16MinUOp) apply(a, b V128Value) V128Value { return simdI8x16MinU(a, b) }

type i8x16MaxSOp struct{}

func (i8x16MaxSOp) apply(a, b V128Value) V128Value { return simdI8x16MaxS(a, b) }

type i8x16MaxUOp struct{}

func (i8x16MaxUOp) apply(a, b V128Value) V128Value { return simdI8x16MaxU(a, b) }

type f64x2TruncOp struct{}

func (f64x2TruncOp) apply(a V128Value) V128Value { return simdF64x2Trunc(a) }

type i8x16AvgrUOp struct{}

func (i8x16AvgrUOp) apply(a, b V128Value) V128Value { return simdI8x16AvgrU(a, b) }

type i16x8ExtaddPairwiseI8x16SOp struct{}

func (i16x8ExtaddPairwiseI8x16SOp) apply(a V128Value) V128Value {
	return simdI16x8ExtaddPairwiseI8x16S(a)
}

type i16x8ExtaddPairwiseI8x16UOp struct{}

func (i16x8ExtaddPairwiseI8x16UOp) apply(a V128Value) V128Value {
	return simdI16x8ExtaddPairwiseI8x16U(a)
}

type i32x4ExtaddPairwiseI16x8SOp struct{}

func (i32x4ExtaddPairwiseI16x8SOp) apply(a V128Value) V128Value {
	return simdI32x4ExtaddPairwiseI16x8S(a)
}

type i32x4ExtaddPairwiseI16x8UOp struct{}

func (i32x4ExtaddPairwiseI16x8UOp) apply(a V128Value) V128Value {
	return simdI32x4ExtaddPairwiseI16x8U(a)
}

type i16x8AbsOp struct{}

func (i16x8AbsOp) apply(a V128Value) V128Value { return simdI16x8Abs(a) }

type i16x8NegOp struct{}

func (i16x8NegOp) apply(a V128Value) V128Value { return simdI16x8Neg(a) }

type i16x8Q15mulrSatSOp struct{}

func (i16x8Q15mulrSatSOp) apply(a, b V128Value) V128Value { return simdI16x8Q15mulrSatS(a, b) }

type i16x8NarrowI32x4SOp struct{}

func (i16x8NarrowI32x4SOp) apply(a, b V128Value) V128Value { return simdI16x8NarrowI32x4S(a, b) }

type i16x8NarrowI32x4UOp struct{}

func (i16x8NarrowI32x4UOp) apply(a, b V128Value) V128Value { return simdI16x8NarrowI32x4U(a, b) }

type i16x8ExtendLowI8x16SOp struct{}

func (i16x8ExtendLowI8x16SOp) apply(a V128Value) V128Value { return simdI16x8ExtendLowI8x16S(a) }

type i16x8ExtendHighI8x16SOp struct{}

func (i16x8ExtendHighI8x16SOp) apply(a V128Value) V128Value { return simdI16x8ExtendHighI8x16S(a) }

type i16x8ExtendLowI8x16UOp struct{}

func (i16x8ExtendLowI8x16UOp) apply(a V128Value) V128Value { return simdI16x8ExtendLowI8x16U(a) }

type i16x8ExtendHighI8x16UOp struct{}

func (i16x8ExtendHighI8x16UOp) apply(a V128Value) V128Value { return simdI16x8ExtendHighI8x16U(a) }

type i16x8ShlOp struct{}

func (i16x8ShlOp) apply(v V128Value, shift int32) V128Value { return simdI16x8Shl(v, shift) }

type i16x8ShrSOp struct{}

func (i16x8ShrSOp) apply(v V128Value, shift int32) V128Value { return simdI16x8ShrS(v, shift) }

type i16x8ShrUOp struct{}

func (i16x8ShrUOp) apply(v V128Value, shift int32) V128Value { return simdI16x8ShrU(v, shift) }

type i16x8AddOp struct{}

func (i16x8AddOp) apply(a, b V128Value) V128Value { return simdI16x8Add(a, b) }

type i16x8AddSatSOp struct{}

func (i16x8AddSatSOp) apply(a, b V128Value) V128Value { return simdI16x8AddSatS(a, b) }

type i16x8AddSatUOp struct{}

func (i16x8AddSatUOp) apply(a, b V128Value) V128Value { return simdI16x8AddSatU(a, b) }

type i16x8SubOp struct{}

func (i16x8SubOp) apply(a, b V128Value) V128Value { return simdI16x8Sub(a, b) }

type i16x8SubSatSOp struct{}

func (i16x8SubSatSOp) apply(a, b V128Value) V128Value { return simdI16x8SubSatS(a, b) }

type i16x8SubSatUOp struct{}

func (i16x8SubSatUOp) apply(a, b V128Value) V128Value { return simdI16x8SubSatU(a, b) }

type f64x2NearestOp struct{}

func (f64x2NearestOp) apply(a V128Value) V128Value { return simdF64x2Nearest(a) }

type i16x8MulOp struct{}

func (i16x8MulOp) apply(a, b V128Value) V128Value { return simdI16x8Mul(a, b) }

type i16x8MinSOp struct{}

func (i16x8MinSOp) apply(a, b V128Value) V128Value { return simdI16x8MinS(a, b) }

type i16x8MinUOp struct{}

func (i16x8MinUOp) apply(a, b V128Value) V128Value { return simdI16x8MinU(a, b) }

type i16x8MaxSOp struct{}

func (i16x8MaxSOp) apply(a, b V128Value) V128Value { return simdI16x8MaxS(a, b) }

type i16x8MaxUOp struct{}

func (i16x8MaxUOp) apply(a, b V128Value) V128Value { return simdI16x8MaxU(a, b) }

type i16x8AvgrUOp struct{}

func (i16x8AvgrUOp) apply(a, b V128Value) V128Value { return simdI16x8AvgrU(a, b) }

type i16x8ExtmulLowI8x16SOp struct{}

func (i16x8ExtmulLowI8x16SOp) apply(a, b V128Value) V128Value { return simdI16x8ExtmulLowI8x16S(a, b) }

type i16x8ExtmulHighI8x16SOp struct{}

func (i16x8ExtmulHighI8x16SOp) apply(a, b V128Value) V128Value {
	return simdI16x8ExtmulHighI8x16S(a, b)
}

type i16x8ExtmulLowI8x16UOp struct{}

func (i16x8ExtmulLowI8x16UOp) apply(a, b V128Value) V128Value { return simdI16x8ExtmulLowI8x16U(a, b) }

type i16x8ExtmulHighI8x16UOp struct{}

func (i16x8ExtmulHighI8x16UOp) apply(a, b V128Value) V128Value {
	return simdI16x8ExtmulHighI8x16U(a, b)
}

type i32x4AbsOp struct{}

func (i32x4AbsOp) apply(a V128Value) V128Value { return simdI32x4Abs(a) }

type i32x4NegOp struct{}

func (i32x4NegOp) apply(a V128Value) V128Value { return simdI32x4Neg(a) }

type i32x4ExtendLowI16x8SOp struct{}

func (i32x4ExtendLowI16x8SOp) apply(a V128Value) V128Value { return simdI32x4ExtendLowI16x8S(a) }

type i32x4ExtendHighI16x8SOp struct{}

func (i32x4ExtendHighI16x8SOp) apply(a V128Value) V128Value { return simdI32x4ExtendHighI16x8S(a) }

type i32x4ExtendLowI16x8UOp struct{}

func (i32x4ExtendLowI16x8UOp) apply(a V128Value) V128Value { return simdI32x4ExtendLowI16x8U(a) }

type i32x4ExtendHighI16x8UOp struct{}

func (i32x4ExtendHighI16x8UOp) apply(a V128Value) V128Value { return simdI32x4ExtendHighI16x8U(a) }

type i32x4ShlOp struct{}

func (i32x4ShlOp) apply(v V128Value, shift int32) V128Value { return simdI32x4Shl(v, shift) }

type i32x4ShrSOp struct{}

func (i32x4ShrSOp) apply(v V128Value, shift int32) V128Value { return simdI32x4ShrS(v, shift) }

type i32x4ShrUOp struct{}

func (i32x4ShrUOp) apply(v V128Value, shift int32) V128Value { return simdI32x4ShrU(v, shift) }

type i32x4AddOp struct{}

func (i32x4AddOp) apply(a, b V128Value) V128Value { return simdI32x4Add(a, b) }

type i32x4SubOp struct{}

func (i32x4SubOp) apply(a, b V128Value) V128Value { return simdI32x4Sub(a, b) }

type i32x4MulOp struct{}

func (i32x4MulOp) apply(a, b V128Value) V128Value { return simdI32x4Mul(a, b) }

type i32x4MinSOp struct{}

func (i32x4MinSOp) apply(a, b V128Value) V128Value { return simdI32x4MinS(a, b) }

type i32x4MinUOp struct{}

func (i32x4MinUOp) apply(a, b V128Value) V128Value { return simdI32x4MinU(a, b) }

type i32x4MaxSOp struct{}

func (i32x4MaxSOp) apply(a, b V128Value) V128Value { return simdI32x4MaxS(a, b) }

type i32x4MaxUOp struct{}

func (i32x4MaxUOp) apply(a, b V128Value) V128Value { return simdI32x4MaxU(a, b) }

type i32x4DotI16x8SOp struct{}

func (i32x4DotI16x8SOp) apply(a, b V128Value) V128Value { return simdI32x4DotI16x8S(a, b) }

type i32x4ExtmulLowI16x8SOp struct{}

func (i32x4ExtmulLowI16x8SOp) apply(a, b V128Value) V128Value { return simdI32x4ExtmulLowI16x8S(a, b) }

type i32x4ExtmulHighI16x8SOp struct{}

func (i32x4ExtmulHighI16x8SOp) apply(a, b V128Value) V128Value {
	return simdI32x4ExtmulHighI16x8S(a, b)
}

type i32x4ExtmulLowI16x8UOp struct{}

func (i32x4ExtmulLowI16x8UOp) apply(a, b V128Value) V128Value { return simdI32x4ExtmulLowI16x8U(a, b) }

type i32x4ExtmulHighI16x8UOp struct{}

func (i32x4ExtmulHighI16x8UOp) apply(a, b V128Value) V128Value {
	return simdI32x4ExtmulHighI16x8U(a, b)
}

type i64x2AbsOp struct{}

func (i64x2AbsOp) apply(a V128Value) V128Value { return simdI64x2Abs(a) }

type i64x2NegOp struct{}

func (i64x2NegOp) apply(a V128Value) V128Value { return simdI64x2Neg(a) }

type i64x2ExtendLowI32x4SOp struct{}

func (i64x2ExtendLowI32x4SOp) apply(a V128Value) V128Value { return simdI64x2ExtendLowI32x4S(a) }

type i64x2ExtendHighI32x4SOp struct{}

func (i64x2ExtendHighI32x4SOp) apply(a V128Value) V128Value { return simdI64x2ExtendHighI32x4S(a) }

type i64x2ExtendLowI32x4UOp struct{}

func (i64x2ExtendLowI32x4UOp) apply(a V128Value) V128Value { return simdI64x2ExtendLowI32x4U(a) }

type i64x2ExtendHighI32x4UOp struct{}

func (i64x2ExtendHighI32x4UOp) apply(a V128Value) V128Value { return simdI64x2ExtendHighI32x4U(a) }

type i64x2ShlOp struct{}

func (i64x2ShlOp) apply(v V128Value, shift int32) V128Value { return simdI64x2Shl(v, shift) }

type i64x2ShrSOp struct{}

func (i64x2ShrSOp) apply(v V128Value, shift int32) V128Value { return simdI64x2ShrS(v, shift) }

type i64x2ShrUOp struct{}

func (i64x2ShrUOp) apply(v V128Value, shift int32) V128Value { return simdI64x2ShrU(v, shift) }

type i64x2AddOp struct{}

func (i64x2AddOp) apply(a, b V128Value) V128Value { return simdI64x2Add(a, b) }

type i64x2SubOp struct{}

func (i64x2SubOp) apply(a, b V128Value) V128Value { return simdI64x2Sub(a, b) }

type i64x2MulOp struct{}

func (i64x2MulOp) apply(a, b V128Value) V128Value { return simdI64x2Mul(a, b) }

type i64x2EqOp struct{}

func (i64x2EqOp) apply(a, b V128Value) V128Value { return simdI64x2Eq(a, b) }

type i64x2NeOp struct{}

func (i64x2NeOp) apply(a, b V128Value) V128Value { return simdI64x2Ne(a, b) }

type i64x2LtSOp struct{}

func (i64x2LtSOp) apply(a, b V128Value) V128Value { return simdI64x2LtS(a, b) }

type i64x2GtSOp struct{}

func (i64x2GtSOp) apply(a, b V128Value) V128Value { return simdI64x2GtS(a, b) }

type i64x2LeSOp struct{}

func (i64x2LeSOp) apply(a, b V128Value) V128Value { return simdI64x2LeS(a, b) }

type i64x2GeSOp struct{}

func (i64x2GeSOp) apply(a, b V128Value) V128Value { return simdI64x2GeS(a, b) }

type i64x2ExtmulLowI32x4SOp struct{}

func (i64x2ExtmulLowI32x4SOp) apply(a, b V128Value) V128Value { return simdI64x2ExtmulLowI32x4S(a, b) }

type i64x2ExtmulHighI32x4SOp struct{}

func (i64x2ExtmulHighI32x4SOp) apply(a, b V128Value) V128Value {
	return simdI64x2ExtmulHighI32x4S(a, b)
}

type i64x2ExtmulLowI32x4UOp struct{}

func (i64x2ExtmulLowI32x4UOp) apply(a, b V128Value) V128Value { return simdI64x2ExtmulLowI32x4U(a, b) }

type i64x2ExtmulHighI32x4UOp struct{}

func (i64x2ExtmulHighI32x4UOp) apply(a, b V128Value) V128Value {
	return simdI64x2ExtmulHighI32x4U(a, b)
}

type f32x4AbsOp struct{}

func (f32x4AbsOp) apply(a V128Value) V128Value { return simdF32x4Abs(a) }

type f32x4NegOp struct{}

func (f32x4NegOp) apply(a V128Value) V128Value { return simdF32x4Neg(a) }

type f32x4SqrtOp struct{}

func (f32x4SqrtOp) apply(a V128Value) V128Value { return simdF32x4Sqrt(a) }

type f32x4AddOp struct{}

func (f32x4AddOp) apply(a, b V128Value) V128Value { return simdF32x4Add(a, b) }

type f32x4SubOp struct{}

func (f32x4SubOp) apply(a, b V128Value) V128Value { return simdF32x4Sub(a, b) }

type f32x4MulOp struct{}

func (f32x4MulOp) apply(a, b V128Value) V128Value { return simdF32x4Mul(a, b) }

type f32x4DivOp struct{}

func (f32x4DivOp) apply(a, b V128Value) V128Value { return simdF32x4Div(a, b) }

type f32x4MinOp struct{}

func (f32x4MinOp) apply(a, b V128Value) V128Value { return simdF32x4Min(a, b) }

type f32x4MaxOp struct{}

func (f32x4MaxOp) apply(a, b V128Value) V128Value { return simdF32x4Max(a, b) }

type f32x4PminOp struct{}

func (f32x4PminOp) apply(a, b V128Value) V128Value { return simdF32x4Pmin(a, b) }

type f32x4PmaxOp struct{}

func (f32x4PmaxOp) apply(a, b V128Value) V128Value { return simdF32x4Pmax(a, b) }

type f64x2AbsOp struct{}

func (f64x2AbsOp) apply(a V128Value) V128Value { return simdF64x2Abs(a) }

type f64x2NegOp struct{}

func (f64x2NegOp) apply(a V128Value) V128Value { return simdF64x2Neg(a) }

type f64x2SqrtOp struct{}

func (f64x2SqrtOp) apply(a V128Value) V128Value { return simdF64x2Sqrt(a) }

type f64x2AddOp struct{}

func (f64x2AddOp) apply(a, b V128Value) V128Value { return simdF64x2Add(a, b) }

type f64x2SubOp struct{}

func (f64x2SubOp) apply(a, b V128Value) V128Value { return simdF64x2Sub(a, b) }

type f64x2MulOp struct{}

func (f64x2MulOp) apply(a, b V128Value) V128Value { return simdF64x2Mul(a, b) }

type f64x2DivOp struct{}

func (f64x2DivOp) apply(a, b V128Value) V128Value { return simdF64x2Div(a, b) }

type f64x2MinOp struct{}

func (f64x2MinOp) apply(a, b V128Value) V128Value { return simdF64x2Min(a, b) }

type f64x2MaxOp struct{}

func (f64x2MaxOp) apply(a, b V128Value) V128Value { return simdF64x2Max(a, b) }

type f64x2PminOp struct{}

func (f64x2PminOp) apply(a, b V128Value) V128Value { return simdF64x2Pmin(a, b) }

type f64x2PmaxOp struct{}

func (f64x2PmaxOp) apply(a, b V128Value) V128Value { return simdF64x2Pmax(a, b) }

type i32x4TruncSatF32x4SOp struct{}

func (i32x4TruncSatF32x4SOp) apply(a V128Value) V128Value { return simdI32x4TruncSatF32x4S(a) }

type i32x4TruncSatF32x4UOp struct{}

func (i32x4TruncSatF32x4UOp) apply(a V128Value) V128Value { return simdI32x4TruncSatF32x4U(a) }

type f32x4ConvertI32x4SOp struct{}

func (f32x4ConvertI32x4SOp) apply(a V128Value) V128Value { return simdF32x4ConvertI32x4S(a) }

type f32x4ConvertI32x4UOp struct{}

func (f32x4ConvertI32x4UOp) apply(a V128Value) V128Value { return simdF32x4ConvertI32x4U(a) }

type i32x4TruncSatF64x2SZeroOp struct{}

func (i32x4TruncSatF64x2SZeroOp) apply(a V128Value) V128Value { return simdI32x4TruncSatF64x2SZero(a) }

type i32x4TruncSatF64x2UZeroOp struct{}

func (i32x4TruncSatF64x2UZeroOp) apply(a V128Value) V128Value { return simdI32x4TruncSatF64x2UZero(a) }

type f64x2ConvertLowI32x4SOp struct{}

func (f64x2ConvertLowI32x4SOp) apply(a V128Value) V128Value { return simdF64x2ConvertLowI32x4S(a) }

type f64x2ConvertLowI32x4UOp struct{}

func (f64x2ConvertLowI32x4UOp) apply(a V128Value) V128Value { return simdF64x2ConvertLowI32x4U(a) }

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
