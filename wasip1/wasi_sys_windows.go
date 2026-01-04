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

//go:build windows

package wasip1

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// symlinkFlagAllowUnprivileged is the
// SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE flag. This enables creating
// symlinks without admin privileges on Windows 10 1703+ when Developer Mode is
// enabled. The value is 0x2.
const symlinkFlagAllowUnprivileged uint32 = 0x2

// maxSymlinkDepth is the maximum number of symlink resolutions allowed.
const maxSymlinkDepth = 40

func mkdirat(dir *os.File, path string, mode uint32) error {
	parentPath, name, err := resolvePath(dir, path, false)
	if err != nil {
		return err
	}

	if name == "." {
		return os.ErrInvalid
	}

	return os.Mkdir(filepath.Join(parentPath, name), os.FileMode(mode))
}

func stat(dir *os.File, path string, followSymlinks bool) (filestat, error) {
	parentPath, name, err := resolvePath(dir, path, followSymlinks)
	if err != nil {
		return filestat{}, err
	}
	return statFromPath(filepath.Join(parentPath, name), followSymlinks)
}

func statFromPath(path string, followSymlinks bool) (filestat, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return filestat{}, err
	}

	attrs := windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_BACKUP_SEMANTICS
	if !followSymlinks {
		attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}

	h, err := windows.CreateFile(
		pathPtr,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		uint32(attrs),
		0,
	)

	if err != nil {
		return filestat{}, err
	}

	defer windows.Close(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return filestat{}, err
	}

	return statFromHandleFileInformation(&info), nil
}

func fileTypeFromWin32Attributes(attrs uint32) int8 {
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return int8(fileTypeSymbolicLink)
	}

	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return int8(fileTypeDirectory)
	}

	return int8(fileTypeRegularFile)
}

func fdstat(file *os.File) (filestat, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info)
	if err != nil {
		return filestat{}, err
	}

	return statFromHandleFileInformation(&info), nil
}

func readDirEntries(dir *os.File) ([]dirEntry, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(dir.Fd()), &info)
	if err != nil {
		return nil, err
	}
	dirInode := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)

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
		dirEntry{name: ".", fileType: int8(fileTypeDirectory), ino: dirInode},
		dirEntry{name: "..", fileType: int8(fileTypeDirectory), ino: 0},
	)

	for _, entry := range entries {
		fs, err := statFromPath(filepath.Join(dir.Name(), entry.Name()), false)
		if err != nil {
			return nil, err
		}

		result = append(result, dirEntry{
			name:     entry.Name(),
			fileType: fileTypeFromMode(entry.Type()),
			ino:      fs.ino,
		})
	}

	return result, nil
}

func setFdFlags(file *os.File, fdFlags int32) error {
	// Not supported on Windows
	return nil
}

func utimes(
	dir *os.File,
	path string,
	atim, mtim int64,
	fstFlags int32,
	followSymlinks bool,
) error {
	parentPath, name, err := resolvePath(dir, path, followSymlinks)
	if err != nil {
		return err
	}

	pathPtr, err := syscall.UTF16PtrFromString(filepath.Join(parentPath, name))
	if err != nil {
		return err
	}

	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !followSymlinks {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}

	h, err := windows.CreateFile(
		pathPtr,
		windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.Close(h)

	return setFileTime(h, atim, mtim, fstFlags)
}

func utimesNanoAt(file *os.File, atim, mtim int64, fstFlags int32) error {
	return setFileTime(windows.Handle(file.Fd()), atim, mtim, fstFlags)
}

func setFileTime(h windows.Handle, atim, mtim int64, fstFlags int32) error {
	if (fstFlags&fstFlagsAtim != 0) && (fstFlags&fstFlagsAtimNow != 0) {
		return os.ErrInvalid
	}
	if (fstFlags&fstFlagsMtim != 0) && (fstFlags&fstFlagsMtimNow != 0) {
		return os.ErrInvalid
	}

	var aptr, mptr *windows.Filetime

	if fstFlags&fstFlagsAtim != 0 {
		ft := windows.NsecToFiletime(atim)
		aptr = &ft
	} else if fstFlags&fstFlagsAtimNow != 0 {
		ft := windows.NsecToFiletime(time.Now().UnixNano())
		aptr = &ft
	}

	if fstFlags&fstFlagsMtim != 0 {
		ft := windows.NsecToFiletime(mtim)
		mptr = &ft
	} else if fstFlags&fstFlagsMtimNow != 0 {
		ft := windows.NsecToFiletime(time.Now().UnixNano())
		mptr = &ft
	}

	if aptr == nil && mptr == nil {
		return nil
	}

	return windows.SetFileTime(h, nil, aptr, mptr)
}

func writeAt(
	file *os.File,
	data []byte,
	offset int64,
	hasAppendFlag bool,
) (int, error) {
	return file.WriteAt(data, offset)
}

// fdWrite writes data to a file descriptor, respecting the current fd flags.
// On Windows, we must manually handle O_APPEND because Windows cannot change
// file access modes after opening, so we check the tracked flags and seek to
// end before writing if append mode is set.
func fdWrite(file *os.File, data []byte, fdFlags uint16) (int, error) {
	if fdFlags&fdFlagsAppend != 0 {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			return 0, err
		}
	}
	return file.Write(data)
}

func linkat(
	oldDir *os.File,
	oldPath string,
	followSymlinks bool,
	newDir *os.File,
	newPath string,
) error {
	if followSymlinks {
		return syscall.EINVAL
	}

	if hasTrailingSlash(newPath) {
		return syscall.ENOENT
	}

	oldParent, oldName, err := resolvePath(oldDir, oldPath, false)
	if err != nil {
		return err
	}
	oldFullPath := filepath.Join(oldParent, oldName)

	newParent, newName, err := resolvePath(newDir, newPath, false)
	if err != nil {
		return err
	}
	newFullPath := filepath.Join(newParent, newName)

	return os.Link(oldFullPath, newFullPath)
}

func readlink(dir *os.File, path string) (string, error) {
	parentPath, name, err := resolvePath(dir, path, false)
	if err != nil {
		return "", err
	}
	return os.Readlink(filepath.Join(parentPath, name))
}

func rmdirat(dir *os.File, path string) error {
	parentPath, name, err := resolvePath(dir, path, false)
	if err != nil {
		return err
	}

	if name == "." {
		return os.ErrInvalid // Cannot remove "."
	}

	pathPtr, err := syscall.UTF16PtrFromString(filepath.Join(parentPath, name))
	if err != nil {
		return err
	}

	return windows.RemoveDirectory(pathPtr)
}

func renameat(
	oldDir *os.File,
	oldPath string,
	newDir *os.File,
	newPath string,
) error {
	oldParent, oldName, err := resolvePath(oldDir, oldPath, false)
	if err != nil {
		return err
	}
	oldFullPath := filepath.Join(oldParent, oldName)

	newParent, newName, err := resolvePath(newDir, newPath, false)
	if err != nil {
		return err
	}
	newFullPath := filepath.Join(newParent, newName)

	oldFi, err := os.Stat(oldFullPath)
	if err != nil {
		return err
	}

	if oldFi.IsDir() {
		if newFi, err := os.Stat(newFullPath); err == nil {
			if !newFi.IsDir() {
				return syscall.ENOTDIR
			}
		}
	} else {
		// Source is a file; target must not be a directory
		// On Windows, we return ACCESS_DENIED to match expected WASI behavior
		if newFi, err := os.Stat(newFullPath); err == nil {
			if newFi.IsDir() {
				return windows.ERROR_ACCESS_DENIED
			}
		}
	}

	oldPtr, err := syscall.UTF16PtrFromString(oldFullPath)
	if err != nil {
		return err
	}
	newPtr, err := syscall.UTF16PtrFromString(newFullPath)
	if err != nil {
		return err
	}

	err = windows.MoveFileEx(oldPtr, newPtr, windows.MOVEFILE_REPLACE_EXISTING)
	if err == nil {
		return nil
	}

	if err == windows.ERROR_ACCESS_DENIED {
		fi, statErr := os.Stat(newFullPath)
		if statErr != nil || !fi.IsDir() {
			return err // We return the original error.
		}

		if rerr := os.Remove(newFullPath); rerr != nil {
			return err // We return the original error.
		}

		return windows.MoveFileEx(oldPtr, newPtr, windows.MOVEFILE_REPLACE_EXISTING)
	}

	return err
}

func symlinkat(target string, dir *os.File, path string) error {
	if hasTrailingSlash(path) {
		return syscall.ENOENT
	}

	if !isRelativePath(target) {
		return syscall.EPERM
	}

	parentPath, name, err := resolvePath(dir, path, false)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(parentPath, name)

	// On Windows, WASI expects ENOENT when the symlink path already exists
	if _, err := os.Lstat(fullPath); err == nil {
		return syscall.ENOENT
	}

	targetPath := filepath.Join(filepath.Dir(fullPath), target)

	var flags uint32 = symlinkFlagAllowUnprivileged
	// Check if target exists and is a directory
	if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
		flags |= windows.SYMBOLIC_LINK_FLAG_DIRECTORY
	}

	// Convert forward slashes to backslashes for Windows
	targetPtr, err := syscall.UTF16PtrFromString(filepath.FromSlash(target))
	if err != nil {
		return err
	}
	pathPtr, err := syscall.UTF16PtrFromString(fullPath)
	if err != nil {
		return err
	}

	return windows.CreateSymbolicLink(pathPtr, targetPtr, flags)
}

func unlinkat(dir *os.File, path string) error {
	parentPath, name, err := resolvePath(dir, path, false)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(parentPath, name)

	if hasTrailingSlash(path) {
		fi, err := os.Stat(fullPath)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return syscall.ENOTDIR
		}
		return syscall.EISDIR
	}

	pathPtr, err := syscall.UTF16PtrFromString(fullPath)
	if err != nil {
		return err
	}

	err = windows.DeleteFile(pathPtr)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			fi, lerr := os.Lstat(fullPath)
			if lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
				// Reparse point (likely directory symlink)
				return windows.RemoveDirectory(pathPtr)
			}
		}
		return err
	}
	return nil
}

func openat(
	dir *os.File,
	path string,
	followSymlinks bool,
	oflags, fdflags int32,
	fsRights uint64,
) (*os.File, error) {
	parentPath, name, err := resolvePath(dir, path, followSymlinks)
	if err != nil {
		return nil, err
	}
	fullPath := filepath.Join(parentPath, name)

	// WASI expects trailing slash to imply directory required
	if hasTrailingSlash(path) {
		fi, err := os.Stat(fullPath)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, syscall.ENOTDIR
		}
	}

	var access uint32 = windows.GENERIC_READ
	if fsRights&uint64(RightsFdWrite) != 0 || fsRights&uint64(RightsFdFilestatSetSize) != 0 {
		access |= windows.GENERIC_WRITE
	}
	// O_TRUNC on Windows requires GENERIC_WRITE access
	if oflags&int32(oFlagsTrunc) != 0 {
		access |= windows.GENERIC_WRITE
	}

	var creationDisposition uint32
	if oflags&int32(oFlagsCreat) != 0 {
		if oflags&int32(oFlagsExcl) != 0 {
			creationDisposition = windows.CREATE_NEW
		} else if oflags&int32(oFlagsTrunc) != 0 {
			creationDisposition = windows.CREATE_ALWAYS
		} else {
			creationDisposition = windows.OPEN_ALWAYS
		}
	} else {
		if oflags&int32(oFlagsTrunc) != 0 {
			creationDisposition = windows.TRUNCATE_EXISTING
		} else {
			creationDisposition = windows.OPEN_EXISTING
		}
	}

	attrs := windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_BACKUP_SEMANTICS
	if !followSymlinks {
		attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}

	pathPtr, err := syscall.UTF16PtrFromString(fullPath)
	if err != nil {
		return nil, err
	}

	mode := windows.FILE_SHARE_READ |
		windows.FILE_SHARE_WRITE |
		windows.FILE_SHARE_DELETE
	h, err := windows.CreateFile(
		pathPtr,
		access,
		uint32(mode),
		nil,
		creationDisposition,
		uint32(attrs),
		0,
	)
	if err != nil {
		return nil, err
	}

	// WASI requires ELOOP if we encounter a symlink when O_NOFOLLOW is set.
	// CreateFile with OPEN_REPARSE_POINT opens the symlink itself.
	if !followSymlinks {
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err == nil {
			if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				windows.Close(h)
				return nil, syscall.ELOOP
			}
		}
	}

	// Verify we didn't open a directory with write access, which WASI forbids.
	// On Windows, FILE_FLAG_BACKUP_SEMANTICS allows opening directories, and
	// GENERIC_WRITE doesn't necessarily fail immediately.
	if access&windows.GENERIC_WRITE != 0 {
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err == nil {
			if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
				// If we opened a directory with GENERIC_WRITE, check if we actually requested
				// rights that are incompatible with directories (like writing data).
				// We allow RightsFdFilestatSetSize (which adds GENERIC_WRITE) on directories
				// because it's part of DefaultDirRights, even though SetSize fails on dirs.
				// But we must reject RightsFdWrite or O_TRUNC.
				if fsRights&uint64(RightsFdWrite) != 0 || oflags&int32(oFlagsTrunc) != 0 {
					windows.Close(h)
					return nil, syscall.EISDIR
				}
			}
		}
	}

	f := os.NewFile(uintptr(h), fullPath)
	if fdflags&int32(fdFlagsAppend) != 0 {
		f.Seek(0, io.SeekEnd)
	}

	if oflags&int32(oFlagsDirectory) != 0 {
		// Use file handle info if possible to avoid race/extra stat
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err == nil {
			// On Windows, reparse points (symlinks) also have FILE_ATTRIBUTE_DIRECTORY if they point to a dir,
			// or if they are directory junctions.
			// But if we opened it with OPEN_REPARSE_POINT (because !followSymlinks), we see the reparse point logic above returned ELOOP.
			// If we are here, we followed symlinks OR it's not a reparse point.
			// So checking FILE_ATTRIBUTE_DIRECTORY is correct.
			if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				f.Close()
				return nil, syscall.ENOTDIR
			}
		} else {
			// Fallback
			stat, err := f.Stat()
			if err != nil {
				f.Close()
				return nil, err
			}
			if !stat.IsDir() {
				f.Close()
				return nil, syscall.ENOTDIR
			}
		}
	}

	return f, nil
}

func accept(file *os.File) (int, error) {
	// This is a simplified implementation. A proper one would use syscalls.
	return 0, syscall.EOPNOTSUPP
}

func shutdown(file *os.File, how int32) error {
	var sdn int
	switch how {
	case shutRd:
		sdn = windows.SHUT_RD
	case shutWr:
		sdn = windows.SHUT_WR
	case shutRdWr:
		sdn = windows.SHUT_RDWR
	default:
		return syscall.EINVAL
	}
	return windows.Shutdown(windows.Handle(file.Fd()), sdn)
}

func fileTypeFromMode(mode os.FileMode) int8 {
	switch {
	case mode.IsDir():
		return int8(fileTypeDirectory)
	case mode.IsRegular():
		return int8(fileTypeRegularFile)
	case mode&os.ModeSymlink != 0:
		return int8(fileTypeSymbolicLink)
	case mode&os.ModeSocket != 0:
		return int8(fileTypeSocketStream)
	case mode&os.ModeNamedPipe != 0:
		return int8(fileTypeCharacterDevice)
	case mode&os.ModeCharDevice != 0:
		return int8(fileTypeCharacterDevice)
	case mode&os.ModeDevice != 0:
		return int8(fileTypeBlockDevice)
	default:
		return int8(fileTypeUnknown)
	}
}

func mapError(err error) int32 {
	// Check specific Windows errors first
	var werr windows.Errno
	if errors.As(err, &werr) {
		switch werr {
		case windows.ERROR_DIR_NOT_EMPTY:
			return errnoNotEmpty
		case windows.ERROR_ALREADY_EXISTS:
			return errnoExist
		case windows.ERROR_FILE_EXISTS:
			return errnoExist
		case windows.ERROR_ACCESS_DENIED:
			return errnoAcces
		case windows.ERROR_SHARING_VIOLATION:
			return errnoBus
		case windows.ERROR_PRIVILEGE_NOT_HELD:
			return errnoPerm
		case windows.ERROR_INVALID_NAME:
			return errnoNoEnt
		case windows.ERROR_DIRECTORY:
			return errnoNotDir
		case windows.ERROR_NEGATIVE_SEEK:
			return errnoInval
		}
	}

	// Check syscall errors (these include Go's abstracted errno values)
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
		case syscall.EIO:
			return errnoIO
		case syscall.ENOTEMPTY:
			return errnoNotEmpty
		case syscall.ELOOP:
			return errnoLoop
		case syscall.EBADF:
			return errnoBadF
		case syscall.EPIPE:
			return errnoPipe
		case windows.ERROR_SHARING_VIOLATION:
			return errnoBus
		case windows.ERROR_ALREADY_EXISTS:
			return errnoExist
		case windows.ERROR_FILE_EXISTS:
			return errnoExist
		}
	}

	// Check generic os errors as fallback
	if errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || errors.Is(err, syscall.ENOTEMPTY) {
		return errnoNotEmpty
	}
	if errors.Is(err, os.ErrNotExist) {
		return errnoNoEnt
	}
	if errors.Is(err, os.ErrExist) {
		return errnoExist
	}
	if errors.Is(err, os.ErrPermission) {
		return errnoAcces
	}
	if errors.Is(err, os.ErrInvalid) {
		return errnoInval
	}

	return errnoNotCapable
}

func isRelativePath(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return false
	}
	return !filepath.IsAbs(path) && !strings.HasPrefix(path, "..")
}

func hasTrailingSlash(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}

func statFromHandleFileInformation(info *windows.ByHandleFileInformation) filestat {
	return filestat{
		dev:      uint64(info.VolumeSerialNumber),
		ino:      uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		filetype: fileTypeFromWin32Attributes(info.FileAttributes),
		nlink:    uint64(info.NumberOfLinks),
		size:     uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow),
		atim:     uint64(info.LastAccessTime.Nanoseconds()),
		mtim:     uint64(info.LastWriteTime.Nanoseconds()),
	}
}

// resolvePath resolves a path relative to a directory os.File.
// It returns the absolute path of the parent directory and the final name.
func resolvePath(
	dir *os.File,
	path string,
	followSymlinks bool,
) (string, string, error) {
	return resolvePathInternal(dir.Name(), path, followSymlinks, 0)
}

func resolvePathInternal(
	rootPath string,
	path string,
	followSymlinks bool,
	depth int,
) (string, string, error) {
	if depth >= maxSymlinkDepth {
		return "", "", syscall.ELOOP
	}

	if !isRelativePath(path) {
		return "", "", syscall.EPERM
	}

	comps, err := getComponents(path)
	if err != nil {
		return "", "", err
	}

	// Handle special case of "."
	if len(comps) == 1 && comps[0] == "." {
		return rootPath, ".", nil
	}

	parentPath, newDepth, err := walkToParent(rootPath, comps, depth)
	if err != nil {
		return "", "", err
	}

	finalName := comps[len(comps)-1]

	// Check final component symlink if needed
	if followSymlinks {
		fullPath := filepath.Join(parentPath, finalName)
		fi, err := os.Lstat(fullPath)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := resolveSymlink(rootPath, parentPath, finalName)
			if err != nil {
				return "", "", err
			}

			// If resolved is absolute and inside root, we can make it relative.
			rel, err := filepath.Rel(rootPath, resolved)
			if err != nil {
				return "", "", err
			}
			return resolvePathInternal(rootPath, rel, true, newDepth+1)
		}
	}

	return parentPath, finalName, nil
}

func walkToParent(
	rootPath string,
	components []string,
	depth int,
) (string, int, error) {
	if depth >= maxSymlinkDepth {
		return "", depth, syscall.ELOOP
	}

	currentPath := rootPath

	// Restart helper
	restart := func(newBaseAbs string, remain []string) (string, int, error) {
		// Calculate new relative path from root to newBaseAbs
		rel, err := filepath.Rel(rootPath, newBaseAbs)
		if err != nil {
			return "", depth, err
		}

		newComponents := append(splitPath(rel), remain...)
		if len(newComponents) == 0 {
			newComponents = []string{"."}
		}

		return walkToParent(rootPath, newComponents, depth+1)
	}

	for i, component := range components[:len(components)-1] {
		if component == "." {
			continue
		}

		if component == ".." {
			// Check if we're at the root
			if currentPath == rootPath {
				return "", depth, syscall.EPERM
			}
			currentPath = filepath.Dir(currentPath)
			// Verify we didn't escape (should be redundant if we check currentPath == rootPath)
			if !strings.HasPrefix(currentPath, rootPath) {
				return "", depth, syscall.EPERM
			}
			continue
		}

		nextPath := filepath.Join(currentPath, component)

		// Check for symlink
		fi, err := os.Lstat(nextPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Component doesn't exist, cannot traverse
				return "", depth, syscall.ENOENT
			}
			return "", depth, err
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := resolveSymlink(rootPath, currentPath, component)
			if err != nil {
				return "", depth, err
			}
			return restart(resolved, components[i+1:])
		}

		if !fi.IsDir() {
			return "", depth, syscall.ENOTDIR
		}

		currentPath = nextPath
	}

	return currentPath, depth, nil
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
		return r == filepath.Separator || r == '/'
	})
	if len(parts) == 0 {
		return []string{"."}
	}
	return parts
}

// resolveSymlink reads a symlink and returns a sandbox-safe resolved path.
//
// Parameters:
//   - rootPath: absolute path of the sandbox root
//   - parentPath: absolute path of the directory containing the symlink
//   - name: name of the symlink within the parent directory
//
// Returns the resolved absolute path.
func resolveSymlink(rootPath, parentPath, name string) (string, error) {
	fullPath := filepath.Join(parentPath, name)
	target, err := os.Readlink(fullPath)
	if err != nil {
		return "", err
	}

	var resolved string
	if filepath.IsAbs(target) || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "\\") {
		// Absolute targets are resolved relative to the sandbox root.
		cleanTarget := filepath.Clean(target)

		// Remove volume/drive if present
		vol := filepath.VolumeName(cleanTarget)
		rel := cleanTarget[len(vol):]
		rel = strings.TrimLeft(rel, "\\/")
		resolved = filepath.Join(rootPath, rel)
	} else {
		resolved = filepath.Clean(filepath.Join(parentPath, target))
	}

	// Check if escapes root
	if !strings.HasPrefix(resolved, rootPath) {
		return "", syscall.EPERM
	}

	return resolved, nil
}
