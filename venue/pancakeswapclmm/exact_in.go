// Package pancakeswapclmm implements ExactInHop for PancakeSwap Solana CLMM swap_v2 (ExactIn).
//
// Authoritative sources:
//   - Program ID from PancakeSwap Solana frontend / @pancakeswap/solana-clmm-sdk:
//     HpNfyc2Saw7RKkQd8nEL4khUcuPhQ7WwY1B2qjx8jxFq
//   - Instruction layout: Raydium CLMM–compatible swap_v2 (same Anchor sighash and 13 fixed
//     accounts + remaining tick arrays). Pancake’s Solana CLMM is a Raydium CLMM derivative;
//     see raydium-idl raydium_clmm.json "swap_v2" and Pancake program streams documenting
//     swap_v2 with 13 accounts + 4 args.
//
// https://www.npmjs.com/package/@pancakeswap/solana-clmm-sdk
// https://github.com/raydium-io/raydium-idl/blob/master/raydium_clmm/raydium_clmm.json
package pancakeswapclmm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is PancakeSwap Solana CLMM on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("HpNfyc2Saw7RKkQd8nEL4khUcuPhQ7WwY1B2qjx8jxFq")

// MemoProgram is required by swap_v2.
var MemoProgram = solana.MustPublicKeyFromBase58("MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr")

// Token2022Program is required by swap_v2.
var Token2022Program = solana.MustPublicKeyFromBase58("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")

var discSwapV2 = [8]byte{43, 4, 237, 11, 26, 201, 30, 98}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const swapV2DataLen = 41

const (
	offAmmConfig      = 9
	offTokenMint0     = 73
	offTokenMint1     = 105
	offTokenVault0    = 137
	offTokenVault1    = 169
	offObservationKey = 201
)

// PoolState holds CLMM pool fields for routing vaults/mints.
type PoolState struct {
	AmmConfig      solana.PublicKey
	TokenMint0     solana.PublicKey
	TokenMint1     solana.PublicKey
	TokenVault0    solana.PublicKey
	TokenVault1    solana.PublicKey
	ObservationKey solana.PublicKey
}

// DecodePoolState parses a PancakeSwap CLMM pool account.
func DecodePoolState(data []byte) (PoolState, error) {
	if len(data) < offObservationKey+32 {
		return PoolState{}, fmt.Errorf("pancakeswap clmm pool data too short: %d", len(data))
	}
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(data[off : off+32])
	}
	return PoolState{
		AmmConfig:      readPK(offAmmConfig),
		TokenMint0:     readPK(offTokenMint0),
		TokenMint1:     readPK(offTokenMint1),
		TokenVault0:    readPK(offTokenVault0),
		TokenVault1:    readPK(offTokenVault1),
		ObservationKey: readPK(offObservationKey),
	}, nil
}

type swapSide struct {
	inputVault, outputVault solana.PublicKey
	inputMint, outputMint   solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint solana.PublicKey) (swapSide, error) {
	switch {
	case inputMint.Equals(p.TokenMint0) && outputMint.Equals(p.TokenMint1):
		return swapSide{p.TokenVault0, p.TokenVault1, p.TokenMint0, p.TokenMint1}, nil
	case inputMint.Equals(p.TokenMint1) && outputMint.Equals(p.TokenMint0):
		return swapSide{p.TokenVault1, p.TokenVault0, p.TokenMint1, p.TokenMint0}, nil
	default:
		return swapSide{}, fmt.Errorf("pancakeswap clmm mints %s/%s do not match %s→%s",
			p.TokenMint0, p.TokenMint1, inputMint, outputMint)
	}
}

// Params configures one PancakeSwap CLMM swap_v2 ExactIn hop.
type Params struct {
	ProgramID     solana.PublicKey
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	TickArrays    []solana.PublicKey
}

// ExactIn is a PancakeSwap CLMM ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
	side swapSide
}

// NewExactIn validates direction and tick arrays.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	s, err := p.Pool.side(p.InputMint, p.OutputMint)
	if err != nil {
		return nil, err
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("pancakeswapclmm: user, pool, and ATAs required")
	}
	if len(p.TickArrays) == 0 {
		return nil, fmt.Errorf("pancakeswapclmm: at least one tick array required")
	}
	return &ExactIn{p: p, prog: prog, side: s}, nil
}

func (e *ExactIn) VenueID() string                        { return "pancakeswap_clmm" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, swapV2DataLen)
	copy(data[:8], discSwapV2[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)
	data[40] = 1 // is_base_input

	metas := solana.AccountMetaSlice{
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.Pool.AmmConfig, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.inputVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.outputVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.ObservationKey, IsWritable: true, IsSigner: false},
		{PublicKey: solana.TokenProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: Token2022Program, IsWritable: false, IsSigner: false},
		{PublicKey: MemoProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.side.inputMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.side.outputMint, IsWritable: false, IsSigner: false},
	}
	for _, ta := range e.p.TickArrays {
		metas = append(metas, &solana.AccountMeta{PublicKey: ta, IsWritable: true, IsSigner: false})
	}

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: solana.NewInstruction(e.prog, metas, data),
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
