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

//go:build !unix && !windows

package wasip1

import (
	"errors"
	"os"
)

type WasiConfig struct {
	Args     []string
	Env      map[string]string
	Preopens []WasiPreopen
	Stdin    *os.File
	Stdout   *os.File
	Stderr   *os.File
}

// WasiModule provides WASI functionality to WebAssembly modules.
// On non-Unix platforms, WASI is not supported.
type WasiModule struct{}

func NewWasiModule(config WasiConfig) (*WasiModule, error) {
	return nil, errors.New("WASI is not supported on this platform")
}

func (w *WasiModule) ToImports() map[string]map[string]any { return nil }

func (w *WasiModule) Close() {}
