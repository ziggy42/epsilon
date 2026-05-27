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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziggy42/epsilon/internal/wabt"
)

const testsuiteDir = "testsuite"

func TestSpec(t *testing.T) {
	entries, err := os.ReadDir(testsuiteDir)
	if err != nil {
		t.Fatalf("failed to read testsuite directory: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".wast") {
			continue
		}
		wastFile := filepath.Join(testsuiteDir, name)
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
