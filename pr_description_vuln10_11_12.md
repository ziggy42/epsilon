_Written by Gemini and Claude_

This PR remediates three high-severity integer overflow vulnerabilities in table and memory operations.

### Table Integer Overflow in Bounds Checks
Two related integer overflow issues exist in table operations:
1. **Negative `Size()` via sign truncation:** `Size()` returns `int32(len(t.elements))`. When the table has ≥ 2³¹ elements, this wraps to a negative value. When subsequently cast to `uint64` via sign extension, the bounds check is bypassed.
2. **Index arithmetic overflow:** `Fill`, `Init`, and `Copy` use `int32` addition for index calculations. Values near `MaxInt32` overflow to negative, causing immediate panics.

#### Root Cause
```go
// Bounds check — bypassed when Size() is negative
uint64(uint32(startIndex)) + uint64(uint32(n)) > uint64(t.Size())
// e.g. t.Size() = -2147483638 → uint64(-2147483638) = 18446744071562067978

// Index arithmetic — overflows
t.elements[startIndex + i] = val
// startIndex=2147483647, i=1 → 2147483647+1 = -2147483648 → PANIC
```

#### Remediation
- Changed `Size()` to return `uint32`.
- Perform all index arithmetic in `uint64`.
- Cast via `uint32` before widening to `uint64` to avoid sign extension.

---

### Memory Allocation Overflow (`NewMemory`)
`NewMemory` computes the initial memory size as `Min * pageSize` using `uint32` arithmetic. When `Min = 65536` (the maximum allowed by the spec, representing 4 GiB), the multiplication overflows: `65536 × 65536 = 0` in `uint32`. The resulting memory has size 0 despite being declared with the maximum page count.

#### Remediation
- Use `uint64` for the size calculation.

---

### Memory Grow Panic
`Memory.Grow(32768)` on an empty memory (0 pages) passes the bounds check (`32768 + 0 > 32768` is false) but panics during the actual allocation due to overflow in the underlying slice operation.

#### Remediation
- Use `uint64` for the growth calculation and validate that the resulting page count multiplied by `pageSize` does not overflow the host's `int` type before allocating.
