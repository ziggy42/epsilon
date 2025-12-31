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
	"time"

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

// defaultFileMode is the default permission mode for newly created files.
const defaultFileMode = 0o600

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

	components, err := getComponents(path)
	if err != nil {
		return err
	}

	if len(components) == 1 && components[0] == "." {
		return os.ErrInvalid
	}

	parentFd, _, _, err := walkToParent(dir, "", components, 0)
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
	dirFd, fileName, err := resolvePath(dir, path, followSymlinks, 0)
	if err != nil {
		return filestat{}, err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}

	var statBuf unix.Stat_t
	flags := unix.AT_SYMLINK_NOFOLLOW
	if err := unix.Fstatat(dirFd, fileName, &statBuf, flags); err != nil {
		return filestat{}, mapErrno(err)
	}
	return statFromUnix(&statBuf), nil
}

// utimes sets the access and modification times of a file or directory.
// This is similar to utimensat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to resolve relative to
//   - path: the relative path of the file or directory
//   - atim: access time in nanoseconds (used if fstFlagsAtim is set)
//   - mtim: modification time in nanoseconds (used if fstFlagsMtim is set)
//   - fstFlags: bitmask controlling which times to set and how
//   - followSymlinks: whether to follow the final symlink
//
// Returns an error if the operation fails.
func utimes(
	dir *os.File,
	path string,
	atim, mtim int64,
	fstFlags int32,
	followSymlinks bool,
) error {
	dirFd, fileName, err := resolvePath(dir, path, followSymlinks, 0)
	if err != nil {
		return err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}

	times := buildTimespec(atim, mtim, fstFlags)
	flags := unix.AT_SYMLINK_NOFOLLOW
	if err := unix.UtimesNanoAt(dirFd, fileName, times, flags); err != nil {
		return mapErrno(err)
	}
	return nil
}

// linkat creates a hard link to an existing file.
// This is similar to linkat in POSIX.
//
// Parameters:
//   - oldDir: the directory os.File for the source path resolution
//   - oldPath: the relative path of the source file
//   - followSymlinks: whether to follow symlinks when resolving oldPath
//   - newDir: the directory os.File for the destination path resolution
//   - newPath: the relative path for the new hard link
//
// Returns an error if the operation fails.
func linkat(
	oldDir *os.File,
	oldPath string,
	followSymlinks bool,
	newDir *os.File,
	newPath string,
) error {
	oldDirFd, oldName, err := resolvePath(oldDir, oldPath, followSymlinks, 0)
	if err != nil {
		return err
	}
	if oldDirFd != int(oldDir.Fd()) {
		defer unix.Close(oldDirFd)
	}

	newDirFd, newName, err := resolvePath(newDir, newPath, false, 0)
	if err != nil {
		return err
	}
	if newDirFd != int(newDir.Fd()) {
		defer unix.Close(newDirFd)
	}

	err = unix.Linkat(oldDirFd, oldName, newDirFd, newName, 0)
	if err != nil {
		return mapErrno(err)
	}

	return nil
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
	// A trailing slash means the path must be a directory.
	// We detect this before calling resolvePath because we lose that info.
	if strings.HasSuffix(path, string(filepath.Separator)) {
		oflags |= int32(oFlagsDirectory)
	}

	dirFd, name, err := resolvePath(dir, path, followSymlinks, 0)
	if err != nil {
		return nil, err
	}
	parentDir := os.NewFile(uintptr(dirFd), "")

	if dirFd == int(dir.Fd()) {
		parentDir = dir
	} else {
		defer parentDir.Close()
	}

	// Determine read/write mode from rights
	canRead := fsRights&uint64(RightsFdRead) != 0
	canWrite := fsRights&uint64(RightsFdWrite) != 0

	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW
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

	fd, err := unix.Openat(int(parentDir.Fd()), name, flags, defaultFileMode)
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
//   - depth: current symlink resolution depth
//
// Returns:
//   - parentFd: fd of the parent directory
//   - parentPath: the path of parentFd relative to dir
//   - newDepth: updated symlink resolution depth
//   - error: any error encountered
//
// The caller is responsible for closing parentFd if it differs from dir.Fd().
func walkToParent(
	dir *os.File,
	basePath string,
	components []string,
	depth int,
) (int, string, int, error) {
	if depth >= maxSymlinkDepth {
		return 0, "", depth, syscall.ELOOP
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
				return 0, "", depth, os.ErrPermission
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
			return walkToParent(dir, "", newComponents, depth+1)
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
					return 0, "", depth, err
				}

				resolvedPath, pathErr := resolveSymlinkTarget(parentPath, target)
				if pathErr != nil {
					if currentDirFd != dirFd {
						unix.Close(currentDirFd)
					}
					return 0, "", depth, pathErr
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
				return walkToParent(dir, "", newComponents, depth+1)
			}

			// Close intermediate fds we opened (but not the original dir)
			if currentDirFd != dirFd {
				unix.Close(currentDirFd)
			}
			return 0, "", depth, mapErrno(err)
		}

		// Close intermediate fds we opened (but not the original dir)
		if currentDirFd != dirFd {
			unix.Close(currentDirFd)
		}

		currentDirFd = newFd
		parentPath = filepath.Join(parentPath, component)
	}

	return currentDirFd, parentPath, depth, nil
}

func getComponents(path string) ([]string, error) {
	components := splitPath(path)
	if len(components) == 0 {
		return nil, os.ErrInvalid
	}

	// If the final component is "..", append "." to ensure it's processed as an
	// intermediate component through safe logical resolution, and we use "." as
	// the final name for the syscall.
	if components[len(components)-1] == ".." {
		components = append(components, ".")
	}

	return components, nil
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
		case syscall.ELOOP, syscall.ENOTDIR, syscall.EISDIR:
			return errno
		}
	}

	return err
}

// resolveSymlinkTarget computes a sandbox-safe resolved path for a symlink.
// Absolute symlinks are resolved relative to the sandbox root, relative
// symlinks are resolved relative to parentPath.
func resolveSymlinkTarget(parentPath, target string) (string, error) {
	var resolved string
	if filepath.IsAbs(target) {
		resolved = strings.TrimPrefix(target, string(filepath.Separator))
	} else {
		resolved = filepath.Clean(filepath.Join(parentPath, target))
	}
	if !filepath.IsLocal(resolved) {
		return "", os.ErrPermission
	}
	return resolved, nil
}

// resolvePath resolves a path relative to a directory os.File. It returns the
// file descriptor of the parent directory and the file or directory name.
//
// It does so by securely resolving path relative to dirFd, following symlinks
// in intermediate components and, if followSymlinks is true, in the final
// component.
//
// If the final path component does not exist, it returns success (and ENOENT
// checks are deferred to the caller), which allows support for O_CREAT.
//
// All of this is done while never escaping the sandbox root.
func resolvePath(
	dir *os.File,
	path string,
	followSymlinks bool,
	depth int,
) (int, string, error) {
	if depth >= maxSymlinkDepth {
		return 0, "", syscall.ELOOP
	}

	if !isRelativePath(path) {
		return 0, "", os.ErrInvalid
	}

	comps, err := getComponents(path)
	if err != nil {
		return 0, "", err
	}

	// Handle special case of stat on "." (the directory itself)
	if len(comps) == 1 && comps[0] == "." {
		return int(dir.Fd()), ".", nil
	}

	parentFd, parentPath, newDepth, err := walkToParent(dir, "", comps, depth)
	if err != nil {
		return 0, "", err
	}
	// Note: We MUST NOT defer close parentFd here because we might return it.
	// We must manually close it on all error paths or recursive calls if it
	// differs from dir.Fd().

	finalName := comps[len(comps)-1]

	// Always stat with AT_SYMLINK_NOFOLLOW first to check if it's a symlink
	var statBuf unix.Stat_t
	flags := unix.AT_SYMLINK_NOFOLLOW
	if err := unix.Fstatat(parentFd, finalName, &statBuf, flags); err != nil {
		// If the error is ENOENT, it simply means the file doesn't exist.
		if errors.Is(err, syscall.ENOENT) {
			return parentFd, finalName, nil
		}

		if parentFd != int(dir.Fd()) {
			unix.Close(parentFd)
		}
		return 0, "", mapErrno(err)
	}

	// If it's not a symlink, or we don't want to follow, return immediately
	if statBuf.Mode&unix.S_IFMT != unix.S_IFLNK || !followSymlinks {
		return parentFd, finalName, nil
	}

	// It's a symlink and we need to follow it securely.
	// Read the symlink target and resolve it within the sandbox.
	target, err := readlinkat(parentFd, finalName)
	if err != nil {
		if parentFd != int(dir.Fd()) {
			unix.Close(parentFd)
		}
		return 0, "", mapErrno(err)
	}

	// We are done with parentFd for this level.
	if parentFd != int(dir.Fd()) {
		unix.Close(parentFd)
	}

	resolvedPath, pathErr := resolveSymlinkTarget(parentPath, target)
	if pathErr != nil {
		return 0, "", pathErr
	}

	// Restart resolution with the new target and updated depth
	return resolvePath(dir, resolvedPath, followSymlinks, newDepth+1)
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

func buildTimespec(atim, mtim int64, fstFlags int32) []unix.Timespec {
	now := time.Now().UnixNano()

	var atimSpec, mtimSpec unix.Timespec
	if fstFlags&fstFlagsAtimNow != 0 {
		atimSpec = unix.NsecToTimespec(now)
	} else if fstFlags&fstFlagsAtim != 0 {
		atimSpec = unix.NsecToTimespec(atim)
	}

	if fstFlags&fstFlagsMtimNow != 0 {
		mtimSpec = unix.NsecToTimespec(now)
	} else if fstFlags&fstFlagsMtim != 0 {
		mtimSpec = unix.NsecToTimespec(mtim)
	}

	return []unix.Timespec{atimSpec, mtimSpec}
}
