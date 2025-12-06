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
	"epsilon/internal/wabt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSpec(t *testing.T, dirPath string) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		t.Fatalf("failed to read wast directory: %v", err)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".wast") {
			continue
		}

		wastFile := filepath.Join(dirPath, file.Name())
		t.Run(file.Name(), func(t *testing.T) {
			jsonData, wasmDict, err := wabt.Wast2json(wastFile)
			if err != nil {
				t.Fatalf("failed to run wast2json: %v", err)
			}
			runner := newSpecRunner(t, wasmDict)
			runner.run(jsonData.Commands)
		})
	}
}

func TestCoreSpec(t *testing.T) {
	testSpec(t, "spec/test/core")
}

func TestSimdSpec(t *testing.T) {
	testSpec(t, "spec/test/core/simd")
}
