// Package gassponsored repays a gas sponsor from user proceeds (Feature).
package gassponsored

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/solfunding"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// RepayMode selects how the sponsor is repaid (SponsorRepayMode).
type RepayMode uint8

const (
	// InterceptSOL asserts user SOL delta covers settle, then System-transfers to Sponsor.
	InterceptSOL RepayMode = iota
	// InterceptWSOL SyncNative + patched UnwrapLamports from UserWSOLATA directly to Sponsor.
	// Keeps the WSOL ATA open (no CloseAccount). Prefer over closing when the ATA is reused.
	InterceptWSOL
	// TokenTransfer asserts user token delta covers settle, then SPL-transfers to SponsorATA.
	// FixedCost is in token raw units for this mode.
	TokenTransfer
)

// Feature baselines balances before the route and, after the route, asserts proceeds cover
// FixedCost (+ optional sponsor-paid ATA rent) then repays via RepayMode.
//
// Orthogonal to FlashRent: flash covers mid-tx rent peak; this covers fee-payer sponsorship.
type Feature struct {
	feature.Base
	Sponsor   solana.PublicKey // repay destination (gas treasury; may differ from fee payer)
	FixedCost uint64           // basic + priority + tip estimate (lamports or token raw units)
	Mode      RepayMode

	// MeasureATA, when set, measures lamports deposited into that ATA between BeforeRoute
	// and the first BeforeEdge (typically after Ata create) and adds it to the settle amount.
	MeasureATA solana.PublicKey

	// InterceptWSOL / TokenTransfer accounts.
	UserTokenATA    solana.PublicKey // source ATA (WSOL or other)
	SponsorTokenATA solana.PublicKey // required for TokenTransfer; ignored for InterceptWSOL
	TokenProgram    solana.PublicKey // zero => Tokenkeg

	userBefore   *typed.ScratchValue
	tokenBefore  *typed.ScratchValue
	ataBefore    *typed.ScratchValue
	ataCost      *typed.ScratchValue
	ataCostReady bool
}

// New returns a GasSponsored Feature with InterceptSOL repay.
func New(sponsor solana.PublicKey, fixedCost uint64) *Feature {
	return &Feature{Sponsor: sponsor, FixedCost: fixedCost, Mode: InterceptSOL}
}

// WithMode sets the repay mode.
func (f *Feature) WithMode(mode RepayMode) *Feature {
	f.Mode = mode
	return f
}

// WithWSOL configures InterceptWSOL (unwrap lamports from user WSOL ATA to Sponsor).
func (f *Feature) WithWSOL(userWSOLATA solana.PublicKey) *Feature {
	f.Mode = InterceptWSOL
	f.UserTokenATA = userWSOLATA
	return f
}

// WithToken configures TokenTransfer repay (FixedCost in token raw units).
func (f *Feature) WithToken(userATA, sponsorATA solana.PublicKey) *Feature {
	f.Mode = TokenTransfer
	f.UserTokenATA = userATA
	f.SponsorTokenATA = sponsorATA
	return f
}

// WithATARent tracks sponsor-paid rent on ata (register GasSponsored before Ata).
func (f *Feature) WithATARent(ata solana.PublicKey) *Feature {
	f.MeasureATA = ata
	return f
}

func (f *Feature) tokenProgram() solana.PublicKey {
	if f.TokenProgram.IsZero() {
		return solana.TokenProgramID
	}
	return f.TokenProgram
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.Sponsor.IsZero() {
		return fmt.Errorf("gassponsored: Sponsor is required")
	}
	if f.FixedCost == 0 && f.MeasureATA.IsZero() {
		return fmt.Errorf("gassponsored: FixedCost or MeasureATA is required")
	}
	switch f.Mode {
	case InterceptWSOL:
		if f.UserTokenATA.IsZero() {
			return fmt.Errorf("gassponsored: UserTokenATA required for InterceptWSOL")
		}
	case TokenTransfer:
		if f.UserTokenATA.IsZero() || f.SponsorTokenATA.IsZero() {
			return fmt.Errorf("gassponsored: UserTokenATA and SponsorTokenATA required for TokenTransfer")
		}
	}

	if !f.MeasureATA.IsZero() && f.Mode != InterceptSOL {
		return fmt.Errorf("gassponsored: MeasureATA is only supported with InterceptSOL")
	}
	lb := cx.Scratch.LetBuilder()
	switch f.Mode {
	case InterceptSOL:
		before, e := lb.Lamports(cx.User)
		if e != nil {
			return e
		}
		f.userBefore = &before
	case InterceptWSOL, TokenTransfer:
		before, e := lb.SplTokenAmount(f.UserTokenATA)
		if e != nil {
			return e
		}
		f.tokenBefore = &before
	}
	if !f.MeasureATA.IsZero() {
		ataB, e := lb.Lamports(f.MeasureATA)
		if e != nil {
			return e
		}
		f.ataBefore = &ataB
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
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
	ataCost, err := f.resolveATACost(cx)
	if err != nil {
		return err
	}
	proceeds, settle, err := f.measureProceedsAndSettle(cx, ataCost)
	if err != nil {
		return err
	}

	assertIx, err := cx.Scratch.IxAssert(expr.Ge(
		expr.Ref(proceeds.Index),
		expr.Ref(settle.Index),
	))
	if err != nil {
		return err
	}
	cx.Emit(assertIx)

	switch f.Mode {
	case InterceptSOL:
		return feature.EmitPatchedSystemTransfer(cx, cx.User, f.Sponsor, settle)
	case InterceptWSOL:
		return f.repayUnwrapWSOL(cx, settle)
	case TokenTransfer:
		return feature.EmitPatchedTokenTransfer(cx, f.UserTokenATA, f.SponsorTokenATA, cx.User, settle)
	default:
		return fmt.Errorf("gassponsored: unknown RepayMode %d", f.Mode)
	}
}

func (f *Feature) resolveATACost(cx *feature.Ctx) (typed.ScratchValue, error) {
	if f.ataCost != nil {
		return *f.ataCost, nil
	}
	lb := cx.Scratch.LetBuilder()
	z, err := lb.LetConstU64(0)
	if err != nil {
		return typed.ScratchValue{}, err
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return typed.ScratchValue{}, err
	}
	cx.Emit(ix)
	return z, nil
}

func (f *Feature) measureProceedsAndSettle(cx *feature.Ctx, ataCost typed.ScratchValue) (typed.ScratchValue, typed.ScratchValue, error) {
	post := cx.Scratch.LetBuilder()
	var proceeds typed.ScratchValue
	var err error
	switch f.Mode {
	case InterceptSOL:
		if f.userBefore == nil {
			return typed.ScratchValue{}, typed.ScratchValue{}, fmt.Errorf("gassponsored: missing SOL baseline")
		}
		userAfter, e := post.Lamports(cx.User)
		if e != nil {
			return typed.ScratchValue{}, typed.ScratchValue{}, e
		}
		proceeds, err = post.LetEval(expr.Sub(
			expr.Ref(userAfter.Index),
			expr.Ref(f.userBefore.Index),
		))
	case InterceptWSOL, TokenTransfer:
		if f.tokenBefore == nil {
			return typed.ScratchValue{}, typed.ScratchValue{}, fmt.Errorf("gassponsored: missing token baseline")
		}
		tokAfter, e := post.SplTokenAmount(f.UserTokenATA)
		if e != nil {
			return typed.ScratchValue{}, typed.ScratchValue{}, e
		}
		proceeds, err = post.LetEval(expr.Sub(
			expr.Ref(tokAfter.Index),
			expr.Ref(f.tokenBefore.Index),
		))
	}
	if err != nil {
		return typed.ScratchValue{}, typed.ScratchValue{}, err
	}
	settle, err := post.LetEval(expr.Add(
		expr.Ref(ataCost.Index),
		expr.U64(f.FixedCost),
	))
	if err != nil {
		return typed.ScratchValue{}, typed.ScratchValue{}, err
	}
	postIx, err := post.BuildIx()
	if err != nil {
		return typed.ScratchValue{}, typed.ScratchValue{}, err
	}
	cx.Emit(postIx)
	return proceeds, settle, nil
}

func (f *Feature) repayUnwrapWSOL(cx *feature.Ctx, settle typed.ScratchValue) error {
	tp := f.tokenProgram()
	cx.Emit(solfunding.SyncNativeInstruction(f.UserTokenATA, tp))
	// Patched UnwrapLamports(Some(settle)) → Sponsor. Keeps ATA open.
	zero := uint64(0)
	template := solfunding.UnwrapLamportsInstruction(f.UserTokenATA, f.Sponsor, cx.User, tp, &zero)
	return feature.EmitRawPatchedCPI(cx, template, feature.RawCpiU64Patch(solfunding.UnwrapLamportsAmountOffset, settle))
}
