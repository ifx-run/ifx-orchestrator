package hop

import "testing"

func TestSplitBpsAdd(t *testing.T) {
	a := MustPartial(3000)
	b := MustPartial(7000)
	sum, err := a.TryAdd(b)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.IsFull() {
		t.Fatalf("expected Full, got %+v", sum)
	}

	c := MustPartial(5000)
	_, err = a.TryAdd(c)
	if err != nil {
		t.Fatal(err)
	}
	partialSum, err := a.TryAdd(c)
	if err != nil {
		t.Fatal(err)
	}
	if partialSum.Bps() != 8000 {
		t.Fatalf("got %d", partialSum.Bps())
	}
}
