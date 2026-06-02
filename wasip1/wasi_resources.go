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
	"io"
	"os"
	"strings"

	"github.com/ziggy42/epsilon/epsilon"
)

const maxFileDescriptors = 2048

var errMaxFileDescriptorsReached = errors.New("max file descriptors reached")

type wasiFileDescriptor struct {
	file             File
	fsRoot           FileSystem // non-nil for directory descriptors
	fileType         FileType
	flags            uint16
	rights           int64
	rightsInheriting int64
	guestPath        string
	isPreopen        bool
	keepOpen         bool // std streams the runtime must not close
}

func (fd *wasiFileDescriptor) close() {
	if fd.fsRoot != nil {
		// fsRoot.Close releases both the directory's open handle (fd.file) and
		// the underlying root.
		fd.fsRoot.Close()
		return
	}
	if !fd.keepOpen {
		fd.file.Close()
	}
}

type wasiResourceTable struct {
	fds map[int32]*wasiFileDescriptor
}

func (rt *wasiResourceTable) closeAll() {
	for _, fd := range rt.fds {
		fd.close()
	}
}

// stdStream is a resolved standard stream: the backing File and whether the
// runtime must leave it open (true for the process stdio).
type stdStream struct {
	file     File
	keepOpen bool
}

func newWasiResourceTable(
	preopens []WasiPreopen,
	stdin, stdout, stderr stdStream,
) (*wasiResourceTable, error) {
	stdRights := RightsFdFilestatGet | RightsPollFdReadwrite
	stdinFd, err := newStdFileDescriptor(stdin, RightsFdRead|stdRights)
	if err != nil {
		return nil, err
	}
	stdoutFd, err := newStdFileDescriptor(stdout, RightsFdWrite|stdRights)
	if err != nil {
		return nil, err
	}
	stderrFd, err := newStdFileDescriptor(stderr, RightsFdWrite|stdRights)
	if err != nil {
		return nil, err
	}

	resourceTable := &wasiResourceTable{
		fds: map[int32]*wasiFileDescriptor{0: stdinFd, 1: stdoutFd, 2: stderrFd},
	}

	for _, pre := range preopens {
		fd, err := newPreopenFileDescriptor(pre)
		if err != nil {
			pre.FS.Close()
			resourceTable.closeAll()
			return nil, err
		}
		newFdIndex, err := resourceTable.allocateFdIndex()
		if err != nil {
			fd.close()
			resourceTable.closeAll()
			return nil, err
		}
		resourceTable.fds[newFdIndex] = fd
	}
	return resourceTable, nil
}

// newFileDescriptor wraps file as a descriptor. If info is non-nil it is the
// caller's already-fetched stat of file, avoiding a redundant Stat; otherwise
// file is stat'd here.
func newFileDescriptor(
	file File,
	fsRoot FileSystem,
	info os.FileInfo,
	rights, rightsInheriting int64,
	flags uint16,
) (*wasiFileDescriptor, error) {
	if info == nil {
		var err error
		info, err = file.Stat()
		if err != nil {
			return nil, err
		}
	}

	if info.IsDir() {
		rights &= ^(RightsFdSeek | RightsFdTell | RightsFdRead | RightsFdWrite)
	}

	return &wasiFileDescriptor{
		file:             file,
		fsRoot:           fsRoot,
		fileType:         getModeFileType(info.Mode()),
		flags:            flags,
		rights:           rights,
		rightsInheriting: rightsInheriting,
		guestPath:        "",
		isPreopen:        false,
	}, nil
}

func newPreopenFileDescriptor(
	pre WasiPreopen,
) (*wasiFileDescriptor, error) {
	fd, err := newFileDescriptor(
		pre.FS.Handle(),
		pre.FS,
		nil,
		pre.Rights,
		pre.RightsInheriting,
		0,
	)
	if err != nil {
		return nil, err
	}
	fd.guestPath = pre.GuestPath
	fd.isPreopen = true
	return fd, nil
}

func newStdFileDescriptor(
	stream stdStream,
	rights int64,
) (*wasiFileDescriptor, error) {
	fd, err := newFileDescriptor(stream.file, nil, nil, rights, 0, 0)
	if err != nil {
		return nil, err
	}
	fd.keepOpen = stream.keepOpen
	return fd, nil
}

func (w *wasiResourceTable) advise(
	fdIndex int32,
	offset, length int64,
	advice int32,
) int32 {
	_, errCode := w.getFileOrDir(fdIndex, RightsFdAdvise)
	if errCode != errnoSuccess {
		return errCode
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
	fd.close()
	delete(w.fds, fdIndex)
	return errnoSuccess
}

func (w *wasiResourceTable) getStat(
	memory *epsilon.Memory,
	fdIndex, fdStatPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	err := memory.StoreByte(0, uint32(fdStatPtr), uint8(fd.fileType))
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

	appendFlag := fdFlags&int32(fdFlagsAppend) != 0
	nonblock := fdFlags&int32(fdFlagsNonblock) != 0
	if err := fd.file.SetFlags(appendFlag, nonblock); err != nil {
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
	memory *epsilon.Memory,
	fdIndex, bufPtr int32,
) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFilestatGet)
	if errCode != errnoSuccess {
		return errCode
	}

	fs, err := fd.file.FileStat()
	if err != nil {
		return mapError(err)
	}

	buf := fs.bytes()
	if err := memory.Set(0, uint32(bufPtr), buf[:]); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) setFileStatSize(fdIndex int32, size int64) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, RightsFdFilestatSetSize)
	if errCode != errnoSuccess {
		return errCode
	}

	if fd.fileType == FileTypeDirectory {
		return errnoIsDir
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

	if err := fd.file.SetTimes(atim, mtim, fstFlags); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pread(
	memory *epsilon.Memory,
	fdIndex, iovecPtr, iovecLength int32,
	offset int64,
	nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdRead|RightsFdSeek)
	if errCode != errnoSuccess {
		return errCode
	}

	readBytes := func(buf []byte, readSoFar int64) (int, error) {
		return fd.file.ReadAt(buf, offset+readSoFar)
	}

	return iterIovec(memory, iovecPtr, iovecLength, nPtr, readBytes)
}

func (w *wasiResourceTable) getPrestat(
	memory *epsilon.Memory,
	fdIndex, prestatPtr int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	if !fd.isPreopen {
		return errnoBadF
	}

	// Note: WASI Preview 1 only supports "Directory" preopens. If the underlying
	// file is a socket or other resource, we must present it as a directory.
	// Guests attempting to open paths under this FD will fail with ENOTDIR.
	err := memory.StoreByte(0, uint32(prestatPtr), preopenTypeDir)
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
	memory *epsilon.Memory,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	fd, ok := w.fds[fdIndex]
	if !ok || !fd.isPreopen {
		return errnoBadF
	}

	if int32(len(fd.guestPath)) > pathLen {
		return errnoNameTooLong
	}

	if err := memory.Set(0, uint32(pathPtr), []byte(fd.guestPath)); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pwrite(
	memory *epsilon.Memory,
	fdIndex, ciovecPtr, ciovecLength int32,
	offset int64,
	nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdWrite|RightsFdSeek)
	if errCode != errnoSuccess {
		return errCode
	}

	currentOffset := offset
	writeBytes := func(data []byte) (int, error) {
		n, err := fd.file.WriteAt(data, currentOffset)
		currentOffset += int64(n)
		return n, err
	}

	return iterCiovec(memory, ciovecPtr, ciovecLength, nPtr, writeBytes)
}

func (w *wasiResourceTable) read(
	memory *epsilon.Memory,
	fdIndex, iovecPtr, iovecLength, nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdRead)
	if errCode != errnoSuccess {
		return errCode
	}

	readBytes := func(buf []byte, _ int64) (int, error) {
		return fd.file.Read(buf)
	}

	return iterIovec(memory, iovecPtr, iovecLength, nPtr, readBytes)
}

func (w *wasiResourceTable) readdir(
	memory *epsilon.Memory,
	fdIndex, bufPtr, bufLen int32,
	cookie int64,
	bufusedPtr int32,
) int32 {
	if cookie < 0 || bufLen < 0 {
		return errnoInval
	}

	fd, errCode := w.getDir(fdIndex, RightsFdReaddir)
	if errCode != errnoSuccess {
		return errCode
	}

	entries, err := fd.fsRoot.ReadDir()
	if err != nil {
		return mapError(err)
	}

	bufOffset := uint32(bufPtr)
	bufEnd := bufOffset + uint32(bufLen)
	written := uint32(0)

	for i := cookie; i < int64(len(entries)); i++ {
		available := bufEnd - bufOffset
		if available == 0 {
			break
		}

		entryBytes := entries[i].bytes(uint64(i + 1))
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

	err = memory.StoreUint32(0, uint32(bufusedPtr), uint32(written))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) renumber(fdIndex, toFdIndex int32) int32 {
	if fdIndex == toFdIndex {
		return errnoSuccess
	}

	fd, ok := w.fds[fdIndex]
	if !ok {
		return errnoBadF
	}

	toFd, exists := w.fds[toFdIndex]
	if !exists {
		return errnoBadF
	}

	toFd.close()
	w.fds[toFdIndex] = fd
	delete(w.fds, fdIndex)
	return errnoSuccess
}

func (w *wasiResourceTable) seek(
	memory *epsilon.Memory,
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

	err = memory.StoreUint64(0, uint32(newOffsetPtr), uint64(newOffset))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) sync(fdIndex int32, rights int64) int32 {
	fd, errCode := w.getFileOrDir(fdIndex, rights)
	if errCode != errnoSuccess {
		return errCode
	}
	if err := fd.file.Sync(); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) tell(
	memory *epsilon.Memory,
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

	err = memory.StoreUint64(0, uint32(offsetPtr), uint64(currentOffset))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) write(
	memory *epsilon.Memory,
	fdIndex, ciovecPtr, ciovecLength, nPtr int32,
) int32 {
	fd, errCode := w.getFile(fdIndex, RightsFdWrite)
	if errCode != errnoSuccess {
		return errCode
	}

	return iterCiovec(memory, ciovecPtr, ciovecLength, nPtr, fd.file.Write)
}

func (w *wasiResourceTable) pathCreateDirectory(
	memory *epsilon.Memory,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathCreateDirectory)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	if err := fd.fsRoot.Mkdir(path, 0o755); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathFilestatGet(
	memory *epsilon.Memory,
	fdIndex, flags, pathPtr, pathLen, filestatPtr int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathFilestatGet)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	followSymlinks := flags&lookupFlagsSymlinkFollow != 0
	fs, err := fd.fsRoot.Stat(path, followSymlinks)
	if err != nil {
		return mapError(err)
	}

	buf := fs.bytes()
	if err := memory.Set(0, uint32(filestatPtr), buf[:]); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathFilestatSetTimes(
	memory *epsilon.Memory,
	fdIndex, flags, pathPtr, pathLen int32,
	atim, mtim int64,
	fstFlags int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathFilestatSetTimes)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	followSymlinks := flags&lookupFlagsSymlinkFollow != 0
	err = fd.fsRoot.Chtimes(path, atim, mtim, fstFlags, followSymlinks)
	if err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathLink(
	memory *epsilon.Memory,
	oldIndex int32,
	oldFlags, oldPathPtr, oldPathLen, newIndex, newPathPtr, newPathLen int32,
) int32 {
	oldFd, errCode := w.getDir(oldIndex, RightsPathLinkSource)
	if errCode != errnoSuccess {
		return errCode
	}

	newFd, errCode := w.getDir(newIndex, RightsPathLinkTarget)
	if errCode != errnoSuccess {
		return errCode
	}

	oldPath, err := w.readString(memory, oldPathPtr, oldPathLen)
	if err != nil {
		return errnoFault
	}

	newPath, err := w.readString(memory, newPathPtr, newPathLen)
	if err != nil {
		return errnoFault
	}

	followSymlinks := oldFlags&lookupFlagsSymlinkFollow != 0
	err = oldFd.fsRoot.Link(oldPath, followSymlinks, newFd.fsRoot, newPath)
	if err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathOpen(
	memory *epsilon.Memory,
	fdIndex, dirflags, pathPtr, pathLen, oflags int32,
	rightsBase, rightsInheriting int64,
	fdflags, newFdPtr int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathOpen)
	if errCode != errnoSuccess {
		return errCode
	}

	// O_CREAT requires RightsPathCreateFile
	if oflags&int32(oFlagsCreat) != 0 {
		if fd.rights&RightsPathCreateFile == 0 {
			return errnoNotCapable
		}
	}
	// O_TRUNC requires RightsPathFilestatSetSize
	if oflags&int32(oFlagsTrunc) != 0 {
		if fd.rights&RightsPathFilestatSetSize == 0 {
			return errnoNotCapable
		}
	}

	// Validate rights: can only request rights that the parent fd can inherit
	if (rightsBase & fd.rightsInheriting) != rightsBase {
		return errnoNotCapable
	}
	if (rightsInheriting & fd.rightsInheriting) != rightsInheriting {
		return errnoNotCapable
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	followSymlinks := dirflags&lookupFlagsSymlinkFollow != 0
	// A trailing slash or an explicit O_DIRECTORY means the path must be a
	// directory, which becomes its own sandboxed root.
	wantDir := oflags&int32(oFlagsDirectory) != 0 ||
		strings.HasSuffix(path, "/")

	var file File
	var childFS FileSystem
	var info os.FileInfo
	if wantDir {
		// A directory cannot be opened for writing.
		if rightsBase&RightsFdWrite != 0 {
			return errnoIsDir
		}
		// Without symlink-follow, a symlink named as a directory must not be
		// followed; opening it as a directory fails.
		if !followSymlinks {
			if info, serr := fd.fsRoot.Stat(path, false); serr == nil &&
				info.FileType == FileTypeSymbolicLink {
				return errnoLoop
			}
		}
		childFS, err = fd.fsRoot.OpenRoot(path)
		if err != nil {
			return mapError(err)
		}
		file = childFS.Handle()
	} else {
		file, err = fd.fsRoot.OpenFile(
			path,
			followSymlinks,
			oflags,
			fdflags,
			uint64(rightsBase),
		)
		if err != nil {
			return mapError(err)
		}

		// A directory may be opened without O_DIRECTORY; promote it to a root so
		// subsequent path operations on the descriptor work.
		info, err = file.Stat()
		if err != nil {
			file.Close()
			return mapError(err)
		}
		if info.IsDir() {
			file.Close()
			childFS, err = fd.fsRoot.OpenRoot(path)
			if err != nil {
				return mapError(err)
			}
			file = childFS.Handle()
			info = nil // file now refers to the root handle; re-stat in allocateFd
		}
	}

	newFdIndex, errCode := w.allocateFd(
		file,
		childFS,
		info,
		rightsBase,
		rightsInheriting,
		uint16(fdflags),
	)
	if errCode != errnoSuccess {
		return errCode
	}

	err = memory.StoreUint32(0, uint32(newFdPtr), uint32(newFdIndex))
	if err != nil {
		w.close(newFdIndex)
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathReadlink(
	memory *epsilon.Memory,
	fdIndex, pathPtr, pathLen, bufPtr, bufLen, bufusedPtr int32,
) int32 {
	if bufLen < 0 {
		return errnoInval
	}

	fd, errCode := w.getDir(fdIndex, RightsPathReadlink)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	target, err := fd.fsRoot.Readlink(path)
	if err != nil {
		return mapError(err)
	}

	// Write as much as fits in the buffer
	targetBytes := []byte(target)[:min(int32(len(target)), bufLen)]
	if err := memory.Set(0, uint32(bufPtr), targetBytes); err != nil {
		return errnoFault
	}
	err = memory.StoreUint32(0, uint32(bufusedPtr), uint32(len(targetBytes)))
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathRemoveDirectory(
	memory *epsilon.Memory,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathRemoveDirectory)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	if err := fd.fsRoot.Rmdir(path); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathRename(
	memory *epsilon.Memory,
	fdIndex, oldPathPtr, oldPathLen, newFdIndex, newPathPtr, newPathLen int32,
) int32 {
	oldFd, errCode := w.getDir(fdIndex, RightsPathRenameSource)
	if errCode != errnoSuccess {
		return errCode
	}

	newFd, errCode := w.getDir(newFdIndex, RightsPathRenameTarget)
	if errCode != errnoSuccess {
		return errCode
	}

	oldPath, err := w.readString(memory, oldPathPtr, oldPathLen)
	if err != nil {
		return errnoFault
	}

	newPath, err := w.readString(memory, newPathPtr, newPathLen)
	if err != nil {
		return errnoFault
	}

	if err := oldFd.fsRoot.Rename(newFd.fsRoot, oldPath, newPath); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathSymlink(
	memory *epsilon.Memory,
	targetPathPtr, targetPathLen, fdIndex, linkPathPtr, linkPathLen int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathSymlink)
	if errCode != errnoSuccess {
		return errCode
	}

	targetPath, err := w.readString(memory, targetPathPtr, targetPathLen)
	if err != nil {
		return errnoFault
	}

	linkPath, err := w.readString(memory, linkPathPtr, linkPathLen)
	if err != nil {
		return errnoFault
	}

	if err := fd.fsRoot.Symlink(targetPath, linkPath); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) pathUnlinkFile(
	memory *epsilon.Memory,
	fdIndex, pathPtr, pathLen int32,
) int32 {
	fd, errCode := w.getDir(fdIndex, RightsPathUnlinkFile)
	if errCode != errnoSuccess {
		return errCode
	}

	path, err := w.readString(memory, pathPtr, pathLen)
	if err != nil {
		return errnoFault
	}

	if err := fd.fsRoot.Unlink(path); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) sockAccept(
	memory *epsilon.Memory,
	fdIndex, flags, fdPtr int32,
) int32 {
	fd, errCode := w.getSocket(fdIndex, RightsSockAccept)
	if errCode != errnoSuccess {
		return errCode
	}

	newFile, err := fd.file.Accept()
	if err != nil {
		return mapError(err)
	}

	// The accepted socket inherits its rights from the listener's
	// rights_inheriting mask.
	rights := fd.rightsInheriting
	// Inheritance for inheriting rights usually follows the same mask in WASI.
	inheritRights := fd.rightsInheriting
	newFdIndex, errCode := w.allocateFd(
		newFile,
		nil,
		nil,
		rights,
		inheritRights,
		uint16(flags),
	)
	if errCode != errnoSuccess {
		return errCode
	}

	err = memory.StoreUint32(0, uint32(fdPtr), uint32(newFdIndex))
	if err != nil {
		w.close(newFdIndex)
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) sockRecv(
	memory *epsilon.Memory,
	fdIndex, riDataPtr, riDataLen, riFlags, roDataLenPtr, roFlagsPtr int32,
	rights int64,
) int32 {
	fd, errCode := w.getSocket(fdIndex, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	// Note: riFlags (like MSG_PEEK) are currently ignored. To support them, we
	// would need to switch to unix.Recvmsg or other lower level APIs directly.
	readBytes := func(data []byte, _ int64) (int, error) {
		return fd.file.Read(data)
	}

	errCode = iterIovec(memory, riDataPtr, riDataLen, roDataLenPtr, readBytes)
	if errCode != errnoSuccess {
		return errCode
	}

	// We use os.File.Read for reading from the socket, which does not return
	// output flags (like MSG_TRUNC). For simple stream operations, returning 0 is
	// acceptable.
	if err := memory.StoreUint16(0, uint32(roFlagsPtr), 0); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *wasiResourceTable) sockSend(
	memory *epsilon.Memory,
	fdIndex, siDataPtr, siDataLen, siFlags, soDataLenPtr int32,
	rights int64,
) int32 {
	fd, errCode := w.getSocket(fdIndex, rights)
	if errCode != errnoSuccess {
		return errCode
	}

	// Note: siFlags (like MSG_OOB) are currently ignored. To support them, we
	// would need to switch to unix.Sendmsg or other lower level APIs directly.
	return iterCiovec(memory, siDataPtr, siDataLen, soDataLenPtr, fd.file.Write)
}

func (w *wasiResourceTable) sockShutdown(fdIndex, how int32) int32 {
	fd, errCode := w.getSocket(fdIndex, RightsSockShutdown)
	if errCode != errnoSuccess {
		return errCode
	}

	if err := fd.file.Shutdown(how); err != nil {
		return mapError(err)
	}
	return errnoSuccess
}

func (w *wasiResourceTable) allocateFdIndex() (int32, error) {
	if len(w.fds) >= maxFileDescriptors {
		return 0, errMaxFileDescriptorsReached
	}

	// Find next available fd starting from 3
	for fd := int32(3); ; fd++ {
		if _, exists := w.fds[fd]; !exists {
			return fd, nil
		}
	}
}

// allocateFd allocates a new file descriptor and returns its index and an error
// code.
func (w *wasiResourceTable) allocateFd(
	file File,
	fsRoot FileSystem,
	info os.FileInfo,
	rights, inheritRights int64,
	flags uint16,
) (int32, int32) {
	fd, err := newFileDescriptor(file, fsRoot, info, rights, inheritRights, flags)
	if err != nil {
		if fsRoot != nil {
			fsRoot.Close()
		} else {
			file.Close()
		}
		return 0, mapError(err)
	}
	newFdIndex, err := w.allocateFdIndex()
	if err != nil {
		fd.close()
		return 0, mapError(err)
	}
	w.fds[newFdIndex] = fd
	return newFdIndex, errnoSuccess
}

func (w *wasiResourceTable) getFile(
	fdIdx int32,
	rights int64,
) (*wasiFileDescriptor, int32) {
	fd, errCode := w.getFileOrDir(fdIdx, rights)
	if errCode != errnoSuccess {
		return nil, errCode
	}
	if fd.fileType == FileTypeDirectory {
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
	if fd.fileType != FileTypeDirectory {
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

func (w *wasiResourceTable) getSocket(
	fdIdx int32,
	rights int64,
) (*wasiFileDescriptor, int32) {
	fd, ok := w.fds[fdIdx]
	if !ok {
		return nil, errnoBadF
	}
	if fd.fileType != FileTypeSocketDgram && fd.fileType != FileTypeSocketStream {
		return nil, errnoNotSock
	}
	if fd.rights&rights == 0 {
		return nil, errnoNotCapable
	}
	return fd, errnoSuccess
}

// iterIovec reads data from the given iovec items and stores it in memory.
// Returns an error code.
func iterIovec(
	memory *epsilon.Memory,
	iovecPtr, iovecLength, totalReadPtr int32,
	readBytes func([]byte, int64) (int, error),
) int32 {
	var totalRead uint32
	for i := range iovecLength {
		iovec, err := memory.Get(0, uint32(iovecPtr)+uint32(i*8), 8)
		if err != nil {
			return errnoFault
		}

		ptr := binary.LittleEndian.Uint32(iovec[0:4])
		length := binary.LittleEndian.Uint32(iovec[4:8])

		dst, err := memory.Get(0, ptr, length)
		if err != nil {
			return errnoFault
		}

		n, err := readBytes(dst, int64(totalRead))
		if err != nil && err != io.EOF {
			return mapError(err)
		}

		totalRead += uint32(n)
		if n < int(length) {
			break
		}
	}
	if err := memory.StoreUint32(0, uint32(totalReadPtr), totalRead); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

// iterCiovec writes data from the given ciovec items using the given writeBytes
// function. Returns an error code.
func iterCiovec(
	memory *epsilon.Memory,
	ciovecPtr, ciovecLength, totalWrittenPtr int32,
	writeBytes func([]byte) (int, error),
) int32 {
	var totalWritten uint32
	for i := range ciovecLength {
		ciovec, err := memory.Get(0, uint32(ciovecPtr)+uint32(i*8), 8)
		if err != nil {
			return errnoFault
		}

		ptr := binary.LittleEndian.Uint32(ciovec[0:4])
		length := binary.LittleEndian.Uint32(ciovec[4:8])

		data, err := memory.Get(0, ptr, length)
		if err != nil {
			return errnoFault
		}

		n, err := writeBytes(data)
		totalWritten += uint32(n)

		if err != nil {
			if totalWritten > 0 {
				break
			}
			return mapError(err)
		}

		if n < len(data) {
			break
		}
	}

	err := memory.StoreUint32(0, uint32(totalWrittenPtr), totalWritten)
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func getModeFileType(mode os.FileMode) FileType {
	switch {
	case mode.IsDir():
		return FileTypeDirectory
	case mode.IsRegular():
		return FileTypeRegularFile
	case mode&os.ModeSymlink != 0:
		return FileTypeSymbolicLink
	case mode&os.ModeSocket != 0:
		return FileTypeSocketStream
	case mode&os.ModeNamedPipe != 0:
		return FileTypeCharacterDevice
	case mode&os.ModeCharDevice != 0:
		return FileTypeCharacterDevice
	case mode&os.ModeDevice != 0:
		return FileTypeBlockDevice
	default:
		return FileTypeUnknown
	}
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
