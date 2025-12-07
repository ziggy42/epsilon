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

package epsilon

func boolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func uint32ToInt32(v uint32) int32 { return int32(v) }
func uint64ToInt64(v uint64) int64 { return int64(v) }

func signExtend8To32(v byte) int32    { return int32(int8(v)) }
func zeroExtend8To32(v byte) int32    { return int32(v) }
func signExtend16To32(v uint16) int32 { return int32(int16(v)) }
func zeroExtend16To32(v uint16) int32 { return int32(v) }

func signExtend8To64(v byte) int64    { return int64(int8(v)) }
func zeroExtend8To64(v byte) int64    { return int64(v) }
func signExtend16To64(v uint16) int64 { return int64(int16(v)) }
func zeroExtend16To64(v uint16) int64 { return int64(v) }
func signExtend32To64(v uint32) int64 { return int64(int32(v)) }
func zeroExtend32To64(v uint32) int64 { return int64(v) }

func identityV128(v V128Value) V128Value { return v }
