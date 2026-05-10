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

import "time"

const clockResolutionNs = 100_000 // To mitigate side-channel attacks.

const (
	clockRealtime         uint32 = 0 // The clock measuring real time.
	clockMonotonic        uint32 = 1 // The store-wide monotonic clock.
	clockProcessCPUTimeID uint32 = 2 // The CPU-time clock for the process.
	clockThreadCPUTimeID  uint32 = 3 // The CPU-time clock for the thread.
)

func getClockResolution(clockId uint32) (uint64, int32) {
	switch clockId {
	case clockRealtime, clockMonotonic:
		return clockResolutionNs, errnoSuccess
	case clockProcessCPUTimeID, clockThreadCPUTimeID:
		return 0, errnoNotSup
	default:
		return 0, errnoInval
	}
}

func getTimestamp(
	monotonicClockStart time.Time,
	clockId uint32,
) (int64, int32) {
	var ts int64
	switch clockId {
	case clockRealtime:
		ts = time.Now().UnixNano()
	case clockMonotonic:
		ts = time.Since(monotonicClockStart).Nanoseconds()
	case clockProcessCPUTimeID, clockThreadCPUTimeID:
		return 0, errnoNotSup
	default:
		return 0, errnoInval
	}

	// Round down to the nearest multiple of clockResolutionNs to mitigate side-channel attacks.
	return ts - (ts % int64(clockResolutionNs)), errnoSuccess
}
