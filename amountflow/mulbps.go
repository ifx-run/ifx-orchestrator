package amountflow

import (
	"fmt"
	"math/bits"
)

// MulBpsFloor returns floor(v * bps / 10000) without overflowing uint64 multiply.
// bps must be in 0..10000.
func MulBpsFloor(v uint64, bps uint16) (uint64, error) {
	if bps > 10_000 {
		return 0, fmt.Errorf("bps %d exceeds 10000", bps)
	}
	if bps == 0 || v == 0 {
		return 0, nil
	}
	if bps == 10_000 {
		return v, nil
	}
	hi, lo := bits.Mul64(v, uint64(bps))
	// (hi:lo) / 10000
	q, _ := bits.Div64(hi, lo, 10_000)
	return q, nil
}
