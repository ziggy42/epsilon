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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// maxSymlinkDepth is the maximum number of symlink resolutions allowed.
const maxSymlinkDepth = 40

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
		utimeNow = -1
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

	parentFd, _, _, err := walkToParent(dir, components, 0)
	if err != nil {
		return err
	}

	if parentFd != int(dir.Fd()) {
		defer unix.Close(parentFd)
	}

	return unix.Mkdirat(parentFd, components[len(components)-1], mode)
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

// setFdFlags sets flags on a file..
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
	times, err := buildTimespec(atim, mtim, fstFlags)
	if err != nil {
		return err
	}

	dirFd, fileName, err := resolvePath(dir, path, followSymlinks, 0)
	if err != nil {
		return err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}

	return unix.UtimesNanoAt(dirFd, fileName, times, unix.AT_SYMLINK_NOFOLLOW)
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
	// Hard link creation does not support directory targets (implied by trailing
	// slash)
	if strings.HasSuffix(oldPath, string(filepath.Separator)) ||
		strings.HasSuffix(newPath, string(filepath.Separator)) {
		return syscall.ENOENT
	}

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

	return unix.Linkat(oldDirFd, oldName, newDirFd, newName, 0)
}

// readlink reads the contents of a symbolic link.
// This is similar to readlinkat in POSIX.
//
// Parameters:
//   - dir: the directory os.File to resolve relative to
//   - path: the relative path of the symbolic link
//
// Returns the symlink target and an error if the operation fails.
func readlink(dir *os.File, path string) (string, error) {
	dirFd, name, err := resolvePath(dir, path, false, 0)
	if err != nil {
		return "", err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}
	return readlinkat(dirFd, name)
}

// rmdirat removes an empty directory.
// This is similar to unlinkat(fd, path, AT_REMOVEDIR) in POSIX.
//
// Parameters:
//   - dir: the directory os.File to resolve relative to
//   - path: the relative path of the directory to remove
//
// Returns an error if the operation fails (e.g., ENOTEMPTY if not empty).
func rmdirat(dir *os.File, path string) error {
	dirFd, name, err := resolvePath(dir, path, false, 0)
	if err != nil {
		return err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}
	return unix.Unlinkat(dirFd, name, unix.AT_REMOVEDIR)
}

// renameat renames a file or directory.
// This is similar to renameat in POSIX.
//
// Parameters:
//   - oldDir: the directory os.File for the source path resolution
//   - oldPath: the relative path of the source file or directory
//   - newDir: the directory os.File for the destination path resolution
//   - newPath: the relative path of the destination
//
// Returns an error if the operation fails.
func renameat(
	oldDir *os.File,
	oldPath string,
	newDir *os.File,
	newPath string,
) error {
	oldDirFd, oldName, err := resolvePath(oldDir, oldPath, false, 0)
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

	return unix.Renameat(oldDirFd, oldName, newDirFd, newName)
}

// symlinkat creates a symbolic link.
// This is similar to symlinkat in POSIX.
//
// Parameters:
//   - target: the contents of the symbolic link (what it points to)
//   - dir: the directory os.File for the link path resolution
//   - path: the relative path at which to create the symlink
//
// Returns an error if the operation fails.
func symlinkat(target string, dir *os.File, path string) error {
	if strings.HasPrefix(target, string(filepath.Separator)) {
		return syscall.EPERM
	}

	if strings.HasSuffix(path, string(filepath.Separator)) {
		return syscall.ENOENT
	}

	dirFd, name, err := resolvePath(dir, path, false, 0)
	if err != nil {
		return err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}

	return unix.Symlinkat(target, dirFd, name)
}

// unlinkat removes a file (but not a directory).
// This is similar to unlinkat(fd, path, 0) in POSIX.
//
// Parameters:
//   - dir: the directory os.File to resolve relative to
//   - path: the relative path of the file to unlink
//
// Returns EISDIR if the path refers to a directory.
func unlinkat(dir *os.File, path string) error {
	dirFd, name, err := resolvePath(dir, path, false, 0)
	if err != nil {
		return err
	}
	if dirFd != int(dir.Fd()) {
		defer unix.Close(dirFd)
	}

	// Restore trailing slash so syscall returns correct error
	if strings.HasSuffix(path, string(filepath.Separator)) {
		name += string(filepath.Separator)
	}

	return unix.Unlinkat(dirFd, name, 0)
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

	var parentDir *os.File
	if dirFd == int(dir.Fd()) {
		parentDir = dir
	} else {
		parentDir = os.NewFile(uintptr(dirFd), "")
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
		return nil, err
	}

	return os.NewFile(uintptr(fd), name), nil
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

// walkToParent walks through intermediate path components (all except the last)
// and returns the fd of the parent directory of the final component.
// Symlinks in intermediate components are followed securely within the sandbox.
//
// Parameters:
//   - dir: the sandbox root directory (symlinks are resolved relative to this)
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
	components []string,
	depth int,
) (int, string, int, error) {
	if depth >= maxSymlinkDepth {
		return 0, "", depth, syscall.ELOOP
	}

	dirFd := int(dir.Fd())
	parentFd := dirFd
	parentPath := ""

	// Helper to close parentFd if it differs from dir.Fd
	closeParent := func() {
		if parentFd != dirFd {
			unix.Close(parentFd)
		}
	}

	// Re-calculates path components and restarts the walk from root
	restart := func(newBase string, remain []string) (int, string, int, error) {
		closeParent()

		newComponents := append(splitPath(newBase), remain...)
		if len(newComponents) == 0 {
			newComponents = []string{"."}
		}

		return walkToParent(dir, newComponents, depth+1)
	}

	for i, component := range components[:len(components)-1] {
		if component == "." {
			continue
		}

		if component == ".." {
			// Check if we're at the root, we cannot go above sandbox
			if parentPath == "" {
				return 0, "", depth, syscall.EPERM
			}
			newParentPath := filepath.Dir(parentPath)
			if newParentPath == "." {
				newParentPath = ""
			}
			return restart(newParentPath, components[i+1:])
		}

		mode := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_DIRECTORY | unix.O_CLOEXEC
		newFd, err := unix.Openat(parentFd, component, mode, 0)

		// Check if this is a symlink we need to follow
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			resolvedPath, err := resolveSymlink(parentFd, parentPath, component)
			if err != nil {
				closeParent()
				return 0, "", depth, err
			}
			return restart(resolvedPath, components[i+1:])
		}

		closeParent()
		if err != nil {
			return 0, "", depth, err
		}

		parentFd = newFd
		parentPath = filepath.Join(parentPath, component)
	}

	return parentFd, parentPath, depth, nil
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
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == filepath.Separator
	})
	if len(parts) == 0 {
		return []string{"."}
	}
	return parts
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

// resolveSymlink reads a symlink and returns a sandbox-safe resolved path.
//
// Parameters:
//   - parentFd: fd of the directory containing the symlink
//   - parentPath: path of parentFd relative to the sandbox root
//   - name: name of the symlink within the parent directory
//
// Returns the resolved path relative to the sandbox root. Absolute symlink
// targets are resolved relative to the sandbox root (not the filesystem root),
// while relative targets are resolved relative to parentPath.
//
// Returns EPERM if the resolved path would escape the sandbox.
func resolveSymlink(parentFd int, parentPath, name string) (string, error) {
	target, err := readlinkat(parentFd, name)
	if err != nil {
		return "", err
	}

	var resolved string
	if filepath.IsAbs(target) {
		resolved = strings.TrimPrefix(target, string(filepath.Separator))
	} else {
		resolved = filepath.Clean(filepath.Join(parentPath, target))
	}
	if !filepath.IsLocal(resolved) {
		return "", syscall.EPERM
	}
	return resolved, nil
}

// readlinkat reads a symlink target, growing the buffer as needed.
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
		buf = make([]byte, len(buf)*2)
	}
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

	parentFd, parentPath, newDepth, err := walkToParent(dir, comps, depth)
	if err != nil {
		return 0, "", err
	}

	// closeParent closes parentFd if we own it (differs from dir.Fd). We cannot
	// use defer because we may return parentFd to the caller.
	closeParent := func() {
		if parentFd != int(dir.Fd()) {
			unix.Close(parentFd)
		}
	}

	finalName := comps[len(comps)-1]

	// Always stat with AT_SYMLINK_NOFOLLOW first to check if it's a symlink
	var statBuf unix.Stat_t
	err = unix.Fstatat(parentFd, finalName, &statBuf, unix.AT_SYMLINK_NOFOLLOW)

	// If the error is ENOENT, the file doesn't exist. We return success (for
	// O_CREAT support)
	if errors.Is(err, syscall.ENOENT) {
		return parentFd, finalName, nil
	}

	if err != nil {
		closeParent()
		return 0, "", err
	}

	// If it's not a symlink, or we don't want to follow, return immediately
	if statBuf.Mode&unix.S_IFMT != unix.S_IFLNK || !followSymlinks {
		return parentFd, finalName, nil
	}

	// It's a symlink and we need to follow it securely.
	resolvedPath, err := resolveSymlink(parentFd, parentPath, finalName)
	closeParent()
	if err != nil {
		return 0, "", err
	}

	// Restart resolution with the new target and updated depth
	return resolvePath(dir, resolvedPath, followSymlinks, newDepth+1)
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

// mapError maps Go/Syscall errors to WASI errno.
func mapError(err error) int32 {
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		err = unwrapped
	}

	if errno, ok := err.(syscall.Errno); ok {
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

	return errnoNotCapable
}
