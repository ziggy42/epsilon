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

type wasmFloat interface {
	float32 | float64
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

func boolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}
