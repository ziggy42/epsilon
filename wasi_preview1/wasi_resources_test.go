// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wasi_preview1

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
	"github.com/ziggy42/epsilon/internal/wabt"
)

func createModuleInstance(t *testing.T) *epsilon.ModuleInstance {
	t.Helper()
	wat := fmt.Sprintf(`(module (memory (export "%s") 1))`, WASIMemoryExportName)
	wasmBin, err := wabt.Wat2Wasm(wat)
	if err != nil {
		t.Fatalf("Failed to convert WAT to WASM: %v", err)
	}

	instance, err := epsilon.NewRuntime().
		InstantiateModuleWithImports(bytes.NewReader(wasmBin), nil)
	if err != nil {
		t.Fatalf("Failed to instantiate minimal module: %v", err)
	}
	return instance
}

func createSocketPair(
	t *testing.T,
	wasiName, hostName string,
) (*os.File, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("Socketpair failed: %v", err)
	}

	wasiFile := os.NewFile(uintptr(fds[0]), wasiName)
	hostFile := os.NewFile(uintptr(fds[1]), hostName)
	return wasiFile, hostFile
}

func createWasiModuleWithPreopen(
	t *testing.T,
	file *os.File,
	guestPath string,
) *WasiModule {
	t.Helper()

	preopen := WasiPreopen{
		File:             file,
		GuestPath:        guestPath,
		Rights:           0xffffff, // Enable everything
		RightsInheriting: 0xffffff,
	}

	wasiMod, err := NewWasiModule(nil, nil, []WasiPreopen{preopen})
	if err != nil {
		t.Fatalf("NewWasiModule failed: %v", err)
	}
	return wasiMod
}

func TestWasiSocketSend(t *testing.T) {
	instance := createModuleInstance(t)
	wasiFile, hostFile := createSocketPair(t, "wasi_socket", "host_socket")
	defer wasiFile.Close()
	defer hostFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, wasiFile, "socket")
	const socketFd = 3 // After stdin, stdout, stderr

	mem, _ := instance.GetMemory(WASIMemoryExportName)
	payload := []byte("payload")
	mem.Set(0, 0, payload)                        // The data to send
	mem.StoreUint32(0, 100, 0)                    // Send input data pointer
	mem.StoreUint32(0, 104, uint32(len(payload))) // Send input data length

	errCode := wasiMod.fs.sockSend(instance, socketFd, 100, 1, 0, 200)
	if errCode != errnoSuccess {
		t.Errorf("sockSend failed: %d", errCode)
	}

	buf := make([]byte, 1024)
	n, err := hostFile.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Errorf("Got %q, want %q", buf[:n], payload)
	}
}

func TestWasiSocketReceive(t *testing.T) {
	instance := createModuleInstance(t)
	wasiFile, hostFile := createSocketPair(t, "wasi_socket", "host_socket")
	defer wasiFile.Close()
	defer hostFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, wasiFile, "socket")
	const socketFd = 3 // After stdin, stdout, stderr

	payload := "payload"
	if _, err := hostFile.Write([]byte(payload)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	mem, _ := instance.GetMemory(WASIMemoryExportName)
	mem.StoreUint32(0, 300, 400) // Read input data pointer
	mem.StoreUint32(0, 304, 100) // Read input data length

	errCode := wasiMod.fs.sockRecv(instance, socketFd, 300, 1, 0, 500, 504)
	if errCode != errnoSuccess {
		t.Errorf("sockRecv failed: %d", errCode)
	}

	receivedLen, _ := mem.LoadUint32(0, 500)
	if receivedLen != uint32(len(payload)) {
		t.Errorf("Read len %d, want %d", receivedLen, len(payload))
	}
	data, _ := mem.Get(0, 400, uint32(len(payload)))
	if string(data) != payload {
		t.Errorf("Read content %q, want %q", data, payload)
	}
}

func TestWasiSocketAccept(t *testing.T) {
	instance := createModuleInstance(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()
	listenerFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File failed: %v", err)
	}
	defer listenerFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, listenerFile, "listener")
	const listenerFd = 3

	// Start a dialer in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			conn.Close()
		}
	}()

	mem, _ := instance.GetMemory(WASIMemoryExportName)
	outputFdPtr := uint32(100)

	errCode := wasiMod.fs.sockAccept(instance, listenerFd, 0, int32(outputFdPtr))
	if errCode != errnoSuccess {
		t.Errorf("sockAccept failed: %d", errCode)
	}

	newFdIdx, _ := mem.LoadUint32(0, outputFdPtr)
	if wasiMod.fs.close(int32(newFdIdx)) != errnoSuccess {
		t.Errorf("Failed to close accepted socket")
	}
}

func TestWasiSocketShutdown(t *testing.T) {
	wasiFile, hostFile := createSocketPair(t, "wasi_shutdown", "host_shutdown")
	defer wasiFile.Close()
	defer hostFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, wasiFile, "socket")
	const socketFd = 3

	errCode := wasiMod.fs.sockShutdown(socketFd, shutWr)
	if errCode != errnoSuccess {
		t.Errorf("sockShutdown failed: %d", errCode)
	}

	buf := make([]byte, 10)
	n, err := hostFile.Read(buf)
	if n != 0 || (err != nil && err != io.EOF) {
		t.Fatalf("Read %d bytes, want 0", n)
	}
}

func TestPathOpen_Normal(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("Hello, WASI!")
	path := "hello.txt"
	err := os.WriteFile(filepath.Join(tmpDir, path), content, 0666)
	if err != nil {
		t.Fatal(err)
	}

	dirFile, err := os.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer dirFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, dirFile, ".")
	const dirFd = 3

	// Write the path to memory
	instance := createModuleInstance(t)
	mem, _ := instance.GetMemory(WASIMemoryExportName)
	pathPtr := uint32(100)
	newFdPtr := uint32(200)
	mem.Set(0, pathPtr, []byte(path)) // Path at offset 100

	errCode := wasiMod.fs.pathOpen(
		instance,
		dirFd,
		0,                // dirflags
		int32(pathPtr),   // pathPtr
		int32(len(path)), // pathLen
		0,                // oflags
		rightsAll,        // rightsBase
		rightsAll,        // rightsInheriting
		0,                // fdflags
		int32(newFdPtr),  // newFdPtr
	)
	if errCode != errnoSuccess {
		t.Errorf("pathOpen failed: %d", errCode)
	}

	// Prepare the iovec to read the content
	iovecPtr := uint32(300)
	bufPtr := uint32(400)
	mem.StoreUint32(0, iovecPtr, bufPtr)
	mem.StoreUint32(0, iovecPtr+4, 100)

	// Read the content
	nPtr := uint32(500)
	newFdIdx, _ := mem.LoadUint32(0, newFdPtr)
	errCode = wasiMod.fs.read(
		instance,
		int32(newFdIdx),
		int32(iovecPtr),
		1,
		int32(nPtr),
	)
	if errCode != errnoSuccess {
		t.Errorf("read failed: %d", errCode)
	}

	nRead, _ := mem.LoadUint32(0, nPtr)
	if nRead != uint32(len(content)) {
		t.Errorf("Read %d bytes, want %d", nRead, len(content))
	}

	readContent, _ := mem.Get(0, bufPtr, nRead)
	if !bytes.Equal(readContent, content) {
		t.Errorf("Read content %q, want %q", readContent, content)
	}

	if wasiMod.fs.close(int32(newFdIdx)) != errnoSuccess {
		t.Errorf("Failed to close opened file")
	}
}

// Note that this test is NOT enough to test the sandboxing.
func TestPathOpen_SymlinkEscape(t *testing.T) {
	// Structure:
	// tmp/
	//   outside.txt
	//   safe/
	//     escape -> ../outside.txt
	tmpDir := t.TempDir()
	outsidePath := filepath.Join(tmpDir, "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	safeDir := filepath.Join(tmpDir, "safe")
	if err := os.Mkdir(safeDir, 0755); err != nil {
		t.Fatal(err)
	}
	escapePath := filepath.Join(safeDir, "escape")
	if err := os.Symlink("../outside.txt", escapePath); err != nil {
		t.Fatal(err)
	}

	dirFile, err := os.Open(safeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer dirFile.Close()

	wasiMod := createWasiModuleWithPreopen(t, dirFile, ".")
	const dirFd = 3

	instance := createModuleInstance(t)
	mem, _ := instance.GetMemory(WASIMemoryExportName)
	path := "escape"
	pathPtr := uint32(100)
	mem.Set(0, pathPtr, []byte(path))
	newFdPtr := uint32(200)

	errCode := wasiMod.fs.pathOpen(
		instance,
		dirFd,
		0,                // dirflags
		int32(pathPtr),   // pathPtr
		int32(len(path)), // pathLen
		0,                // oflags
		rightsAll,        // rightsBase
		rightsAll,        // rightsInheriting
		0,                // fdflags
		int32(newFdPtr),  // newFdPtr
	)

	if errCode == errnoSuccess {
		t.Errorf("pathOpen should have failed for symlink escape, but succeeded")
	}
}
