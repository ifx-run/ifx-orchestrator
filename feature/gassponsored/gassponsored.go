// Package gassponsored repays a gas sponsor from user SOL proceeds (Feature).
package gassponsored

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Feature baselines user lamports before the route and, after the route, asserts
// proceeds cover FixedCost (+ optional sponsor-paid ATA rent) then patches a repay transfer.
//
// Orthogonal to FlashRent: flash covers mid-tx rent peak; this covers fee-payer sponsorship.
type Feature struct {
	feature.Base
	Sponsor   solana.PublicKey // repay destination (gas treasury; may differ from fee payer)
	FixedCost uint64           // basic + priority + tip estimate (lamports)

	// MeasureATA, when set, measures lamports deposited into that ATA between BeforeRoute
	// and the first BeforeEdge (typically after Ata create) and adds it to the settle amount.
	MeasureATA solana.PublicKey

	userBefore  *typed.ScratchValue
	ataBefore   *typed.ScratchValue
	ataCost     *typed.ScratchValue
	ataCostReady bool
}

// New returns a GasSponsored Feature.
func New(sponsor solana.PublicKey, fixedCost uint64) *Feature {
	return &Feature{Sponsor: sponsor, FixedCost: fixedCost}
}

// WithATARent tracks sponsor-paid rent on ata (call after Feature(Ata) registration order:
// GasSponsored must be registered before Ata so baselines run first).
func (f *Feature) WithATARent(ata solana.PublicKey) *Feature {
	f.MeasureATA = ata
	return f
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.Sponsor.IsZero() {
		return fmt.Errorf("gassponsored: Sponsor is required")
	}
	if f.FixedCost == 0 && f.MeasureATA.IsZero() {
		return fmt.Errorf("gassponsored: FixedCost or MeasureATA is required")
	}
	lb := cx.Scratch.LetBuilder()
	before, err := lb.Lamports(cx.User)
	if err != nil {
		return err
	}
	if !f.MeasureATA.IsZero() {
		ataB, err := lb.Lamports(f.MeasureATA)
		if err != nil {
			return err
		}
		f.ataBefore = &ataB
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	f.userBefore = &before
	f.ataCostReady = false
	f.ataCost = nil
	return nil
}

func (f *Feature) BeforeEdge(cx *feature.Ctx, edgeIndex int) error {
	if edgeIndex != 0 || f.MeasureATA.IsZero() || f.ataCostReady || f.ataBefore == nil {
		return nil
	}
	lb := cx.Scratch.LetBuilder()
	after, err := lb.Lamports(f.MeasureATA)
	if err != nil {
		return err
	}
	cost, err := lb.LetEval(expr.Sub(
		expr.Ref(after.Index),
		expr.Ref(f.ataBefore.Index),
	))
	if err != nil {
		return err
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	f.ataCost = &cost
	f.ataCostReady = true
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.userBefore == nil {
		return fmt.Errorf("gassponsored: missing baseline (BeforeRoute not run)")
	}

	ataCost := f.ataCost
	if ataCost == nil {
		lb := cx.Scratch.LetBuilder()
		z, err := lb.LetConstU64(0)
		if err != nil {
			return err
		}
		ix, err := lb.BuildIx()
		if err != nil {
			return err
		}
		cx.Emit(ix)
		ataCost = &z
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
	settle, err := post.LetEval(expr.Add(
		expr.Ref(ataCost.Index),
		expr.U64(f.FixedCost),
	))
	if err != nil {
		return err
	}
	postIx, err := post.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(postIx)

	assertIx, err := cx.Scratch.IxAssert(expr.Ge(
		expr.Ref(proceeds.Index),
		expr.Ref(settle.Index),
	))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)

	return feature.EmitPatchedSystemTransfer(cx, cx.User, f.Sponsor, settle)
}
