// Package gassponsored repays a gas sponsor from user proceeds (Feature).
//
// Two default modes:
//
//  1. FromNative — repay in SOL from intermediate SOL and/or WSOL proceeds.
//     settle = BpsMulCeil(estimatedCost (+ optional ATA rent), protectionBps).
//     WSOL is converted via UnwrapLamports (ATA kept open).
//
//  2. FromToken — repay a fixed token amount chosen at construction
//     (mint/ATA + raw amount); no on-chain gas estimation.
package gassponsored

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/solfunding"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Mode selects the repay strategy.
type Mode uint8

const (
	// ModeNative repays SOL from user SOL/WSOL trade proceeds (with protection bps).
	ModeNative Mode = iota
	// ModeToken repays a construction-time fixed SPL token amount.
	ModeToken
)

// DefaultProtectionBps is 1.0× (no markup). Use e.g. 12000 for 1.2× cushion.
const DefaultProtectionBps uint16 = 10_000

// Feature baselines balances before the route and, after the route, asserts
// proceeds cover the settle amount then pays RepayTo (may differ from Sponsor).
//
// Orthogonal to FlashRent: flash covers mid-tx rent peak; this covers fee-payer sponsorship.
type Feature struct {
	feature.Base

	Mode Mode

	// Sponsor is the fee-payer / fronting wallet identity (may equal RepayTo).
	// Not written on-chain by this Feature; kept for callers / logging.
	Sponsor solana.PublicKey
	// RepayTo receives native SOL repay (System transfer / UnwrapLamports destination).
	// Zero => fall back to Sponsor.
	RepayTo solana.PublicKey

	// --- ModeNative ---
	// EstimatedCost is the off-chain estimate (sig + priority + tip, …) in lamports
	// before the protection coefficient.
	EstimatedCost uint64
	// ProtectionBps scales EstimatedCost (+ optional ATA rent): settle = ceil(base * bps / 10000).
	// 10000 = 1.0×, 12000 = 1.2×. Must be > 0.
	ProtectionBps uint16
	// UserWSOLATA, when set, also baselines WSOL and prefers UnwrapLamports → RepayTo
	// before taking remaining settle from native SOL.
	UserWSOLATA solana.PublicKey
	// MeasureATA, when set, adds sponsor-paid ATA rent into the native settle base (ModeNative only).
	MeasureATA solana.PublicKey

	// --- ModeToken ---
	UserTokenATA  solana.PublicKey
	RepayTokenATA solana.PublicKey // collection ATA (may belong to a treasury distinct from Sponsor)
	TokenAmount   uint64           // fixed raw units at construction
	TokenProgram  solana.PublicKey

	userBefore   *typed.ScratchValue
	wsolBefore   *typed.ScratchValue
	tokenBefore  *typed.ScratchValue
	ataBefore    *typed.ScratchValue
	ataCost      *typed.ScratchValue
	ataCostReady bool
}

// Phase is Setup so WithATARent can baseline before Ata create.
// Register before AtaPolicy within this phase (stable sort). AfterRoute reverse
// then closes ATAs before sponsor repay when registered in that order.
func (f *Feature) Phase() feature.Phase { return feature.PhaseSetup }

// FromNative repays in SOL from user SOL/WSOL proceeds.
// sponsor is used as both fee-payer identity and default RepayTo; call WithRepayTo when they differ.
// protectionBps is the safety markup (10000 = 1.0×). Zero defaults to DefaultProtectionBps.
func FromNative(sponsor solana.PublicKey, estimatedCostLamports uint64, protectionBps uint16) *Feature {
	if protectionBps == 0 {
		protectionBps = DefaultProtectionBps
	}
	return &Feature{
		Mode:          ModeNative,
		Sponsor:       sponsor,
		RepayTo:       sponsor,
		EstimatedCost: estimatedCostLamports,
		ProtectionBps: protectionBps,
	}
}

// FromToken repays a fixed SPL token amount to repayATA (collection account).
// Optionally set WithSponsor when the fee payer differs from the token treasury owner.
func FromToken(userATA, repayATA solana.PublicKey, amount uint64) *Feature {
	return &Feature{
		Mode:          ModeToken,
		UserTokenATA:  userATA,
		RepayTokenATA: repayATA,
		TokenAmount:   amount,
	}
}

// New is an alias for FromNative(sponsor, estimatedCost, DefaultProtectionBps).
func New(sponsor solana.PublicKey, estimatedCostLamports uint64) *Feature {
	return FromNative(sponsor, estimatedCostLamports, DefaultProtectionBps)
}

// WithSponsor sets the fee-payer identity (does not change RepayTo / RepayTokenATA).
func (f *Feature) WithSponsor(sponsor solana.PublicKey) *Feature {
	f.Sponsor = sponsor
	return f
}

// WithRepayTo sets the native-SOL collection account when it differs from Sponsor.
func (f *Feature) WithRepayTo(repayTo solana.PublicKey) *Feature {
	f.RepayTo = repayTo
	return f
}

// WithProtectionBps sets the native-mode safety markup (e.g. 12000 = 1.2×).
func (f *Feature) WithProtectionBps(bps uint16) *Feature {
	f.ProtectionBps = bps
	return f
}

// WithWSOL enables WSOL proceeds as a native repay source (converted via UnwrapLamports to RepayTo).
func (f *Feature) WithWSOL(userWSOLATA solana.PublicKey) *Feature {
	f.UserWSOLATA = userWSOLATA
	return f
}

// WithATARent tracks sponsor-paid rent on ata and folds it into the native settle base
// before applying ProtectionBps (ModeNative only; register before Ata).
func (f *Feature) WithATARent(ata solana.PublicKey) *Feature {
	f.MeasureATA = ata
	return f
}

func (f *Feature) repayDest() solana.PublicKey {
	if !f.RepayTo.IsZero() {
		return f.RepayTo
	}
	return f.Sponsor
}

func (f *Feature) tokenProgram() solana.PublicKey {
	if f.TokenProgram.IsZero() {
		return solana.TokenProgramID
	}
	return f.TokenProgram
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	switch f.Mode {
	case ModeNative:
		return f.beforeNative(cx)
	case ModeToken:
		return f.beforeToken(cx)
	default:
		return fmt.Errorf("gassponsored: unknown Mode %d", f.Mode)
	}
}

func (f *Feature) beforeNative(cx *feature.Ctx) error {
	if f.repayDest().IsZero() {
		return fmt.Errorf("gassponsored: RepayTo or Sponsor is required")
	}
	if f.EstimatedCost == 0 && f.MeasureATA.IsZero() {
		return fmt.Errorf("gassponsored: EstimatedCost or MeasureATA is required")
	}
	if f.ProtectionBps == 0 {
		return fmt.Errorf("gassponsored: ProtectionBps must be > 0")
	}

	lb, err := cx.Let()
	if err != nil {
		return err
	}
	before, err := lb.Lamports(cx.User)
	if err != nil {
		return err
	}
	f.userBefore = &before

	if !f.UserWSOLATA.IsZero() {
		wb, err := lb.SplTokenAmount(f.UserWSOLATA)
		if err != nil {
			return err
		}
		f.wsolBefore = &wb
	}
	if !f.MeasureATA.IsZero() {
		ataB, err := lb.Lamports(f.MeasureATA)
		if err != nil {
			return err
		}
		f.ataBefore = &ataB
	}
	f.ataCostReady = false
	f.ataCost = nil
	return nil
}

func (f *Feature) beforeToken(cx *feature.Ctx) error {
	if f.UserTokenATA.IsZero() || f.RepayTokenATA.IsZero() {
		return fmt.Errorf("gassponsored: UserTokenATA and RepayTokenATA are required")
	}
	if f.TokenAmount == 0 {
		return fmt.Errorf("gassponsored: TokenAmount must be > 0")
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	before, err := lb.SplTokenAmount(f.UserTokenATA)
	if err != nil {
		return err
	}
	f.tokenBefore = &before
	return nil
}

func (f *Feature) BeforeEdge(cx *feature.Ctx, edgeIndex int) error {
	if f.Mode != ModeNative || edgeIndex != 0 || f.MeasureATA.IsZero() || f.ataCostReady || f.ataBefore == nil {
		return nil
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
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
	f.ataCost = &cost
	f.ataCostReady = true
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	switch f.Mode {
	case ModeNative:
		return f.afterNative(cx)
	case ModeToken:
		return f.afterToken(cx)
	default:
		return fmt.Errorf("gassponsored: unknown Mode %d", f.Mode)
	}
}

func (f *Feature) afterNative(cx *feature.Ctx) error {
	if f.userBefore == nil {
		return fmt.Errorf("gassponsored: missing SOL baseline")
	}

	post, err := cx.Let()
	if err != nil {
		return err
	}
	userAfter, err := post.Lamports(cx.User)
	if err != nil {
		return err
	}
	solProceeds, err := post.LetEval(expr.Sub(
		expr.Ref(userAfter.Index),
		expr.Ref(f.userBefore.Index),
	))
	if err != nil {
		return err
	}

	var wsolProceeds *typed.ScratchValue
	if f.wsolBefore != nil {
		wAfter, err := post.SplTokenAmount(f.UserWSOLATA)
		if err != nil {
			return err
		}
		wp, err := post.LetEval(expr.Sub(
			expr.Ref(wAfter.Index),
			expr.Ref(f.wsolBefore.Index),
		))
		if err != nil {
			return err
		}
		wsolProceeds = &wp
	}

	ataCostExpr := expr.U64(0)
	if f.ataCost != nil {
		ataCostExpr = expr.Ref(f.ataCost.Index)
	}
	base, err := post.LetEval(expr.Add(
		ataCostExpr,
		expr.U64(f.EstimatedCost),
	))
	if err != nil {
		return err
	}
	bpsConst, err := post.LetConstU64(uint64(f.ProtectionBps))
	if err != nil {
		return err
	}
	// Ceil markup so the sponsor is not under-repaid on rounding.
	settle, err := post.LetEval(expr.BpsMulCeil(
		expr.Ref(base.Index),
		expr.Ref(bpsConst.Index),
	))
	if err != nil {
		return err
	}

	var available typed.ScratchValue
	if wsolProceeds != nil {
		available, err = post.LetEval(expr.Add(
			expr.Ref(solProceeds.Index),
			expr.Ref(wsolProceeds.Index),
		))
		if err != nil {
			return err
		}
	} else {
		available = solProceeds
	}

	// Prefer WSOL: fromWSOL = min(wsolProceeds, settle); fromSOL = settle - fromWSOL.
	var fromWSOL, fromSOL typed.ScratchValue
	if wsolProceeds != nil {
		fromWSOL, err = post.LetEval(expr.Min(
			expr.Ref(wsolProceeds.Index),
			expr.Ref(settle.Index),
		))
		if err != nil {
			return err
		}
		fromSOL, err = post.LetEval(expr.Sub(
			expr.Ref(settle.Index),
			expr.Ref(fromWSOL.Index),
		))
		if err != nil {
			return err
		}
	} else {
		fromSOL = settle
	}

	assertIx, err := cx.Scratch.IxAssert(expr.Ge(
		expr.Ref(available.Index),
		expr.Ref(settle.Index),
	))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)

	if wsolProceeds != nil {
		if err := f.repayUnwrapWSOL(cx, fromWSOL); err != nil {
			return err
		}
	}
	return feature.EmitPatchedSystemTransfer(cx, cx.User, f.repayDest(), fromSOL)
}

func (f *Feature) afterToken(cx *feature.Ctx) error {
	if f.tokenBefore == nil {
		return fmt.Errorf("gassponsored: missing token baseline")
	}
	post, err := cx.Let()
	if err != nil {
		return err
	}
	after, err := post.SplTokenAmount(f.UserTokenATA)
	if err != nil {
		return err
	}
	proceeds, err := post.LetEval(expr.Sub(
		expr.Ref(after.Index),
		expr.Ref(f.tokenBefore.Index),
	))
	if err != nil {
		return err
	}

	assertIx, err := cx.Scratch.IxAssert(expr.Ge(
		expr.Ref(proceeds.Index),
		expr.U64(f.TokenAmount),
	))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)

	feature.EmitFixedTokenTransfer(cx, f.UserTokenATA, f.RepayTokenATA, cx.User, f.TokenAmount)
	return cx.Err()
}

func (f *Feature) repayUnwrapWSOL(cx *feature.Ctx, amount typed.ScratchValue) error {
	tp := f.tokenProgram()
	cx.Emit(solfunding.SyncNativeInstruction(f.UserWSOLATA, tp))
	zero := uint64(0)
	template := solfunding.UnwrapLamportsInstruction(f.UserWSOLATA, f.repayDest(), cx.User, tp, &zero)
	return feature.EmitRawPatchedCPI(cx, template, feature.RawCpiU64Patch(solfunding.UnwrapLamportsAmountOffset, amount))
}
