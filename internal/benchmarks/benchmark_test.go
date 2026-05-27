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

// The .wasm files loaded below are built from src/*.c via `make build-wasm`.
package benchmarks

import (
	"bytes"
	"os"
	"testing"

	"github.com/ziggy42/epsilon/epsilon"
)

func BenchmarkFactorialRecursive(b *testing.B) {
	instance, err := instantiate("wasm/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("fac_recursive", int32(1000), int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFactorialIterative(b *testing.B) {
	instance, err := instantiate("wasm/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("fac_iterative", int32(10000), int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciRecursive(b *testing.B) {
	instance, err := instantiate("wasm/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("fib_recursive", int32(1), int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciIterative(b *testing.B) {
	instance, err := instantiate("wasm/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("fib_iterative", int32(10000), int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkIndirect(b *testing.B) {
	instance, err := instantiate("wasm/indirect.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("run_indirect_calls", int32(10000))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMatrixMultiplication(b *testing.B) {
	instance, err := instantiate("wasm/matrix_multiplication.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("run_matrix_multiplication", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkVectorMath(b *testing.B) {
	instance, err := instantiate("wasm/vector_math.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("compute_vector_math", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMemoryAccess(b *testing.B) {
	instance, err := instantiate("wasm/memory_access.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("run_memcpy", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkTrigonometrySin(b *testing.B) {
	instance, err := instantiate("wasm/trigonometry.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("compute_sin", float32(42.7))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingBubbleSort(b *testing.B) {
	instance, err := instantiate("wasm/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("bubble_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingMergeSort(b *testing.B) {
	instance, err := instantiate("wasm/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("merge_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingQuickSort(b *testing.B) {
	instance, err := instantiate("wasm/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("quick_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkHostCall(b *testing.B) {
	wasm, err := os.ReadFile("wasm/host_call.wasm")
	if err != nil {
		b.Fatalf("failed to read wasm: %v", err)
	}
	imports := epsilon.NewModuleImports("env").
		AddHostFunc("noop", func(_ *epsilon.ModuleInstance, args ...any) []any {
			return []any{args[0]}
		})
	instance, err := epsilon.NewRuntime().
		InstantiateModuleWithImports(bytes.NewReader(wasm), imports)
	if err != nil {
		b.Fatalf("failed to instantiate: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("run_host_calls", int32(10000))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkInstantiateSmall(b *testing.B) {
	wasm, err := os.ReadFile("wasm/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to read wasm: %v", err)
	}
	for b.Loop() {
		_, err := epsilon.NewRuntime().InstantiateModuleFromBytes(wasm)
		if err != nil {
			b.Fatalf("failed to instantiate: %v", err)
		}
	}
}

func BenchmarkInstantiateLarge(b *testing.B) {
	wasm, err := os.ReadFile("wasm/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to read wasm: %v", err)
	}
	for b.Loop() {
		_, err := epsilon.NewRuntime().InstantiateModuleFromBytes(wasm)
		if err != nil {
			b.Fatalf("failed to instantiate: %v", err)
		}
	}
}

func BenchmarkSHA256(b *testing.B) {
	instance, err := instantiate("wasm/sha256.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := instance.Invoke("run_sha256", int32(64))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func instantiate(wasmPath string) (*epsilon.ModuleInstance, error) {
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	return epsilon.NewRuntime().InstantiateModuleFromBytes(wasm)
}
