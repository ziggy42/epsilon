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

package spec_tests

import (
	"epsilon/wabt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const simdSpecTestsDirPath = "../spec/test/core/simd"

func TestSimdSpec(t *testing.T) {
	files, err := os.ReadDir(simdSpecTestsDirPath)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}

	for _, file := range files {
		name := file.Name()
		if !strings.HasSuffix(name, ".wast") {
			continue
		}

		if name == "simd_conversions.wast" || name == "simd_f64x2_arith.wast" {
			// TODO(pivetta): These test require to properly handle NaN for simd
			//   instructions.
			continue
		}

		wastFile := filepath.Join(simdSpecTestsDirPath, name)
		t.Run(name, func(t *testing.T) {
			jsonData, wasmDict, err := wabt.Wast2json(wastFile)
			if err != nil {
				t.Fatalf("failed to run wast2json: %v", err)
			}
			runner := newSpecRunner(t, wasmDict)
			runner.run(jsonData.Commands)
		})
	}
}
