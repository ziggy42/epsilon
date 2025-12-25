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

package wasi_preview1

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
	"golang.org/x/sys/unix"
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

const preopenTypeDir uint8 = 0

const (
	whenceSet uint8 = 0 // Seek relative to start-of-file.
	whenceCur uint8 = 1 // Seek relative to current position.
	whenceEnd uint8 = 2 // Seek relative to end-of-file.
)

type wasiFileType uint8

const (
	fileTypeUnknown         wasiFileType = 0
	fileTypeBlockDevice     wasiFileType = 1
	fileTypeCharacterDevice wasiFileType = 2
	fileTypeDirectory       wasiFileType = 3
	fileTypeRegularFile     wasiFileType = 4
	fileTypeSocketDgram     wasiFileType = 5
	fileTypeSocketStream    wasiFileType = 6
	fileTypeSymbolicLink    wasiFileType = 7
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

const lookupFlagsSymlinkFollow int32 = 1 << 0

type WasiPreopenDir struct {
	HostPath         string
	GuestPath        string
	Rights           int64
	RightsInheriting int64
}

type dirEntry struct {
	name     string
	fileType wasiFileType
	ino      uint64
}

type wasiFileDescriptor struct {
	file             *os.File
	fileType         wasiFileType
	flags            uint16
	rights           int64
	rightsInheriting int64
	guestPath        string
	inode            uint64
	isPreopen        bool
}

type wasiResourceTable struct {
	fds map[int32]*wasiFileDescriptor
}

func newWasiResourceTable(
	preopenDirs []WasiPreopenDir,
) (*wasiResourceTable, error) {
	stdRights := RightsFdFilestatGet | RightsPollFdReadwrite
	stdin, err := newStdFileDescriptor(os.Stdin, RightsFdRead|stdRights)
	if err != nil {
		return nil, err
	}
	stdout, err := newStdFileDescriptor(os.Stdout, RightsFdWrite|stdRights)
	if err != nil {
		return nil, err
	}
	stderr, err := newStdFileDescriptor(os.Stderr, RightsFdWrite|stdRights)
	if err != nil {
		return nil, err
	}

	resourceTable := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{0: stdin, 1: stdout, 2: stderr},
	}
	for _, dir := range preopenDirs {
		fd, err := newPreopenFileDescriptor(dir)
		if err != nil {
			return nil, err
		}
		resourceTable.fds[resourceTable.allocateFdIndex()] = fd
	}
	return resourceTable, nil
}

func newFileDescriptor(
	file *os.File,
	rights, rightsInheriting int64,
	flags uint16,
) (*wasiFileDescriptor, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	mode := info.Mode()
	if mode.IsDir() {
		// TODO: why do we need to do this?
		rights &= ^(RightsFdSeek | RightsFdTell | RightsFdRead | RightsFdWrite)
	}

	ino, err := getInode(file.Name())
	if err != nil {
		return nil, err
	}

	return &wasiFileDescriptor{
		file:             file,
		fileType:         getModeFileType(mode),
		flags:            flags,
		rights:           rights,
		rightsInheriting: rightsInheriting,
		guestPath:        "",
		inode:            ino,
		isPreopen:        false,
	}, nil
}

func newPreopenFileDescriptor(
	preopenDir WasiPreopenDir,
) (*wasiFileDescriptor, error) {
	file, err := os.Open(preopenDir.HostPath)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.IsDir() {
		file.Close()
		return nil, fmt.Errorf("%q is not a directory", preopenDir.HostPath)
	}

	rights := preopenDir.Rights
	fd, err := newFileDescriptor(file, rights, preopenDir.RightsInheriting, 0)
	if err != nil {
		file.Close()
		return nil, err
	}
	fd.guestPath = preopenDir.GuestPath
	fd.isPreopen = true
	return fd, nil
}

func newStdFileDescriptor(
	file *os.File,
	rights int64,
) (*wasiFileDescriptor, error) {
	return newFileDescriptor(file, rights, 0, 0)
}

func (w *wasiResourceTable) advise(
	fdIndex int32,
	offset, length int64,
	advice int32,
) int32 {
	if _, ok := w.fds[fdIndex]; !ok {
		return errnoBadF
	}
	// This WASI implementation does not use the hints provided by this API.
	return errnoSuccess
}

func (w *wasiResourceTable) allocate(
	fdIndex int32,
	offset, length int64,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdAllocate)
	if errCode != errnoSuccess {
		return errCode
	}

	info, err := fd.file.Stat()
	if err != nil {
		return mapError(err)
	}

	targetSize := offset + length
	if targetSize <= info.Size() {
		return errnoSuccess
	}

	if err := fd.file.Truncate(targetSize); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) close(fdIndex int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	fd.file.Close()
	delete(w.fds, fdIndex)
	return errnoSuccess
}

func (w *wasiResourceTable) dataSync(fdIndex int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if err := fd.file.Sync(); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) getStat(
	inst *epsilon.ModuleInstance,
	fdIndex, fdStatPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	err = memory.StoreByte(0, uint32(fdStatPtr), uint8(fd.fileType))
	if err != nil {
		return errnoFault
	}
	err = memory.StoreUint16(0, uint32(fdStatPtr+2), fd.flags)
	if err != nil {
		return errnoFault
	}
	err = memory.StoreUint64(0, uint32(fdStatPtr+8), uint64(fd.rights))
	if err != nil {
		return errnoFault
	}
	err = memory.StoreUint64(0, uint32(fdStatPtr+16), uint64(fd.rightsInheriting))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) setStatFlags(fdIndex, fdFlags int32) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFdstatSetFlags)
	if errCode != errnoSuccess {
		return errCode
	}

	fd.flags = uint16(fdFlags)

	var osFlags int
	if fdFlags&int32(fdFlagsAppend) != 0 {
		osFlags |= unix.O_APPEND
	}
	if fdFlags&int32(fdFlagsNonblock) != 0 {
		osFlags |= unix.O_NONBLOCK
	}

	if _, err := unix.FcntlInt(fd.file.Fd(), unix.F_SETFL, osFlags); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) setStatRights(
	fdIndex int32,
	rightsBase, rightsInheriting int64,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	// Can only remove rights, not add them
	if (rightsBase & ^fd.rights) != 0 {
		return errnoNotCapable
	}
	if (rightsInheriting & ^fd.rightsInheriting) != 0 {
		return errnoNotCapable
	}

	fd.rights = rightsBase
	fd.rightsInheriting = rightsInheriting
	return errnoSuccess
}

func (w *wasiResourceTable) getFileStat(
	inst *epsilon.ModuleInstance,
	fdIndex, bufPtr int32,
) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFilestatGet)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	var stat unix.Stat_t
	if err := unix.Fstat(int(fd.file.Fd()), &stat); err != nil {
		return mapError(err)
	}

	return writeFilestat(memory, uint32(bufPtr), fd.fileType, stat)
}

func (w *wasiResourceTable) setFileStatSize(fdIndex int32, size int64) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFilestatSetSize)
	if errCode != errnoSuccess {
		return errCode
	}

	if err := fd.file.Truncate(size); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) setFileStatTimes(
	fdIndex int32,
	atim, mtim int64,
	fstFlags int32,
) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFilestatSetTimes)
	if errCode != errnoSuccess {
		return errCode
	}

	return computeAndSetTimestamps(
		fd.file.Name(),
		atim,
		mtim,
		fstFlags,
		0, // no special utime flags for fd-based operations
		func() (int64, int64, error) {
			var stat unix.Stat_t
			if err := unix.Fstat(int(fd.file.Fd()), &stat); err != nil {
				return 0, 0, err
			}
			atimNs := stat.Atim.Sec*1e9 + stat.Atim.Nsec
			mtimNs := stat.Mtim.Sec*1e9 + stat.Mtim.Nsec
			return atimNs, mtimNs, nil
		},
	)
}

func (w *wasiResourceTable) pread(
	inst *epsilon.ModuleInstance,
	fdIndex, iovecPtr, iovecLength int32,
	offset int64,
	nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdRead)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	var totalRead uint32
	for i := range iovecLength {
		ptr, length, err := readIovecItemPtr(memory, uint32(iovecPtr)+uint32(i*8))
		if err != nil {
			return errnoFault
		}

		buf := make([]byte, length)
		n, err := fd.file.ReadAt(buf, offset+int64(totalRead))
		if err != nil && err != io.EOF {
			return mapError(err)
		}

		if err := memory.Set(0, ptr, buf[:n]); err != nil {
			return errnoFault
		}

		totalRead += uint32(n)
		if n < int(length) {
			break
		}
	}

	if err := memory.StoreUint32(0, uint32(nPtr), totalRead); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) getPrestat(
	inst *epsilon.ModuleInstance,
	fdIndex, prestatPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	if !fd.isPreopen {
		return errnoBadF
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	// Write prestat struct directly to memory:
	// - Byte 0: type (always PreopenTypeDir = 0)
	// - Bytes 4-7: name length as uint32
	err = memory.StoreByte(0, uint32(prestatPtr), preopenTypeDir)
	if err != nil {
		return errnoFault
	}
	err = memory.StoreUint32(0, uint32(prestatPtr+4), uint32(len(fd.guestPath)))
	if err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) prestatDirName(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok || !fd.isPreopen {
		return errnoBadF
	}

	if int32(len(fd.guestPath)) > pathLen {
		return errnoNameTooLong
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	if err := memory.Set(0, uint32(pathPtr), []byte(fd.guestPath)); err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pwrite(
	inst *epsilon.ModuleInstance,
	fdIndex, ciovecPtr, ciovecLength int32,
	offset int64,
	nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdWrite)
	if errCode != errnoSuccess {
		return errCode
	}

	if offset < 0 {
		return errnoInval
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	var totalWritten uint32
	currentOffset := offset

	for i := range ciovecLength {
		data, err := readCiovecItem(memory, uint32(ciovecPtr)+uint32(i*8))
		if err != nil {
			return errnoFault
		}

		var n int
		var writeErr error
		if fd.flags&fdFlagsAppend != 0 {
			// Go's WriteAt returns error for O_APPEND files, but WASI expects it to
			// work (even if it appends on Linux). Use unix.Pwrite directly to bypass
			// Go's check.
			n, writeErr = unix.Pwrite(int(fd.file.Fd()), data, currentOffset)
		} else {
			n, writeErr = fd.file.WriteAt(data, currentOffset)
		}
		totalWritten += uint32(n)
		currentOffset += int64(n)

		if writeErr != nil {
			if totalWritten > 0 {
				break
			}
			return mapError(writeErr)
		}

		if n < len(data) {
			break
		}
	}

	if err := memory.StoreUint32(0, uint32(nPtr), totalWritten); err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) read(
	inst *epsilon.ModuleInstance,
	fdIndex, iovecPtr, iovecLength, nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdRead)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	var totalRead uint32
	for i := range iovecLength {
		ptr, length, err := readIovecItemPtr(memory, uint32(iovecPtr)+uint32(i*8))
		if err != nil {
			return errnoFault
		}

		buf := make([]byte, length)
		n, err := fd.file.Read(buf)
		if err != nil && err != io.EOF {
			return mapError(err)
		}

		if err := memory.Set(0, ptr, buf[:n]); err != nil {
			return errnoFault
		}

		totalRead += uint32(n)

		if n < int(length) {
			break
		}
	}

	if err := memory.StoreUint32(0, uint32(nPtr), totalRead); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) readdir(
	inst *epsilon.ModuleInstance,
	fdIndex, bufPtr, bufLen int32,
	cookie int64,
	bufusedPtr int32,
) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdReaddir)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	// Open fresh handle to ensure full listing regardless of seek state
	// Note: this avoids issues where previous reads consumed entries.
	// Since WASI cookies allow random access, we must be able to restart.
	dirFile, err := os.Open(fd.file.Name())
	if err != nil {
		return mapError(err)
	}
	defer dirFile.Close()

	// Synthesize . and .. entries
	dotIno, err := getInode(fd.file.Name())
	if err != nil {
		return mapError(err)
	}
	currentDir := dirEntry{name: ".", fileType: fileTypeDirectory, ino: dotIno}

	dotDotIno, err := getInode(filepath.Dir(fd.file.Name()))
	if err != nil {
		return mapError(err)
	}
	upperDir := dirEntry{name: "..", fileType: fileTypeDirectory, ino: dotDotIno}

	wasiEntries := []dirEntry{currentDir, upperDir}

	entries, err := dirFile.Readdir(-1)
	if err != nil {
		return mapError(err)
	}

	for _, entry := range entries {
		ino, err := getInode(filepath.Join(fd.file.Name(), entry.Name()))
		if err != nil {
			return mapError(err)
		}

		wasiEntries = append(wasiEntries, dirEntry{
			name:     entry.Name(),
			fileType: getModeFileType(entry.Mode()),
			ino:      ino,
		})
	}

	// Sort entries by name to ensure consistent order/cookies
	sort.Slice(wasiEntries, func(i, j int) bool {
		return wasiEntries[i].name < wasiEntries[j].name
	})

	// Cookie is essentially the index in the list
	startIndex := int(cookie)
	if startIndex >= len(wasiEntries) {
		memory.StoreUint32(0, uint32(bufusedPtr), 0)
		return errnoSuccess
	}

	bufOffset := uint32(bufPtr)
	bufEnd := bufOffset + uint32(bufLen)
	written := uint32(0)

	for i := startIndex; i < len(wasiEntries); i++ {
		entry := wasiEntries[i]
		nameLen := uint32(len(entry.name))
		entrySize := 24 + nameLen

		// Prepare the full entry bytes
		entryBytes := make([]byte, entrySize)
		nextCookie := uint64(i + 1)
		binary.LittleEndian.PutUint64(entryBytes[0:8], nextCookie)
		binary.LittleEndian.PutUint64(entryBytes[8:16], entry.ino)
		binary.LittleEndian.PutUint32(entryBytes[16:20], nameLen)
		entryBytes[20] = uint8(entry.fileType)
		// Padding bytes 21, 22, 23 are zero
		copy(entryBytes[24:], entry.name)

		available := bufEnd - bufOffset
		if available == 0 {
			break
		}

		toWrite := min(uint32(len(entryBytes)), available)

		if err := memory.Set(0, bufOffset, entryBytes[:toWrite]); err != nil {
			return errnoFault
		}

		bufOffset += toWrite
		written += toWrite

		// If we couldn't write the full entry, we stop here.
		// The client will see a full buffer and a partial last entry (or no last
		// entry if it didn't fit header), and should handle it (e.g. by re-reading
		// with larger buffer or checking cookies).
		if toWrite < uint32(len(entryBytes)) {
			break
		}
	}

	if err := memory.StoreUint32(0, uint32(bufusedPtr), written); err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) renumber(fdIndex, toFdIndex int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	toFd, exists := w.fds[toFdIndex]
	if !exists {
		return errnoBadF
	}

	toFd.file.Close()
	w.fds[toFdIndex] = fd
	delete(w.fds, fdIndex)
	return errnoSuccess
}

func (w *wasiResourceTable) seek(
	inst *epsilon.ModuleInstance,
	fdIndex int32,
	offset int64,
	whence, newOffsetPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdSeek)
	if errCode != errnoSuccess {
		return errCode
	}

	var goWhence int
	switch uint8(whence) {
	case whenceSet:
		goWhence = io.SeekStart
	case whenceCur:
		goWhence = io.SeekCurrent
	case whenceEnd:
		goWhence = io.SeekEnd
	default:
		return errnoInval
	}

	newOffset, err := fd.file.Seek(offset, goWhence)
	if err != nil {
		return mapError(err)
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	err = memory.StoreUint64(0, uint32(newOffsetPtr), uint64(newOffset))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) sync(fdIndex int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if err := fd.file.Sync(); err != nil {
		return errnoIO
	}
	return errnoSuccess
}

func (w *wasiResourceTable) tell(
	inst *epsilon.ModuleInstance,
	fdIndex, offsetPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdTell)
	if errCode != errnoSuccess {
		return errCode
	}

	// Get current position using Seek
	currentOffset, err := fd.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return mapError(err)
	}

	memory, memErr := inst.GetMemory(WASIMemoryExportName)
	if memErr != nil {
		return errnoFault
	}

	err = memory.StoreUint64(0, uint32(offsetPtr), uint64(currentOffset))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) write(
	inst *epsilon.ModuleInstance,
	fdIndex, ciovecPtr, ciovecLength, nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdWrite)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	var written uint32
	for i := range ciovecLength {
		data, err := readCiovecItem(memory, uint32(ciovecPtr)+uint32(i*8))
		if err != nil {
			return errnoFault
		}

		n, err := fd.file.Write(data)
		if err != nil {
			return mapError(err)
		}
		written += uint32(n)
		if n < len(data) {
			break
		}
	}

	if err := memory.StoreUint32(0, uint32(nPtr), written); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathCreateDirectory(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	rights := RightsPathCreateDirectory
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	if err := os.Mkdir(path, 0755); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathFilestatGet(
	inst *epsilon.ModuleInstance,
	fdIndex, flags, pathPtr, pathLen, filestatPtr int32,
) int32 {
	rights := RightsPathFilestatGet
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	// Get file info - follow symlinks unless LookupFlagsSymlinkFollow is NOT set
	var info os.FileInfo
	if flags&lookupFlagsSymlinkFollow != 0 {
		info, err = os.Stat(path)
	} else {
		info, err = os.Lstat(path)
	}
	if err != nil {
		return mapError(err)
	}

	// Get actual timestamps using unix.Stat or unix.Lstat
	// Follow symlinks based on the same flag used above
	var unixStat unix.Stat_t
	if flags&int32(lookupFlagsSymlinkFollow) != 0 {
		err = unix.Stat(path, &unixStat)
	} else {
		err = unix.Lstat(path, &unixStat)
	}
	if err != nil {
		return mapError(err)
	}

	fileType := getModeFileType(info.Mode())
	return writeFilestat(memory, uint32(filestatPtr), fileType, unixStat)
}

func (w *wasiResourceTable) pathFilestatSetTimes(
	inst *epsilon.ModuleInstance,
	fdIndex, flags int32, pathPtr, pathLen int32,
	atim, mtim int64,
	fstFlags int32,
) int32 {
	rights := RightsPathFilestatSetTimes
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	// Determine utime flags based on symlink follow flag
	utimeFlags := 0
	if flags&int32(lookupFlagsSymlinkFollow) == 0 {
		utimeFlags = unix.AT_SYMLINK_NOFOLLOW
	}

	return computeAndSetTimestamps(
		path,
		atim,
		mtim,
		fstFlags,
		utimeFlags,
		func() (int64, int64, error) {
			var stat unix.Stat_t
			if err := unix.Stat(path, &stat); err != nil {
				return 0, 0, err
			}
			atimNs := stat.Atim.Sec*1e9 + stat.Atim.Nsec
			mtimNs := stat.Mtim.Sec*1e9 + stat.Mtim.Nsec
			return atimNs, mtimNs, nil
		},
	)
}

func (w *wasiResourceTable) pathLink(
	inst *epsilon.ModuleInstance,
	oldIndex int32,
	oldFlags, oldPathPtr, oldPathLen, newIndex, newPathPtr, newPathLen int32,
) int32 {
	if oldFlags&lookupFlagsSymlinkFollow != 0 {
		return errnoInval
	}

	rights := RightsPathLinkSource
	from, errCode := w.resolvePath(inst, oldIndex, oldPathPtr, oldPathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	rights = RightsPathLinkTarget
	to, errCode := w.resolvePath(inst, newIndex, newPathPtr, newPathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	if err := os.Link(from, to); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathOpen(
	inst *epsilon.ModuleInstance,
	fdIndex, dirflags, pathPtr, pathLen, oflags int32,
	rightsBase, rightsInheriting int64,
	fdflags, newFdPtr int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathOpen)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	rights := RightsPathOpen
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	isDirFlag := oflags&int32(oFlagsDirectory) != 0
	if isDirFlag {
		stat, err := os.Stat(path)
		if err == nil && !stat.IsDir() {
			return errnoNotDir
		}
	}

	// Determine open flags
	var osFlags int
	if oflags&int32(oFlagsCreat) != 0 {
		osFlags |= os.O_CREATE
	}
	if oflags&int32(oFlagsExcl) != 0 {
		osFlags |= os.O_EXCL
	}
	if oflags&int32(oFlagsTrunc) != 0 {
		// Truncation requires PATH_FILESTAT_SET_SIZE right
		if fd.rights&RightsPathFilestatSetSize == 0 {
			return errnoNotCapable
		}
		osFlags |= os.O_TRUNC
	}
	if fdflags&int32(fdFlagsAppend) != 0 {
		osFlags |= os.O_APPEND
	}
	if dirflags&int32(lookupFlagsSymlinkFollow) == 0 {
		osFlags |= syscall.O_NOFOLLOW
	}

	// Determine read/write mode based on rights
	rights = rightsBase & fd.rightsInheriting
	inheritRights := rightsInheriting & fd.rightsInheriting
	hasRead := rights&RightsFdRead != 0
	hasWrite := rights&RightsFdWrite != 0

	if hasRead && hasWrite {
		osFlags |= os.O_RDWR
	} else if hasWrite {
		osFlags |= os.O_WRONLY
	} else {
		osFlags |= os.O_RDONLY
	}

	// Open the file/directory
	file, err := os.OpenFile(path, osFlags, 0666)
	if err != nil {
		return mapError(err)
	}

	// Use newWasiFileDescriptor to create the descriptor
	flags := uint16(fdflags)
	createdFd, err := newFileDescriptor(file, rights, inheritRights, flags)
	if err != nil {
		file.Close()
		return mapError(err)
	}
	newFdIndex := w.allocateFdIndex()
	w.fds[newFdIndex] = createdFd

	// Write the new fd to memory
	err = memory.StoreUint32(0, uint32(newFdPtr), uint32(newFdIndex))
	if err != nil {
		delete(w.fds, newFdIndex)
		file.Close()
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathReadlink(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen, bufPtr, bufLen, bufusedPtr int32,
) int32 {
	rights := RightsPathReadlink
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	target, err := os.Readlink(path)
	if err != nil {
		return mapError(err)
	}

	targetBytes := []byte(target)
	length := min(uint32(len(targetBytes)), uint32(bufLen))

	if err := memory.Set(0, uint32(bufPtr), targetBytes[:length]); err != nil {
		return errnoFault
	}

	if err := memory.StoreUint32(0, uint32(bufusedPtr), length); err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathRemoveDirectory(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	rights := RightsPathRemoveDirectory
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	info, err := os.Lstat(path)
	if err != nil {
		return mapError(err)
	}
	if !info.IsDir() {
		return errnoNotDir
	}

	err = os.Remove(path)
	if err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathRename(
	inst *epsilon.ModuleInstance,
	fdIndex, oldPathPtr, oldPathLen, newFdIndex, newPathPtr, newPathLen int32,
) int32 {
	rights := RightsPathRenameSource
	from, errCode := w.resolvePath(inst, fdIndex, oldPathPtr, oldPathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	// For destination, we need custom handling because it might not exist yet
	newFd, errCode := w.getDir(newFdIndex, RightsPathRenameTarget)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	newPath, err := w.readString(memory, newPathPtr, newPathLen)
	if err != nil {
		return errnoFault
	}

	if filepath.IsAbs(newPath) {
		return errnoPerm
	}

	to := filepath.Join(newFd.file.Name(), newPath)

	// Ensure resolved path stays within base directory
	relPath, err := filepath.Rel(newFd.file.Name(), to)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return errnoNotCapable
	}

	// Get source file info
	oldInfo, err := os.Stat(from)
	if err != nil {
		return mapError(err)
	}

	// Check if destination exists
	newInfo, err := os.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return mapError(err)
	}

	if err == nil {
		// Destination exists - check POSIX rename semantics
		oldIsDir := oldInfo.IsDir()
		newIsDir := newInfo.IsDir()

		// Cannot replace a directory with a non-directory or vice versa
		if oldIsDir != newIsDir {
			if newIsDir {
				return errnoIsDir
			}
			return errnoNotDir
		}

		// If both are directories, destination must be empty
		if oldIsDir && newIsDir {
			entries, err := os.ReadDir(to)
			if err != nil {
				return mapError(err)
			}
			if len(entries) > 0 {
				return errnoNotEmpty
			}
			// Remove the empty destination directory first
			// os.Rename doesn't replace directories on all platforms (e.g., macOS)
			if err := os.Remove(to); err != nil {
				return mapError(err)
			}
		}
		// For files replacing files, os.Rename will handle the overwrite
	}

	if err := os.Rename(from, to); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathSymlink(
	inst *epsilon.ModuleInstance,
	oldPathPtr, oldPathLen, fdIndex, newPathPtr, newPathLen int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathSymlink)
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	oldPath, err := w.readString(memory, oldPathPtr, oldPathLen)
	if err != nil {
		return errnoFault
	}

	newPath, err := w.readString(memory, newPathPtr, newPathLen)
	if err != nil {
		return errnoFault
	}

	// Symlink destination cannot have a trailing slash
	if strings.HasSuffix(newPath, "/") {
		return errnoNoEnt
	}

	if strings.HasPrefix(oldPath, "/") {
		return errnoNoEnt
	}

	// Resolve path relative to the fd's directory (openat semantics)
	fullNewPath := filepath.Join(fd.file.Name(), newPath)

	if err := os.Symlink(oldPath, fullNewPath); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) pathUnlinkFile(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	rights := RightsPathUnlinkFile
	path, errCode := w.resolvePath(inst, fdIndex, pathPtr, pathLen, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	// Check if it is a directory
	info, err := os.Lstat(path)
	if err != nil {
		return mapError(err)
	}
	if info.IsDir() {
		return errnoIsDir
	}

	if err := os.Remove(path); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func (w *wasiResourceTable) sockAccept(fdIndex, flags, fdPtr int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if fd.fileType != fileTypeSocketDgram && fd.fileType != fileTypeSocketStream {
		return errnoNotSock
	}
	return errnoNotSup
}

func (w *wasiResourceTable) sockRecv(
	fdIndex, riDataPtr, riDataLen, riFlags, roDataLenPtr, roFlagsPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if fd.fileType != fileTypeSocketDgram && fd.fileType != fileTypeSocketStream {
		return errnoNotSock
	}
	// TODO: implement
	return errnoNotSup
}

func (w *wasiResourceTable) sockSend(
	fdIndex, siDataPtr, siDataLen, siFlags, soDataLenPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if fd.fileType != fileTypeSocketDgram && fd.fileType != fileTypeSocketStream {
		return errnoNotSock
	}
	// TODO: implement
	return errnoNotSup
}

func (w *wasiResourceTable) sockShutdown(fdIndex, how int32) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}
	if fd.fileType != fileTypeSocketDgram && fd.fileType != fileTypeSocketStream {
		return errnoNotSock
	}
	// TODO: implement
	return errnoNotSup
}

func (w *wasiResourceTable) allocateFdIndex() int32 {
	// Find next available fd starting from 3
	for fd := int32(3); ; fd++ {
		if _, exists := w.fds[fd]; !exists {
			return fd
		}
	}
}

func (w *wasiResourceTable) resolvePath(
	inst *epsilon.ModuleInstance,
	fdIndex, pathPtr, pathLen int32,
	rights int64,
) (string, int32) {
	fd, errCode := w.getDir(fdIndex, rights)
	if errCode != errnoSuccess {
		return "", errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return "", errnoFault
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return "", errnoFault
	}

	if filepath.IsAbs(path) {
		return "", errnoPerm
	}

	fullPath := filepath.Join(fd.file.Name(), path)

	// Ensure resolved path stays within base directory
	relPath, err := filepath.Rel(fd.file.Name(), fullPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", errnoNotCapable
	}

	// If path has trailing slash, verify it's a directory
	if strings.HasSuffix(path, "/") {
		stat, err := os.Stat(fullPath)
		if err != nil {
			return "", mapError(err)
		}
		if !stat.IsDir() {
			return "", errnoNotDir
		}
	}

	return fullPath, errnoSuccess
}

func (w *wasiResourceTable) getFile(
	fdIdx int32,
	rights int64,
) (*wasiFileDescriptor, int32) {
	fd, errCode := w.getFileOrDir(fdIdx, rights)
	if errCode != errnoSuccess {
		return nil, errCode
	}
	if fd.fileType == fileTypeDirectory {
		return nil, errnoIsDir
	}
	return fd, errnoSuccess
}

func (w *wasiResourceTable) getDir(
	fdIdx int32,
	rights int64,
) (*wasiFileDescriptor, int32) {
	fd, errCode := w.getFileOrDir(fdIdx, rights)
	if errCode != errnoSuccess {
		return nil, errCode
	}
	if fd.fileType != fileTypeDirectory {
		return nil, errnoNotDir
	}
	return fd, errnoSuccess
}

func (w *wasiResourceTable) getFileOrDir(
	fdIdx int32,
	rights int64,
) (*wasiFileDescriptor, int32) {
	fd, ok := w.fds[fdIdx]
	if !ok {
		return nil, errnoBadF
	}
	if fd.rights&rights == 0 {
		return nil, errnoNotCapable
	}
	return fd, errnoSuccess
}

func readIovecItemPtr(
	mem *epsilon.Memory,
	iovecPtr uint32,
) (uint32, uint32, error) {
	iovec, err := mem.Get(0, iovecPtr, 8)
	if err != nil {
		return 0, 0, err
	}

	ptr := binary.LittleEndian.Uint32(iovec[0:4])
	length := binary.LittleEndian.Uint32(iovec[4:8])
	return ptr, length, nil
}

func readCiovecItem(mem *epsilon.Memory, ciovecPtr uint32) ([]byte, error) {
	ciovec, err := mem.Get(0, ciovecPtr, 8)
	if err != nil {
		return nil, err
	}

	ptr := binary.LittleEndian.Uint32(ciovec[0:4])
	length := binary.LittleEndian.Uint32(ciovec[4:8])

	data, err := mem.Get(0, ptr, length)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func getInode(path string) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, err
	}
	return stat.Ino, nil
}

func getModeFileType(mode os.FileMode) wasiFileType {
	switch {
	case mode.IsDir():
		return fileTypeDirectory
	case mode.IsRegular():
		return fileTypeRegularFile
	case mode&os.ModeSymlink != 0:
		return fileTypeSymbolicLink
	case mode&os.ModeSocket != 0:
		return fileTypeSocketStream
	case mode&os.ModeNamedPipe != 0:
		return fileTypeCharacterDevice
	case mode&os.ModeCharDevice != 0:
		return fileTypeCharacterDevice
	case mode&os.ModeDevice != 0:
		return fileTypeBlockDevice
	default:
		return fileTypeUnknown
	}
}

func writeFilestat(
	mem *epsilon.Memory,
	offset uint32,
	fileType wasiFileType,
	unixStat unix.Stat_t,
) int32 {
	if err := mem.StoreUint64(0, offset, uint64(unixStat.Dev)); err != nil {
		return errnoFault
	}
	if err := mem.StoreUint64(0, offset+8, unixStat.Ino); err != nil {
		return errnoFault
	}
	if err := mem.StoreByte(0, offset+16, uint8(fileType)); err != nil {
		return errnoFault
	}
	if err := mem.StoreUint64(0, offset+24, uint64(unixStat.Nlink)); err != nil {
		return errnoFault
	}
	if err := mem.StoreUint64(0, offset+32, uint64(unixStat.Size)); err != nil {
		return errnoFault
	}
	atim := unixStat.Atim.Sec*1e9 + unixStat.Atim.Nsec
	if err := mem.StoreUint64(0, offset+40, uint64(atim)); err != nil {
		return errnoFault
	}
	mtim := unixStat.Mtim.Sec*1e9 + unixStat.Mtim.Nsec
	if err := mem.StoreUint64(0, offset+48, uint64(mtim)); err != nil {
		return errnoFault
	}
	ctim := unixStat.Ctim.Sec*1e9 + unixStat.Ctim.Nsec
	if err := mem.StoreUint64(0, offset+56, uint64(ctim)); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) readString(
	memory *epsilon.Memory,
	ptr, length int32,
) (string, error) {
	oldPathBytes, err := memory.Get(0, uint32(ptr), uint32(length))
	if err != nil {
		return "", err
	}
	return string(oldPathBytes), nil
}

// computeAndSetTimestamps validates flags, computes final timestamp values,
// and applies them to the specified path. The getCurrentTimestamps function
// should return current atime and mtime in nanoseconds if needed.
func computeAndSetTimestamps(
	path string,
	atim, mtim int64,
	fstFlags int32,
	utimeFlags int,
	getCurrentTimestamps func() (atimNs, mtimNs int64, err error),
) int32 {
	// Validate flags: cannot have both SET and NOW
	if (fstFlags&fstFlagsAtim != 0) && (fstFlags&fstFlagsAtimNow != 0) {
		return errnoInval
	}
	if (fstFlags&fstFlagsMtim != 0) && (fstFlags&fstFlagsMtimNow != 0) {
		return errnoInval
	}

	// Check if we're actually setting any timestamps
	settingAtim := (fstFlags&fstFlagsAtim != 0) || (fstFlags&fstFlagsAtimNow != 0)
	settingMtim := (fstFlags&fstFlagsMtim != 0) || (fstFlags&fstFlagsMtimNow != 0)

	// If not setting any timestamps, return success immediately
	if !settingAtim && !settingMtim {
		return errnoSuccess
	}

	// Get current timestamps if we need to preserve them
	var currentAtimNs, currentMtimNs int64
	if !settingAtim || !settingMtim {
		var err error
		currentAtimNs, currentMtimNs, err = getCurrentTimestamps()
		if err != nil {
			return mapError(err)
		}
	}

	// Determine final timestamp values
	var finalAtimNs, finalMtimNs int64

	now := time.Now()
	if fstFlags&fstFlagsAtimNow != 0 {
		finalAtimNs = now.UnixNano()
	} else if fstFlags&fstFlagsAtim != 0 {
		finalAtimNs = atim
	} else {
		finalAtimNs = currentAtimNs
	}

	if fstFlags&fstFlagsMtimNow != 0 {
		finalMtimNs = now.UnixNano()
	} else if fstFlags&fstFlagsMtim != 0 {
		finalMtimNs = mtim
	} else {
		finalMtimNs = currentMtimNs
	}

	ts := []unix.Timespec{
		unix.NsecToTimespec(finalAtimNs),
		unix.NsecToTimespec(finalMtimNs),
	}

	if err := unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, utimeFlags); err != nil {
		return mapError(err)
	}

	return errnoSuccess
}

func mapError(err error) int32 {
	if err == nil {
		return errnoSuccess
	}

	// Unpack os.PathError/LinkError
	if pe, ok := err.(*os.PathError); ok {
		err = pe.Err
	}
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	if se, ok := err.(*os.SyscallError); ok {
		err = se.Err
	}

	// Check specific errors
	if err == os.ErrNotExist {
		return errnoNoEnt
	}
	if err == os.ErrExist {
		return errnoExist
	}
	if err == os.ErrPermission {
		return errnoAcces
	}

	// Check syscall errno
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
		}
	}

	// Fallback
	return errnoIO
}
