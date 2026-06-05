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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// defaultFileMode is the default permission mode for newly created files.
const defaultFileMode = 0o600

// utimeNow and utimeOmit are special values for Timespec.Nsec.
var (
	utimeNow  int64 // Set to current time
	utimeOmit int64 // Don't change
)

func init() {
	switch runtime.GOOS {
	case "linux":
		// https://github.com/torvalds/linux/blob/master/include/linux/stat.h#L15-L16
		utimeNow = (1 << 30) - 1
		utimeOmit = (1 << 30) - 2
	case "darwin":
		// https://github.com/apple/darwin-xnu/blob/main/bsd/sys/stat.h#L575-L576
		utimeNow = -1
		utimeOmit = -2
	case "openbsd":
		// https://github.com/openbsd/src/blob/master/sys/sys/stat.h#L188-L189
		utimeNow = -2
		utimeOmit = -1
	default:
		// Most (all?) other UNIXes use -1/-2, e.g. FreeBSD:
		// https://github.com/freebsd/freebsd-src/blob/main/sys/sys/stat.h#L359-L360
		utimeNow = -1
		utimeOmit = -2
	}
}

// mkdirat creates a directory relative to a directory.
// This is similar to mkdirat in POSIX.
//
// Parameters:
//   - root: the os.Root to create relative to
//   - name: the relative path of the directory to create
//   - mode: the file mode bits for the new directory
//
// Returns an error if the operation fails.
func mkdirat(root *os.Root, name string, mode uint32) error {
	if name == "." || !filepath.IsLocal(name) {
		return os.ErrInvalid
	}
	return root.Mkdir(name, fs.FileMode(mode))
}

// stat returns the filestat of a file or directory relative to a directory.
// This is similar to fstatat in POSIX.
//
// Parameters:
//   - root: the os.Root to stat relative to
//   - name: the relative path of the file or directory to inspect
//   - followSymlinks: whether to follow final symlink
//
// Returns the filestat and an error if the operation fails.
func stat(
	root *os.Root,
	name string,
	followSymlinks bool,
) (filestat, error) {
	atFlags := unix.AT_SYMLINK_NOFOLLOW
	if followSymlinks {
		// os.Root.Stat gates the follow-mode Fstatat: it fails if the followed
		// leaf escapes the root. No-follow needs no gate (NOFOLLOW never
		// dereferences the leaf).
		if _, err := root.Stat(name); err != nil {
			return filestat{}, err
		}
		atFlags = 0
	}

	parent, base, err := leafParent(root, name)
	if err != nil {
		return filestat{}, err
	}
	defer parent.Close()

	var statBuf unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), base, &statBuf, atFlags); err != nil {
		return filestat{}, err
	}
	return statFromUnix(&statBuf), nil
}

// fdstat returns a filestat from a file.
func fdstat(file *os.File) (filestat, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return filestat{}, err
	}
	return statFromUnix(&stat), nil
}

// readDirEntries reads directory entries from a directory, returning synthetic
// "." and ".." entries followed by actual directory content. This is required
// by WASI fd_readdir specification.
//
// For each entry, the inode is obtained via Fstatat. The "." entry uses the
// directory's own inode; ".." uses inode 0 because we cannot safely access
// the parent directory due to sandboxing.
func readDirEntries(dir *os.File) ([]dirEntry, error) {
	// Get the directory's own inode for "."
	var dirStat unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &dirStat); err != nil {
		return nil, err
	}

	// Seek to start to ensure we read all entries
	if _, err := dir.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	result := make([]dirEntry, 0, len(entries)+2)
	result = append(
		result,
		dirEntry{name: ".", fileType: int8(fileTypeDirectory), ino: dirStat.Ino},
		dirEntry{name: "..", fileType: int8(fileTypeDirectory), ino: 0},
	)

	dirFd := int(dir.Fd())
	for _, entry := range entries {
		var statBuf unix.Stat_t
		err := unix.Fstatat(dirFd, entry.Name(), &statBuf, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil {
			return nil, err
		}

		result = append(result, dirEntry{
			name:     entry.Name(),
			fileType: fileTypeFromMode(uint32(statBuf.Mode)),
			ino:      statBuf.Ino,
		})
	}

	return result, nil
}

// setFdFlags sets flags on a file.
func setFdFlags(file *os.File, fdFlags int32) error {
	var osFlags int
	if fdFlags&int32(fdFlagsAppend) != 0 {
		osFlags |= unix.O_APPEND
	}
	if fdFlags&int32(fdFlagsNonblock) != 0 {
		osFlags |= unix.O_NONBLOCK
	}

	_, err := unix.FcntlInt(file.Fd(), unix.F_SETFL, osFlags)
	return err
}

// utimes sets the access and modification times of a file or directory.
// This is similar to utimensat in POSIX.
//
// Parameters:
//   - root: the os.Root to resolve relative to
//   - name: the relative path of the file or directory
//   - atim: access time in nanoseconds (used if fstFlagsAtim is set)
//   - mtim: modification time in nanoseconds (used if fstFlagsMtim is set)
//   - fstFlags: bitmask controlling which times to set and how
//   - followSymlinks: whether to follow the final symlink
//
// Returns an error if the operation fails.
func utimes(
	root *os.Root,
	name string,
	atim, mtim int64,
	fstFlags int32,
	followSymlinks bool,
) error {
	times, err := buildTimespec(atim, mtim, fstFlags)
	if err != nil {
		return err
	}

	atFlags := unix.AT_SYMLINK_NOFOLLOW
	if followSymlinks {
		// Gate the follow-mode UtimesNanoAt (which follows the leaf via the
		// kernel) so it cannot escape the root.
		if _, err := root.Stat(name); err != nil {
			return err
		}
		atFlags = 0
	}

	parent, base, err := leafParent(root, name)
	if err != nil {
		return err
	}
	defer parent.Close()

	return unix.UtimesNanoAt(int(parent.Fd()), base, times, atFlags)
}

func utimesNanoAt(file *os.File, atim, mtim int64, fstFlags int32) error {
	times, err := buildTimespec(atim, mtim, fstFlags)
	if err != nil {
		return err
	}
	// Uses /dev/fd/N to reference the open file descriptor, avoiding TOCTOU races
	// while maintaining nanosecond precision.
	path := fmt.Sprintf("/dev/fd/%d", file.Fd())
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, times, 0)
}

// writeAt writes data at the specified offset. It handles the case where the
// file was opened with O_APPEND, which normally causes os.File.WriteAt to fail.
func writeAt(
	file *os.File,
	data []byte,
	offset int64,
	hasAppendFlag bool,
) (int, error) {
	if hasAppendFlag {
		return unix.Pwrite(int(file.Fd()), data, offset)
	}
	return file.WriteAt(data, offset)
}

// linkat creates a hard link to an existing file.
// This is similar to linkat in POSIX.
//
// Parameters:
//   - oldRoot: the os.Root for the source path resolution
//   - oldName: the relative path of the source file
//   - followSymlinks: whether to follow symlinks when resolving oldName
//   - newRoot: the os.Root for the destination path resolution
//   - newName: the relative path for the new hard link
//
// Returns an error if the operation fails.
func linkat(
	oldRoot *os.Root,
	oldName string,
	followSymlinks bool,
	newRoot *os.Root,
	newName string,
) error {
	// Hard link creation does not support directory targets (implied by trailing
	// slash)
	if strings.HasSuffix(oldName, "/") || strings.HasSuffix(newName, "/") {
		return syscall.ENOENT
	}

	// Linkat with AT_SYMLINK_FOLLOW follows the source leaf via the kernel,
	// which could escape the root. Validate through os.Root first.
	var flags int
	if followSymlinks {
		if _, err := oldRoot.Stat(oldName); err != nil {
			return err
		}
		flags = unix.AT_SYMLINK_FOLLOW
	}

	oldParent, oldBase, err := leafParent(oldRoot, oldName)
	if err != nil {
		return err
	}
	defer oldParent.Close()

	newParent, newBase, err := leafParent(newRoot, newName)
	if err != nil {
		return err
	}
	defer newParent.Close()

	return unix.Linkat(
		int(oldParent.Fd()), oldBase,
		int(newParent.Fd()), newBase,
		flags,
	)
}

// readlink reads the contents of a symbolic link.
// This is similar to readlinkat in POSIX.
//
// Parameters:
//   - root: the os.Root to resolve relative to
//   - name: the relative path of the symbolic link
//
// Returns the symlink target and an error if the operation fails.
func readlink(root *os.Root, name string) (string, error) {
	return root.Readlink(name)
}

// rmdirat removes an empty directory.
// This is similar to unlinkat(fd, path, AT_REMOVEDIR) in POSIX.
//
// Parameters:
//   - root: the os.Root to resolve relative to
//   - name: the relative path of the directory to remove
//
// Returns an error if the operation fails (e.g., ENOTEMPTY if not empty).
func rmdirat(root *os.Root, name string) error {
	// AT_REMOVEDIR yields ENOTDIR on a non-directory and ENOTEMPTY on a non-empty
	// directory, matching WASI path_remove_directory semantics.
	parent, base, err := leafParent(root, name)
	if err != nil {
		return err
	}
	defer parent.Close()
	return unix.Unlinkat(int(parent.Fd()), base, unix.AT_REMOVEDIR)
}

// renameat renames a file or directory.
// This is similar to renameat in POSIX.
//
// Parameters:
//   - oldRoot: the os.Root for the source path resolution
//   - oldName: the relative path of the source file or directory
//   - newRoot: the os.Root for the destination path resolution
//   - newName: the relative path of the destination
//
// Returns an error if the operation fails.
func renameat(
	oldRoot *os.Root,
	oldName string,
	newRoot *os.Root,
	newName string,
) error {
	oldParent, oldBase, err := leafParent(oldRoot, oldName)
	if err != nil {
		return err
	}
	defer oldParent.Close()

	newParent, newBase, err := leafParent(newRoot, newName)
	if err != nil {
		return err
	}
	defer newParent.Close()

	return unix.Renameat(
		int(oldParent.Fd()), oldBase,
		int(newParent.Fd()), newBase,
	)
}

// symlinkat creates a symbolic link.
// This is similar to symlinkat in POSIX.
//
// Parameters:
//   - target: the contents of the symbolic link (what it points to)
//   - root: the os.Root for the link path resolution
//   - name: the relative path at which to create the symlink
//
// Returns an error if the operation fails.
func symlinkat(target string, root *os.Root, name string) error {
	if strings.HasPrefix(target, "/") {
		return syscall.EPERM
	}
	if strings.HasSuffix(name, "/") {
		return syscall.ENOENT
	}
	return root.Symlink(target, name)
}

// unlinkat removes a file (but not a directory).
// This is similar to unlinkat(fd, path, 0) in POSIX.
//
// Parameters:
//   - root: the os.Root to resolve relative to
//   - name: the relative path of the file to unlink
//
// Returns EISDIR if the path refers to a directory.
func unlinkat(root *os.Root, name string) error {
	// Without AT_REMOVEDIR, unlinkat yields EISDIR/EPERM on a directory, matching
	// WASI path_unlink_file semantics.
	parent, base, err := leafParent(root, name)
	if err != nil {
		return err
	}
	defer parent.Close()
	return unix.Unlinkat(int(parent.Fd()), base, 0)
}

// openat opens a file or directory relative to a directory.
// This is similar to openat in POSIX.
//
// Parameters:
//   - root: the os.Root to open relative to
//   - name: the relative path of the file or directory to open
//   - followSymlinks: whether to follow final symlink
//   - oflags: flags determining the method by which to open the file
//   - fsRights: base rights for operations using the returned os.File
//   - fdflags: file descriptor flags
//
// A directory (requested via O_DIRECTORY or a trailing slash, or discovered
// after opening) is returned as its own sandboxed os.Root in childRoot, with
// file as its directory handle; a regular file is returned in file with
// childRoot nil.
func openat(
	root *os.Root,
	name string,
	followSymlinks bool,
	oflags, fdflags int32,
	fsRights uint64,
) (file *os.File, childRoot *os.Root, err error) {
	// A trailing slash or an explicit O_DIRECTORY means the path must be a
	// directory, which becomes its own sandboxed root.
	if oflags&int32(oFlagsDirectory) != 0 || strings.HasSuffix(name, "/") {
		// A directory cannot be opened for writing.
		if fsRights&uint64(RightsFdWrite) != 0 {
			return nil, nil, syscall.EISDIR
		}
		// Without symlink-follow, a symlink named as a directory must not be
		// followed; opening it as a directory fails.
		if !followSymlinks && isSymlinkLeaf(root, name) {
			return nil, nil, syscall.ELOOP
		}
		childRoot, file, err = openRootHandle(root.OpenRoot(name))
		return file, childRoot, err
	}

	// os.Root.OpenFile follows a final symlink within the root, so for no-follow
	// reject a symlink leaf explicitly. O_EXCL never follows.
	if !followSymlinks && oflags&int32(oFlagsExcl) == 0 && isSymlinkLeaf(root, name) {
		return nil, nil, syscall.ELOOP
	}

	file, err = root.OpenFile(name, openMode(fsRights, oflags, fdflags), defaultFileMode)
	if err != nil {
		return nil, nil, err
	}

	// A directory may be opened without O_DIRECTORY; promote it to a root so
	// subsequent path operations on the descriptor work.
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		file.Close()
		childRoot, file, err = openRootHandle(root.OpenRoot(name))
		return file, childRoot, err
	}
	return file, nil, nil
}

// isSymlinkLeaf reports whether name's final component is a symlink. Intermediate
// components are resolved within root.
func isSymlinkLeaf(root *os.Root, name string) bool {
	info, err := root.Lstat(name)
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

// accept accepts a connection on the socket file descriptor.
func accept(file *os.File) (int, error) {
	nfd, _, err := unix.Accept(int(file.Fd()))
	return nfd, err
}

// shutdown shuts down a socket.
func shutdown(file *os.File, how int32) error {
	switch how {
	case shutRd:
		return unix.Shutdown(int(file.Fd()), unix.SHUT_RD)
	case shutWr:
		return unix.Shutdown(int(file.Fd()), unix.SHUT_WR)
	case shutRdWr:
		return unix.Shutdown(int(file.Fd()), unix.SHUT_RDWR)
	default:
		return syscall.EINVAL
	}
}

// openRootHandle opens root's own "." handle, closing root on failure. It
// threads the error from os.OpenRoot so callers can write
// openRootHandle(os.OpenRoot(dir)).
func openRootHandle(root *os.Root, err error) (*os.Root, *os.File, error) {
	if err != nil {
		return nil, nil, err
	}
	handle, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	return root, handle, nil
}

// leafParent opens name's parent directory through os.Root and returns its
// handle and the single-component leaf. The caller must close parent.
func leafParent(
	root *os.Root,
	name string,
) (parent *os.File, base string, err error) {
	// Preserve a trailing slash on the leaf so the kernel enforces directory
	// semantics (e.g. unlinking "file/" yields ENOTDIR, removing "dir/" still
	// succeeds).
	trailingSlash := strings.HasSuffix(name, "/")
	dir, base := path.Split(strings.TrimRight(name, "/"))
	dir = strings.TrimSuffix(dir, "/")
	if trailingSlash {
		base += "/"
	}
	if dir == "" {
		dir = "."
	}

	// os.Root only guards the parent (dir); the leaf is passed straight to the
	// raw *at syscall, which has no sandbox awareness. A non-local leaf escapes:
	// ".." climbs above the parent, and an absolute leaf ("/") makes *at ignore
	// the dir fd and resolve from the host root. Reject anything that is not a
	// safe in-directory name. A trailing slash is kept on the leaf for the kernel
	// (filepath.IsLocal cleans it away, so "dir/" stays allowed).
	if !filepath.IsLocal(base) {
		return nil, "", syscall.EPERM
	}
	parent, err = root.Open(dir)
	if err != nil {
		return nil, "", err
	}
	return parent, base, nil
}

func openMode(rights uint64, oflags, fdflags int32) int {
	// Determine read/write mode from rights
	canRead := rights&uint64(RightsFdRead) != 0
	canWrite := rights&uint64(RightsFdWrite) != 0

	var flag int
	switch {
	case canRead && canWrite:
		flag = os.O_RDWR
	case canWrite:
		flag = os.O_WRONLY
	default:
		flag = os.O_RDONLY
	}

	// Convert WASI oflags to Unix flags
	if oflags&int32(oFlagsCreat) != 0 {
		flag |= os.O_CREATE
	}
	if oflags&int32(oFlagsDirectory) != 0 {
		flag |= syscall.O_DIRECTORY
	}
	if oflags&int32(oFlagsExcl) != 0 {
		flag |= os.O_EXCL
	}
	if oflags&int32(oFlagsTrunc) != 0 {
		flag |= os.O_TRUNC
	}

	// Convert WASI fdflags to Unix flags
	if fdflags&int32(fdFlagsAppend) != 0 {
		flag |= os.O_APPEND
	}
	if fdflags&int32(fdFlagsDsync) != 0 {
		flag |= syscall.O_DSYNC
	}
	if fdflags&int32(fdFlagsNonblock) != 0 {
		flag |= syscall.O_NONBLOCK
	}
	// fdFlagsRsync maps to O_RSYNC, which equals O_SYNC on most systems.
	if fdflags&int32(fdFlagsSync) != 0 || fdflags&int32(fdFlagsRsync) != 0 {
		flag |= syscall.O_SYNC
	}
	return flag
}

// statFromUnix converts a unix.Stat_t to a filestat.
func statFromUnix(s *unix.Stat_t) filestat {
	return filestat{
		dev:      uint64(s.Dev),
		ino:      s.Ino,
		filetype: fileTypeFromMode(uint32(s.Mode)),
		nlink:    uint64(s.Nlink),
		size:     uint64(s.Size),
		atim:     uint64(unix.TimespecToNsec(s.Atim)),
		mtim:     uint64(unix.TimespecToNsec(s.Mtim)),
		ctim:     uint64(unix.TimespecToNsec(s.Ctim)),
	}
}

// fileTypeFromMode extracts the WASI file type from a Unix mode.
func fileTypeFromMode(mode uint32) int8 {
	switch mode & unix.S_IFMT {
	case unix.S_IFBLK:
		return int8(fileTypeBlockDevice)
	case unix.S_IFCHR:
		return int8(fileTypeCharacterDevice)
	case unix.S_IFDIR:
		return int8(fileTypeDirectory)
	case unix.S_IFREG:
		return int8(fileTypeRegularFile)
	case unix.S_IFSOCK:
		return int8(fileTypeSocketStream)
	case unix.S_IFLNK:
		return int8(fileTypeSymbolicLink)
	default:
		return int8(fileTypeUnknown)
	}
}

func buildTimespec(atim, mtim int64, fstFlags int32) ([]unix.Timespec, error) {
	// ATIM and ATIM_NOW are mutually exclusive, as are MTIM and MTIM_NOW
	if (fstFlags&fstFlagsAtim != 0 && fstFlags&fstFlagsAtimNow != 0) ||
		(fstFlags&fstFlagsMtim != 0 && fstFlags&fstFlagsMtimNow != 0) {
		return nil, syscall.EINVAL
	}

	var atimSpec unix.Timespec
	switch {
	case fstFlags&fstFlagsAtimNow != 0:
		atimSpec = unix.Timespec{Nsec: utimeNow}
	case fstFlags&fstFlagsAtim != 0:
		atimSpec = unix.NsecToTimespec(atim)
	default:
		atimSpec = unix.Timespec{Nsec: utimeOmit}
	}

	var mtimSpec unix.Timespec
	switch {
	case fstFlags&fstFlagsMtimNow != 0:
		mtimSpec = unix.Timespec{Nsec: utimeNow}
	case fstFlags&fstFlagsMtim != 0:
		mtimSpec = unix.NsecToTimespec(mtim)
	default:
		mtimSpec = unix.Timespec{Nsec: utimeOmit}
	}

	return []unix.Timespec{atimSpec, mtimSpec}, nil
}

// mapError maps Go, io/fs, and syscall errors to a WASI errno.
func mapError(err error) int32 {
	if err == nil {
		return errnoSuccess
	}

	// os.Root reports these conditions through a *fs.PathError wrapping an
	// unexported errors.New sentinel, so the inner message is the only thing that
	// distinguishes them. Sandbox escapes and empty paths map to errnoPerm to
	// match the previous hand-rolled resolver.
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		switch pathErr.Err.Error() {
		case "path escapes from parent", "empty path":
			return errnoPerm
		case "not a directory":
			return errnoNotDir
		}
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
