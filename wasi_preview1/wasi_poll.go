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
	"time"
	"unsafe"

	"github.com/ziggy42/epsilon/epsilon"
)

const subclockFlagsSubscriptionClockAbstime uint16 = 1 << 0

type subscriptionClock struct {
	identifier uint64
	clockId    uint32
	timeout    uint64
	precision  uint64
	flags      uint16
}

type subscriptionFdReadwrite struct {
	fd uint32
}

type subscription struct {
	userData         uint64
	subscriptionType uint8
	// Padding ensures the Body field starts at offset 16 (8-byte aligned).
	// In C, the union after a u8 field would be padded to maintain alignment.
	// Without this, Body would start at offset 9, but it needs 8-byte alignment
	// because it contains u64 fields. So we add 7 bytes of padding (9 + 7 = 16).
	_ [7]byte
	// body contains the union of SubscriptionClock and SubscriptionFdReadwrite.
	// Max size is SubscriptionClock (30 bytes: u64 + u32 + u64 + u64 + u16).
	body [32]byte
}

type eventFdReadwrite struct {
	nBytes uint64
	flags  uint16
}

type event struct {
	userData    uint64
	errorCode   int16 // u16 in WASI spec, not u32
	eventType   uint8
	fdReadWrite eventFdReadwrite
}

// parseSubscription reads a Subscription struct from memory.
func parseSubscription(
	memory *epsilon.Memory,
	offset uint32,
) (subscription, error) {
	data, err := memory.Get(0, offset, uint32(unsafe.Sizeof(subscription{})))
	if err != nil {
		return subscription{}, err
	}

	var sub subscription
	sub.userData = binary.LittleEndian.Uint64(data[0:8])
	sub.subscriptionType = data[8]
	copy(sub.body[:], data[16:48])
	return sub, nil
}

// parseSubscriptionClock extracts clock subscription fields from the body.
func parseSubscriptionClock(body [32]byte) subscriptionClock {
	return subscriptionClock{
		identifier: binary.LittleEndian.Uint64(body[0:8]),
		clockId:    binary.LittleEndian.Uint32(body[8:12]),
		timeout:    binary.LittleEndian.Uint64(body[12:20]),
		precision:  binary.LittleEndian.Uint64(body[20:28]),
		flags:      binary.LittleEndian.Uint16(body[28:30]),
	}
}

// parseSubscriptionFdReadwrite extracts FD subscription fields from the body.
func parseSubscriptionFdReadwrite(body [32]byte) subscriptionFdReadwrite {
	return subscriptionFdReadwrite{
		fd: binary.LittleEndian.Uint32(body[0:4]),
	}
}

// writeEvent writes an Event struct to memory.
func writeEvent(memory *epsilon.Memory, offset uint32, event event) error {
	data := make([]byte, 32)
	binary.LittleEndian.PutUint64(data[0:8], event.userData)
	binary.LittleEndian.PutUint16(data[8:10], uint16(event.errorCode))
	data[10] = event.eventType
	binary.LittleEndian.PutUint64(data[16:24], event.fdReadWrite.nBytes)
	binary.LittleEndian.PutUint16(data[24:26], event.fdReadWrite.flags)
	return memory.Set(0, offset, data)
}

func (w *WasiModule) pollOneoff(
	inst *epsilon.ModuleInstance,
	inPtr, outPtr, nsubscriptions, neventsPtr int32,
) int32 {
	if nsubscriptions == 0 {
		return ErrnoInval
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	subscriptions := make([]subscription, nsubscriptions)
	for i := range nsubscriptions {
		offset := uint32(inPtr) + uint32(i)*uint32(unsafe.Sizeof(subscription{}))
		sub, err := parseSubscription(memory, offset)
		if err != nil {
			return ErrnoFault
		}
		subscriptions[i] = sub
	}

	// Process subscriptions and generate events
	events := make([]event, 0, nsubscriptions)
	var sleepDuration time.Duration
	var clockEvents []event // Store clock events separately

	for _, sub := range subscriptions {
		switch sub.subscriptionType {
		case EventTypeClock:
			// Parse clock subscription
			clockSub := parseSubscriptionClock(sub.body)

			// Calculate timeout
			var timeout int64
			if clockSub.flags&subclockFlagsSubscriptionClockAbstime != 0 {
				// Absolute time
				now, err := w.getTimestamp(clockSub.clockId)
				if err != nil {
					// Invalid clock ID
					events = append(events, event{
						userData:  sub.userData,
						errorCode: int16(ErrnoInval),
						eventType: EventTypeClock,
					})
					continue
				}
				timeout = max(int64(clockSub.timeout)-now, 0)
			} else {
				// Relative time
				timeout = int64(clockSub.timeout)
			}

			// Track the maximum sleep duration
			duration := time.Duration(timeout)
			if duration > sleepDuration {
				sleepDuration = duration
			}

			// Store clock event for later - only add it if we actually sleep
			clockEvents = append(clockEvents, event{
				userData:  sub.userData,
				errorCode: int16(ErrnoSuccess),
				eventType: EventTypeClock,
			})
		case EventTypeFdRead, EventTypeFdWrite:
			fdSub := parseSubscriptionFdReadwrite(sub.body)
			fdIndex := int32(fdSub.fd)

			// Validate the FD and check rights
			fd, errCode := w.fs.getFileOrDir(fdIndex, RightsPollFdReadwrite)
			if errCode != ErrnoSuccess {
				events = append(events, event{
					userData:  sub.userData,
					errorCode: int16(errCode),
					eventType: sub.subscriptionType,
				})
				continue
			}

			// Check if the FD is actually ready for the requested operation
			var isReady bool
			var nbytes uint64

			// For stdin/stdout/stderr, check based on the FD and operation type
			switch fdIndex {
			case 0: // stdin
				// stdin is readable (if there's data), but NOT writable
				isReady = (sub.subscriptionType == EventTypeFdRead)
			case 1, 2: // stdout, stderr
				// stdout/stderr are writable, but NOT readable
				isReady = (sub.subscriptionType == EventTypeFdWrite)
			default:
				// Regular files are always ready for both read and write
				if fd.fileType == FileTypeRegularFile {
					isReady = true
					// Try to get file size for regular files
					if info, err := fd.file.Stat(); err == nil {
						nbytes = uint64(info.Size())
					}
				} else {
					// For other file types (directories, etc.), consider them ready
					isReady = true
				}
			}

			// Only generate an event if the FD is ready
			if isReady {
				events = append(events, event{
					userData:  sub.userData,
					errorCode: int16(ErrnoSuccess),
					eventType: sub.subscriptionType,
					fdReadWrite: eventFdReadwrite{
						nBytes: nbytes,
						// TODO: Implement proper hangup detection using syscall.Select or
						// unix.Poll to set EventRwFlagsFdReadwriteHangup when appropriate.
						flags: 0,
					},
				})
			}

		default:
			// Unknown event type
			events = append(events, event{
				userData:  sub.userData,
				errorCode: int16(ErrnoInval),
				eventType: sub.subscriptionType,
			})
		}
	}

	// Determine if we need to sleep
	// Only sleep if there are no immediately ready FD events
	hasReadyFdEvents := len(events) > 0
	if sleepDuration > 0 && !hasReadyFdEvents {
		// No FD events are ready, so we sleep and then return clock events
		time.Sleep(sleepDuration)
		events = append(events, clockEvents...)
	}
	// If there ARE ready FD events, we don't sleep and don't return clock events

	// Write events to output memory
	for i, e := range events {
		offset := uint32(outPtr) + uint32(i)*uint32(unsafe.Sizeof(event{}))
		if err := writeEvent(memory, offset, e); err != nil {
			return ErrnoFault
		}
	}

	// Write the number of events
	err = memory.StoreUint32(0, uint32(neventsPtr), uint32(len(events)))
	if err != nil {
		return ErrnoFault
	}

	return ErrnoSuccess
}
