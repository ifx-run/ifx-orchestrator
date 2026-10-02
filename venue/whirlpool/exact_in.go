// Package whirlpool implements ExactInHop for Orca Whirlpool swap (ExactIn).
//
// Authoritative: https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/instructions/swap.rs
package whirlpool

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Orca Whirlpool on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc")

var discSwap = [8]byte{0xf8, 0xc6, 0x9e, 0x91, 0xe1, 0x75, 0x87, 0xc8}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const swapDataLen = 42 // disc + u64 + u64 + u128 + bool + bool

// PoolState holds whirlpool token sides.
type PoolState struct {
	TokenMintA  solana.PublicKey
	TokenMintB  solana.PublicKey
	TokenVaultA solana.PublicKey
	TokenVaultB solana.PublicKey
}

type swapSide struct {
	aToB                   bool
	userAccountA           solana.PublicKey
	userAccountB           solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint, userIn, userOut solana.PublicKey) (swapSide, error) {
	switch {
	case inputMint.Equals(p.TokenMintA) && outputMint.Equals(p.TokenMintB):
		return swapSide{true, userIn, userOut}, nil
	case inputMint.Equals(p.TokenMintB) && outputMint.Equals(p.TokenMintA):
		return swapSide{false, userOut, userIn}, nil
	default:
		return swapSide{}, fmt.Errorf("whirlpool mints %s/%s do not match %s→%s",
			p.TokenMintA, p.TokenMintB, inputMint, outputMint)
	}
}

// Params configures one Whirlpool swap ExactIn hop.
type Params struct {
	ProgramID     solana.PublicKey
	User          solana.PublicKey
	Whirlpool     solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	TokenProgram  solana.PublicKey // zero => Tokenkeg
	TickArray0    solana.PublicKey
	TickArray1    solana.PublicKey
	TickArray2    solana.PublicKey
	Oracle        solana.PublicKey
	// RemainingTickArrays are supplemental tick arrays (optional).
	RemainingTickArrays []solana.PublicKey
}

// ExactIn is an Orca Whirlpool ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
	side swapSide
}

// NewExactIn validates swap direction and accounts.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	s, err := p.Pool.side(p.InputMint, p.OutputMint, p.UserInputATA, p.UserOutputATA)
	if err != nil {
		return nil, err
	}
	if p.User.IsZero() || p.Whirlpool.IsZero() || p.Oracle.IsZero() {
		return nil, fmt.Errorf("whirlpool: user, pool, and oracle required")
	}
	if p.TickArray0.IsZero() || p.TickArray1.IsZero() || p.TickArray2.IsZero() {
		return nil, fmt.Errorf("whirlpool: tick arrays 0..2 required")
	}
	return &ExactIn{p: p, prog: prog, side: s}, nil
}

func (e *ExactIn) VenueID() string                        { return "orca_whirlpool" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, swapDataLen)
	copy(data[:8], discSwap[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)
	// sqrt_price_limit = 0
	data[40] = 1 // amount_specified_is_input
	if e.side.aToB {
		data[41] = 1
	}

	tokenProgram := e.p.TokenProgram
	if tokenProgram.IsZero() {
		tokenProgram = solana.TokenProgramID
	}

	metas := solana.AccountMetaSlice{
		{PublicKey: tokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.Whirlpool, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.userAccountA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.TokenVaultA, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.userAccountB, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.TokenVaultB, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.TickArray0, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.TickArray1, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.TickArray2, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Oracle, IsWritable: true, IsSigner: false},
	}
	for _, ta := range e.p.RemainingTickArrays {
		metas = append(metas, &solana.AccountMeta{PublicKey: ta, IsWritable: true, IsSigner: false})
	}

	minOff := MinOutOffset
	ix := solana.NewInstruction(e.prog, metas, data)
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
