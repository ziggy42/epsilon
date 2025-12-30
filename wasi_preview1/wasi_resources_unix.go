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

package wasi_preview1

import (
	"os"

	"golang.org/x/sys/unix"
)

// getFilestat returns a filestat from a file.
func getFilestat(file *os.File) (filestat, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return filestat{}, err
	}
	return filestat{
		dev:      uint64(stat.Dev),
		ino:      stat.Ino,
		filetype: getFileTypeFromMode(uint32(stat.Mode)),
		nlink:    uint64(stat.Nlink),
		size:     uint64(stat.Size),
		atim:     uint64(stat.Atim.Sec*1e9 + stat.Atim.Nsec),
		mtim:     uint64(stat.Mtim.Sec*1e9 + stat.Mtim.Nsec),
		ctim:     uint64(stat.Ctim.Sec*1e9 + stat.Ctim.Nsec),
	}, nil
}

// getFilestatFromPath returns a filestat from a path relative to dir.
func getFilestatFromPath(
	dir *os.File,
	path string,
	followSymlink bool,
) (filestat, error) {
	var flags int
	if !followSymlink {
		flags = unix.AT_SYMLINK_NOFOLLOW
	}

	var stat unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), path, &stat, flags); err != nil {
		return filestat{}, err
	}

	return filestat{
		dev:      uint64(stat.Dev),
		ino:      stat.Ino,
		filetype: getFileTypeFromMode(uint32(stat.Mode)),
		nlink:    uint64(stat.Nlink),
		size:     uint64(stat.Size),
		atim:     uint64(stat.Atim.Sec*1e9 + stat.Atim.Nsec),
		mtim:     uint64(stat.Mtim.Sec*1e9 + stat.Mtim.Nsec),
		ctim:     uint64(stat.Ctim.Sec*1e9 + stat.Ctim.Nsec),
	}, nil
}

// setFdFlags sets the file descriptor flags using fcntl F_SETFL.
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

// linkat creates a hard link from oldPath (relative to oldDir) to newPath
// (relative to newDir). If followSymlink is true, symlinks are followed when
// resolving oldPath.
func linkat(
	oldDir *os.File,
	oldPath string,
	newDir *os.File,
	newPath string,
	followSymlink bool,
) error {
	var flags int
	if followSymlink {
		flags = unix.AT_SYMLINK_FOLLOW
	}
	oldDirFd := int(oldDir.Fd())
	newDirFd := int(newDir.Fd())
	return unix.Linkat(oldDirFd, oldPath, newDirFd, newPath, flags)
}

// renameat renames/moves oldPath (relative to oldDir) to newPath (relative to
// newDir).
func renameat(
	oldDir *os.File,
	oldPath string,
	newDir *os.File,
	newPath string,
) error {
	return unix.Renameat(int(oldDir.Fd()), oldPath, int(newDir.Fd()), newPath)
}

// getFileTypeFromMode converts Unix stat mode bits to a WASI file type.
func getFileTypeFromMode(mode uint32) wasiFileType {
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return fileTypeDirectory
	case unix.S_IFREG:
		return fileTypeRegularFile
	case unix.S_IFLNK:
		return fileTypeSymbolicLink
	case unix.S_IFSOCK:
		return fileTypeSocketStream
	case unix.S_IFBLK:
		return fileTypeBlockDevice
	case unix.S_IFCHR:
		return fileTypeCharacterDevice
	case unix.S_IFIFO:
		return fileTypeCharacterDevice
	default:
		return fileTypeUnknown
	}
}

func getInodeByPath(path string) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, err
	}
	return stat.Ino, nil
}

func getInode(file *os.File) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return 0, err
	}
	return stat.Ino, nil
}

// getTimestamps returns the access and modification times in ns for the given
// file.
func getTimestamps(file *os.File) (int64, int64, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return 0, 0, err
	}
	atimNs := stat.Atim.Sec*1e9 + stat.Atim.Nsec
	mtimNs := stat.Mtim.Sec*1e9 + stat.Mtim.Nsec
	return atimNs, mtimNs, nil
}

// getTimestampsFromPath returns the access and modification times in ns for the
// path relative to dir.
func getTimestampsFromPath(
	dir *os.File,
	path string,
	followSymlink bool,
) (int64, int64, error) {
	var flags int
	if !followSymlink {
		flags = unix.AT_SYMLINK_NOFOLLOW
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), path, &stat, flags); err != nil {
		return 0, 0, err
	}
	atimNs := stat.Atim.Sec*1e9 + stat.Atim.Nsec
	mtimNs := stat.Mtim.Sec*1e9 + stat.Mtim.Nsec
	return atimNs, mtimNs, nil
}

// utimesNanoAt sets the access and modification times for the path relative to
// dir.
func utimesNanoAt(
	dir *os.File,
	path string,
	atimNs, mtimNs int64,
	followSymlink bool,
) error {
	ts := []unix.Timespec{
		{Sec: atimNs / 1e9, Nsec: atimNs % 1e9},
		{Sec: mtimNs / 1e9, Nsec: mtimNs % 1e9},
	}
	var flags int
	if !followSymlink {
		flags = unix.AT_SYMLINK_NOFOLLOW
	}
	return unix.UtimesNanoAt(int(dir.Fd()), path, ts, flags)
}

// utimesNano sets the access and modification times for the file descriptor by
// using its path. This relies on the path being reachable from the current
// working directory.
func utimesNano(file *os.File, atimNs, mtimNs int64) error {
	ts := []unix.Timespec{
		{Sec: atimNs / 1e9, Nsec: atimNs % 1e9},
		{Sec: mtimNs / 1e9, Nsec: mtimNs % 1e9},
	}
	return unix.UtimesNanoAt(unix.AT_FDCWD, file.Name(), ts, 0)
}

// accept accepts a connection on the socket file descriptor.
func accept(file *os.File) (int, error) {
	nfd, _, err := unix.Accept(int(file.Fd()))
	return nfd, err
}
