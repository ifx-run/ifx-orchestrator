package hop_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
)

func TestWithMinOut(t *testing.T) {
	p := solana.NewWallet().PublicKey()
	a, b := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	inner := mock.New("m", p, a, b, ata, nil)

	h := hop.WithMinOut(inner, 0)
	min, bare := hop.SplitMinOut(h)
	if min == nil || *min != 0 {
		t.Fatalf("min %+v", min)
	}
	if bare.VenueID() != "m" {
		t.Fatalf("bare %s", bare.VenueID())
	}

	h2 := hop.WithMinOut(h, 42) // replace
	min2, _ := hop.SplitMinOut(h2)
	if min2 == nil || *min2 != 42 {
		t.Fatalf("replaced min %+v", min2)
	}
}
