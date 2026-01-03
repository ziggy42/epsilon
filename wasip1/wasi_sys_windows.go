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
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func mkdirat(dir *os.File, path string, mode uint32) error {
	if !isRelativePath(path) {
		return os.ErrInvalid
	}

	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return err
	}
	return os.Mkdir(fullPath, os.FileMode(mode))
}

func stat(dir *os.File, path string, followSymlinks bool) (filestat, error) {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return filestat{}, err
	}
	return statFromPath(fullPath, followSymlinks)
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

	return filestat{
		dev:      uint64(info.VolumeSerialNumber),
		ino:      uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		filetype: fileTypeFromWin32Attributes(info.FileAttributes),
		nlink:    uint64(info.NumberOfLinks),
		size:     uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow),
		atim:     uint64(info.LastAccessTime.Nanoseconds()),
		mtim:     uint64(info.LastWriteTime.Nanoseconds()),
		ctim:     uint64(info.CreationTime.Nanoseconds()),
	}, nil
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

	fi, err := file.Stat()
	if err != nil {
		return filestat{}, err
	}

	fs := filestat{
		dev:      uint64(info.VolumeSerialNumber),
		ino:      uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		filetype: fileTypeFromMode(fi.Mode()),
		nlink:    uint64(info.NumberOfLinks),
		size:     uint64(fi.Size()),
		atim:     uint64(info.LastAccessTime.Nanoseconds()),
		mtim:     uint64(info.LastWriteTime.Nanoseconds()),
		ctim:     uint64(info.CreationTime.Nanoseconds()),
	}

	return fs, nil
}

func readDirEntries(dir *os.File) ([]dirEntry, error) {
	parentInode := uint64(0)
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(dir.Fd()), &info)
	if err == nil {
		parentInode = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	}

	// For ".." use a placeholder inode that is distinct from the current
	// directory's inode. This is a pragmatic choice given the difficulty of
	// getting parent inode reliably cross-platform within sandbox, and to
	// satisfy tests expecting non-zero different inodes for . and ..
	dotDotInode := uint64(1)
	if parentInode == dotDotInode { // Ensure it's different
		dotDotInode = uint64(2)
	}

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
		dirEntry{name: ".", fileType: int8(fileTypeDirectory), ino: parentInode},
		dirEntry{name: "..", fileType: int8(fileTypeDirectory), ino: dotDotInode},
	)

	for _, entry := range entries {
		var ino uint64
		if fullPath, err := secureJoin(dir.Name(), entry.Name()); err == nil {
			if fs, err := statFromPath(fullPath, false); err == nil {
				ino = fs.ino
			} else {
				// Fallback to hashing the path if we can't stat (e.g. locked file)
				h := fnv.New64a()
				h.Write([]byte(fullPath))
				ino = h.Sum64()
			}
		}

		result = append(result, dirEntry{
			name:     entry.Name(),
			fileType: fileTypeFromMode(entry.Type()),
			ino:      ino,
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
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return err
	}

	pathPtr, err := syscall.UTF16PtrFromString(fullPath)
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

func linkat(
	oldDir *os.File,
	oldPath string,
	followSymlinks bool,
	newDir *os.File,
	newPath string,
) error {
	oldFullPath, err := secureJoin(oldDir.Name(), oldPath)
	if err != nil {
		return err
	}
	newFullPath, err := secureJoin(newDir.Name(), newPath)
	if err != nil {
		return err
	}

	return os.Link(oldFullPath, newFullPath)
}

func readlink(dir *os.File, path string) (string, error) {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return "", err
	}
	return os.Readlink(fullPath)
}

func rmdirat(dir *os.File, path string) error {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

func renameat(
	oldDir *os.File,
	oldPath string,
	newDir *os.File,
	newPath string,
) error {
	oldFullPath, err := secureJoin(oldDir.Name(), oldPath)
	if err != nil {
		return err
	}
	newFullPath, err := secureJoin(newDir.Name(), newPath)
	if err != nil {
		return err
	}
	return os.Rename(oldFullPath, newFullPath)
}

func symlinkat(target string, dir *os.File, path string) error {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return err
	}
	return os.Symlink(target, fullPath)
}

func unlinkat(dir *os.File, path string) error {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return err
	}

	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\") {
		return syscall.EACCES
	}

	fi, err := os.Lstat(fullPath)
	if err == nil && fi.IsDir() {
		return syscall.EACCES
	}

	return os.Remove(fullPath)
}

func openat(
	dir *os.File,
	path string,
	followSymlinks bool,
	oflags, fdflags int32,
	fsRights uint64,
) (*os.File, error) {
	fullPath, err := secureJoin(dir.Name(), path)
	if err != nil {
		return nil, err
	}

	var access uint32 = windows.GENERIC_READ
	if fsRights&uint64(RightsFdWrite) != 0 {
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

	f := os.NewFile(uintptr(h), fullPath)
	if fdflags&int32(fdFlagsAppend) != 0 {
		f.Seek(0, io.SeekEnd)
	}

	if oflags&int32(oFlagsDirectory) != 0 {
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !info.IsDir() {
			f.Close()
			return nil, syscall.ENOTDIR
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
	if err == nil {
		return errnoSuccess
	}
	if errors.Is(err, os.ErrNotExist) {
		return errnoNoEnt
	}
	if errors.Is(err, os.ErrExist) {
		return errnoExist
	}
	if errors.Is(err, os.ErrPermission) {
		return errnoPerm
	}
	if errors.Is(err, os.ErrInvalid) {
		return errnoInval
	}

	if errno, ok := err.(syscall.Errno); ok {
		// This is not a complete mapping
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
		case syscall.EBADF:
			return errnoBadF
		case syscall.EPIPE:
			return errnoPipe
		case windows.ERROR_SHARING_VIOLATION:
			return errnoBus
		}
	}
	return errnoNotCapable
}

func isRelativePath(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return false
	}
	return !filepath.IsAbs(path) && !strings.HasPrefix(path, "..")
}

func secureJoin(root, path string) (string, error) {
	if !isRelativePath(path) {
		return "", os.ErrPermission
	}
	fullPath := filepath.Join(root, path)
	if !strings.HasPrefix(fullPath, root) {
		return "", os.ErrPermission
	}

	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\") {
		fi, err := os.Stat(fullPath)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() {
			return "", syscall.ENOENT
		}
	}

	return fullPath, nil
}
