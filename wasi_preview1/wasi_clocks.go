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

import "time"

const clockResolutionNs = 1

const (
	clockRealtime         uint32 = 0 // The clock measuring real time.
	clockMonotonic        uint32 = 1 // The store-wide monotonic clock.
	clockProcessCPUTimeID uint32 = 2 // The CPU-time clock for the process.
	clockThreadCPUTimeID  uint32 = 3 // The CPU-time clock for the thread.
)

func getClockResolution(clockId uint32) (uint64, int32) {
	if !isValidClockId(clockId) {
		return 0, ErrnoInval
	}

	return clockResolutionNs, ErrnoSuccess
}

func getTimestamp(monotonicClockStartNs int64, clockId uint32) (int64, int32) {
	if !isValidClockId(clockId) {
		return 0, ErrnoInval
	}

	switch clockId {
	case clockRealtime:
		return time.Now().UnixNano(), ErrnoSuccess
	case clockMonotonic:
		return time.Now().UnixNano() - monotonicClockStartNs, ErrnoSuccess
	default:
		// TODO: ClockProcessCPUTimeID, ClockThreadCPUTimeID are not supported.
		return 0, ErrnoNotSup
	}
}

func isValidClockId(clockId uint32) bool {
	switch clockId {
	case clockRealtime,
		clockMonotonic,
		clockProcessCPUTimeID,
		clockThreadCPUTimeID:
		return true
	default:
		return false
	}
}
