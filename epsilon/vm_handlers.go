// Copyright 2026 Google LLC
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

import "math"

// This file holds the per-op opcode handlers called by the dispatch switch in
// runLoop: one concrete function per opcode, taking no func-valued parameters
// (those would reintroduce indirect calls on the hot path). Their cost keeps
// them outlined from runLoop while their stack pops and pushes inline fully
// inside them. Which ops live here versus as inline case bodies in the switch
// is benchmark-driven; do not convert an op between the two shapes without
// measuring.

// The load/store handlers below repeat the memarg decoding inline rather than
// sharing a helper: a shared decoder exceeds the inlining budget (the embedded
// stack pop alone is most of it), and an extra call per memory instruction is
// measurable in the dispatch hot path.
func (vm *vm) handleI32Load(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint32(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(int32(v))
	return nil
}

func (vm *vm) handleI64Load(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint64(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(v))
	return nil
}

func (vm *vm) handleF32Load(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint32(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushFloat32(math.Float32frombits(v))
	return nil
}

func (vm *vm) handleF64Load(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint64(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushFloat64(math.Float64frombits(v))
	return nil
}

func (vm *vm) handleI32Load8S(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadByte(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(int32(int8(v)))
	return nil
}

func (vm *vm) handleI32Load8U(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadByte(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(int32(v))
	return nil
}

func (vm *vm) handleI32Load16S(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint16(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(int32(int16(v)))
	return nil
}

func (vm *vm) handleI32Load16U(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint16(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(int32(v))
	return nil
}

func (vm *vm) handleI64Load8S(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadByte(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(int8(v)))
	return nil
}

func (vm *vm) handleI64Load8U(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadByte(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(v))
	return nil
}

func (vm *vm) handleI64Load16S(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint16(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(int16(v)))
	return nil
}

func (vm *vm) handleI64Load16U(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint16(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(v))
	return nil
}

func (vm *vm) handleI64Load32S(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint32(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(int32(v)))
	return nil
}

func (vm *vm) handleI64Load32U(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadUint32(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(int64(v))
	return nil
}

func (vm *vm) handleI32Store(frame *callFrame) error {
	val := uint32(vm.stack.popInt32())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint32(offset, index, val)
}

func (vm *vm) handleI64Store(frame *callFrame) error {
	val := uint64(vm.stack.popInt64())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint64(offset, index, val)
}

func (vm *vm) handleF32Store(frame *callFrame) error {
	val := math.Float32bits(vm.stack.popFloat32())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint32(offset, index, val)
}

func (vm *vm) handleF64Store(frame *callFrame) error {
	val := math.Float64bits(vm.stack.popFloat64())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint64(offset, index, val)
}

func (vm *vm) handleI32Store8(frame *callFrame) error {
	val := byte(vm.stack.popInt32())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreByte(offset, index, val)
}

func (vm *vm) handleI32Store16(frame *callFrame) error {
	val := uint16(vm.stack.popInt32())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint16(offset, index, val)
}

func (vm *vm) handleI64Store8(frame *callFrame) error {
	val := byte(vm.stack.popInt64())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreByte(offset, index, val)
}

func (vm *vm) handleI64Store16(frame *callFrame) error {
	val := uint16(vm.stack.popInt64())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint16(offset, index, val)
}

func (vm *vm) handleI64Store32(frame *callFrame) error {
	val := uint32(vm.stack.popInt64())
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreUint32(offset, index, val)
}

func (vm *vm) handleF32Eq() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a == b))
}

func (vm *vm) handleF32Ne() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a != b))
}

func (vm *vm) handleF32Lt() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a < b))
}

func (vm *vm) handleF32Gt() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a > b))
}

func (vm *vm) handleF32Le() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a <= b))
}

func (vm *vm) handleF32Ge() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushInt32(boolToInt32(a >= b))
}

func (vm *vm) handleF64Eq() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a == b))
}

func (vm *vm) handleF64Ne() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a != b))
}

func (vm *vm) handleF64Lt() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a < b))
}

func (vm *vm) handleF64Gt() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a > b))
}

func (vm *vm) handleF64Le() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a <= b))
}

func (vm *vm) handleF64Ge() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushInt32(boolToInt32(a >= b))
}

func (vm *vm) handleI32DivS() error {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	res, err := divS32(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32DivU() error {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	res, err := divU32(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32RemS() error {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	res, err := remS32(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32RemU() error {
	b := vm.stack.popInt32()
	a := vm.stack.popInt32()
	res, err := remU32(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI64DivS() error {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	res, err := divS64(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64DivU() error {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	res, err := divU64(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64RemS() error {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	res, err := remS64(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64RemU() error {
	b := vm.stack.popInt64()
	a := vm.stack.popInt64()
	res, err := remU64(a, b)
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleF32Add() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(a + b)
}

func (vm *vm) handleF32Sub() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(a - b)
}

func (vm *vm) handleF32Mul() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(a * b)
}

func (vm *vm) handleF32Div() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(a / b)
}

func (vm *vm) handleF32Min() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(min(a, b))
}

func (vm *vm) handleF32Max() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(max(a, b))
}

func (vm *vm) handleF32Copysign() {
	b := vm.stack.popFloat32()
	a := vm.stack.popFloat32()
	vm.stack.pushFloat32(copysign(a, b))
}

func (vm *vm) handleF64Add() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(a + b)
}

func (vm *vm) handleF64Sub() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(a - b)
}

func (vm *vm) handleF64Mul() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(a * b)
}

func (vm *vm) handleF64Div() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(a / b)
}

func (vm *vm) handleF64Min() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(min(a, b))
}

func (vm *vm) handleF64Max() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(max(a, b))
}

func (vm *vm) handleF64Copysign() {
	b := vm.stack.popFloat64()
	a := vm.stack.popFloat64()
	vm.stack.pushFloat64(copysign(a, b))
}

func (vm *vm) handleI32TruncF32S() error {
	res, err := truncF32SToI32(vm.stack.popFloat32())
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32TruncF32U() error {
	res, err := truncF32UToI32(vm.stack.popFloat32())
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32TruncF64S() error {
	res, err := truncF64SToI32(vm.stack.popFloat64())
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI32TruncF64U() error {
	res, err := truncF64UToI32(vm.stack.popFloat64())
	if err != nil {
		return err
	}
	vm.stack.pushInt32(res)
	return nil
}

func (vm *vm) handleI64TruncF32S() error {
	res, err := truncF32SToI64(vm.stack.popFloat32())
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64TruncF32U() error {
	res, err := truncF32UToI64(vm.stack.popFloat32())
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64TruncF64S() error {
	res, err := truncF64SToI64(vm.stack.popFloat64())
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI64TruncF64U() error {
	res, err := truncF64UToI64(vm.stack.popFloat64())
	if err != nil {
		return err
	}
	vm.stack.pushInt64(res)
	return nil
}

func (vm *vm) handleI8x16Shuffle(frame *callFrame) {
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()

	body := frame.function.body
	pc := frame.pc
	vm.stack.pushV128(simdI8x16Shuffle(v1, v2,
		byte(body[pc]), byte(body[pc+1]), byte(body[pc+2]), byte(body[pc+3]),
		byte(body[pc+4]), byte(body[pc+5]), byte(body[pc+6]), byte(body[pc+7]),
		byte(body[pc+8]), byte(body[pc+9]), byte(body[pc+10]), byte(body[pc+11]),
		byte(body[pc+12]), byte(body[pc+13]), byte(body[pc+14]), byte(body[pc+15]),
	))
	frame.pc += 16
}

func (vm *vm) handleV128Load(frame *callFrame) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	v, err := memory.LoadV128(offset, index)
	if err != nil {
		return err
	}
	vm.stack.pushV128(v)
	return nil
}

func (vm *vm) handleV128Store(frame *callFrame) error {
	val := vm.stack.popV128()
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.StoreV128(offset, index, val)
}

func (vm *vm) handleI8x16Swizzle() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Swizzle(a, b))
}

func (vm *vm) handleI8x16Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Eq(a, b))
}

func (vm *vm) handleI8x16Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Ne(a, b))
}

func (vm *vm) handleI8x16LtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16LtS(a, b))
}

func (vm *vm) handleI8x16LtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16LtU(a, b))
}

func (vm *vm) handleI8x16GtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16GtS(a, b))
}

func (vm *vm) handleI8x16GtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16GtU(a, b))
}

func (vm *vm) handleI8x16LeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16LeS(a, b))
}

func (vm *vm) handleI8x16LeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16LeU(a, b))
}

func (vm *vm) handleI8x16GeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16GeS(a, b))
}

func (vm *vm) handleI8x16GeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16GeU(a, b))
}

func (vm *vm) handleI16x8Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Eq(a, b))
}

func (vm *vm) handleI16x8Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Ne(a, b))
}

func (vm *vm) handleI16x8LtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8LtS(a, b))
}

func (vm *vm) handleI16x8LtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8LtU(a, b))
}

func (vm *vm) handleI16x8GtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8GtS(a, b))
}

func (vm *vm) handleI16x8GtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8GtU(a, b))
}

func (vm *vm) handleI16x8LeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8LeS(a, b))
}

func (vm *vm) handleI16x8LeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8LeU(a, b))
}

func (vm *vm) handleI16x8GeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8GeS(a, b))
}

func (vm *vm) handleI16x8GeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8GeU(a, b))
}

func (vm *vm) handleI32x4Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Eq(a, b))
}

func (vm *vm) handleI32x4Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Ne(a, b))
}

func (vm *vm) handleI32x4LtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4LtS(a, b))
}

func (vm *vm) handleI32x4LtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4LtU(a, b))
}

func (vm *vm) handleI32x4GtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4GtS(a, b))
}

func (vm *vm) handleI32x4GtU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4GtU(a, b))
}

func (vm *vm) handleI32x4LeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4LeS(a, b))
}

func (vm *vm) handleI32x4LeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4LeU(a, b))
}

func (vm *vm) handleI32x4GeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4GeS(a, b))
}

func (vm *vm) handleI32x4GeU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4GeU(a, b))
}

func (vm *vm) handleF32x4Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Eq(a, b))
}

func (vm *vm) handleF32x4Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Ne(a, b))
}

func (vm *vm) handleF32x4Lt() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Lt(a, b))
}

func (vm *vm) handleF32x4Gt() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Gt(a, b))
}

func (vm *vm) handleF32x4Le() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Le(a, b))
}

func (vm *vm) handleF32x4Ge() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Ge(a, b))
}

func (vm *vm) handleF64x2Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Eq(a, b))
}

func (vm *vm) handleF64x2Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Ne(a, b))
}

func (vm *vm) handleF64x2Lt() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Lt(a, b))
}

func (vm *vm) handleF64x2Gt() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Gt(a, b))
}

func (vm *vm) handleF64x2Le() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Le(a, b))
}

func (vm *vm) handleF64x2Ge() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Ge(a, b))
}

func (vm *vm) handleV128And() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdV128And(a, b))
}

func (vm *vm) handleV128Andnot() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdV128Andnot(a, b))
}

func (vm *vm) handleV128Or() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdV128Or(a, b))
}

func (vm *vm) handleV128Xor() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdV128Xor(a, b))
}

func (vm *vm) handleI8x16NarrowI16x8S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16NarrowI16x8S(a, b))
}

func (vm *vm) handleI8x16NarrowI16x8U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16NarrowI16x8U(a, b))
}

func (vm *vm) handleI8x16Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Add(a, b))
}

func (vm *vm) handleI8x16AddSatS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16AddSatS(a, b))
}

func (vm *vm) handleI8x16AddSatU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16AddSatU(a, b))
}

func (vm *vm) handleI8x16Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Sub(a, b))
}

func (vm *vm) handleI8x16SubSatS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16SubSatS(a, b))
}

func (vm *vm) handleI8x16SubSatU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16SubSatU(a, b))
}

func (vm *vm) handleI8x16MinS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16MinS(a, b))
}

func (vm *vm) handleI8x16MinU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16MinU(a, b))
}

func (vm *vm) handleI8x16MaxS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16MaxS(a, b))
}

func (vm *vm) handleI8x16MaxU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16MaxU(a, b))
}

func (vm *vm) handleI8x16AvgrU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16AvgrU(a, b))
}

func (vm *vm) handleI16x8Q15mulrSatS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Q15mulrSatS(a, b))
}

func (vm *vm) handleI16x8NarrowI32x4S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8NarrowI32x4S(a, b))
}

func (vm *vm) handleI16x8NarrowI32x4U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8NarrowI32x4U(a, b))
}

func (vm *vm) handleI16x8Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Add(a, b))
}

func (vm *vm) handleI16x8AddSatS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8AddSatS(a, b))
}

func (vm *vm) handleI16x8AddSatU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8AddSatU(a, b))
}

func (vm *vm) handleI16x8Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Sub(a, b))
}

func (vm *vm) handleI16x8SubSatS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8SubSatS(a, b))
}

func (vm *vm) handleI16x8SubSatU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8SubSatU(a, b))
}

func (vm *vm) handleI16x8Mul() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Mul(a, b))
}

func (vm *vm) handleI16x8MinS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8MinS(a, b))
}

func (vm *vm) handleI16x8MinU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8MinU(a, b))
}

func (vm *vm) handleI16x8MaxS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8MaxS(a, b))
}

func (vm *vm) handleI16x8MaxU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8MaxU(a, b))
}

func (vm *vm) handleI16x8AvgrU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8AvgrU(a, b))
}

func (vm *vm) handleI16x8ExtmulLowI8x16S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ExtmulLowI8x16S(a, b))
}

func (vm *vm) handleI16x8ExtmulHighI8x16S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ExtmulHighI8x16S(a, b))
}

func (vm *vm) handleI16x8ExtmulLowI8x16U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ExtmulLowI8x16U(a, b))
}

func (vm *vm) handleI16x8ExtmulHighI8x16U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ExtmulHighI8x16U(a, b))
}

func (vm *vm) handleI32x4Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Add(a, b))
}

func (vm *vm) handleI32x4Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Sub(a, b))
}

func (vm *vm) handleI32x4Mul() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Mul(a, b))
}

func (vm *vm) handleI32x4MinS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4MinS(a, b))
}

func (vm *vm) handleI32x4MinU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4MinU(a, b))
}

func (vm *vm) handleI32x4MaxS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4MaxS(a, b))
}

func (vm *vm) handleI32x4MaxU() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4MaxU(a, b))
}

func (vm *vm) handleI32x4DotI16x8S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4DotI16x8S(a, b))
}

func (vm *vm) handleI32x4ExtmulLowI16x8S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ExtmulLowI16x8S(a, b))
}

func (vm *vm) handleI32x4ExtmulHighI16x8S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ExtmulHighI16x8S(a, b))
}

func (vm *vm) handleI32x4ExtmulLowI16x8U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ExtmulLowI16x8U(a, b))
}

func (vm *vm) handleI32x4ExtmulHighI16x8U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ExtmulHighI16x8U(a, b))
}

func (vm *vm) handleI64x2Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Add(a, b))
}

func (vm *vm) handleI64x2Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Sub(a, b))
}

func (vm *vm) handleI64x2Mul() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Mul(a, b))
}

func (vm *vm) handleI64x2Eq() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Eq(a, b))
}

func (vm *vm) handleI64x2Ne() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Ne(a, b))
}

func (vm *vm) handleI64x2LtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2LtS(a, b))
}

func (vm *vm) handleI64x2GtS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2GtS(a, b))
}

func (vm *vm) handleI64x2LeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2LeS(a, b))
}

func (vm *vm) handleI64x2GeS() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2GeS(a, b))
}

func (vm *vm) handleI64x2ExtmulLowI32x4S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ExtmulLowI32x4S(a, b))
}

func (vm *vm) handleI64x2ExtmulHighI32x4S() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ExtmulHighI32x4S(a, b))
}

func (vm *vm) handleI64x2ExtmulLowI32x4U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ExtmulLowI32x4U(a, b))
}

func (vm *vm) handleI64x2ExtmulHighI32x4U() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ExtmulHighI32x4U(a, b))
}

func (vm *vm) handleF32x4Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Add(a, b))
}

func (vm *vm) handleF32x4Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Sub(a, b))
}

func (vm *vm) handleF32x4Mul() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Mul(a, b))
}

func (vm *vm) handleF32x4Div() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Div(a, b))
}

func (vm *vm) handleF32x4Min() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Min(a, b))
}

func (vm *vm) handleF32x4Max() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Max(a, b))
}

func (vm *vm) handleF32x4Pmin() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Pmin(a, b))
}

func (vm *vm) handleF32x4Pmax() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4Pmax(a, b))
}

func (vm *vm) handleF64x2Add() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Add(a, b))
}

func (vm *vm) handleF64x2Sub() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Sub(a, b))
}

func (vm *vm) handleF64x2Mul() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Mul(a, b))
}

func (vm *vm) handleF64x2Div() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Div(a, b))
}

func (vm *vm) handleF64x2Min() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Min(a, b))
}

func (vm *vm) handleF64x2Max() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Max(a, b))
}

func (vm *vm) handleF64x2Pmin() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Pmin(a, b))
}

func (vm *vm) handleF64x2Pmax() {
	b := vm.stack.popV128()
	a := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2Pmax(a, b))
}

func (vm *vm) handleI8x16Shl() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16Shl(v, shift))
}

func (vm *vm) handleI8x16ShrU() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16ShrU(v, shift))
}

func (vm *vm) handleI8x16ShrS() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16ShrS(v, shift))
}

func (vm *vm) handleI16x8Shl() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8Shl(v, shift))
}

func (vm *vm) handleI16x8ShrS() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ShrS(v, shift))
}

func (vm *vm) handleI16x8ShrU() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ShrU(v, shift))
}

func (vm *vm) handleI32x4Shl() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4Shl(v, shift))
}

func (vm *vm) handleI32x4ShrS() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ShrS(v, shift))
}

func (vm *vm) handleI32x4ShrU() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ShrU(v, shift))
}

func (vm *vm) handleI64x2Shl() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2Shl(v, shift))
}

func (vm *vm) handleI64x2ShrS() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ShrS(v, shift))
}

func (vm *vm) handleI64x2ShrU() {
	shift := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ShrU(v, shift))
}

func (vm *vm) handleV128Bitselect() {
	v3 := vm.stack.popV128()
	v2 := vm.stack.popV128()
	v1 := vm.stack.popV128()
	vm.stack.pushV128(simdV128Bitselect(v1, v2, v3))
}

func (vm *vm) handleI8x16ExtractLaneS(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt32(simdI8x16ExtractLaneS(v, laneIndex))
}

func (vm *vm) handleI8x16ExtractLaneU(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt32(simdI8x16ExtractLaneU(v, laneIndex))
}

func (vm *vm) handleI16x8ExtractLaneS(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt32(simdI16x8ExtractLaneS(v, laneIndex))
}

func (vm *vm) handleI16x8ExtractLaneU(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt32(simdI16x8ExtractLaneU(v, laneIndex))
}

func (vm *vm) handleI32x4ExtractLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt32(simdI32x4ExtractLane(v, laneIndex))
}

func (vm *vm) handleI64x2ExtractLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushInt64(simdI64x2ExtractLane(v, laneIndex))
}

func (vm *vm) handleF32x4ExtractLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushFloat32(simdF32x4ExtractLane(v, laneIndex))
}

func (vm *vm) handleF64x2ExtractLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	vm.stack.pushFloat64(simdF64x2ExtractLane(v, laneIndex))
}

func (vm *vm) handleI8x16ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI8x16ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) handleI16x8ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI16x8ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) handleI32x4ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popInt32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI32x4ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) handleI64x2ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popInt64()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdI64x2ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) handleF32x4ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popFloat32()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdF32x4ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) handleF64x2ReplaceLane(frame *callFrame) {
	laneIndex := uint32(frame.next())
	laneValue := vm.stack.popFloat64()
	v := vm.stack.popV128()
	vm.stack.pushV128(simdF64x2ReplaceLane(v, laneIndex, laneValue))
}

func (vm *vm) memGet(frame *callFrame, size uint32) ([]byte, error) {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	index := uint32(vm.stack.popInt32())
	return memory.Get(offset, index, size)
}

func (vm *vm) handleSimdLoadLane(frame *callFrame, laneSize uint32) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	laneValue, err := memory.Get(offset, uint32(index), laneSize/8)
	if err != nil {
		return err
	}

	vm.stack.pushV128(simdLoadLane(v, laneIndex, laneValue))
	return nil
}

func (vm *vm) handleSimdStoreLane(frame *callFrame, laneSize uint32) error {
	frame.pc++ // skip align (unused at runtime)
	memory := frame.module.memories[frame.next()]
	offset := uint32(frame.next())
	laneIndex := uint32(frame.next())
	v := vm.stack.popV128()
	index := vm.stack.popInt32()

	lanesPerUint64 := 64 / laneSize
	shift := (laneIndex % lanesPerUint64) * laneSize
	var val uint64
	if laneIndex < lanesPerUint64 {
		val = v.Low >> shift
	} else {
		val = v.High >> shift
	}

	switch laneSize {
	case 8:
		return memory.StoreByte(offset, uint32(index), byte(val))
	case 16:
		return memory.StoreUint16(offset, uint32(index), uint16(val))
	case 32:
		return memory.StoreUint32(offset, uint32(index), uint32(val))
	case 64:
		return memory.StoreUint64(offset, uint32(index), val)
	}
	return nil
}
