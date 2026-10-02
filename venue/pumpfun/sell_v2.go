package pumpfun

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// SellQuoteV2Params configures SPL-quote sell_v2.
type SellQuoteV2Params struct {
	User              solana.PublicKey
	BaseMint          solana.PublicKey
	QuoteMint         solana.PublicKey
	BaseTokenProgram  solana.PublicKey
	QuoteTokenProgram solana.PublicKey
	Curve             CurveMeta
	UserBaseATA       solana.PublicKey
	UserQuoteATA      solana.PublicKey
}

// SellV2 is Pump.fun v2 ExactIn: base → quote.
type SellV2 struct {
	p            SellQuoteV2Params
	userBaseATA  solana.PublicKey
	userQuoteATA solana.PublicKey
}

// NewSellV2 validates and returns a v2 sell hop.
func NewSellV2(p SellQuoteV2Params) (*SellV2, error) {
	if p.User.IsZero() || p.BaseMint.IsZero() || p.QuoteMint.IsZero() || p.Curve.Creator.IsZero() {
		return nil, fmt.Errorf("pumpfun v2 sell: user, mints, and curve creator required")
	}
	baseTP := p.BaseTokenProgram
	if baseTP.IsZero() {
		baseTP = solana.TokenProgramID
	}
	quoteTP := p.QuoteTokenProgram
	if quoteTP.IsZero() {
		quoteTP = solana.TokenProgramID
	}
	baseATA := p.UserBaseATA
	if baseATA.IsZero() {
		var err error
		baseATA, err = ataAddress(p.User, p.BaseMint, baseTP)
		if err != nil {
			return nil, err
		}
	}
	quoteATA := p.UserQuoteATA
	if quoteATA.IsZero() {
		var err error
		quoteATA, err = ataAddress(p.User, p.QuoteMint, quoteTP)
		if err != nil {
			return nil, err
		}
	}
	return &SellV2{p: p, userBaseATA: baseATA, userQuoteATA: quoteATA}, nil
}

func (e *SellV2) VenueID() string             { return "pumpfun_sell_v2" }
func (e *SellV2) InputMint() solana.PublicKey { return e.p.BaseMint }
func (e *SellV2) OutputMint() solana.PublicKey {
	return e.p.QuoteMint
}
func (e *SellV2) OutputMeasureAccount() solana.PublicKey { return e.userQuoteATA }

func (e *SellV2) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	accts, err := resolveQuoteV2Accounts(BuyQuoteV2Params{
		User: e.p.User, BaseMint: e.p.BaseMint, QuoteMint: e.p.QuoteMint,
		BaseTokenProgram: e.p.BaseTokenProgram, QuoteTokenProgram: e.p.QuoteTokenProgram,
		Curve: e.p.Curve,
	}, e.userBaseATA, e.userQuoteATA)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	// sell_v2 omits global volume accumulator accounts present on buy.
	accts = trimSellV2Accounts(accts)

	data := make([]byte, 24)
	copy(data[:8], discSellV2[:])
	putU64LE(data[8:16], 0)
	putU64LE(data[16:24], 0)

	program := ProgramID
	ix := solana.NewInstruction(program, accts, data)
	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*SellV2)(nil)
