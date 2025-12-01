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

// ExperimentalFeatures are features that have not yet been validated. In
// particular, these are WASM 3.0 features for which we don't have spec tests
// yet (see https://github.com/WebAssembly/wabt/issues/2648)
// See also https://webassembly.org/news/2025-09-17-wasm-3.0/.
type ExperimentalFeatures struct {
	MultipleMemories bool
}
