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
	ErrIntegerDivideByZero        = errors.New("integer divide by zero")
	ErrIntegerDivideOverflow      = errors.New("integer divide overflow")
	ErrIntegerOverflow            = errors.New("integer overflow")
	ErrInvalidConversionToInteger = errors.New("invalid conversion to integer")
)

const (
	maxInt32Plus1  = 2147483648.0
	maxUint32Plus1 = 4294967296.0
	maxInt64Plus1  = 9223372036854775808.0
	maxUint64Plus1 = 18446744073709551616.0
)

type WasmNumber interface {
	int32 | int64 | float32 | float64
}

type WasmFloat interface {
	float32 | float64
}

type WasmInt interface {
	int32 | int64
}

func EqualZero[T WasmNumber](a T) bool {
	return a == 0
}

func Equal[T WasmNumber](a, b T) bool {
	return a == b
}

func NotEqual[T WasmNumber](a, b T) bool {
	return a != b
}

func LessThan[T WasmNumber](a, b T) bool {
	return a < b
}

func LessThanUnsigned32(a, b int32) bool {
	return uint32(a) < uint32(b)
}

func LessThanUnsigned64(a, b int64) bool {
	return uint64(a) < uint64(b)
}

func GreaterThan[T WasmNumber](a, b T) bool {
	return a > b
}

func GreaterThanUnsigned32(a, b int32) bool {
	return uint32(a) > uint32(b)
}

func GreaterThanUnsigned64(a, b int64) bool {
	return uint64(a) > uint64(b)
}

func LessOrEqual[T WasmNumber](a, b T) bool {
	return a <= b
}

func LessOrEqualUnsigned32(a, b int32) bool {
	return uint32(a) <= uint32(b)
}

func LessOrEqualUnsigned64(a, b int64) bool {
	return uint64(a) <= uint64(b)
}

func GreaterOrEqual[T WasmNumber](a, b T) bool {
	return a >= b
}

func GreaterOrEqualUnsigned32(a, b int32) bool {
	return uint32(a) >= uint32(b)
}

func GreaterOrEqualUnsigned64(a, b int64) bool {
	return uint64(a) >= uint64(b)
}

func Add[T WasmNumber](a, b T) T {
	return a + b
}

func Sub[T WasmNumber](a, b T) T {
	return a - b
}

func Mul[T WasmNumber](a, b T) T {
	return a * b
}

func Div[T WasmFloat](a, b T) T {
	return a / b
}

func DivS32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	if a == math.MinInt32 && b == -1 {
		return 0, ErrIntegerDivideOverflow
	}
	return a / b, nil
}

func DivS64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrIntegerDivideOverflow
	}
	return a / b, nil
}

func DivU32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return int32(uint32(a) / uint32(b)), nil
}

func DivU64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return int64(uint64(a) / uint64(b)), nil
}

func RemS32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return a % b, nil
}

func RemS64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return a % b, nil
}

func RemU32(a, b int32) (int32, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return int32(uint32(a) % uint32(b)), nil
}

func RemU64(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrIntegerDivideByZero
	}
	return int64(uint64(a) % uint64(b)), nil
}

func Clz32(a int32) int32 {
	return int32(bits.LeadingZeros32(uint32(a)))
}

func Ctz32(a int32) int32 {
	return int32(bits.TrailingZeros32(uint32(a)))
}

func Popcnt32(a int32) int32 {
	return int32(bits.OnesCount32(uint32(a)))
}

func Clz64(a int64) int64 {
	return int64(bits.LeadingZeros64(uint64(a)))
}

func Ctz64(a int64) int64 {
	return int64(bits.TrailingZeros64(uint64(a)))
}

func Popcnt64(a int64) int64 {
	return int64(bits.OnesCount64(uint64(a)))
}

func And[T WasmInt](a, b T) T {
	return a & b
}

func Or[T WasmInt](a, b T) T {
	return a | b
}

func Xor[T WasmInt](a, b T) T {
	return a ^ b
}

func Shl32(a, b int32) int32 {
	return a << (uint32(b) % 32)
}

func ShrS32(a, b int32) int32 {
	return a >> (uint32(b) % 32)
}

func ShrU32(a, b int32) int32 {
	return int32(uint32(a) >> (uint32(b) % 32))
}

func Rotl32(a, b int32) int32 {
	return int32(bits.RotateLeft32(uint32(a), int(uint32(b)%32)))
}

func Rotr32(a, b int32) int32 {
	shiftAmount := uint32(b) % 32
	return int32(bits.RotateLeft32(uint32(a), int(32-shiftAmount)))
}

func Shl64(a, b int64) int64 {
	return a << (uint64(b) % 64)
}

func ShrS64(a, b int64) int64 {
	return a >> (uint64(b) % 64)
}

func ShrU64(a, b int64) int64 {
	return int64(uint64(a) >> (uint64(b) % 64))
}

func Rotl64(a, b int64) int64 {
	return int64(bits.RotateLeft64(uint64(a), int(uint64(b)%64)))
}

func Rotr64(a, b int64) int64 {
	shiftAmount := uint64(b) % 64
	return int64(bits.RotateLeft64(uint64(a), int(64-shiftAmount)))
}

func Ceil[T WasmFloat](a T) T {
	return T(math.Ceil(float64(a)))
}

func Floor[T WasmFloat](a T) T {
	return T(math.Floor(float64(a)))
}

func Trunc[T WasmFloat](a T) T {
	return T(math.Trunc(float64(a)))
}

func Nearest[T WasmFloat](a T) T {
	f64 := float64(a)
	return T(math.Copysign(math.RoundToEven(f64), f64))
}

func Sqrt[T WasmFloat](a T) T {
	return T(math.Sqrt(float64(a)))
}

func Min[T WasmFloat](a, b T) T {
	if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
		return T(math.NaN())
	}
	return min(a, b)
}

func Max[T WasmFloat](a, b T) T {
	if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
		return T(math.NaN())
	}
	return max(a, b)
}

func Abs[T WasmFloat](a T) T {
	return T(math.Abs(float64(a)))
}

func Neg[T WasmFloat](a T) T {
	return -a
}

func Copysign[T WasmFloat](a, b T) T {
	return T(math.Copysign(float64(a), float64(b)))
}

func Extend8STo32(a int32) int32 {
	return int32(int8(a))
}

func Extend16STo32(a int32) int32 {
	return int32(int16(a))
}

func Extend8STo64(a int64) int64 {
	return int64(int8(a))
}

func Extend16STo64(a int64) int64 {
	return int64(int16(a))
}

func Extend32STo64(a int64) int64 {
	return int64(int32(a))
}

func WrapI64ToI32(a int64) int32 {
	return int32(a)
}

func TruncF32SToI32(a float32) (int32, error) {
	if math.IsNaN(float64(a)) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < math.MinInt32 || truncated >= maxInt32Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int32(truncated), nil
}

func TruncF32UToI32(a float32) (int32, error) {
	if math.IsNaN(float64(a)) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < 0 || truncated >= maxUint32Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int32(uint32(truncated)), nil
}

func TruncF64SToI32(a float64) (int32, error) {
	if math.IsNaN(a) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < math.MinInt32 || truncated >= maxInt32Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int32(truncated), nil
}

func TruncF64UToI32(a float64) (int32, error) {
	if math.IsNaN(a) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < 0 || truncated >= maxUint32Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int32(uint32(truncated)), nil
}

func TruncF32SToI64(a float32) (int64, error) {
	if math.IsNaN(float64(a)) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < math.MinInt64 || truncated >= maxInt64Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int64(truncated), nil
}

func TruncF32UToI64(a float32) (int64, error) {
	if math.IsNaN(float64(a)) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(float64(a))
	if truncated < 0 || truncated >= maxUint64Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int64(uint64(truncated)), nil
}

func TruncF64SToI64(a float64) (int64, error) {
	if math.IsNaN(a) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < math.MinInt64 || truncated >= maxInt64Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int64(truncated), nil
}

func TruncF64UToI64(a float64) (int64, error) {
	if math.IsNaN(a) {
		return 0, ErrInvalidConversionToInteger
	}
	truncated := math.Trunc(a)
	if truncated < 0 || truncated >= maxUint64Plus1 {
		return 0, ErrIntegerOverflow
	}
	return int64(uint64(truncated)), nil
}

func TruncSatF32SToI32(a float32) int32 {
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

func TruncSatF32UToI32(a float32) int32 {
	if math.IsNaN(float64(a)) || a < 0 {
		return 0
	}
	if a >= maxUint32Plus1 {
		return -1
	}
	return int32(uint32(a))
}

func TruncSatF64SToI32(a float64) int32 {
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

func TruncSatF64UToI32(a float64) int32 {
	if math.IsNaN(a) || a < 0 {
		return 0
	}
	if a >= maxUint32Plus1 {
		return -1
	}
	return int32(uint32(a))
}

func TruncSatF32SToI64(a float32) int64 {
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

func TruncSatF32UToI64(a float32) int64 {
	if math.IsNaN(float64(a)) || a < 0 {
		return 0
	}
	if a >= maxUint64Plus1 {
		return -1
	}
	return int64(uint64(a))
}

func TruncSatF64SToI64(a float64) int64 {
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

func TruncSatF64UToI64(a float64) int64 {
	if math.IsNaN(a) || a < 0 {
		return 0
	}
	if a >= maxUint64Plus1 {
		return -1
	}
	return int64(uint64(a))
}

func ConvertI32SToF32(a int32) float32 {
	return float32(a)
}

func ConvertI32UToF32(a int32) float32 {
	return float32(uint32(a))
}

func ConvertI64SToF32(a int64) float32 {
	return float32(a)
}

func ConvertI64UToF32(a int64) float32 {
	return float32(uint64(a))
}

func ConvertI32SToF64(a int32) float64 {
	return float64(a)
}

func ConvertI32UToF64(a int32) float64 {
	return float64(uint32(a))
}

func ConvertI64SToF64(a int64) float64 {
	return float64(a)
}

func ConvertI64UToF64(a int64) float64 {
	return float64(uint64(a))
}

func DemoteF64ToF32(a float64) float32 {
	return float32(a)
}

func PromoteF32ToF64(a float32) float64 {
	return float64(a)
}

func ReinterpretF32ToI32(a float32) int32 {
	return int32(math.Float32bits(a))
}

func ReinterpretF64ToI64(a float64) int64 {
	return int64(math.Float64bits(a))
}

func ReinterpretI32ToF32(a int32) float32 {
	return math.Float32frombits(uint32(a))
}

func ReinterpretI64ToF64(a int64) float64 {
	return math.Float64frombits(uint64(a))
}

func ExtendI32SToI64(a int32) int64 {
	return int64(a)
}

func ExtendI32UToI64(a int32) int64 {
	return int64(uint32(a))
}
