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

// Generated from
// https://github.com/WebAssembly/WASI/blob/8d7fb6eff7d5964b2375beb10518d2327a1dcfe8/legacy/preview0/witx/wasi_unstable.witx

const (
	ClockRealtime         uint32 = 0 // The clock measuring real time.
	ClockMonotonic        uint32 = 1 // The store-wide monotonic clock.
	ClockProcessCPUTimeID uint32 = 2 // The CPU-time clock for the process.
	ClockThreadCPUTimeID  uint32 = 3 // The CPU-time clock for the thread.
)

const (
	ErrnoSuccess        int32 = 0  // No error occurred.
	Errno2Big           int32 = 1  // Argument list too long.
	ErrnoAcces          int32 = 2  // Permission denied.
	ErrnoAddrInUse      int32 = 3  // Address in use.
	ErrnoAddrNotAvail   int32 = 4  // Address not available.
	ErrnoAfNoSupport    int32 = 5  // Address family not supported.
	ErrnoAgain          int32 = 6  // Resource unavailable, or would block.
	ErrnoAlready        int32 = 7  // Connection already in progress.
	ErrnoBadF           int32 = 8  // Bad file descriptor.
	ErrnoBadMsg         int32 = 9  // Bad message.
	ErrnoBusy           int32 = 10 // Device or resource busy.
	ErrnoCanceled       int32 = 11 // Operation canceled.
	ErrnoChild          int32 = 12 // No child processes.
	ErrnoConnAborted    int32 = 13 // Connection aborted.
	ErrnoConnRefused    int32 = 14 // Connection refused.
	ErrnoConnReset      int32 = 15 // Connection reset.
	ErrnoDeadLk         int32 = 16 // Resource deadlock would occur.
	ErrnoDestAddrReq    int32 = 17 // Destination address required.
	ErrnoDom            int32 = 18 // Mathematics argument out of domain.
	ErrnoDQuot          int32 = 19 // Reserved.
	ErrnoExist          int32 = 20 // File exists.
	ErrnoFault          int32 = 21 // Bad address.
	ErrnoFBig           int32 = 22 // File too large.
	ErrnoHostUnreach    int32 = 23 // Host is unreachable.
	ErrnoIdrm           int32 = 24 // Identifier removed.
	ErrnoIlSeq          int32 = 25 // Illegal byte sequence.
	ErrnoInProgress     int32 = 26 // Operation in progress.
	ErrnoIntr           int32 = 27 // Interrupted function.
	ErrnoInval          int32 = 28 // Invalid argument.
	ErrnoIO             int32 = 29 // I/O error.
	ErrnoIsConn         int32 = 30 // Socket is connected.
	ErrnoIsDir          int32 = 31 // Is a directory.
	ErrnoLoop           int32 = 32 // Too many levels of symbolic links.
	ErrnoMFile          int32 = 33 // File descriptor value too large.
	ErrnoMLink          int32 = 34 // Too many links.
	ErrnoMsgSize        int32 = 35 // Message too large.
	ErrnoMultihop       int32 = 36 // Reserved.
	ErrnoNameTooLong    int32 = 37 // Filename too long.
	ErrnoNetDown        int32 = 38 // Network is down.
	ErrnoNetReset       int32 = 39 // Connection aborted by network.
	ErrnoNetUnreach     int32 = 40 // Network unreachable.
	ErrnoNFile          int32 = 41 // Too many files open in system.
	ErrnoNoBufs         int32 = 42 // No buffer space available.
	ErrnoNoDev          int32 = 43 // No such device.
	ErrnoNoEnt          int32 = 44 // No such file or directory.
	ErrnoNoExec         int32 = 45 // Executable file format error.
	ErrnoNoLck          int32 = 46 // No locks available.
	ErrnoNoLink         int32 = 47 // Reserved.
	ErrnoNoMem          int32 = 48 // Not enough space.
	ErrnoNoMsg          int32 = 49 // No message of the desired type.
	ErrnoNoProtoOpt     int32 = 50 // Protocol not available.
	ErrnoNoSpc          int32 = 51 // No space left on device.
	ErrnoNoSys          int32 = 52 // Function not supported.
	ErrnoNotConn        int32 = 53 // The socket is not connected.
	ErrnoNotDir         int32 = 54 // Not a directory or symbolic link.
	ErrnoNotEmpty       int32 = 55 // Directory not empty.
	ErrnoNotRecoverable int32 = 56 // State not recoverable.
	ErrnoNotSock        int32 = 57 // Not a socket.
	ErrnoNotSup         int32 = 58 // Not supported.
	ErrnoNoTty          int32 = 59 // Inappropriate I/O control operation.
	ErrnoNxIO           int32 = 60 // No such device or address.
	ErrnoOverflow       int32 = 61 // Value too large to be stored.
	ErrnoOwnerDead      int32 = 62 // Previous owner died.
	ErrnoPerm           int32 = 63 // Operation not permitted.
	ErrnoPipe           int32 = 64 // Broken pipe.
	ErrnoProto          int32 = 65 // Protocol error.
	ErrnoProtoNoSupport int32 = 66 // Protocol not supported.
	ErrnoProtoType      int32 = 67 // Protocol wrong type for socket.
	ErrnoRange          int32 = 68 // Result too large.
	ErrnoRoFs           int32 = 69 // Read-only file system.
	ErrnoSPipe          int32 = 70 // Invalid seek.
	ErrnoSrch           int32 = 71 // No such process.
	ErrnoStale          int32 = 72 // Reserved.
	ErrnoTimedOut       int32 = 73 // Connection timed out.
	ErrnoTxtBsy         int32 = 74 // Text file busy.
	ErrnoXDev           int32 = 75 // Cross-device link.
	ErrnoNotCapable     int32 = 76 // Extension: Capabilities insufficient.
)

type wasiRights uint64

const (
	RightsFdDatasync           wasiRights = 1 << 0
	RightsFdRead               wasiRights = 1 << 1
	RightsFdSeek               wasiRights = 1 << 2
	RightsFdFdstatSetFlags     wasiRights = 1 << 3
	RightsFdSync               wasiRights = 1 << 4
	RightsFdTell               wasiRights = 1 << 5
	RightsFdWrite              wasiRights = 1 << 6
	RightsFdAdvise             wasiRights = 1 << 7
	RightsFdAllocate           wasiRights = 1 << 8
	RightsPathCreateDirectory  wasiRights = 1 << 9
	RightsPathCreateFile       wasiRights = 1 << 10
	RightsPathLinkSource       wasiRights = 1 << 11
	RightsPathLinkTarget       wasiRights = 1 << 12
	RightsPathOpen             wasiRights = 1 << 13
	RightsFdReaddir            wasiRights = 1 << 14
	RightsPathReadlink         wasiRights = 1 << 15
	RightsPathRenameSource     wasiRights = 1 << 16
	RightsPathRenameTarget     wasiRights = 1 << 17
	RightsPathFilestatGet      wasiRights = 1 << 18
	RightsPathFilestatSetSize  wasiRights = 1 << 19
	RightsPathFilestatSetTimes wasiRights = 1 << 20
	RightsFdFilestatGet        wasiRights = 1 << 21
	RightsFdFilestatSetSize    wasiRights = 1 << 22
	RightsFdFilestatSetTimes   wasiRights = 1 << 23
	RightsPathSymlink          wasiRights = 1 << 24
	RightsPathRemoveDirectory  wasiRights = 1 << 25
	RightsPathUnlinkFile       wasiRights = 1 << 26
	RightsPollFdReadwrite      wasiRights = 1 << 27
	RightsSockShutdown         wasiRights = 1 << 28
)

const (
	WhenceSet uint8 = 0 // Seek relative to start-of-file.
	WhenceCur uint8 = 1 // Seek relative to current position.
	WhenceEnd uint8 = 2 // Seek relative to end-of-file.

)

type wasiFileType uint8

const (
	FileTypeUnknown         wasiFileType = 0
	FileTypeBlockDevice     wasiFileType = 1
	FileTypeCharacterDevice wasiFileType = 2
	FileTypeDirectory       wasiFileType = 3
	FileTypeRegularFile     wasiFileType = 4
	FileTypeSocketDgram     wasiFileType = 5
	FileTypeSocketStream    wasiFileType = 6
	FileTypeSymbolicLink    wasiFileType = 7
)

const (
	AdviceNormal     uint8 = 0
	AdviceSequential uint8 = 1
	AdviceRandom     uint8 = 2
	AdviceWillNeed   uint8 = 3
	AdviceDontNeed   uint8 = 4
	AdviceNoReuse    uint8 = 5
)

const (
	FdFlagsAppend   uint16 = 1 << 0
	FdFlagsDsync    uint16 = 1 << 1
	FdFlagsNonblock uint16 = 1 << 2
	FdFlagsRsync    uint16 = 1 << 3
	FdFlagsSync     uint16 = 1 << 4
)

const (
	FstFlagsAtim    int32 = 1 << 0
	FstFlagsAtimNow int32 = 1 << 1
	FstFlagsMtim    int32 = 1 << 2
	FstFlagsMtimNow int32 = 1 << 3
)

const (
	LookupFlagsSymlinkFollow int32 = 1 << 0
)

const (
	OFlagsCreat     uint16 = 1 << 0
	OFlagsDirectory uint16 = 1 << 1
	OFlagsExcl      uint16 = 1 << 2
	OFlagsTrunc     uint16 = 1 << 3
)

const (
	EventTypeClock   uint8 = 0
	EventTypeFdRead  uint8 = 1
	EventTypeFdWrite uint8 = 2
)

const (
	EventRwFlagsFdReadwriteHangup uint16 = 1 << 0
)

// ExitCode is the exit code generated by a process when exiting.
type ExitCode uint32

const (
	SignalNone   uint8 = 0
	SignalHup    uint8 = 1
	SignalInt    uint8 = 2
	SignalQuit   uint8 = 3
	SignalIll    uint8 = 4
	SignalTrap   uint8 = 5
	SignalAbrt   uint8 = 6
	SignalBus    uint8 = 7
	SignalFpe    uint8 = 8
	SignalKill   uint8 = 9
	SignalUsr1   uint8 = 10
	SignalSegv   uint8 = 11
	SignalUsr2   uint8 = 12
	SignalPipe   uint8 = 13
	SignalAlrm   uint8 = 14
	SignalTerm   uint8 = 15
	SignalChld   uint8 = 16
	SignalCont   uint8 = 17
	SignalStop   uint8 = 18
	SignalTstp   uint8 = 19
	SignalTtin   uint8 = 20
	SignalTtou   uint8 = 21
	SignalUrg    uint8 = 22
	SignalXcpu   uint8 = 23
	SignalXfsz   uint8 = 24
	SignalVtalrm uint8 = 25
	SignalProf   uint8 = 26
	SignalWinch  uint8 = 27
	SignalPoll   uint8 = 28
	SignalPwr    uint8 = 29
	SignalSys    uint8 = 30
)

const (
	RiFlagsRecvPeek    uint16 = 1 << 0
	RiFlagsRecvWaitall uint16 = 1 << 1
)

const (
	RoFlagsRecvDataTruncated uint16 = 1 << 0
)

const (
	SdFlagsRd uint8 = 1 << 0
	SdFlagsWr uint8 = 1 << 1
)

const (
	PreopenTypeDir uint8 = 0
)

type Prestat struct {
	Type uint8
	// Padding to align the union.
	_ [3]byte
	// Body contains the union (PrestatDir).
	Body [4]byte
}
