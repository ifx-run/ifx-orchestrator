// Package pumpamm implements ExactInHop for Pump.fun AMM buy/sell.
//
// Authoritative:
//   - IDL: https://github.com/pump-fun/pump-public-docs/blob/main/idl/pump_amm.json ("buy", "sell")
//   - Remaining pool_v2 (required on mainnet after program upgrade):
//     https://github.com/pump-fun/pump-public-docs/issues/29
package pumpamm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Pump AMM on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA")

// FeeProgramID is the Pump fee program (fee_config PDA owner).
var FeeProgramID = solana.MustPublicKeyFromBase58("pfeeUxB6jkeY1Hxd7CsFCAjcbHA9rWtchMGdZ6VojVZ")

// GlobalConfig is the mainnet pump AMM global config account.
var GlobalConfig = solana.MustPublicKeyFromBase58("ADyA8hdefvWN2dbGGWFotbzWxrAvLW83WG6QCVXvJKqw")

var (
	discBuy  = [8]byte{0x66, 0x06, 0x3d, 0x12, 0x01, 0xda, 0xeb, 0xea}
	discSell = [8]byte{0x33, 0xe6, 0x85, 0xa4, 0x01, 0x7f, 0x83, 0xad}

	feeConfigConst = []byte{
		12, 20, 222, 252, 130, 94, 198, 118, 148, 37, 8, 24, 187, 101, 64, 101,
		244, 41, 141, 49, 86, 213, 113, 180, 212, 248, 9, 12, 24, 233, 168, 99,
	}
)

const (
	// IDL fixed accounts; +1 pool_v2 remaining (required on mainnet).
	buyAccountCount  = 24
	sellAccountCount = 22
)

// Sell: base_amount_in @8, min_quote_amount_out @16.
const (
	SellAmountInOffset uint16 = 8
	SellMinOutOffset   uint16 = 16
)

// Buy (quote→base ExactIn): max_quote_amount_in @16, min base_amount_out @8.
const (
	BuyAmountInOffset uint16 = 16
	BuyMinOutOffset   uint16 = 8
)

// PoolState holds pump AMM pool fields for account metas.
type PoolState struct {
	BaseMint              solana.PublicKey
	QuoteMint             solana.PublicKey
	PoolBaseTokenAccount  solana.PublicKey
	PoolQuoteTokenAccount solana.PublicKey
	CoinCreator           solana.PublicKey
}

func (p PoolState) validateSell(baseIn, quoteOut solana.PublicKey) error {
	if !baseIn.Equals(p.BaseMint) || !quoteOut.Equals(p.QuoteMint) {
		return fmt.Errorf("pumpamm sell: mints %s/%s vs %s→%s", p.BaseMint, p.QuoteMint, baseIn, quoteOut)
	}
	return nil
}

func (p PoolState) validateBuy(quoteIn, baseOut solana.PublicKey) error {
	if !quoteIn.Equals(p.QuoteMint) || !baseOut.Equals(p.BaseMint) {
		return fmt.Errorf("pumpamm buy: mints %s/%s vs %s→%s", p.QuoteMint, p.BaseMint, quoteIn, baseOut)
	}
	return nil
}

func eventAuthority(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("__event_authority")}, programID)
	return pda, err
}

func globalVolumeAccumulator(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("global_volume_accumulator")}, programID)
	return pda, err
}

func userVolumeAccumulator(programID, user solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("user_volume_accumulator"), user.Bytes()}, programID)
	return pda, err
}

// PoolV2PDA is the required remaining account after fee_program (seeds: "pool-v2", base_mint).
func PoolV2PDA(programID, baseMint solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("pool-v2"), baseMint.Bytes()}, programID)
	return pda, err
}

func feeConfigPDA() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("fee_config"), feeConfigConst},
		FeeProgramID,
	)
	return pda, err
}

func coinCreatorVaultAuthority(programID, coinCreator solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("creator_vault"), coinCreator.Bytes()},
		programID,
	)
	return pda, err
}

func associatedTokenAddress(owner, mint, tokenProgram solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{owner.Bytes(), tokenProgram.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	return pda, err
}

// SellParams configures base→quote ExactIn (sell).
type SellParams struct {
	ProgramID                    solana.PublicKey
	User                         solana.PublicKey
	PoolID                       solana.PublicKey
	Pool                         PoolState
	UserBaseATA                  solana.PublicKey
	UserQuoteATA                 solana.PublicKey
	BaseTokenProgram             solana.PublicKey
	QuoteTokenProgram            solana.PublicKey
	ProtocolFeeRecipient         solana.PublicKey
	ProtocolFeeRecipientTokenATA solana.PublicKey
}

// SellExactIn sells base for quote.
type SellExactIn struct {
	p         SellParams
	prog      solana.PublicKey
	eventAuth solana.PublicKey
}

// NewSellExactIn validates and returns a sell hop.
func NewSellExactIn(p SellParams) (*SellExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	if err := p.Pool.validateSell(p.Pool.BaseMint, p.Pool.QuoteMint); err != nil {
		return nil, err
	}
	if p.Pool.CoinCreator.IsZero() {
		return nil, fmt.Errorf("pumpamm sell: pool coin_creator required")
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserBaseATA.IsZero() || p.UserQuoteATA.IsZero() {
		return nil, fmt.Errorf("pumpamm sell: user, pool, and ATAs required")
	}
	if p.ProtocolFeeRecipient.IsZero() || p.ProtocolFeeRecipientTokenATA.IsZero() {
		return nil, fmt.Errorf("pumpamm sell: protocol fee accounts required")
	}
	ea, err := eventAuthority(prog)
	if err != nil {
		return nil, err
	}
	baseTP := p.BaseTokenProgram
	if baseTP.IsZero() {
		baseTP = solana.TokenProgramID
	}
	quoteTP := p.QuoteTokenProgram
	if quoteTP.IsZero() {
		quoteTP = solana.TokenProgramID
	}
	p.BaseTokenProgram = baseTP
	p.QuoteTokenProgram = quoteTP
	return &SellExactIn{p: p, prog: prog, eventAuth: ea}, nil
}

func (e *SellExactIn) VenueID() string             { return "pump_amm_sell" }
func (e *SellExactIn) InputMint() solana.PublicKey { return e.p.Pool.BaseMint }
func (e *SellExactIn) OutputMint() solana.PublicKey {
	return e.p.Pool.QuoteMint
}
func (e *SellExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserQuoteATA }

func (e *SellExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	return buildSwapBlueprint(e.p, e.prog, e.eventAuth, discSell, SellAmountInOffset, SellMinOutOffset, false)
}

// BuyParams configures quote→base ExactIn via buy (max quote in, min base out).
type BuyParams struct {
	ProgramID                    solana.PublicKey
	User                         solana.PublicKey
	PoolID                       solana.PublicKey
	Pool                         PoolState
	UserBaseATA                  solana.PublicKey
	UserQuoteATA                 solana.PublicKey
	BaseTokenProgram             solana.PublicKey
	QuoteTokenProgram            solana.PublicKey
	ProtocolFeeRecipient         solana.PublicKey
	ProtocolFeeRecipientTokenATA solana.PublicKey
}

// BuyExactIn buys base with quote (ExactIn on quote side).
type BuyExactIn struct {
	p         BuyParams
	prog      solana.PublicKey
	eventAuth solana.PublicKey
}

// NewBuyExactIn validates and returns a buy hop.
func NewBuyExactIn(p BuyParams) (*BuyExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	if err := p.Pool.validateBuy(p.Pool.QuoteMint, p.Pool.BaseMint); err != nil {
		return nil, err
	}
	if p.Pool.CoinCreator.IsZero() {
		return nil, fmt.Errorf("pumpamm buy: pool coin_creator required")
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserBaseATA.IsZero() || p.UserQuoteATA.IsZero() {
		return nil, fmt.Errorf("pumpamm buy: user, pool, and ATAs required")
	}
	if p.ProtocolFeeRecipient.IsZero() || p.ProtocolFeeRecipientTokenATA.IsZero() {
		return nil, fmt.Errorf("pumpamm buy: protocol fee accounts required")
	}
	ea, err := eventAuthority(prog)
	if err != nil {
		return nil, err
	}
	if p.BaseTokenProgram.IsZero() {
		p.BaseTokenProgram = solana.TokenProgramID
	}
	if p.QuoteTokenProgram.IsZero() {
		p.QuoteTokenProgram = solana.TokenProgramID
	}
	return &BuyExactIn{p: p, prog: prog, eventAuth: ea}, nil
}

func (e *BuyExactIn) VenueID() string             { return "pump_amm_buy" }
func (e *BuyExactIn) InputMint() solana.PublicKey { return e.p.Pool.QuoteMint }
func (e *BuyExactIn) OutputMint() solana.PublicKey {
	return e.p.Pool.BaseMint
}
func (e *BuyExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserBaseATA }

func (e *BuyExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	sp := SellParams{
		ProgramID: e.p.ProgramID, User: e.p.User, PoolID: e.p.PoolID, Pool: e.p.Pool,
		UserBaseATA: e.p.UserBaseATA, UserQuoteATA: e.p.UserQuoteATA,
		BaseTokenProgram: e.p.BaseTokenProgram, QuoteTokenProgram: e.p.QuoteTokenProgram,
		ProtocolFeeRecipient: e.p.ProtocolFeeRecipient, ProtocolFeeRecipientTokenATA: e.p.ProtocolFeeRecipientTokenATA,
	}
	return buildSwapBlueprint(sp, e.prog, e.eventAuth, discBuy, BuyAmountInOffset, BuyMinOutOffset, true)
}

func buildSwapBlueprint(
	p SellParams,
	prog, eventAuth solana.PublicKey,
	disc [8]byte,
	amountOff, minOff uint16,
	isBuy bool,
) (hop.HopBlueprint, error) {
	creatorVaultAuth, err := coinCreatorVaultAuthority(prog, p.Pool.CoinCreator)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	creatorVaultATA, err := associatedTokenAddress(creatorVaultAuth, p.Pool.QuoteMint, p.QuoteTokenProgram)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	feeCfg, err := feeConfigPDA()
	if err != nil {
		return hop.HopBlueprint{}, err
	}

	data := make([]byte, 24)
	copy(data[:8], disc[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)

	metas := solana.AccountMetaSlice{
		{PublicKey: p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: p.User, IsWritable: true, IsSigner: true},
		{PublicKey: GlobalConfig, IsWritable: false, IsSigner: false},
		{PublicKey: p.Pool.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: p.Pool.QuoteMint, IsWritable: false, IsSigner: false},
		{PublicKey: p.UserBaseATA, IsWritable: true, IsSigner: false},
		{PublicKey: p.UserQuoteATA, IsWritable: true, IsSigner: false},
		{PublicKey: p.Pool.PoolBaseTokenAccount, IsWritable: true, IsSigner: false},
		{PublicKey: p.Pool.PoolQuoteTokenAccount, IsWritable: true, IsSigner: false},
		{PublicKey: p.ProtocolFeeRecipient, IsWritable: false, IsSigner: false},
		{PublicKey: p.ProtocolFeeRecipientTokenATA, IsWritable: true, IsSigner: false},
		{PublicKey: p.BaseTokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: p.QuoteTokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SPLAssociatedTokenAccountProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: prog, IsWritable: false, IsSigner: false},
		{PublicKey: creatorVaultATA, IsWritable: true, IsSigner: false},
		{PublicKey: creatorVaultAuth, IsWritable: false, IsSigner: false},
	}
	if isBuy {
		gva, err := globalVolumeAccumulator(prog)
		if err != nil {
			return hop.HopBlueprint{}, err
		}
		uva, err := userVolumeAccumulator(prog, p.User)
		if err != nil {
			return hop.HopBlueprint{}, err
		}
		metas = append(metas,
			&solana.AccountMeta{PublicKey: gva, IsWritable: false, IsSigner: false},
			&solana.AccountMeta{PublicKey: uva, IsWritable: true, IsSigner: false},
		)
	}
	metas = append(metas,
		&solana.AccountMeta{PublicKey: feeCfg, IsWritable: false, IsSigner: false},
		&solana.AccountMeta{PublicKey: FeeProgramID, IsWritable: false, IsSigner: false},
	)
	// Mainnet upgrade remaining account (required for cashback and non-cashback).
	poolV2, err := PoolV2PDA(prog, p.Pool.BaseMint)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	metas = append(metas, &solana.AccountMeta{PublicKey: poolV2, IsWritable: false, IsSigner: false})

	want := sellAccountCount
	if isBuy {
		want = buyAccountCount
	}
	if len(metas) != want {
		return hop.HopBlueprint{}, fmt.Errorf("pumpamm: built %d accounts, want %d", len(metas), want)
	}

	minSite := hop.PatchSite{Offset: minOff}
	ix := solana.NewInstruction(prog, metas, data)
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: amountOff},
		MinOut:   &minSite,
	}, nil
}

var _ hop.ExactInHop = (*SellExactIn)(nil)
var _ hop.ExactInHop = (*BuyExactIn)(nil)
