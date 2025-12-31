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
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// This file contains Unix-specific file system operations similar to POSIX *at
// functions.
// Each operation takes an os.File and a path relative to it.
// Unlike POSIX *at functions, these do not support `AT_FDCWD`:
// every operation is strictly relative to a valid file descriptor.
//
// This design prevents sandbox escape attacks using symlinks.
// Additionally, all operations are implemented to avoid TOCTOU attacks.
//
// Any new function added to this file MUST follow these security principles.

// maxSymlinkDepth is the maximum number of symlink resolutions allowed.
const maxSymlinkDepth = 40

// openat opens a file or directory relative to a directory.
// This is similar to openat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to open relative to
//   - path: the relative path of the file or directory to open
//   - lookupFlags: flags for symlink handling (lookupFlagsSymlinkFollow)
//   - oflags: flags determining the method by which to open the file
//   - fsRightsBase: base rights for operations using the returned os.File
//   - fdflags: file descriptor flags
//
// The implementation may return an os.File with fewer rights than specified,
// if and only if those rights do not apply to the type of file being opened.
//
// Returns the opened os.File and an error if the operation fails.
func openat(
	dir *os.File,
	path string,
	lookupFlags, oflags, fdflags int32,
	fsRights uint64,
) (*os.File, error) {
	root := dir
	currentDir := dir
	virtualPath := ""
	return pathOpenInternal(
		root,
		currentDir,
		virtualPath,
		path,
		lookupFlags,
		oflags,
		fdflags,
		fsRights,
		0,
	)
}

// pathOpenInternal implements the core path opening logic.
// Parameters:
//   - root: the original sandbox root directory (never changes)
//   - currentDir: the current directory we're opening relative to
//   - virtualPath: the path from root to currentDir (for symlink resolution)
//   - path: the remaining path to open
//   - depth: symlink resolution depth counter
func pathOpenInternal(
	root *os.File,
	currentDir *os.File,
	virtualPath string,
	path string,
	lookupFlags, oflags, fdflags int32,
	fsRights uint64,
	depth int,
) (*os.File, error) {
	if depth >= maxSymlinkDepth {
		return nil, syscall.ELOOP
	}

	// The path must be local (no absolute paths, no leading ..)
	if !filepath.IsLocal(path) {
		return nil, os.ErrInvalid
	}

	// POSIX/WASI: a trailing slash means the path must be a directory.
	// We detect this before splitting since filepath.Clean removes it.
	if strings.HasSuffix(path, string(filepath.Separator)) {
		oflags |= int32(oFlagsDirectory)
	}

	components := splitPath(path)
	if len(components) == 0 {
		return nil, os.ErrInvalid
	}

	followFinalSymlink := lookupFlags&lookupFlagsSymlinkFollow != 0

	// Handle special case of opening "." (the directory itself)
	if len(components) == 1 && components[0] == "." {
		return openFinalSecure(
			root,
			currentDir,
			virtualPath,
			".",
			followFinalSymlink,
			oflags,
			fdflags,
			fsRights,
			depth,
		)
	}

	// Walk through intermediate directories (all except the last component).
	// Each intermediate component is opened with O_NOFOLLOW|O_DIRECTORY to
	// prevent symlink following and to ensure we're always opening directories.
	dirFd := int(currentDir.Fd())
	currentDirFd := dirFd
	currentVirtualPath := virtualPath

	for _, component := range components[:len(components)-1] {
		// Skip "." components in the middle of the path
		if component == "." {
			continue
		}

		newFd, err := unix.Openat(
			currentDirFd,
			component,
			unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC,
			0,
		)

		// Close intermediate fds we opened (but not the original dir)
		if currentDirFd != dirFd {
			unix.Close(currentDirFd)
		}

		if err != nil {
			return nil, mapErrno(err)
		}

		currentDirFd = newFd
		currentVirtualPath = filepath.Join(currentVirtualPath, component)
	}

	// Create a temporary os.File for the parent directory so we can pass it
	// to openFinalSecure. If we opened intermediate dirs, os.NewFile takes
	// ownership.
	var parentDir *os.File
	if currentDirFd != dirFd {
		parentDir = os.NewFile(uintptr(currentDirFd), "")
		defer parentDir.Close()
	} else {
		parentDir = currentDir
	}

	return openFinalSecure(
		root,
		parentDir,
		currentVirtualPath,
		components[len(components)-1],
		followFinalSymlink,
		oflags,
		fdflags,
		fsRights,
		depth,
	)
}

// openFinalSecure opens the final path component securely.
// When followSymlink is true and the target is a symlink, it manually resolves
// it by reading the link target and recursively walking through pathOpen.
func openFinalSecure(
	root *os.File,
	parentDir *os.File,
	virtualPath string,
	name string,
	followSymlink bool,
	oflags, fdflags int32,
	fsRights uint64,
	depth int,
) (*os.File, error) {
	// Always try opening with O_NOFOLLOW first
	file, err := openFinal(parentDir, name, oflags, fdflags, fsRights)
	if err == nil {
		return file, nil
	}

	// If not following symlinks, or error isn't ELOOP, return the error
	if !followSymlink || !errors.Is(err, syscall.ELOOP) {
		return nil, err
	}

	// The target is a symlink and we need to follow it. Read the symlink target.
	target, err := readlinkat(parentDir, name)
	if err != nil {
		return nil, mapErrno(err)
	}

	// Resolve the symlink target relative to its containing directory.
	// Join the virtual path with the symlink target, then clean it.
	// This handles cases like "link -> ../other/file.txt" correctly.
	resolvedPath := filepath.Clean(filepath.Join(virtualPath, target))

	// Security check: the resolved path must stay within the sandbox.
	// After cleaning, if it starts with ".." it would escape.
	if !filepath.IsLocal(resolvedPath) {
		return nil, os.ErrPermission
	}

	// Resolve the symlink by walking from the root with the resolved path.
	// This ensures we apply all security checks to the resolved path.
	return pathOpenInternal(
		root,
		root,
		"",
		resolvedPath,
		lookupFlagsSymlinkFollow,
		oflags,
		fdflags,
		fsRights,
		depth+1,
	)
}

// readlinkat reads the target of a symlink relative to a directory.
func readlinkat(dir *os.File, name string) (string, error) {
	// Start with a reasonable buffer size
	buf := make([]byte, 256)
	for {
		n, err := unix.Readlinkat(int(dir.Fd()), name, buf)
		if err != nil {
			return "", err
		}
		if n < len(buf) {
			return string(buf[:n]), nil
		}
		// Buffer was too small, double it and retry
		buf = make([]byte, len(buf)*2)
	}
}

// openFinal opens the final path component with the appropriate flags.
// This always uses O_NOFOLLOW; symlink following is handled by openFinalSecure.
func openFinal(
	parentDir *os.File,
	name string,
	oflags, fdflags int32,
	fsRightsBase uint64,
) (*os.File, error) {
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW

	// Determine read/write mode from rights
	canRead := fsRightsBase&uint64(RightsFdRead) != 0
	canWrite := fsRightsBase&uint64(RightsFdWrite) != 0

	switch {
	case canRead && canWrite:
		flags |= unix.O_RDWR
	case canWrite:
		flags |= unix.O_WRONLY
	default:
		flags |= unix.O_RDONLY
	}

	// Convert WASI oflags to Unix flags
	if oflags&int32(oFlagsCreat) != 0 {
		flags |= unix.O_CREAT
	}
	if oflags&int32(oFlagsDirectory) != 0 {
		flags |= unix.O_DIRECTORY
	}
	if oflags&int32(oFlagsExcl) != 0 {
		flags |= unix.O_EXCL
	}
	if oflags&int32(oFlagsTrunc) != 0 {
		flags |= unix.O_TRUNC
	}

	// Convert WASI fdflags to Unix flags
	if fdflags&int32(fdFlagsAppend) != 0 {
		flags |= unix.O_APPEND
	}
	if fdflags&int32(fdFlagsDsync) != 0 {
		flags |= unix.O_DSYNC
	}
	if fdflags&int32(fdFlagsNonblock) != 0 {
		flags |= unix.O_NONBLOCK
	}
	if fdflags&int32(fdFlagsSync) != 0 {
		flags |= unix.O_SYNC
	}
	// Note: fdFlagsRsync maps to O_RSYNC, which equals O_SYNC on most systems.
	if fdflags&int32(fdFlagsRsync) != 0 {
		flags |= unix.O_SYNC
	}

	fd, err := unix.Openat(int(parentDir.Fd()), name, flags, 0o666)
	if err != nil {
		return nil, mapErrno(err)
	}

	return os.NewFile(uintptr(fd), name), nil
}

// splitPath splits a path into its components.
// It handles both forward slashes and the OS-specific separator.
func splitPath(path string) []string {
	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}

	// If the path was just ".", return that
	if len(result) == 0 && (path == "." || path == "") {
		return []string{"."}
	}

	return result
}

// mapErrno converts a syscall error to an appropriate os error.
func mapErrno(err error) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT:
			return os.ErrNotExist
		case syscall.EEXIST:
			return os.ErrExist
		case syscall.EACCES, syscall.EPERM:
			return os.ErrPermission
		case syscall.EINVAL:
			return os.ErrInvalid
		case syscall.ELOOP:
			// Keep ELOOP as-is so openFinalSecure can detect symlinks
			return syscall.ELOOP
		case syscall.ENOTDIR:
			return syscall.ENOTDIR
		case syscall.EISDIR:
			return syscall.EISDIR
		}
	}

	return err
}
