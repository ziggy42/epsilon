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

// See github.com/WebAssembly/WASI/blob/wasi-0.1/preview1/witx/typenames.witx
// for a lot more error codes.
const (
	errnoSuccess     int32 = 0  // No error occurred.
	errnoAcces       int32 = 2  // Permission denied.
	errnoBadF        int32 = 8  // Bad file descriptor.
	errnoExist       int32 = 20 // File exists.
	errnoFault       int32 = 21 // Bad address.
	errnoInval       int32 = 28 // Invalid argument.
	errnoIO          int32 = 29 // I/O error.
	errnoIsDir       int32 = 31 // Is a directory.
	errnoLoop        int32 = 32 // Too many levels of symbolic links.
	errnoNameTooLong int32 = 37 // Filename too long.
	errnoNFile       int32 = 41 // Too many files open in system.
	errnoNoEnt       int32 = 44 // No such file or directory.
	errnoNotDir      int32 = 54 // Not a directory or symbolic link.
	errnoNotEmpty    int32 = 55 // Directory not empty.
	errnoNotSock     int32 = 57 // Not a socket.
	errnoNotSup      int32 = 58 // Not supported.
	errnoPerm        int32 = 63 // Operation not permitted.
	errnoPipe        int32 = 64 // Broken pipe.
	errnoNotCapable  int32 = 76 // Extension: Capabilities insufficient.
)

const (
	RightsFdDatasync           int64 = 1 << 0
	RightsFdRead               int64 = 1 << 1
	RightsFdSeek               int64 = 1 << 2
	RightsFdFdstatSetFlags     int64 = 1 << 3
	RightsFdSync               int64 = 1 << 4
	RightsFdTell               int64 = 1 << 5
	RightsFdWrite              int64 = 1 << 6
	RightsFdAdvise             int64 = 1 << 7
	RightsFdAllocate           int64 = 1 << 8
	RightsPathCreateDirectory  int64 = 1 << 9
	RightsPathCreateFile       int64 = 1 << 10
	RightsPathLinkSource       int64 = 1 << 11
	RightsPathLinkTarget       int64 = 1 << 12
	RightsPathOpen             int64 = 1 << 13
	RightsFdReaddir            int64 = 1 << 14
	RightsPathReadlink         int64 = 1 << 15
	RightsPathRenameSource     int64 = 1 << 16
	RightsPathRenameTarget     int64 = 1 << 17
	RightsPathFilestatGet      int64 = 1 << 18
	RightsPathFilestatSetSize  int64 = 1 << 19
	RightsPathFilestatSetTimes int64 = 1 << 20
	RightsFdFilestatGet        int64 = 1 << 21
	RightsFdFilestatSetSize    int64 = 1 << 22
	RightsFdFilestatSetTimes   int64 = 1 << 23
	RightsPathSymlink          int64 = 1 << 24
	RightsPathRemoveDirectory  int64 = 1 << 25
	RightsPathUnlinkFile       int64 = 1 << 26
	RightsPollFdReadwrite      int64 = 1 << 27
	RightsSockShutdown         int64 = 1 << 28
)
