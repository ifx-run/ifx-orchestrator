package pumpfun

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// BuyQuoteV2Params configures SPL-quote buy_exact_quote_in_v2.
type BuyQuoteV2Params struct {
	User              solana.PublicKey
	BaseMint          solana.PublicKey
	QuoteMint         solana.PublicKey
	BaseTokenProgram  solana.PublicKey
	QuoteTokenProgram solana.PublicKey
	Curve             CurveMeta
	UserBaseATA       solana.PublicKey // zero => derive
	UserQuoteATA      solana.PublicKey // zero => derive
}

// BuyExactQuoteInV2 is Pump.fun v2 ExactIn: quote → base.
type BuyExactQuoteInV2 struct {
	p            BuyQuoteV2Params
	userBaseATA  solana.PublicKey
	userQuoteATA solana.PublicKey
}

// NewBuyExactQuoteInV2 validates and returns a v2 buy hop.
func NewBuyExactQuoteInV2(p BuyQuoteV2Params) (*BuyExactQuoteInV2, error) {
	if p.User.IsZero() || p.BaseMint.IsZero() || p.QuoteMint.IsZero() || p.Curve.Creator.IsZero() {
		return nil, fmt.Errorf("pumpfun v2 buy: user, mints, and curve creator required")
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
	return &BuyExactQuoteInV2{p: p, userBaseATA: baseATA, userQuoteATA: quoteATA}, nil
}

func (e *BuyExactQuoteInV2) VenueID() string { return "pumpfun_buy_exact_quote_in_v2" }
func (e *BuyExactQuoteInV2) Input() hop.Port {
	return hop.TokenPort(e.p.QuoteMint, e.userQuoteATA)
}
func (e *BuyExactQuoteInV2) Output() hop.Port {
	return hop.TokenPort(e.p.BaseMint, e.userBaseATA)
}

func (e *BuyExactQuoteInV2) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	accts, err := resolveQuoteV2Accounts(e.p, e.userBaseATA, e.userQuoteATA)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	data := make([]byte, 24)
	copy(data[:8], discBuyExactQuoteInV2[:])
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

var _ hop.ExactInHop = (*BuyExactQuoteInV2)(nil)
