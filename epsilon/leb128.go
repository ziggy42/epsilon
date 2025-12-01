// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package epsilon

const (
	continuationBit = 0x80
	payloadMask     = 0x7F
	signBit         = 0x40
)

func readUleb128(
	readByte func() (byte, error),
	maxBytes int,
) (uint64, int, error) {
	var result uint64
	var shift uint
	bytesRead := 0

	for {
		b, err := readByte()
		if err != nil {
			return 0, bytesRead, err
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, bytesRead, ErrIntRepresentationTooLong
		}

		group := uint64(b & payloadMask)
		result |= group << shift

		// If the continuation bit (MSB) is 0, we are done.
		if (b & continuationBit) == 0 {
			return result, bytesRead, nil
		}

		shift += 7
	}
}

// readSleb128 decodes a signed 64-bit integer immediate (SLEB128).
func readSleb128(readByte func() (byte, error), maxBytes int) (uint64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	bytesRead := 0

	for {
		b, err = readByte()
		if err != nil {
			return 0, err
		}
		bytesRead++
		if bytesRead > maxBytes {
			return 0, ErrIntRepresentationTooLong
		}

		// Each byte read contains 7 bits of "integer" and 1 bit to signal if the
		// parsing should continue. When reading int64, we can read up to
		// ceil(64/7) = 10 bytes. The last 10th byte will contain 1 continuation bit
		// (the most significant bit), 6 bits we should not use and the final, least
		// significant bit that we should interpret as the last 64th bit of the
		// integer we are tying to parse, the sign bit. The remaining 6 bits should
		// be all 0s for positive integers and all 1s for negative integers.
		if bytesRead == 10 {
			sign := b & 1
			remainingBits := (b & 0x7E) >> 1
			if sign == 0 && remainingBits != 0 {
				return 0, ErrIntegerTooLarge
			} else if sign == 1 && remainingBits != 0x3F {
				return 0, ErrIntegerTooLarge
			}
		}

		result |= int64(b&payloadMask) << shift

		// Check the continuation bit (MSB). If it's 0, this is the last byte.
		if (b & continuationBit) == 0 {
			break
		}

		shift += 7
	}

	if (b & signBit) != 0 {
		result |= -1 << (shift + 7)
	}

	return uint64(result), nil
}
