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

package wasip1

import (
	"encoding/binary"
	"os"
)

const (
	WASIMemoryExportName = "memory"
	WASIModuleName       = "wasi_snapshot_preview1"
)

const rightsAll int64 = ^0

// DefaultDirRights are the rights inherent to the directory handle itself. We
// exclude rights that imply reading/writing "data" from the directory stream
// itself (which is not how WASI reads directories, it uses fd_readdir) or
// seeking.
const DefaultDirRights int64 = rightsAll &^
	(RightsFdRead | RightsFdWrite | RightsFdSeek | RightsFdTell)

// DefaultDirInheritingRights are the rights that newly opened files/directories
// will inherit from this directory. This effectively allows full access to
// children.
const DefaultDirInheritingRights int64 = rightsAll

const (
	RightsFdDatasync           int64 = 1 << 0
	RightsFdRead               int64 = 1 << 1
	RightsFdSeek               int64 = 1 << 2
	RightsFdFdstatSetFlags     int64 = 1 << 3
	RightsFdSync               int64 = 1 << 4
	RightsFdTell               int64 = 1 << 5
	RightsFdWrite              int64 = 1 << 6
	RightsFdAdvise             int64 = 1 << 7
	RightsFdAllocate           int64 = 1 << 8
	RightsPathCreateDirectory  int64 = 1 << 9
	RightsPathCreateFile       int64 = 1 << 10
	RightsPathLinkSource       int64 = 1 << 11
	RightsPathLinkTarget       int64 = 1 << 12
	RightsPathOpen             int64 = 1 << 13
	RightsFdReaddir            int64 = 1 << 14
	RightsPathReadlink         int64 = 1 << 15
	RightsPathRenameSource     int64 = 1 << 16
	RightsPathRenameTarget     int64 = 1 << 17
	RightsPathFilestatGet      int64 = 1 << 18
	RightsPathFilestatSetSize  int64 = 1 << 19
	RightsPathFilestatSetTimes int64 = 1 << 20
	RightsFdFilestatGet        int64 = 1 << 21
	RightsFdFilestatSetSize    int64 = 1 << 22
	RightsFdFilestatSetTimes   int64 = 1 << 23
	RightsPathSymlink          int64 = 1 << 24
	RightsPathRemoveDirectory  int64 = 1 << 25
	RightsPathUnlinkFile       int64 = 1 << 26
	RightsPollFdReadwrite      int64 = 1 << 27
	RightsSockShutdown         int64 = 1 << 28
)

// WasiPreopen represents a pre-opened os.File to be provided to WASI.
//
// If the WasiModule is successfully created, it takes ownership of the File and
// will close it when appropriate. If creation fails, ownership stays with the
// caller.
type WasiPreopen struct {
	File             *os.File
	GuestPath        string
	Rights           int64
	RightsInheriting int64
}

// See github.com/WebAssembly/WASI/blob/wasi-0.1/preview1/witx/typenames.witx
// for a lot more error codes.
const (
	errnoSuccess     int32 = 0  // No error occurred.
	errnoAcces       int32 = 2  // Permission denied.
	errnoAgain       int32 = 6  // Try again.
	errnoBadF        int32 = 8  // Bad file descriptor.
	errnoExist       int32 = 20 // File exists.
	errnoFault       int32 = 21 // Bad address.
	errnoInval       int32 = 28 // Invalid argument.
	errnoIO          int32 = 29 // I/O error.
	errnoIsDir       int32 = 31 // Is a directory.
	errnoLoop        int32 = 32 // Too many levels of symbolic links.
	errnoNameTooLong int32 = 37 // Filename too long.
	errnoNFile       int32 = 41 // Too many files open in system.
	errnoNoEnt       int32 = 44 // No such file or directory.
	errnoNotDir      int32 = 54 // Not a directory or symbolic link.
	errnoNotEmpty    int32 = 55 // Directory not empty.
	errnoNotSock     int32 = 57 // Not a socket.
	errnoNotSup      int32 = 58 // Not supported.
	errnoPerm        int32 = 63 // Operation not permitted.
	errnoPipe        int32 = 64 // Broken pipe.
	errnoNotCapable  int32 = 76 // Extension: Capabilities insufficient.
)

const (
	fileTypeUnknown         uint8 = 0
	fileTypeBlockDevice     uint8 = 1
	fileTypeCharacterDevice uint8 = 2
	fileTypeDirectory       uint8 = 3
	fileTypeRegularFile     uint8 = 4
	fileTypeSocketDgram     uint8 = 5
	fileTypeSocketStream    uint8 = 6
	fileTypeSymbolicLink    uint8 = 7
)

const preopenTypeDir uint8 = 0

const (
	whenceSet uint8 = 0 // Seek relative to start-of-file.
	whenceCur uint8 = 1 // Seek relative to current position.
	whenceEnd uint8 = 2 // Seek relative to end-of-file.
)

const (
	oFlagsCreat     uint16 = 1 << 0
	oFlagsDirectory uint16 = 1 << 1
	oFlagsExcl      uint16 = 1 << 2
	oFlagsTrunc     uint16 = 1 << 3
)

const (
	fstFlagsAtim    int32 = 1 << 0
	fstFlagsAtimNow int32 = 1 << 1
	fstFlagsMtim    int32 = 1 << 2
	fstFlagsMtimNow int32 = 1 << 3
)

const (
	fdFlagsAppend   uint16 = 1 << 0
	fdFlagsDsync    uint16 = 1 << 1
	fdFlagsNonblock uint16 = 1 << 2
	fdFlagsRsync    uint16 = 1 << 3
	fdFlagsSync     uint16 = 1 << 4
)

const (
	shutRd   = 1
	shutWr   = 2
	shutRdWr = 3
)

const lookupFlagsSymlinkFollow int32 = 1 << 0

// dirEntry represents a directory entry for fd_readdir.
type dirEntry struct {
	name     string
	fileType int8
	ino      uint64
}

// bytes serializes the dirEntry to the WASI dirent layout.
// The cookie parameter is the position marker for this entry.
func (d *dirEntry) bytes(cookie uint64) []byte {
	nameLen := len(d.name)
	buf := make([]byte, 24+nameLen)
	binary.LittleEndian.PutUint64(buf[0:8], cookie)
	binary.LittleEndian.PutUint64(buf[8:16], d.ino)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(nameLen))
	buf[20] = uint8(d.fileType)
	copy(buf[24:], d.name)
	return buf
}

// filestat represents the WASI filestat structure (64 bytes total).
type filestat struct {
	dev      uint64 // offset 0:  device ID
	ino      uint64 // offset 8:  inode
	filetype int8   // offset 16: file type (1 byte, 7 padding)
	nlink    uint64 // offset 24: number of hard links
	size     uint64 // offset 32: file size in bytes
	atim     uint64 // offset 40: access time (nanoseconds)
	mtim     uint64 // offset 48: modification time (nanoseconds)
	ctim     uint64 // offset 56: status change time (nanoseconds)
}

// bytes serializes the filestat to the WASI memory layout (64 bytes).
func (fs *filestat) bytes() [64]byte {
	var buf [64]byte
	binary.LittleEndian.PutUint64(buf[0:8], fs.dev)
	binary.LittleEndian.PutUint64(buf[8:16], fs.ino)
	buf[16] = uint8(fs.filetype)
	// buf[17:24] padding (already zero)
	binary.LittleEndian.PutUint64(buf[24:32], fs.nlink)
	binary.LittleEndian.PutUint64(buf[32:40], fs.size)
	binary.LittleEndian.PutUint64(buf[40:48], fs.atim)
	binary.LittleEndian.PutUint64(buf[48:56], fs.mtim)
	binary.LittleEndian.PutUint64(buf[56:64], fs.ctim)
	return buf
}
