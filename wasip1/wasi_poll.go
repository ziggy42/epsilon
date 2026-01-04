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
	"math"
	"time"
	"unsafe"

	"github.com/ziggy42/epsilon/epsilon"
)

const subclockFlagsSubscriptionClockAbstime uint16 = 1 << 0

const (
	eventTypeClock   uint8 = 0
	eventTypeFdRead  uint8 = 1
	eventTypeFdWrite uint8 = 2
)

type subscriptionClock struct {
	clockId   uint32
	timeout   uint64
	precision uint64
	flags     uint16
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
	body [32]byte
}

type eventFdReadwrite struct {
	nBytes uint64
	flags  uint16
}

type event struct {
	userData    uint64
	errorCode   int16
	eventType   uint8
	fdReadWrite eventFdReadwrite
}

func (e event) bytes() []byte {
	var data [32]byte
	binary.LittleEndian.PutUint64(data[0:8], e.userData)
	binary.LittleEndian.PutUint16(data[8:10], uint16(e.errorCode))
	data[10] = e.eventType
	binary.LittleEndian.PutUint64(data[16:24], e.fdReadWrite.nBytes)
	binary.LittleEndian.PutUint16(data[24:26], e.fdReadWrite.flags)
	return data[:]
}

// pendingClock stores successful clock subscriptions with a future timeout.
type pendingClock struct {
	timeout time.Duration
	event   event
}

// parseSubscriptionClock extracts clock subscription fields from the body.
func parseSubscriptionClock(body [32]byte) subscriptionClock {
	return subscriptionClock{
		clockId: binary.LittleEndian.Uint32(body[0:4]),
		// body[4:8] is padding
		timeout:   binary.LittleEndian.Uint64(body[8:16]),
		precision: binary.LittleEndian.Uint64(body[16:24]),
		flags:     binary.LittleEndian.Uint16(body[24:26]),
	}
}

// parseSubscriptionFdReadwrite extracts FD subscription fields from the body.
func parseSubscriptionFdReadwrite(body [32]byte) subscriptionFdReadwrite {
	return subscriptionFdReadwrite{
		fd: binary.LittleEndian.Uint32(body[0:4]),
	}
}

const maxSubscriptions = 4096

// handleClockSubscription processes a clock subscription and returns the
// timeout duration and an event. If the clock ID is invalid, it returns an
// error event.
func (w *WasiModule) handleClockSubscription(
	sub subscription,
) (time.Duration, event) {
	clockSub := parseSubscriptionClock(sub.body)
	isAbsoluteTime := clockSub.flags&subclockFlagsSubscriptionClockAbstime != 0

	var timeout int64
	if isAbsoluteTime {
	now, errCode := getTimestamp(w.monotonicClockStart, clockMonotonic)
		if errCode != errnoSuccess {
			return 0, event{
				userData:  sub.userData,
				errorCode: int16(errnoInval),
				eventType: eventTypeClock,
			}
		}
		timeout = max(int64(clockSub.timeout)-now, 0)
	} else {
		timeout = int64(clockSub.timeout)
	}

	event := event{
		userData:  sub.userData,
		errorCode: int16(errnoSuccess),
		eventType: eventTypeClock,
	}
	return time.Duration(timeout), event
}

// handleFdSubscription processes an FD read/write subscription and returns an
// event if the FD is ready, or nil if it's not ready yet.
func (w *WasiModule) handleFdSubscription(sub subscription) *event {
	fdSub := parseSubscriptionFdReadwrite(sub.body)

	fd, errCode := w.fs.getFileOrDir(int32(fdSub.fd), RightsPollFdReadwrite)
	if errCode != errnoSuccess {
		return &event{
			userData:  sub.userData,
			errorCode: int16(errCode),
			eventType: sub.subscriptionType,
		}
	}

	switch fdSub.fd {
	case 0: // stdin is readable but not writable
		if sub.subscriptionType != eventTypeFdRead {
			return nil
		}
	case 1, 2: // stdout/stderr are writable but not readable
		if sub.subscriptionType != eventTypeFdWrite {
			return nil
		}
	}

	var nbytes uint64
	if fd.fileType == fileTypeRegularFile {
		if info, err := fd.file.Stat(); err == nil {
			nbytes = uint64(info.Size())
		}
	}

	return &event{
		userData:  sub.userData,
		errorCode: int16(errnoSuccess),
		eventType: sub.subscriptionType,
		fdReadWrite: eventFdReadwrite{
			nBytes: nbytes,
			// TODO: Implement proper hangup detection using syscall.Select or
			// unix.Poll to set eventRwFlagsFdReadwriteHangup when appropriate.
			flags: 0,
		},
	}
}

func getSubscriptions(
	memory *epsilon.Memory,
	ptr, nsubscriptions int32,
) ([]subscription, error) {
	size := uint32(unsafe.Sizeof(subscription{}))
	data, err := memory.Get(uint32(ptr), 0, uint32(nsubscriptions)*size)
	if err != nil {
		return nil, err
	}

	subscriptions := make([]subscription, nsubscriptions)
	for i := range nsubscriptions {
		offset := uint32(i) * size
		subscriptions[i] = subscription{
			userData:         binary.LittleEndian.Uint64(data[offset : offset+8]),
			subscriptionType: data[offset+8],
		}
		copy(subscriptions[i].body[:], data[offset+16:offset+48])
	}
	return subscriptions, nil
}

func (w *WasiModule) pollOneoff(
	memory *epsilon.Memory,
	inPtr, outPtr, nsubscriptions, neventsPtr int32,
) int32 {
	if nsubscriptions <= 0 || nsubscriptions > maxSubscriptions {
		return errnoInval
	}

	subscriptions, err := getSubscriptions(memory, inPtr, nsubscriptions)
	if err != nil {
		return errnoFault
	}

	var pendingClocks []pendingClock
	var events []event
	minSleep := time.Duration(math.MaxInt64)
	for _, sub := range subscriptions {
		switch sub.subscriptionType {
		case eventTypeClock:
			timeout, clockEvent := w.handleClockSubscription(sub)
			if clockEvent.errorCode != int16(errnoSuccess) {
				events = append(events, clockEvent)
				continue
			}

			if timeout <= 0 {
				events = append(events, clockEvent)
			} else {
				minSleep = min(minSleep, timeout)
				pendingClocks = append(pendingClocks, pendingClock{timeout, clockEvent})
			}
		case eventTypeFdRead, eventTypeFdWrite:
			if fdEvent := w.handleFdSubscription(sub); fdEvent != nil {
				events = append(events, *fdEvent)
			}
		default:
			events = append(events, event{
				userData:  sub.userData,
				errorCode: int16(errnoInval),
				eventType: sub.subscriptionType,
			})
		}
	}

	// Only sleep if there are no immediate events and we have pending clocks
	if len(events) == 0 && len(pendingClocks) > 0 {
		time.Sleep(minSleep)
		for _, pc := range pendingClocks {
			// Include any clock event that should have fired by now.
			if pc.timeout <= minSleep {
				events = append(events, pc.event)
			}
		}
	}

	nEvents := uint32(len(events))
	if nEvents > 0 {
		const eventSize = 32
		buf := make([]byte, nEvents*eventSize)
		for i, e := range events {
			copy(buf[i*eventSize:], e.bytes())
		}

		if err := memory.Set(uint32(outPtr), 0, buf); err != nil {
			return errnoFault
		}
	}

	if err := memory.StoreUint32(uint32(neventsPtr), 0, nEvents); err != nil {
		return errnoFault
	}
	return errnoSuccess
}
