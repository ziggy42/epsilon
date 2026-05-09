// Copyright 2026 Google LLC
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
	"testing"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
)

func TestClocks_Resolution(t *testing.T) {
	// Verify current resolution is degraded to 100µs
	res, errno := getClockResolution(clockRealtime)
	if errno != errnoSuccess {
		t.Fatalf("getClockResolution failed: %d", errno)
	}
	if res != 100000 {
		t.Errorf("expected resolution 100000 ns (100µs), got %d ns", res)
	}
}

func TestClocks_Consistency(t *testing.T) {
	// clock_res_get returns ENOTSUP for CPU clocks
	_, errno := getClockResolution(clockProcessCPUTimeID)
	if errno != errnoNotSup {
		t.Errorf("expected ENOTSUP (58) for CPU clock resolution, got %d", errno)
	}

	// clock_time_get returns ENOTSUP
	_, errno = getTimestamp(time.Now(), clockProcessCPUTimeID)
	if errno != errnoNotSup {
		t.Errorf("expected ENOTSUP (58) for CPU clock timestamp, got %d", errno)
	}
}

func TestClocks_TimeGetConsistency(t *testing.T) {
	mem := epsilon.NewMemory(epsilon.MemoryType{Limits: epsilon.Limits{Min: 1}})
	w := &WasiModule{monotonicClockStart: time.Now()}

	// Verify clock_res_get consistency with clock_time_get for CPU clocks
	errno := w.clockResGet(mem, int32(clockProcessCPUTimeID), 0)
	if errno != errnoNotSup {
		t.Errorf("expected clockResGet(CPU) to return ENOTSUP, got %d", errno)
	}

	errno = w.clockTimeGet(mem, int32(clockProcessCPUTimeID), 8)
	if errno != errnoNotSup {
		t.Errorf("expected clockTimeGet(CPU) to return ENOTSUP, got %d", errno)
	}
}
