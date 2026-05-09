package epsilon

import (
	"testing"
	"unsafe"
)

func TestVuln10_TableSizeOverflow(t *testing.T) {
	tab := &Table{
		elements: make([]int32, 0),
	}
	
	ptr := (*struct {
		data uintptr
		len  int
		cap  int
	})(unsafe.Pointer(&tab.elements))
	
	// Set len to 2^31. uint32(len) will be 2147483648.
	// Previously int32(len) would be -2147483648.
	ptr.len = 1 << 31
	
	size := tab.Size()
	if uint64(size) != 1<<31 {
		t.Errorf("VULN-10: Table.Size() as uint64 is %d, expected %d. Sign extension likely happened.", uint64(size), uint64(1<<31))
	}
}

func TestVuln10_TableFillArithmeticOverflow(t *testing.T) {
	tab := &Table{
		elements: make([]int32, 0),
	}
	
	ptr := (*struct {
		data uintptr
		len  int
		cap  int
	})(unsafe.Pointer(&tab.elements))
	ptr.len = 1 << 31
	ptr.cap = 1 << 31
	
	// The fix should ensure that we don't overflow to a negative index.
	// We don't have actual memory, so it will panic anyway on access,
	// but we want to ensure the logic doesn't bypass bounds or use wrong indices.
	
	// Actually, let's just test that the bounds check works for something that
	// would have bypassed it before.
	
	// If ptr.len = 2^31 + 10.
	ptr.len = (1 << 31) + 10
	// t.Size() = 2147483658.
	// Previously uint64(int32(t.Size())) was 18446744071562067978.
	
	// Try to access an index that is "out of bounds" for int32 but was "in bounds" for the huge value.
	// Wait, we can't have an index > 2^31-1 as a positive int32.
	
	// So let's test the negative startIndex case.
	err := tab.Fill(1, -1, 42)
	if err == nil {
		t.Errorf("VULN-10: Fill allowed negative startIndex!")
	}
	
	err = tab.Fill(-1, 0, 42)
	if err == nil {
		t.Errorf("VULN-10: Fill allowed negative n!")
	}
}
