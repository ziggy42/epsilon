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

// mkdirat creates a directory relative to a directory.
// This is similar to mkdirat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to create relative to
//   - path: the relative path of the directory to create
//   - mode: the file mode bits for the new directory
//
// Returns an error if the operation fails.
func mkdirat(dir *os.File, path string, mode uint32) error {
	if !isRelativePath(path) {
		return os.ErrInvalid
	}

	components := splitPath(path)
	if len(components) == 0 {
		return os.ErrInvalid
	}

	if len(components) == 1 && components[0] == "." {
		return os.ErrInvalid
	}

	// If the final component is "..", append "." to ensure it's processed as an
	// intermediate component through safe logical resolution, and we use "." as
	// the final name for the syscall.
	if components[len(components)-1] == ".." {
		components = append(components, ".")
	}

	parentFd, _, err := walkToParent(dir, "", components)
	if err != nil {
		return err
	}

	if parentFd != int(dir.Fd()) {
		defer unix.Close(parentFd)
	}

	finalName := components[len(components)-1]
	if err := unix.Mkdirat(parentFd, finalName, mode); err != nil {
		return mapErrno(err)
	}

	return nil
}

// stat returns the filestat of a file or directory relative to a directory.
// This is similar to fstatat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to stat relative to
//   - path: the relative path of the file or directory to inspect
//   - followSymlinks: whether to follow final symlink
//
// Returns the filestat and an error if the operation fails.
func stat(dir *os.File, path string, followSymlinks bool) (filestat, error) {
	return statInternal(dir, path, followSymlinks, 0)
}

// statInternal implements the core stat logic with symlink resolution.
func statInternal(
	dir *os.File,
	path string,
	followSymlinks bool,
	depth int,
) (filestat, error) {
	if depth >= maxSymlinkDepth {
		return filestat{}, syscall.ELOOP
	}

	if !isRelativePath(path) {
		return filestat{}, os.ErrInvalid
	}

	components := splitPath(path)
	if len(components) == 0 {
		return filestat{}, os.ErrInvalid
	}

	// Handle special case of stat on "." (the directory itself)
	if len(components) == 1 && components[0] == "." {
		var statBuf unix.Stat_t
		if err := unix.Fstat(int(dir.Fd()), &statBuf); err != nil {
			return filestat{}, mapErrno(err)
		}
		return statFromUnix(&statBuf), nil
	}

	// If the final component is "..", append "." to ensure it's processed as an
	// intermediate component through safe logical resolution, and we use "." as
	// the final name for the syscall.
	if components[len(components)-1] == ".." {
		components = append(components, ".")
	}

	parentFd, parentPath, err := walkToParent(dir, "", components)
	if err != nil {
		return filestat{}, err
	}
	if parentFd != int(dir.Fd()) {
		defer unix.Close(parentFd)
	}

	finalName := components[len(components)-1]

	// Always stat with AT_SYMLINK_NOFOLLOW first to check if it's a symlink
	var statBuf unix.Stat_t
	flags := unix.AT_SYMLINK_NOFOLLOW
	if err := unix.Fstatat(parentFd, finalName, &statBuf, flags); err != nil {
		return filestat{}, mapErrno(err)
	}

	// If it's not a symlink, or we don't want to follow, return immediately
	if statBuf.Mode&unix.S_IFMT != unix.S_IFLNK || !followSymlinks {
		return statFromUnix(&statBuf), nil
	}

	// It's a symlink and we need to follow it securely.
	// Read the symlink target and resolve it within the sandbox.
	target, err := readlinkat(parentFd, finalName)
	if err != nil {
		return filestat{}, mapErrno(err)
	}
	// Resolve the symlink target.
	// Absolute symlinks are resolved relative to the sandbox root.
	// Relative symlinks are resolved relative to the symlink's containing dir.
	var resolvedPath string
	if filepath.IsAbs(target) {
		resolvedPath = strings.TrimPrefix(target, string(filepath.Separator))
	} else {
		resolvedPath = filepath.Clean(filepath.Join(parentPath, target))
	}
	if !filepath.IsLocal(resolvedPath) {
		return filestat{}, os.ErrPermission
	}

	// Recursively stat from the root with the resolved path.
	return statInternal(dir, resolvedPath, followSymlinks, depth+1)
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

// openat opens a file or directory relative to a directory.
// This is similar to openat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to open relative to
//   - path: the relative path of the file or directory to open
//   - followSymlinks: whether to follow final symlink
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
	followSymlinks bool,
	oflags, fdflags int32,
	fsRights uint64,
) (*os.File, error) {
	root := dir
	return pathOpenInternal(
		root,
		path,
		followSymlinks,
		oflags,
		fdflags,
		fsRights,
		0,
	)
}

// pathOpenInternal implements the core path opening logic.
// Parameters:
//   - root: the original sandbox root directory (never changes)
//   - path: the remaining path to open
//   - depth: symlink resolution depth counter
func pathOpenInternal(
	root *os.File,
	path string,
	followSymlinks bool,
	oflags, fdflags int32,
	fsRights uint64,
	depth int,
) (*os.File, error) {
	if depth >= maxSymlinkDepth {
		return nil, syscall.ELOOP
	}

	// The path must be local (no absolute paths, no leading ..)
	if !isRelativePath(path) {
		return nil, os.ErrInvalid
	}

	// A trailing slash means the path must be a directory.
	// We detect this before splitting since filepath.Clean removes it.
	if strings.HasSuffix(path, string(filepath.Separator)) {
		oflags |= int32(oFlagsDirectory)
	}

	components := splitPath(path)
	if len(components) == 0 {
		return nil, os.ErrInvalid
	}

	// Handle special case of opening "." (the directory itself)
	if len(components) == 1 && components[0] == "." {
		return openFinalSecure(
			root,
			root,
			"",
			".",
			followSymlinks,
			oflags,
			fdflags,
			fsRights,
			depth,
		)
	}

	// If the final component is "..", append "." to ensure it's processed as an
	// intermediate component through safe logical resolution, and we use "." as
	// the final name for the syscall.
	if components[len(components)-1] == ".." {
		components = append(components, ".")
	}

	parentFd, parentPath, err := walkToParent(root, "", components)
	if err != nil {
		return nil, err
	}

	// Create a temporary os.File for the parent directory so we can pass it to
	// openFinalSecure. If we opened intermediate dirs, NewFile takes ownership.
	var parentDir *os.File
	if parentFd != int(root.Fd()) {
		parentDir = os.NewFile(uintptr(parentFd), "")
		defer parentDir.Close()
	} else {
		parentDir = root
	}

	return openFinalSecure(
		root,
		parentDir,
		parentPath,
		components[len(components)-1],
		followSymlinks,
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
	root, parentDir *os.File,
	virtualPath, name string,
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
	target, err := readlinkat(int(parentDir.Fd()), name)
	if err != nil {
		return nil, mapErrno(err)
	}

	// Resolve the symlink target.
	// Absolute symlinks are resolved relative to the sandbox root.
	// Relative symlinks are resolved relative to the symlink's containing dir.
	var resolvedPath string
	if filepath.IsAbs(target) {
		// Strip leading "/" to make it relative to sandbox root
		resolvedPath = strings.TrimPrefix(target, string(filepath.Separator))
	} else {
		// Relative symlink: join with virtualPath and clean
		resolvedPath = filepath.Clean(filepath.Join(virtualPath, target))
	}

	// Security check: the resolved path must stay within the sandbox.
	// After cleaning, if it starts with ".." it would escape.
	if !filepath.IsLocal(resolvedPath) {
		return nil, os.ErrPermission
	}

	// Resolve the symlink by walking from the root with the resolved path.
	// This ensures we apply all security checks to the resolved path.
	return pathOpenInternal(
		root,
		resolvedPath,
		true, // followSymlinks
		oflags,
		fdflags,
		fsRights,
		depth+1,
	)
}

// readlinkat reads the target of a symlink relative to a directory fd.
func readlinkat(dirFd int, name string) (string, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Readlinkat(dirFd, name, buf)
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

	// Default to 0o600 (owner read/write only) for secure file creation.
	fd, err := unix.Openat(int(parentDir.Fd()), name, flags, 0o600)
	if err != nil {
		return nil, mapErrno(err)
	}

	return os.NewFile(uintptr(fd), name), nil
}

// walkToParent walks through intermediate path components (all except the last)
// and returns the fd of the parent directory of the final component.
// Symlinks in intermediate components are followed securely within the sandbox.
//
// Parameters:
//   - dir: the sandbox root directory (symlinks are resolved relative to this)
//   - basePath: prepended to the returned parentPath (use "" to get a path
//     relative to dir)
//   - components: the path components to walk
//
// Returns:
//   - parentFd: fd of the parent directory (may equal dir.Fd() if no
//     intermediate directories were opened)
//   - parentPath: the path of parentFd relative to dir (with basePath
//     prepended)
//   - error: any error encountered
//
// The caller is responsible for closing parentFd if it differs from dir.Fd().
func walkToParent(
	dir *os.File,
	basePath string,
	components []string,
) (int, string, error) {
	return walkToParentInternal(dir, basePath, components, 0)
}

// walkToParentInternal implements walkToParent with symlink depth tracking.
func walkToParentInternal(
	dir *os.File,
	basePath string,
	components []string,
	depth int,
) (int, string, error) {
	if depth >= maxSymlinkDepth {
		return 0, "", syscall.ELOOP
	}

	dirFd := int(dir.Fd())
	currentDirFd := dirFd
	parentPath := basePath

	for i, component := range components[:len(components)-1] {
		// Skip "." components in the middle of the path
		if component == "." {
			continue
		}

		// Handle ".." component - must traverse to parent safely
		// SECURITY: We do NOT use physical ".." traversal (unix.Openat with "..")
		// because that is vulnerable to TOCTOU attacks via directory renaming.
		// Instead, we compute the new logical path and re-walk from root.
		if component == ".." {
			// Check if we're at the root - cannot go above sandbox
			if parentPath == "" {
				if currentDirFd != dirFd {
					unix.Close(currentDirFd)
				}
				return 0, "", os.ErrPermission
			}

			// Close current fd before restarting
			if currentDirFd != dirFd {
				unix.Close(currentDirFd)
			}

			// Compute new path: remove last component from parentPath, add remaining
			newParentPath := filepath.Dir(parentPath)
			if newParentPath == "." {
				newParentPath = ""
			}

			// Build new components: newParentPath + remaining components
			remaining := components[i+1:]
			var newComponents []string
			if newParentPath != "" {
				newComponents = splitPath(newParentPath)
			}
			newComponents = append(newComponents, remaining...)

			// If no components left, we're at root - add a placeholder
			if len(newComponents) == 0 {
				newComponents = []string{"."}
			}

			// Restart from root with the new path (safe, no physical ".." used)
			return walkToParentInternal(dir, "", newComponents, depth+1)
		}

		newFd, err := unix.Openat(
			currentDirFd,
			component,
			unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC,
			0,
		)

		if err != nil {
			// Check if this is a symlink we need to follow
			if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
				// Could be a symlink - try to read it
				target, readErr := readlinkat(currentDirFd, component)
				if readErr != nil {
					// Not a symlink, return the original error
					if currentDirFd != dirFd {
						unix.Close(currentDirFd)
					}
					return 0, "", err
				}

				// Resolve the symlink target.
				// Absolute symlinks are resolved relative to the sandbox root.
				// Relative symlinks are resolved relative to the current path.
				var resolvedPath string
				if filepath.IsAbs(target) {
					resolvedPath = strings.TrimPrefix(target, string(filepath.Separator))
				} else {
					resolvedPath = filepath.Clean(filepath.Join(parentPath, target))
				}
				if !filepath.IsLocal(resolvedPath) {
					if currentDirFd != dirFd {
						unix.Close(currentDirFd)
					}
					return 0, "", os.ErrPermission
				}

				// Build the new full path: resolved symlink + remaining components
				remaining := components[i+1:]
				newComponents := splitPath(resolvedPath)
				newComponents = append(newComponents, remaining...)

				// Close current fd before restarting
				if currentDirFd != dirFd {
					unix.Close(currentDirFd)
				}

				// Restart from root with the resolved path
				return walkToParentInternal(dir, "", newComponents, depth+1)
			}

			// Close intermediate fds we opened (but not the original dir)
			if currentDirFd != dirFd {
				unix.Close(currentDirFd)
			}
			return 0, "", mapErrno(err)
		}

		// Close intermediate fds we opened (but not the original dir)
		if currentDirFd != dirFd {
			unix.Close(currentDirFd)
		}

		currentDirFd = newFd
		parentPath = filepath.Join(parentPath, component)
	}

	return currentDirFd, parentPath, nil
}

// splitPath splits a path into its components without lexically resolving "..".
// This is critical for security: ".." must be traversed at runtime to enforce
// permission and existence checks on intermediate directories.
// It handles both forward slashes and the OS-specific separator.
func splitPath(path string) []string {
	// Normalize multiple slashes and handle "." components, but preserve ".."
	// We cannot use filepath.Clean because it lexically removes ".."
	parts := strings.Split(path, string(filepath.Separator))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			// Skip empty parts and current directory references
			continue
		default:
			result = append(result, part)
		}
	}

	// If the path was just "." or empty, return that
	if len(result) == 0 {
		return []string{"."}
	}

	return result
}

// isRelativePath checks if a path is a valid relative path for sandbox
// traversal. It rejects:
//   - Absolute paths (starting with /)
//   - Paths that start with .. (immediate sandbox escape)
//   - Empty paths
//
// Unlike filepath.IsLocal, it ALLOWS internal ".." components like "a/../b"
// because these will be traversed at runtime, enforcing permission checks.
func isRelativePath(path string) bool {
	if path == "" {
		return false
	}
	if filepath.IsAbs(path) {
		return false
	}
	// Check if path starts with ".."
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return false
	}
	return true
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
