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

//go:build unix

package wasip1

import (
	"math"
	"os"
	"testing"

	"github.com/ziggy42/epsilon/epsilon"
)

func testMemory(pages uint32) *epsilon.Memory {
	return epsilon.NewMemory(epsilon.MemoryType{
		Limits: epsilon.Limits{Min: pages},
	})
}

func testResourceTable(t *testing.T, entries ...fsEntry) *wasiResourceTable {
	t.Helper()
	_, dirFd := testFS(t, entries...)

	rt, err := newWasiResourceTable(
		[]WasiPreopen{{
			File:             dirFd,
			GuestPath:        "/",
			Rights:           DefaultDirRights,
			RightsInheriting: DefaultDirInheritingRights,
		}},
		os.Stdin,
		os.Stdout,
		os.Stderr,
	)
	if err != nil {
		t.Fatalf("failed to create resource table: %v", err)
	}
	t.Cleanup(func() { rt.closeAll() })
	return rt
}

func TestReaddir_NegativeCookieReturnsError(t *testing.T) {
	rt := testResourceTable(t, dir("subdir"))
	memory := testMemory(1)

	// The preopened directory is fd 3. Open the subdir under it.
	subdirFdIndex, errCode := rt.allocateFd(
		mustOpen(t, rt, memory, 3, "subdir"),
		DefaultDirRights,
		DefaultDirInheritingRights,
		0,
	)
	if errCode != errnoSuccess {
		t.Fatalf("failed to allocate subdir fd: %d", errCode)
	}

	// Use a negative cookie — this should return an error, not panic.
	bufPtr := int32(0)
	bufLen := int32(256)
	bufusedPtr := int32(bufLen + 4)
	result := rt.readdir(memory, subdirFdIndex, bufPtr, bufLen, -1, bufusedPtr)
	if result != errnoInval {
		t.Errorf("readdir with negative cookie: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

func TestReaddir_MaxNegativeCookieReturnsError(t *testing.T) {
	rt := testResourceTable(t, dir("subdir"))
	memory := testMemory(1)

	subdirFdIndex, errCode := rt.allocateFd(
		mustOpen(t, rt, memory, 3, "subdir"),
		DefaultDirRights,
		DefaultDirInheritingRights,
		0,
	)
	if errCode != errnoSuccess {
		t.Fatalf("failed to allocate subdir fd: %d", errCode)
	}

	result := rt.readdir(memory, subdirFdIndex, 0, 256, math.MinInt64, 260)
	if result != errnoInval {
		t.Errorf("readdir with MinInt64 cookie: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

func TestRandomGet_NegativeLengthReturnsError(t *testing.T) {
	w, err := NewWasiModuleBuilder().Build()
	if err != nil {
		t.Fatalf("failed to build wasi module: %v", err)
	}
	defer w.Close()

	memory := testMemory(1)

	// Negative buf_len should return an error, not panic.
	result := w.randomGet(memory, 0, -1)
	if result != errnoInval {
		t.Errorf("randomGet with negative length: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

func TestRandomGet_MinInt32LengthReturnsError(t *testing.T) {
	w, err := NewWasiModuleBuilder().Build()
	if err != nil {
		t.Fatalf("failed to build wasi module: %v", err)
	}
	defer w.Close()

	memory := testMemory(1)

	result := w.randomGet(memory, 0, math.MinInt32)
	if result != errnoInval {
		t.Errorf("randomGet with MinInt32 length: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

func TestPathReadlink_NegativeBufLenReturnsError(t *testing.T) {
	rt := testResourceTable(t,
		file("target.txt", "content"),
		link("mylink", "target.txt"),
	)
	memory := testMemory(1)

	// Write the path "mylink" into guest memory.
	path := []byte("mylink")
	if err := memory.Set(0, 0, path); err != nil {
		t.Fatalf("failed to write path: %v", err)
	}

	pathPtr := int32(0)
	pathLen := int32(len(path))
	bufPtr := int32(100)
	bufLen := int32(-1) // negative length
	bufusedPtr := int32(200)

	// Negative buf_len should return an error, not panic.
	result := rt.pathReadlink(memory, 3, pathPtr, pathLen, bufPtr, bufLen, bufusedPtr)
	if result != errnoInval {
		t.Errorf("pathReadlink with negative bufLen: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

func TestPathOpen_FdLeakOnMemoryWriteFailure(t *testing.T) {
	rt := testResourceTable(t, file("test.txt", "content"))

	// Use a tiny memory (1 page = 64KiB). We'll set newFdPtr to an address
	// beyond memory bounds to trigger StoreUint32 failure.
	memory := testMemory(1)

	fdCountBefore := len(rt.fds)

	// Write path into memory
	path := []byte("test.txt")
	if err := memory.Set(0, 0, path); err != nil {
		t.Fatalf("failed to write path: %v", err)
	}

	// Use an out-of-bounds newFdPtr so StoreUint32 fails after the file is opened.
	outOfBoundsPtr := int32(math.MaxUint16 * 2) // beyond 1 page memory
	result := rt.pathOpen(
		memory,
		3,                          // preopened dir fd
		lookupFlagsSymlinkFollow,   // dirflags
		0,                          // pathPtr
		int32(len(path)),           // pathLen
		0,                          // oflags
		int64(DefaultDirRights),    // rightsBase
		int64(DefaultDirRights),    // rightsInheriting
		0,                          // fdflags
		outOfBoundsPtr,             // newFdPtr — out of bounds
	)
	if result != errnoFault {
		t.Fatalf("pathOpen with OOB newFdPtr: got errno %d, want %d (EFAULT)", result, errnoFault)
	}

	fdCountAfter := len(rt.fds)
	if fdCountAfter != fdCountBefore {
		t.Errorf("fd leak: had %d fds before, now have %d", fdCountBefore, fdCountAfter)
	}
}

func TestReaddir_NegativeBufLenReturnsError(t *testing.T) {
	rt := testResourceTable(t, dir("subdir"))
	memory := testMemory(1)

	subdirFdIndex, errCode := rt.allocateFd(
		mustOpen(t, rt, memory, 3, "subdir"),
		DefaultDirRights,
		DefaultDirInheritingRights,
		0,
	)
	if errCode != errnoSuccess {
		t.Fatalf("failed to allocate subdir fd: %d", errCode)
	}

	// Negative bufLen should return an error, not panic.
	result := rt.readdir(memory, subdirFdIndex, 0, -1, 0, 300)
	if result != errnoInval {
		t.Errorf("readdir with negative bufLen: got errno %d, want %d (EINVAL)", result, errnoInval)
	}
}

// mustOpen opens a path under a preopened directory fd via pathOpen and returns
// the opened *os.File. It fails the test if any step fails.
func mustOpen(t *testing.T, rt *wasiResourceTable, memory *epsilon.Memory, dirFdIndex int32, path string) *os.File {
	t.Helper()
	fd, errCode := rt.getDir(dirFdIndex, RightsPathOpen)
	if errCode != errnoSuccess {
		t.Fatalf("getDir failed: %d", errCode)
	}
	f, err := openat(fd.file, path, false, int32(oFlagsDirectory), 0, uint64(RightsFdRead|RightsFdReaddir))
	if err != nil {
		t.Fatalf("openat %q failed: %v", path, err)
	}
	return f
}
