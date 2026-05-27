// Copyright 2026 Google LLC
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
	"os"
	"path/filepath"
)

// resolveBinary returns the path to the WABT binary strictly in the project's
// local `.toolchain` directory. It does not fall back to the host system path.
func resolveBinary(name string) string {
	dir, err := os.Getwd()
	if err != nil {
		return filepath.Join(".toolchain", "wabt", "bin", name)
	}
	// Traverse up to find the project root (where go.mod lives)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, ".toolchain", "wabt", "bin", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(".toolchain", "wabt", "bin", name)
}
