// Package arbcheck asserts a minimum cycle profit after the route (Feature).
//
// Typical atomic arb (two aggregator legs A→B→A):
//
//	.Feature(arbcheck.Token(userA, minProfit)) // register first → AfterRoute asserts last
//	.Feature(mevtip.New(jitoTip, bribe))       // inclusion bribe; reverts with the tx if unprofitable
//	.Hop(jupAB).Hop(jupBA)
//
// Token mode: SPL ATA delta (after − before) must be ≥ MinProfit (0 = break-even).
// Native mode: user lamports (+ optional WSOL) delta must be ≥ MinProfit.
package arbcheck

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Feature snapshots balances in BeforeRoute and asserts a floor in AfterRoute.
type Feature struct {
	feature.Base

	TokenATA solana.PublicKey
	MinToken uint64

	Native    bool
	MinNative uint64
	WSOLATA   solana.PublicKey

	tokenBefore *typed.ScratchValue
	solBefore   *typed.ScratchValue
	wsolBefore  *typed.ScratchValue
}

// Token requires the ATA balance after the route to be at least before + minProfit.
func Token(ata solana.PublicKey, minProfit uint64) *Feature {
	return &Feature{TokenATA: ata, MinToken: minProfit}
}

// Native requires user lamports after the route to be at least before + minProfit.
func Native(minProfit uint64) *Feature {
	return &Feature{Native: true, MinNative: minProfit}
}

// WithWSOL adds WSOL ATA delta into the native profit (same units as lamports).
func (f *Feature) WithWSOL(wsolATA solana.PublicKey) *Feature {
	f.WSOLATA = wsolATA
	f.Native = true
	return f
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.TokenATA.IsZero() && !f.Native {
		return fmt.Errorf("arbcheck: Token ATA or Native is required")
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	if !f.TokenATA.IsZero() {
		before, err := lb.SplTokenAmount(f.TokenATA)
		if err != nil {
			return err
		}
		f.tokenBefore = &before
	}
	if f.Native {
		before, err := lb.Lamports(cx.User)
		if err != nil {
			return err
		}
		f.solBefore = &before
		if !f.WSOLATA.IsZero() {
			w, err := lb.SplTokenAmount(f.WSOLATA)
			if err != nil {
				return err
			}
			f.wsolBefore = &w
		}
	}
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if !f.TokenATA.IsZero() {
		if err := f.assertToken(cx); err != nil {
			return err
		}
	}
	if f.Native {
		if err := f.assertNative(cx); err != nil {
			return err
		}
	}
	return nil
}

func (f *Feature) assertToken(cx *feature.Ctx) error {
	if f.tokenBefore == nil {
		return fmt.Errorf("arbcheck: missing token baseline")
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	after, err := lb.SplTokenAmount(f.TokenATA)
	if err != nil {
		return err
	}
	need, err := lb.LetEval(expr.Add(expr.Ref(f.tokenBefore.Index), expr.U64(f.MinToken)))
	if err != nil {
		return err
	}
	assertIx, err := cx.Scratch.IxAssert(expr.Ge(expr.Ref(after.Index), expr.Ref(need.Index)))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)
	return cx.Err()
}

func (f *Feature) assertNative(cx *feature.Ctx) error {
	if f.solBefore == nil {
		return fmt.Errorf("arbcheck: missing native baseline")
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
	need, err := lb.LetEval(expr.Add(expr.Ref(start.Index), expr.U64(f.MinNative)))
	if err != nil {
		return err
	}
	assertIx, err := cx.Scratch.IxAssert(expr.Ge(expr.Ref(end.Index), expr.Ref(need.Index)))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)
	return cx.Err()
}
