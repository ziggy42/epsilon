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
	"bytes"
	"epsilon/epsilon"
	"os"
	"testing"
)

func BenchmarkFactorialRecursive(b *testing.B) {
	vm, instance, err := getVm("code/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "fac_recursive", int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFactorialIterative(b *testing.B) {
	vm, instance, err := getVm("code/factorial.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "fac_iterative", int64(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciRecursive(b *testing.B) {
	vm, instance, err := getVm("code/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "fib_recursive", int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkFibonacciIterative(b *testing.B) {
	vm, instance, err := getVm("code/fibonacci.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "fib_iterative", int32(25))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkIndirect(b *testing.B) {
	vm, instance, err := getVm("code/indirect.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "run_indirect_calls", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMatrixMultiplication(b *testing.B) {
	vm, instance, err := getVm("code/matrix_multiplication.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "run_matrix_multiplication", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkMemoryAccess(b *testing.B) {
	vm, instance, err := getVm("code/memory_access.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "run_memcpy", int32(100))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkTrigonometrySin(b *testing.B) {
	vm, instance, err := getVm("code/trigonometry.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "compute_sin", float32(42.7))
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingBubbleSort(b *testing.B) {
	vm, instance, err := getVm("code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "bubble_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingMergeSort(b *testing.B) {
	vm, instance, err := getVm("code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "merge_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func BenchmarkSortingQuickSort(b *testing.B) {
	vm, instance, err := getVm("code/sorting.wasm")
	if err != nil {
		b.Fatalf("failed to initialize test: %v", err)
	}

	for b.Loop() {
		_, err := vm.Invoke(instance, "quick_sort")
		if err != nil {
			b.Fatalf("failed to execute benchmark: %v", err)
		}
	}
}

func getVm(wasmPath string) (*epsilon.VM, *epsilon.ModuleInstance, error) {
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, nil, err
	}
	parser := epsilon.NewParser(bytes.NewReader(wasm))
	module, err := parser.Parse()
	if err != nil {
		return nil, nil, err
	}
	vm := epsilon.NewVM()
	moduleInstance, err := vm.Instantiate(module, map[string]map[string]any{})
	if err != nil {
		return nil, nil, err
	}
	return vm, moduleInstance, nil
}
