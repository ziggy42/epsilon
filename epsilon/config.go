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

// Config controls the behavior and resource limits of the VM.
type Config struct {
	// MaxCallStackDepth is the hard limit on call stack depth to prevent
	// infinite recursion. Default: 1000.
	MaxCallStackDepth int

	// CallStackPreallocationSize controls how many call frames to preallocate.
	// Caches (controlStackCache, localsCache) are sized to this value.
	// Beyond this depth, allocations fall back to heap. Default: 1000.
	CallStackPreallocationSize int

	// ExperimentalMultipleMemories enables WASM 3.0 multiple memories.
	// This feature has not yet been fully validated with spec tests.
	// See https://github.com/WebAssembly/wabt/issues/2648
	ExperimentalMultipleMemories bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxCallStackDepth:          1000,
		CallStackPreallocationSize: 1000,
	}
}
