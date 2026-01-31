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

//go:build !unix

package wasip1

import (
	"errors"
	"os"
)

// WasiModuleBuilder is a builder for creating WasiModule instances.
// On non-Unix platforms, WASI is not supported.
type WasiModuleBuilder struct{}

// WasiModule provides WASI functionality to WebAssembly modules.
// On non-Unix platforms, WASI is not supported.
type WasiModule struct{}

// NewWasiModuleBuilder creates a new WasiModuleBuilder.
func NewWasiModuleBuilder() *WasiModuleBuilder {
	return &WasiModuleBuilder{}
}

// WithArgs sets the command-line arguments for the WASI module.
func (b *WasiModuleBuilder) WithArgs(args ...string) *WasiModuleBuilder {
	return b
}

// WithEnv adds an environment variable to the WASI module.
func (b *WasiModuleBuilder) WithEnv(key, value string) *WasiModuleBuilder {
	return b
}

// WithDir mounts a directory with default rights.
func (b *WasiModuleBuilder) WithDir(
	guestPath string,
	hostDir *os.File,
) *WasiModuleBuilder {
	return b
}

// WithDirRights mounts a directory with explicit rights.
func (b *WasiModuleBuilder) WithDirRights(
	guestPath string,
	hostDir *os.File,
	rights, rightsInheriting int64,
) *WasiModuleBuilder {
	return b
}

// WithStdin sets the stdin file for the WASI module.
func (b *WasiModuleBuilder) WithStdin(f *os.File) *WasiModuleBuilder {
	return b
}

// WithStdout sets the stdout file for the WASI module.
func (b *WasiModuleBuilder) WithStdout(f *os.File) *WasiModuleBuilder {
	return b
}

// WithStderr sets the stderr file for the WASI module.
func (b *WasiModuleBuilder) WithStderr(f *os.File) *WasiModuleBuilder {
	return b
}

// Build constructs a WasiModule from the builder configuration.
// On non-Unix platforms, this always returns an error.
func (b *WasiModuleBuilder) Build() (*WasiModule, error) {
	return nil, errors.New("WASI is not supported on this platform")
}

func (w *WasiModule) ToImports() map[string]map[string]any { return nil }

func (w *WasiModule) Close() {}
