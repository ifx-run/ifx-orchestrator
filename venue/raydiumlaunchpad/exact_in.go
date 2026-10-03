// Package raydiumlaunchpad implements ExactInHop for Raydium LaunchLab buy/sell exact in.
//
// Authoritative: https://github.com/raydium-io/raydium-idl/blob/master/raydium_launchpad/raydium_launchpad.json ("buy_exact_in", "sell_exact_in").
package raydiumlaunchpad

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Raydium LaunchLab on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("LanMV9sAd7wArD4vJFi2qDdfnVhFxYSUg6eADduJ3uj")

// Authority and event authority PDAs (mainnet).
var (
	LaunchpadAuthority = solana.MustPublicKeyFromBase58("WLHv2UAZm6z4KyaaELi5pjdbJh6RESMva1Rnn8pJVVh")
	EventAuthority     = solana.MustPublicKeyFromBase58("2DPAtwB8L12vrMRExbLuyGnC7n2J5LNoZQSejeQGpwkr")
)

var (
	discBuyExactIn  = [8]byte{250, 234, 13, 123, 213, 156, 19, 236}
	discSellExactIn = [8]byte{149, 39, 222, 155, 211, 124, 152, 26}
)

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const launchAccountCount = 15

// PoolMeta is launchpad pool routing metadata.
type PoolMeta struct {
	ConfigID   solana.PublicKey
	PlatformID solana.PublicKey
	BaseVault  solana.PublicKey
	QuoteVault solana.PublicKey
	BaseMint   solana.PublicKey
	QuoteMint  solana.PublicKey
}

// BuyParams configures quote→base buy_exact_in.
type BuyParams struct {
	User             solana.PublicKey
	PoolID           solana.PublicKey
	Pool             PoolMeta
	UserQuoteATA     solana.PublicKey
	UserBaseATA      solana.PublicKey
	BaseTokenProgram solana.PublicKey // zero => Tokenkeg
}

// BuyExactIn is LaunchLab buy_exact_in (ExactIn on quote).
type BuyExactIn struct {
	p BuyParams
}

// NewBuyExactIn validates accounts.
func NewBuyExactIn(p BuyParams) (*BuyExactIn, error) {
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserQuoteATA.IsZero() || p.UserBaseATA.IsZero() {
		return nil, fmt.Errorf("raydiumlaunchpad buy: user, pool, and ATAs required")
	}
	if p.Pool.ConfigID.IsZero() || p.Pool.PlatformID.IsZero() {
		return nil, fmt.Errorf("raydiumlaunchpad buy: config and platform required")
	}
	return &BuyExactIn{p: p}, nil
}

func (e *BuyExactIn) VenueID() string { return "raydium_launchpad_buy" }
func (e *BuyExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.Pool.QuoteMint, e.p.UserQuoteATA)
}
func (e *BuyExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.Pool.BaseMint, e.p.UserBaseATA)
}

func (e *BuyExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	return buildBlueprint(e.p.User, e.p.PoolID, e.p.Pool, e.p.UserBaseATA, e.p.UserQuoteATA, e.p.BaseTokenProgram, discBuyExactIn)
}

// SellParams configures base→quote sell_exact_in.
type SellParams struct {
	User             solana.PublicKey
	PoolID           solana.PublicKey
	Pool             PoolMeta
	UserBaseATA      solana.PublicKey
	UserQuoteATA     solana.PublicKey
	BaseTokenProgram solana.PublicKey
}

// SellExactIn is LaunchLab sell_exact_in.
type SellExactIn struct {
	p SellParams
}

// NewSellExactIn validates accounts.
func NewSellExactIn(p SellParams) (*SellExactIn, error) {
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserBaseATA.IsZero() || p.UserQuoteATA.IsZero() {
		return nil, fmt.Errorf("raydiumlaunchpad sell: user, pool, and ATAs required")
	}
	if p.Pool.ConfigID.IsZero() || p.Pool.PlatformID.IsZero() {
		return nil, fmt.Errorf("raydiumlaunchpad sell: config and platform required")
	}
	return &SellExactIn{p: p}, nil
}

func (e *SellExactIn) VenueID() string { return "raydium_launchpad_sell" }
func (e *SellExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.Pool.BaseMint, e.p.UserBaseATA)
}
func (e *SellExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.Pool.QuoteMint, e.p.UserQuoteATA)
}

func (e *SellExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	return buildBlueprint(e.p.User, e.p.PoolID, e.p.Pool, e.p.UserBaseATA, e.p.UserQuoteATA, e.p.BaseTokenProgram, discSellExactIn)
}

func buildBlueprint(
	user, poolID solana.PublicKey,
	pool PoolMeta,
	userBase, userQuote solana.PublicKey,
	baseTokenProgram solana.PublicKey,
	disc [8]byte,
) (hop.HopBlueprint, error) {
	tp := baseTokenProgram
	if tp.IsZero() {
		tp = solana.TokenProgramID
	}
	data := make([]byte, 24)
	copy(data[:8], disc[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)

	ix := solana.NewInstruction(ProgramID, solana.AccountMetaSlice{
		{PublicKey: user, IsWritable: true, IsSigner: true},
		{PublicKey: LaunchpadAuthority, IsWritable: false, IsSigner: false},
		{PublicKey: pool.ConfigID, IsWritable: false, IsSigner: false},
		{PublicKey: pool.PlatformID, IsWritable: false, IsSigner: false},
		{PublicKey: poolID, IsWritable: true, IsSigner: false},
		{PublicKey: userBase, IsWritable: true, IsSigner: false},
		{PublicKey: userQuote, IsWritable: true, IsSigner: false},
		{PublicKey: pool.BaseVault, IsWritable: true, IsSigner: false},
		{PublicKey: pool.QuoteVault, IsWritable: true, IsSigner: false},
		{PublicKey: pool.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: pool.QuoteMint, IsWritable: false, IsSigner: false},
		{PublicKey: tp, IsWritable: false, IsSigner: false},
		{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: EventAuthority, IsWritable: false, IsSigner: false},
		{PublicKey: ProgramID, IsWritable: false, IsSigner: false},
	}, data)

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*BuyExactIn)(nil)
var _ hop.ExactInHop = (*SellExactIn)(nil)
