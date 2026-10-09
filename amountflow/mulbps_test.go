package amountflow

import (
	"math"
	"math/bits"
	"testing"
)

func TestMulBpsFloorBasic(t *testing.T) {
	got, err := MulBpsFloor(10_000, 3000)
	if err != nil || got != 3000 {
		t.Fatalf("got %d err %v", got, err)
	}
	got, err = MulBpsFloor(10_000, 7000)
	if err != nil || got != 7000 {
		t.Fatalf("got %d err %v", got, err)
	}
	got, err = MulBpsFloor(1, 1)
	if err != nil || got != 0 {
		t.Fatalf("floor tiny got %d", got)
	}
}

func TestMulBpsFloorNoOverflow(t *testing.T) {
	got, err := MulBpsFloor(math.MaxUint64, 9999)
	if err != nil {
		t.Fatal(err)
	}
	hi, lo := bits.Mul64(math.MaxUint64, 9999)
	want, _ := bits.Div64(hi, lo, 10_000)
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
