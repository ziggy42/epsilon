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

const (
	DefaultMaxCallStackDepth          = 1000
	DefaultCallStackPreallocationSize = 1000
	DefaultMaxTableElements           = 10_000_000
	DefaultMaxMemoryPages             = 65536
	DefaultMaxLocalsPerFunction       = 50_000
)

// Config controls the behavior and resource limits of the VM.
type Config struct {
	// MaxCallStackDepth is the hard limit on call stack depth to prevent
	// infinite recursion.
	// Default: DefaultMaxCallStackDepth.
	MaxCallStackDepth int

	// CallStackPreallocationSize controls how many call frames to preallocate.
	// Caches (controlStackCache, localsCache) are sized to this value.
	// Beyond this depth, allocations fall back to heap.
	// Default: DefaultCallStackPreallocationSize.
	CallStackPreallocationSize int

	// ExperimentalMultipleMemories enables WASM 3.0 multiple memories.
	// This feature has not yet been fully validated with spec tests.
	// See https://github.com/WebAssembly/wabt/issues/2648.
	// Default: false.
	ExperimentalMultipleMemories bool

	// EnableFuel enables instruction fuel (gas) mechanism to prevent infinite
	// loops and control execution time. When enabled, the VM will decrement
	// the available fuel by 1 for every WASM instruction executed.
	// NOTE: Enabling fuel has a non-trivial performance impact on the VM.
	// Default: false.
	EnableFuel bool

	// Fuel is the initial amount of fuel available to the VM.
	// One unit of fuel equals one WASM instruction.
	// Only used if EnableFuel is true.
	Fuel uint64

	// MaxTableElements is the upper bound on a declared table's Limits.Min (and
	// Limits.Max, if present). A module declaring a table above this ceiling is
	// rejected.
	// Default: DefaultMaxTableElements.
	MaxTableElements uint32

	// MaxMemoryPages is the upper bound on a declared memory's Limits.Min (and
	// Limits.Max, if present), expressed in 64 KiB WebAssembly pages. A module
	// declaring a memory above this ceiling is rejected.
	// Default: DefaultMaxMemoryPages (= 4 GiB, the WebAssembly 32-bit maximum).
	MaxMemoryPages uint32

	// MaxLocalsPerFunction is the upper bound on the total number of declared
	// locals (sum of all local entry counts) for a single function body.
	// Function parameters are not counted. A function exceeding this ceiling is
	// rejected.
	// Default: DefaultMaxLocalsPerFunction.
	MaxLocalsPerFunction uint32
}

// DefaultConfig returns a Config with every field set to its corresponding
// Default* constant.
func DefaultConfig() Config {
	return Config{
		MaxCallStackDepth:          DefaultMaxCallStackDepth,
		CallStackPreallocationSize: DefaultCallStackPreallocationSize,
		MaxTableElements:           DefaultMaxTableElements,
		MaxMemoryPages:             DefaultMaxMemoryPages,
		MaxLocalsPerFunction:       DefaultMaxLocalsPerFunction,
	}
}
