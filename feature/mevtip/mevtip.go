// Package mevtip pays a SOL tip after the route (Feature).
//
// Fixed: New(receiver, lamports) — Jito inclusion bribe (reverts with the tx if a later
// arbcheck assert fails).
//
// ShareNative: tip floor(native_profit * bps / 10000), optionally capped and including WSOL.
package mevtip

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Feature emits a System Program transfer to TipReceiver in AfterRoute.
type Feature struct {
	feature.Base
	TipReceiver solana.PublicKey
	Lamports    uint64

	ShareBps    uint16
	MaxLamports uint64
	WSOLATA     solana.PublicKey

	solBefore  *typed.ScratchValue
	wsolBefore *typed.ScratchValue
}

// Phase settles after route baselines/asserts (same band as fees; registration order is stable).
func (f *Feature) Phase() feature.Phase { return feature.PhaseSettlement }

// New returns a fixed-lamports MevTip Feature.
func New(tipReceiver solana.PublicKey, lamports uint64) *Feature {
	return &Feature{TipReceiver: tipReceiver, Lamports: lamports}
}

// ShareNative tips a fraction of native (optional WSOL) profit realized by the route.
func ShareNative(tipReceiver solana.PublicKey, bps uint16) *Feature {
	return &Feature{TipReceiver: tipReceiver, ShareBps: bps}
}

// WithMax caps a ShareNative tip (0 = no cap).
func (f *Feature) WithMax(lamports uint64) *Feature {
	f.MaxLamports = lamports
	return f
}

// WithWSOL includes WSOL ATA delta in ShareNative profit.
func (f *Feature) WithWSOL(wsolATA solana.PublicKey) *Feature {
	f.WSOLATA = wsolATA
	return f
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.ShareBps == 0 {
		return nil
	}
	if f.TipReceiver.IsZero() {
		return fmt.Errorf("mevtip: TipReceiver is required")
	}
	if f.ShareBps > 10_000 {
		return fmt.Errorf("mevtip: ShareBps must be 1..10000")
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	sol, err := lb.Lamports(cx.User)
	if err != nil {
		return err
	}
	f.solBefore = &sol
	if !f.WSOLATA.IsZero() {
		w, err := lb.SplTokenAmount(f.WSOLATA)
		if err != nil {
			return err
		}
		f.wsolBefore = &w
	}
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.TipReceiver.IsZero() {
		return fmt.Errorf("mevtip: TipReceiver is required")
	}
	if f.ShareBps > 0 {
		return f.afterShare(cx)
	}
	if f.Lamports == 0 {
		return nil
	}
	feature.EmitFixedSystemTransfer(cx, cx.User, f.TipReceiver, f.Lamports)
	return nil
}

func (f *Feature) afterShare(cx *feature.Ctx) error {
	if f.solBefore == nil {
		return fmt.Errorf("mevtip: missing native baseline")
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	after, err := lb.Lamports(cx.User)
	if err != nil {
		return err
	}
	end := after
	start := *f.solBefore
	if f.wsolBefore != nil {
		wAfter, err := lb.SplTokenAmount(f.WSOLATA)
		if err != nil {
			return err
		}
		end, err = lb.LetEval(expr.Add(expr.Ref(after.Index), expr.Ref(wAfter.Index)))
		if err != nil {
			return err
		}
		start, err = lb.LetEval(expr.Add(expr.Ref(f.solBefore.Index), expr.Ref(f.wsolBefore.Index)))
		if err != nil {
			return err
		}
	}
	// Saturating: spending SOL (non-cycle) yields tip 0 instead of wrapping or reverting.
	profit, err := lb.LetEval(expr.SaturatingSub(expr.Ref(end.Index), expr.Ref(start.Index)))
	if err != nil {
		return err
	}
	bpsConst, err := lb.LetConstU64(uint64(f.ShareBps))
	if err != nil {
		return err
	}
	tip, err := lb.LetEval(expr.BpsMulFloor(expr.Ref(profit.Index), expr.Ref(bpsConst.Index)))
	if err != nil {
		return err
	}
	// System transfer can only spend native lamports, not WSOL.
	tip, err = lb.LetEval(expr.Min(expr.Ref(tip.Index), expr.Ref(after.Index)))
	if err != nil {
		return err
	}
	if f.MaxLamports > 0 {
		tip, err = lb.LetEval(expr.Min(expr.Ref(tip.Index), expr.U64(f.MaxLamports)))
		if err != nil {
			return err
		}
	}
	return feature.EmitPatchedSystemTransfer(cx, cx.User, f.TipReceiver, tip)
}
