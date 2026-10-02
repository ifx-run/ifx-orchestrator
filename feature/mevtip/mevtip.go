// Package mevtip pays a fixed SOL tip after the route (Feature).
package mevtip

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
)

// Feature emits a System Program transfer of Lamports to TipReceiver in AfterRoute.
type Feature struct {
	feature.Base
	TipReceiver solana.PublicKey
	Lamports    uint64
}

// New returns a MevTip Feature.
func New(tipReceiver solana.PublicKey, lamports uint64) *Feature {
	return &Feature{TipReceiver: tipReceiver, Lamports: lamports}
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.TipReceiver.IsZero() {
		return fmt.Errorf("mevtip: TipReceiver is required")
	}
	if f.Lamports == 0 {
		return nil
	}
	feature.EmitFixedSystemTransfer(cx, cx.User, f.TipReceiver, f.Lamports)
	return nil
}
