// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package epsilon

import (
	"bytes"
	"os"
	"testing"
)

var benchmarkParsedModule *moduleDefinition

func BenchmarkParse(b *testing.B) {
	for _, name := range []string{
		"factorial",
		"sorting",
		"trigonometry",
		"vector_math",
	} {
		wasm, err := os.ReadFile("../internal/benchmarks/wasm/" + name + ".wasm")
		if err != nil {
			b.Fatal(err)
		}

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(wasm)))
			for b.Loop() {
				module, err := newParser(
					bytes.NewReader(wasm),
					DefaultConfig(),
				).parse()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkParsedModule = module
			}
		})
	}
}
