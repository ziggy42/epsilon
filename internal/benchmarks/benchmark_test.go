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

package benchmarks

import (
	"context"
	"os"
	"testing"

	"github.com/ziggy42/epsilon/epsilon"
)

func BenchmarkFactorialRecursive(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "fac_recursive", int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFactorialIterative(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "fac_iterative", int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciRecursive(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "fib_recursive", int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciIterative(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "fib_iterative", int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkIndirect(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/indirect.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "run_indirect_calls", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMatrixMultiplication(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/matrix_multiplication.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "run_matrix_multiplication", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMemoryAccess(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/memory_access.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "run_memcpy", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkTrigonometrySin(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/trigonometry.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "compute_sin", float32(42.7))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingBubbleSort(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "bubble_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingMergeSort(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "merge_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingQuickSort(b *testing.B) {
	ctx := context.Background()
	instance, err := instantiate(ctx, "code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke(ctx, "quick_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func instantiate(
	ctx context.Context,
	wasmPath string,
) (*epsilon.ModuleInstance, error) {
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	return epsilon.NewRuntime().InstantiateModuleFromBytes(ctx, wasm)
}
