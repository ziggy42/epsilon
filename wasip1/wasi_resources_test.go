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
	"encoding/binary"
	"net"
	"testing"
	"time"
	"unsafe"

	"github.com/ziggy42/epsilon/epsilon"
)

func TestRightsEscalation_FdSync(t *testing.T) {
	_, dirFd := testFS(t, file("test.txt", "hello"))
	defer dirFd.Close()

	// Open file WITH RightsFdRead but WITHOUT RightsFdSync
	f, err := openat(dirFd, "test.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat failed: %v", err)
	}
	defer f.Close()

	rt := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{
			3: {
				file:     f,
				fileType: fileTypeRegularFile,
				rights:   RightsFdRead,
			},
		},
	}

	// This should fail because sync() checks rights
	errno := rt.sync(3, RightsFdSync)
	if errno != errnoNotCapable {
		t.Errorf("expected errnoNotCapable (76), got %d", errno)
	}
}

func TestRightsEscalation_FdFilestatGet(t *testing.T) {
	_, dirFd := testFS(t, file("test.txt", "hello"))
	defer dirFd.Close()

	f, err := openat(dirFd, "test.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat failed: %v", err)
	}
	defer f.Close()

	memory := epsilon.NewMemory(epsilon.MemoryType{Limits: epsilon.Limits{Min: 1}})
	rt := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{
			3: {
				file:     f,
				fileType: fileTypeRegularFile,
				rights:   RightsFdRead, // Missing RightsFdFilestatGet
			},
		},
	}

	errno := rt.getFileStat(memory, 3, 0)
	if errno != errnoNotCapable {
		t.Errorf("expected errnoNotCapable (76), got %d", errno)
	}
}

func TestRightsEscalation_SockShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	file, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("failed to get file from listener: %v", err)
	}
	defer file.Close()

	rt := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{
			3: {
				file:     file,
				fileType: fileTypeSocketStream,
				rights:   0, // No rights
			},
		},
	}

	// This should fail because sockShutdown checks rights
	errno := rt.sockShutdown(3, shutRdWr)
	if errno != errnoNotCapable {
		t.Errorf("expected errnoNotCapable (76), got %d", errno)
	}
}

func TestRightsEscalation_SockAccept_Inheritance(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	// Connect to it so accept returns
	go func() {
		conn, _ := net.Dial("tcp", ln.Addr().String())
		if conn != nil {
			conn.Close()
		}
	}()

	file, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("failed to get file from listener: %v", err)
	}
	defer file.Close()

	memory := epsilon.NewMemory(epsilon.MemoryType{Limits: epsilon.Limits{Min: 1}})
	
	// Define a custom right to see if it's NOT inherited if not in rightsInheriting
	const customRight = RightsFdRead
	
	rt := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{
			3: {
				file:             file,
				fileType:         fileTypeSocketStream,
				rights:           rightsAll,
				rightsInheriting: customRight, 
			},
		},
	}

	errno := rt.sockAccept(memory, 3, 0, 0)
	if errno != errnoSuccess {
		t.Fatalf("sockAccept failed: %d", errno)
	}

	newFdPtr, _ := memory.LoadUint32(0, 0)
	newFd := rt.fds[int32(newFdPtr)]
	
	// The new FD should ONLY have customRight (RightsFdRead) + maybe some defaults?
	// But the vulnerability is that it gets connectedSocketDefaultRights regardless.
	
	if (newFd.rights & ^customRight) != 0 {
		t.Errorf("new FD has rights it should not have: %x (expected only %x or subset)", newFd.rights, customRight)
	}
}

func TestPollOneoff_ClockOverflow(t *testing.T) {
	memory := epsilon.NewMemory(epsilon.MemoryType{Limits: epsilon.Limits{Min: 1}})
	w := &WasiModule{
		monotonicClockStart: time.Now(), // now - start = now - 1
	}

	// Create a clock subscription with a past timeout
	
	sub := subscription{
		userData: 0x1234,
		subscriptionType: eventTypeClock,
	}
	clockSub := subscriptionClock{
		clockId: clockMonotonic,
		timeout: 100,
		flags: subclockFlagsSubscriptionClockAbstime,
	}
	binary.LittleEndian.PutUint32(sub.body[0:4], clockSub.clockId)
	binary.LittleEndian.PutUint64(sub.body[8:16], clockSub.timeout)
	binary.LittleEndian.PutUint16(sub.body[24:26], clockSub.flags)

	// Write subscription to memory
	subSize := uint32(unsafe.Sizeof(subscription{}))
	subBytes := make([]byte, subSize)
	binary.LittleEndian.PutUint64(subBytes[0:8], sub.userData)
	subBytes[8] = sub.subscriptionType
	copy(subBytes[16:], sub.body[:])
	
	memory.Set(0, 0, subBytes)

	// Call pollOneoff
	start := time.Now()
	// We use a timeout to avoid hanging the test if it fails
	done := make(chan int32)
	go func() {
		done <- w.pollOneoff(memory, 0, 128, 1, 256)
	}()

	select {
	case errno := <-done:
		if errno != errnoSuccess {
			t.Errorf("pollOneoff failed with errno %d", errno)
		}
		duration := time.Since(start)
		// If it overflows, it sleeps for ~292 years.
		// If it's fixed, it should return immediately (because timeout is in the past).
		if duration > 100*time.Millisecond {
			t.Errorf("pollOneoff took too long: %v (likely overflow)", duration)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pollOneoff timed out (likely overflow causing indefinite sleep)")
	}
}
