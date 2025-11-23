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

package wabt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Wat2Wasm(watCode string) ([]byte, error) {
	tmpdir, err := os.MkdirTemp("", "wat2wasm")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)

	watPath := filepath.Join(tmpdir, "test.wat")
	if err := os.WriteFile(watPath, []byte(watCode), 0644); err != nil {
		return nil, err
	}

	wasmPath := filepath.Join(tmpdir, "test.wasm")
	cmd := exec.Command(
		"wat2wasm",
		"--enable-multi-memory",
		watPath,
		"-o",
		wasmPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wat2wasm failed: %v\n%s", err, stderr.String())
	}

	return os.ReadFile(wasmPath)
}
