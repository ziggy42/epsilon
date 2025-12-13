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
	"math"
	"math/bits"
)

var (
	errIntegerDivideByZero        = errors.New("integer divide by zero")
	errIntegerDivideOverflow      = errors.New("integer divide overflow")
	errIntegerOverflow            = errors.New("integer overflow")
	errInvalidConversionToInteger = errors.New("invalid conversion to integer")
)

const (
	maxInt32Plus1  = 2147483648.0
	maxUint32Plus1 = 4294967296.0
	maxInt64Plus1  = 9223372036854775808.0
	maxUint64Plus1 = 18446744073709551616.0
)

type wasmNumber interface {
	int32 | int64 | float32 | float64
}

type wasmFloat interface {
	float32 | float64
}

type wasmInt interface {
	int32 | int64
}

func equal[T wasmNumber](a, b T) bool {
	return a == b
}

func notEqual[T wasmNumber](a, b T) bool {
	return a != b
}

func lessThan[T wasmNumber](a, b T) bool {
	return a < b
}

func lessThanU32(a, b int32) bool {
	return uint32(a) < uint32(b)
}

func lessThanU64(a, b int64) bool {
	return uint64(a) < uint64(b)
}

func lessOrEqual[T wasmNumber](a, b T) bool {
	return a <= b
}

func lessOrEqualU32(a, b int32) bool {
	return uint32(a) <= uint32(b)
}

func lessOrEqualU64(a, b int64) bool {
	return uint64(a) <= uint64(b)
}

func greaterThan[T wasmNumber](a, b T) bool {
	return a > b
}

func greaterThanU32(a, b int32) bool {
	return uint32(a) > uint32(b)
}

func greaterThanU64(a, b int64) bool {
	return uint64(a) > uint64(b)
}

func greaterOrEqual[T wasmNumber](a, b T) bool {
	return a >= b
}

func greaterOrEqualU32(a, b int32) bool {
	return uint32(a) >= uint32(b)
}

func greaterOrEqualU64(a, b int64) bool {
	return uint64(a) >= uint64(b)
}

func add[T wasmNumber](a, b T) T {
	return a + b
}

func sub[T wasmNumber](a, b T) T {
	return a - b
}

func mul[T wasmNumber](a, b T) T {
	return a * b
}

func div[T wasmFloat](a, b T) T {
	return a / b
}

func divS32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	if a == math.MinInt32 && b == -1 {
		return 0, errIntegerDivideOverflow
	}
	return a / b, nil
}

func divS64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	if a == math.MinInt64 && b == -1 {
		return 0, errIntegerDivideOverflow
	}
	return a / b, nil
}

func divU32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return int32(uint32(a) / uint32(b)), nil
}

func divU64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return int64(uint64(a) / uint64(b)), nil
}

func remS32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return a % b, nil
}

func remS64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return a % b, nil
}

func remU32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return int32(uint32(a) % uint32(b)), nil
}

func remU64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errIntegerDivideByZero
	}
	return int64(uint64(a) % uint64(b)), nil
}

func and[T wasmInt](a, b T) T {
	return a & b
}

func or[T wasmInt](a, b T) T {
	return a | b
}

func xor[T wasmInt](a, b T) T {
	return a ^ b
}

func shl32(a, b int32) int32 {
	return a << (uint32(b) % 32)
}

func shrS32(a, b int32) int32 {
	return a >> (uint32(b) % 32)
}

func shrU32(a, b int32) int32 {
	return int32(uint32(a) >> (uint32(b) % 32))
}

func shl64(a, b int64) int64 {
	return a << (uint64(b) % 64)
}

func shrS64(a, b int64) int64 {
	return a >> (uint64(b) % 64)
}

func shrU64(a, b int64) int64 {
	return int64(uint64(a) >> (uint64(b) % 64))
}

func rotl32(a, b int32) int32 {
	return int32(bits.RotateLeft32(uint32(a), int(b)))
}

func rotr32(a, b int32) int32 {
	return int32(bits.RotateLeft32(uint32(a), -int(b)))
}

func rotl64(a, b int64) int64 {
	return int64(bits.RotateLeft64(uint64(a), int(b)))
}

func rotr64(a, b int64) int64 {
	return int64(bits.RotateLeft64(uint64(a), -int(b)))
}

func clz32(a int32) int32 {
	return int32(bits.LeadingZeros32(uint32(a)))
}

func clz64(a int64) int64 {
	return int64(bits.LeadingZeros64(uint64(a)))
}

func ctz32(a int32) int32 {
	return int32(bits.TrailingZeros32(uint32(a)))
}

func ctz64(a int64) int64 {
	return int64(bits.TrailingZeros64(uint64(a)))
}

func popcnt32(a int32) int32 {
	return int32(bits.OnesCount32(uint32(a)))
}

func popcnt64(a int64) int64 {
	return int64(bits.OnesCount64(uint64(a)))
}

func abs[T wasmFloat](a T) T {
	return T(math.Abs(float64(a)))
}

func ceil[T wasmFloat](a T) T {
	return T(math.Ceil(float64(a)))
}

func floor[T wasmFloat](a T) T {
	return T(math.Floor(float64(a)))
}

func trunc[T wasmFloat](a T) T {
	return T(math.Trunc(float64(a)))
}

func nearest[T wasmFloat](a T) T {
	f64 := float64(a)
	return T(math.Copysign(math.RoundToEven(f64), f64))
}

func sqrt[T wasmFloat](a T) T {
	return T(math.Sqrt(float64(a)))
}

func wasmMin[T wasmFloat](a, b T) T {
	return min(a, b)
}

func wasmMax[T wasmFloat](a, b T) T {
	return max(a, b)
}

func copysign[T wasmFloat](a, b T) T {
	return T(math.Copysign(float64(a), float64(b)))
}

func truncF32SToI32(a float32) (int32, error) {
	if math.IsNaN(float64(a)) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < math.MinInt32 || truncated >= maxInt32Plus1 {
		return 0, errIntegerOverflow
	}
	return int32(truncated), nil
}

func truncF32UToI32(a float32) (int32, error) {
	if math.IsNaN(float64(a)) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < 0 || truncated >= maxUint32Plus1 {
		return 0, errIntegerOverflow
	}
	return int32(uint32(truncated)), nil
}

func truncF64SToI32(a float64) (int32, error) {
	if math.IsNaN(a) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < math.MinInt32 || truncated >= maxInt32Plus1 {
		return 0, errIntegerOverflow
	}
	return int32(truncated), nil
}

func truncF64UToI32(a float64) (int32, error) {
	if math.IsNaN(a) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < 0 || truncated >= maxUint32Plus1 {
		return 0, errIntegerOverflow
	}
	return int32(uint32(truncated)), nil
}

func truncF32SToI64(a float32) (int64, error) {
	if math.IsNaN(float64(a)) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < math.MinInt64 || truncated >= maxInt64Plus1 {
		return 0, errIntegerOverflow
	}
	return int64(truncated), nil
}

func truncF32UToI64(a float32) (int64, error) {
	if math.IsNaN(float64(a)) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < 0 || truncated >= maxUint64Plus1 {
		return 0, errIntegerOverflow
	}
	return int64(uint64(truncated)), nil
}

func truncF64SToI64(a float64) (int64, error) {
	if math.IsNaN(a) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < math.MinInt64 || truncated >= maxInt64Plus1 {
		return 0, errIntegerOverflow
	}
	return int64(truncated), nil
}

func truncF64UToI64(a float64) (int64, error) {
	if math.IsNaN(a) {
		return 0, errInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < 0 || truncated >= maxUint64Plus1 {
		return 0, errIntegerOverflow
	}
	return int64(uint64(truncated)), nil
}

func truncSatF32SToI32(a float32) int32 {
	if math.IsNaN(float64(a)) {
		return 0
	}
	if a < math.MinInt32 {
		return math.MinInt32
	}
	if a >= maxInt32Plus1 {
		return math.MaxInt32
	}
	return int32(a)
}

func truncSatF32UToI32(a float32) int32 {
	if math.IsNaN(float64(a)) || a < 0 {
		return 0
	}
	if a >= maxUint32Plus1 {
		return -1
	}
	return int32(uint32(a))
}

func truncSatF64SToI32(a float64) int32 {
	if math.IsNaN(a) {
		return 0
	}
	if a < math.MinInt32 {
		return math.MinInt32
	}
	if a >= maxInt32Plus1 {
		return math.MaxInt32
	}
	return int32(a)
}

func truncSatF64UToI32(a float64) int32 {
	if math.IsNaN(a) || a < 0 {
		return 0
	}
	if a >= maxUint32Plus1 {
		return -1
	}
	return int32(uint32(a))
}

func truncSatF32SToI64(a float32) int64 {
	if math.IsNaN(float64(a)) {
		return 0
	}
	if a < math.MinInt64 {
		return math.MinInt64
	}
	if a >= maxInt64Plus1 {
		return math.MaxInt64
	}
	return int64(a)
}

func truncSatF32UToI64(a float32) int64 {
	if math.IsNaN(float64(a)) || a < 0 {
		return 0
	}
	if a >= maxUint64Plus1 {
		return -1
	}
	return int64(uint64(a))
}

func truncSatF64SToI64(a float64) int64 {
	if math.IsNaN(a) {
		return 0
	}
	if a < math.MinInt64 {
		return math.MinInt64
	}
	if a >= maxInt64Plus1 {
		return math.MaxInt64
	}
	return int64(a)
}

func truncSatF64UToI64(a float64) int64 {
	if math.IsNaN(a) || a < 0 {
		return 0
	}
	if a >= maxUint64Plus1 {
		return -1
	}
	return int64(uint64(a))
}

func convertI32SToF32(a int32) float32 {
	return float32(a)
}

func convertI32UToF32(a int32) float32 {
	return float32(uint32(a))
}

func convertI64SToF32(a int64) float32 {
	return float32(a)
}

func convertI64UToF32(a int64) float32 {
	return float32(uint64(a))
}

func convertI32SToF64(a int32) float64 {
	return float64(a)
}

func convertI32UToF64(a int32) float64 {
	return float64(uint32(a))
}

func convertI64SToF64(a int64) float64 {
	return float64(a)
}

func convertI64UToF64(a int64) float64 {
	return float64(uint64(a))
}

func demoteF64ToF32(a float64) float32 {
	return float32(a)
}

func promoteF32ToF64(a float32) float64 {
	return float64(a)
}

func reinterpretF32ToI32(a float32) int32 {
	return int32(math.Float32bits(a))
}

func reinterpretF64ToI64(a float64) int64 {
	return int64(math.Float64bits(a))
}

func reinterpretI32ToF32(a int32) float32 {
	return math.Float32frombits(uint32(a))
}

func reinterpretI64ToF64(a int64) float64 {
	return math.Float64frombits(uint64(a))
}

func wrapI64ToI32(a int64) int32 {
	return int32(a)
}

func extendI32SToI64(a int32) int64 {
	return int64(a)
}

func extendI32UToI64(a int32) int64 {
	return int64(uint32(a))
}

func extend8STo32(a int32) int32 {
	return int32(int8(a))
}

func extend16STo32(a int32) int32 {
	return int32(int16(a))
}

func extend8STo64(a int64) int64 {
	return int64(int8(a))
}

func extend16STo64(a int64) int64 {
	return int64(int16(a))
}

func extend32STo64(a int64) int64 {
	return int64(int32(a))
}

func boolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func uint32ToInt32(v uint32) int32 {
	return int32(v)
}

func uint64ToInt64(v uint64) int64 {
	return int64(v)
}

func signExtend8To32(v byte) int32 {
	return int32(int8(v))
}

func zeroExtend8To32(v byte) int32 {
	return int32(v)
}

func signExtend16To32(v uint16) int32 {
	return int32(int16(v))
}

func zeroExtend16To32(v uint16) int32 {
	return int32(v)
}

func signExtend8To64(v byte) int64 {
	return int64(int8(v))
}

func zeroExtend8To64(v byte) int64 {
	return int64(v)
}

func signExtend16To64(v uint16) int64 {
	return int64(int16(v))
}

func zeroExtend16To64(v uint16) int64 {
	return int64(v)
}

func signExtend32To64(v uint32) int64 {
	return int64(int32(v))
}

func zeroExtend32To64(v uint32) int64 {
	return int64(v)
}
