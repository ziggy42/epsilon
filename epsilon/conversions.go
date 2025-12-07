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

func BoolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func U32ToI32(v uint32) int32 { return int32(v) }
func U64ToI64(v uint64) int64 { return int64(v) }

func SignExtend8To32(v byte) int32    { return int32(int8(v)) }
func ZeroExtend8To32(v byte) int32    { return int32(v) }
func SignExtend16To32(v uint16) int32 { return int32(int16(v)) }
func ZeroExtend16To32(v uint16) int32 { return int32(v) }

func SignExtend8To64(v byte) int64    { return int64(int8(v)) }
func ZeroExtend8To64(v byte) int64    { return int64(v) }
func SignExtend16To64(v uint16) int64 { return int64(int16(v)) }
func ZeroExtend16To64(v uint16) int64 { return int64(v) }
func SignExtend32To64(v uint32) int64 { return int64(int32(v)) }
func ZeroExtend32To64(v uint32) int64 { return int64(v) }

func IdentityV128(v V128Value) V128Value { return v }
