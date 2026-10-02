// Package feehook charges a platform fee from user SOL (Feature).
package feehook

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Feature takes either a fixed lamports fee and/or a bps of user SOL proceeds after the route.
//
// Exact fee_node_index (mid-graph) is deferred; this MVP settles once in AfterRoute.
type Feature struct {
	feature.Base
	Recipient solana.PublicKey
	// FixedLamports is transferred literally when > 0.
	FixedLamports uint64
	// ProceedsBps takes floor(bps * (user_lamports_after - baseline) / 10000) when > 0.
	ProceedsBps uint16

	userBefore *typed.ScratchValue
}

// Fixed returns a FeeHook that transfers a constant lamports amount.
func Fixed(recipient solana.PublicKey, lamports uint64) *Feature {
	return &Feature{Recipient: recipient, FixedLamports: lamports}
}

// ProceedsBPS returns a FeeHook that takes bps of user SOL delta after the route.
func ProceedsBPS(recipient solana.PublicKey, bps uint16) *Feature {
	return &Feature{Recipient: recipient, ProceedsBps: bps}
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.Recipient.IsZero() {
		return fmt.Errorf("feehook: Recipient is required")
	}
	if f.FixedLamports == 0 && f.ProceedsBps == 0 {
		return fmt.Errorf("feehook: FixedLamports or ProceedsBps is required")
	}
	if f.ProceedsBps == 0 {
		return nil
	}
	lb := cx.Scratch.LetBuilder()
	before, err := lb.Lamports(cx.User)
	if err != nil {
		return err
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	f.userBefore = &before
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.FixedLamports > 0 {
		feature.EmitFixedSystemTransfer(cx, cx.User, f.Recipient, f.FixedLamports)
	}
	if f.ProceedsBps == 0 {
		return nil
	}
	if f.userBefore == nil {
		return fmt.Errorf("feehook: missing baseline for ProceedsBps")
	}

	post := cx.Scratch.LetBuilder()
	userAfter, err := post.Lamports(cx.User)
	if err != nil {
		return err
	}
	proceeds, err := post.LetEval(expr.Sub(
		expr.Ref(userAfter.Index),
		expr.Ref(f.userBefore.Index),
	))
	if err != nil {
		return err
	}
	bpsConst, err := post.LetConstU64(uint64(f.ProceedsBps))
	if err != nil {
		return err
	}
	fee, err := post.LetEval(expr.BpsMulFloor(
		expr.Ref(proceeds.Index),
		expr.Ref(bpsConst.Index),
	))
	if err != nil {
		return err
	}
	postIx, err := post.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(postIx)

	return feature.EmitPatchedSystemTransfer(cx, cx.User, f.Recipient, fee)
}
