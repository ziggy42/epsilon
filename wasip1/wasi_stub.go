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

//go:build !unix

package wasip1

import (
	"errors"
	"os"
)

// errUnsupportedPlatform is returned when constructing the host filesystem
// backend on a platform that does not support it. The core WASI state machine
// compiles everywhere; only the syscall-based host backend is Unix-only.
var errUnsupportedPlatform = errors.New("WASI is not supported on this platform")

// OpenHostFileSystem reports that the syscall-based host backend is
// unavailable on this platform.
func OpenHostFileSystem(dir string) (FileSystem, error) {
	return nil, errUnsupportedPlatform
}

// NewHostFile reports that the syscall-based host backend is unavailable on
// this platform.
func NewHostFile(*os.File) (File, error) {
	return nil, errUnsupportedPlatform
}
