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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"syscall"
)

// ProcExitError is an error that signals that the process should exit with the
// given code. It is used to implement proc_exit.
type ProcExitError struct {
	Code int32
}

func (e *ProcExitError) Error() string {
	return fmt.Sprintf("proc_exit: %d", e.Code)
}

// WASI Application ABI constants.
// See: https://github.com/WebAssembly/WASI/blob/wasi-0.1/application-abi.md
const (
	MemoryExportName  = "memory"
	StartFunctionName = "_start"
	ModuleName        = "wasi_snapshot_preview1"
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
	RightsSockAccept           int64 = 1 << 29
)

// WasiPreopen represents a pre-opened FileSystem to be provided to WASI as a
// directory capability mounted at GuestPath.
//
// The WasiModule (or builder) takes ownership of FS and closes it when
// appropriate.
type WasiPreopen struct {
	FS               FileSystem
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

// FileType is a WASI Preview 1 filetype, as reported in FileStat and DirEntry.
type FileType uint8

const (
	FileTypeUnknown         FileType = 0
	FileTypeBlockDevice     FileType = 1
	FileTypeCharacterDevice FileType = 2
	FileTypeDirectory       FileType = 3
	FileTypeRegularFile     FileType = 4
	FileTypeSocketDgram     FileType = 5
	FileTypeSocketStream    FileType = 6
	FileTypeSymbolicLink    FileType = 7
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

// DirEntry is a single directory entry returned by FileSystem.ReadDir, which is
// expected to include the synthetic "." and ".." entries required by
// fd_readdir.
type DirEntry struct {
	Name     string
	FileType FileType
	Ino      uint64
}

// bytes serializes the DirEntry to the WASI dirent layout.
// The nextCookie parameter is the cookie for the next entry in the directory.
func (d *DirEntry) bytes(nextCookie uint64) []byte {
	nameLen := len(d.Name)
	buf := make([]byte, 24+nameLen)
	binary.LittleEndian.PutUint64(buf[0:8], nextCookie)
	binary.LittleEndian.PutUint64(buf[8:16], d.Ino)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(nameLen))
	buf[20] = uint8(d.FileType)
	copy(buf[24:], d.Name)
	return buf
}

// FileStat is the WASI FileStat structure (64 bytes when serialized). Times are
// nanoseconds since the Unix epoch.
type FileStat struct {
	Dev      uint64   // device ID
	Ino      uint64   // inode
	FileType FileType // file type
	Nlink    uint64   // number of hard links
	Size     uint64   // file size in bytes
	Atim     uint64   // access time
	Mtim     uint64   // modification time
	Ctim     uint64   // status change time
}

// bytes serializes the FileStat to the WASI memory layout (64 bytes).
func (fs *FileStat) bytes() [64]byte {
	var buf [64]byte
	binary.LittleEndian.PutUint64(buf[0:8], fs.Dev)
	binary.LittleEndian.PutUint64(buf[8:16], fs.Ino)
	buf[16] = uint8(fs.FileType)
	// buf[17:24] padding (already zero)
	binary.LittleEndian.PutUint64(buf[24:32], fs.Nlink)
	binary.LittleEndian.PutUint64(buf[32:40], fs.Size)
	binary.LittleEndian.PutUint64(buf[40:48], fs.Atim)
	binary.LittleEndian.PutUint64(buf[48:56], fs.Mtim)
	binary.LittleEndian.PutUint64(buf[56:64], fs.Ctim)
	return buf
}

// File is an open file, directory, or socket handle used by the WASI
// implementation. It extends the read-only io/fs.File with the write, seek,
// and metadata operations WASI requires. The default implementation
// (hostFileSystem) is backed by *os.File; alternative backends may provide
// virtualized or in-memory handles.
type File interface {
	fs.File // Stat() (fs.FileInfo, error); Read([]byte) (int, error); Close() error
	io.Writer
	io.Seeker
	io.ReaderAt
	io.WriterAt

	Truncate(size int64) error
	Sync() error

	// FileStat returns the metadata of the open handle (fd_filestat_get).
	FileStat() (FileStat, error)
	// SetFlags updates the O_APPEND/O_NONBLOCK status flags of the open handle
	// (fd_fdstat_set_flags).
	SetFlags(appendFlag, nonblock bool) error
	// SetTimes sets the access/modification times of the open handle
	// (fd_filestat_set_times). atim and mtim are nanoseconds; fstFlags selects
	// which times to set and how.
	SetTimes(atim, mtim int64, fstFlags int32) error

	// Accept accepts a connection on a socket handle (sock_accept).
	Accept() (File, error)
	// Shutdown shuts down a socket handle (sock_shutdown).
	Shutdown(how int32) error
}

// FileSystem is a sandboxed, directory-rooted view of a filesystem. Every name
// is resolved relative to the root and may never escape it, mirroring the WASI
// directory-capability model where each directory descriptor is its own root.
// The default host implementation resolves every path relative to the root
// using *at syscalls, providing traversal-resistant ("..") and symlink-escape
// protections.
type FileSystem interface {
	fs.FS // Open(name string) (fs.File, error)

	// OpenFile opens a regular file within the root. followSymlink controls
	// whether a symlink in the final path component is followed.
	OpenFile(name string, followSymlink bool, oflags, fdflags int32, rights uint64) (File, error)
	// OpenRoot opens a subdirectory as a new sandboxed FileSystem.
	OpenRoot(name string) (FileSystem, error)

	Mkdir(name string, perm fs.FileMode) error
	// Stat returns the metadata of name. followSymlink controls whether a
	// symlink in the final path component is followed.
	Stat(name string, followSymlink bool) (FileStat, error)
	Readlink(name string) (string, error)
	Symlink(target, name string) error
	// Unlink removes a non-directory entry (EISDIR on a directory).
	Unlink(name string) error
	// Rmdir removes an empty directory (ENOTDIR on a non-directory).
	Rmdir(name string) error
	Rename(newDir FileSystem, oldName, newName string) error
	Link(oldName string, followSymlink bool, newDir FileSystem, newName string) error
	// Chtimes sets the access/modification times of name. followSymlink controls
	// whether a symlink in the final path component is followed.
	Chtimes(name string, atim, mtim int64, fstFlags int32, followSymlink bool) error
	// ReadDir reads the entries of the root directory, including the synthetic
	// "." and ".." entries required by fd_readdir.
	ReadDir() ([]DirEntry, error)

	// Handle returns the directory's own open handle, used for fd-level
	// operations (fd_fdstat_get, fd_readdir, fd_filestat_get, fd_sync).
	Handle() File
	Close() error
}

// mapError maps Go, io/fs, and syscall errors to a WASI errno.
func mapError(err error) int32 {
	if err == nil {
		return errnoSuccess
	}

	// Match the specific syscall.Errno before the broader io/fs sentinels:
	// syscall.Errno.Is maps several distinct errnos onto the same fs sentinel
	// (e.g. both EEXIST and ENOTEMPTY satisfy fs.ErrExist), which would
	// otherwise lose the distinction WASI requires.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EACCES:
			return errnoAcces
		case syscall.EPERM:
			return errnoPerm
		case syscall.ENOENT:
			return errnoNoEnt
		case syscall.EEXIST:
			return errnoExist
		case syscall.EISDIR:
			return errnoIsDir
		case syscall.ENOTDIR:
			return errnoNotDir
		case syscall.EINVAL:
			return errnoInval
		case syscall.ENOTEMPTY:
			return errnoNotEmpty
		case syscall.ELOOP:
			return errnoLoop
		case syscall.EBADF:
			return errnoBadF
		case syscall.EMFILE, syscall.ENFILE:
			return errnoNFile
		case syscall.ENAMETOOLONG:
			return errnoNameTooLong
		case syscall.EPIPE:
			return errnoPipe
		case syscall.EAGAIN:
			return errnoAgain
		}
	}

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errnoNoEnt
	case errors.Is(err, fs.ErrExist):
		return errnoExist
	case errors.Is(err, fs.ErrPermission):
		return errnoAcces
	case errors.Is(err, fs.ErrInvalid):
		return errnoInval
	}

	return errnoNotCapable
}
