package epsilon

import (
	"testing"
)

func TestVuln11_MemoryAllocationOverflow(t *testing.T) {
	// 65536 * 65536 overflows uint32 to 0.
	// We don't want to actually allocate 4GiB if we can avoid it in a test,
	// but the vulnerability is that it allocates 0.
	// However, make([]byte, 0) won't panic.
	// The problem is that it SHOULD have allocated 4GiB (or failed).
	mem := NewMemory(MemoryType{
		Limits: Limits{Min: 65536},
	})
	if mem.Size() == 0 {
		t.Errorf("VULN-11: Memory size is 0, expected 65536. Overflow occurred.")
	}
}

func TestVuln12_MemoryGrowPanic(t *testing.T) {
	mem := NewMemory(MemoryType{Limits: Limits{Min: 0}})
	
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("VULN-12: Memory.Grow panicked: %v", r)
		}
	}()
	
	// 32768 * 65536 = 2,147,483,648.
	// On some systems this might panic if it tries to allocate and fails,
	// but the report says it panics due to overflow in underlying slice operation.
	// Specifically if it wraps to negative.
	mem.Grow(32768)
}
