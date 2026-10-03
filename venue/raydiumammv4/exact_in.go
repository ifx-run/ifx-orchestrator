// Package raydiumammv4 implements ExactInHop for Raydium AMM v4 SwapBaseInV2.
//
// Authoritative: https://github.com/raydium-io/raydium-amm/blob/master/program/src/instruction.rs (swap_base_in_v2).
package raydiumammv4

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Raydium AMM v4 on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8")

const discSwapBaseInV2 uint8 = 16

const (
	AmountInOffset uint16 = 1
	MinOutOffset   uint16 = 9
)

const (
	offBaseVault  = 336
	offQuoteVault = 368
	offBaseMint   = 400
	offQuoteMint  = 432
)

// PoolState mirrors Raydium AMM v4 LiquidityStateV4 fields used for swaps.
type PoolState struct {
	BaseVault  solana.PublicKey
	QuoteVault solana.PublicKey
	BaseMint   solana.PublicKey
	QuoteMint  solana.PublicKey
}

// Authority is the shared Raydium AMM v4 authority PDA.
func Authority() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1")
}

// DecodePoolState parses an AMM v4 pool account (no anchor disc).
func DecodePoolState(data []byte) (PoolState, error) {
	if len(data) < offQuoteMint+32 {
		return PoolState{}, fmt.Errorf("amm v4 pool data too short: %d", len(data))
	}
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(data[off : off+32])
	}
	return PoolState{
		BaseVault:  readPK(offBaseVault),
		QuoteVault: readPK(offQuoteVault),
		BaseMint:   readPK(offBaseMint),
		QuoteMint:  readPK(offQuoteMint),
	}, nil
}

func (p PoolState) validateDirection(inputMint, outputMint solana.PublicKey) error {
	ok := (inputMint.Equals(p.BaseMint) && outputMint.Equals(p.QuoteMint)) ||
		(inputMint.Equals(p.QuoteMint) && outputMint.Equals(p.BaseMint))
	if !ok {
		return fmt.Errorf("amm v4 pool mints %s/%s do not match swap %s→%s",
			p.BaseMint, p.QuoteMint, inputMint, outputMint)
	}
	return nil
}

// Params configures one ExactIn AMM v4 hop.
type Params struct {
	ProgramID     solana.PublicKey // zero => ProgramID
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	TokenProgram  solana.PublicKey // zero => Tokenkeg
}

// ExactIn is a Raydium AMM v4 ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
}

// NewExactIn validates and returns an ExactInHop.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	if err := p.Pool.validateDirection(p.InputMint, p.OutputMint); err != nil {
		return nil, err
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("raydiumammv4: user, pool, and ATAs are required")
	}
	return &ExactIn{p: p, prog: prog}, nil
}

func (e *ExactIn) VenueID() string { return "raydium_amm_v4" }
func (e *ExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.InputMint, e.p.UserInputATA)
}
func (e *ExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.OutputMint, e.p.UserOutputATA)
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 17)
	data[0] = discSwapBaseInV2
	binary.LittleEndian.PutUint64(data[1:9], 0)
	binary.LittleEndian.PutUint64(data[9:17], 0)

	tokenProgram := e.p.TokenProgram
	if tokenProgram.IsZero() {
		tokenProgram = solana.TokenProgramID
	}

	ix := solana.NewInstruction(e.prog, solana.AccountMetaSlice{
		{PublicKey: tokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: Authority(), IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.BaseVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.QuoteVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
	}, data)

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
