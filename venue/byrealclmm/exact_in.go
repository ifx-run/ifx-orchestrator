// Package byrealclmm implements ExactInHop for Byreal CLMM swap_v2 (ExactIn).
//
// Authoritative: Byreal official IDL (byreal_amm_v3 / byreal_clmm) instruction "swap_v2"
// https://cdn.jsdelivr.net/npm/@byreal-io/byreal-clmm-sdk@0.2.2/dist/esm/instructions/target/idl/byreal_amm_v3.json
// Contract: https://github.com/byreal-git/byreal-clmm
// Docs: https://docs.byreal.io/developer/smart-contracts-and-api-and-sdk
package byrealclmm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Byreal CLMM on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("REALQqNEomY6cQGZJUGwywTBD2UmDT32rZcNnfxQ5N2")

// MemoProgram is required by swap_v2.
var MemoProgram = solana.MustPublicKeyFromBase58("MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr")

// Token2022Program is required by swap_v2.
var Token2022Program = solana.MustPublicKeyFromBase58("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")

// swap_v2 discriminator from Byreal IDL (same sighash as Raydium CLMM swap_v2).
var discSwapV2 = [8]byte{43, 4, 237, 11, 26, 201, 30, 98}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const swapV2DataLen = 41 // disc + amount + threshold + sqrt_limit + is_base_input

// Pool account field offsets (absolute, including 8-byte Anchor disc) — CLMM PoolState layout.
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

// DecodePoolState parses a Byreal CLMM pool account.
func DecodePoolState(data []byte) (PoolState, error) {
	if len(data) < offObservationKey+32 {
		return PoolState{}, fmt.Errorf("byreal clmm pool data too short: %d", len(data))
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
		return swapSide{}, fmt.Errorf("byreal clmm mints %s/%s do not match %s→%s",
			p.TokenMint0, p.TokenMint1, inputMint, outputMint)
	}
}

// Params configures one Byreal CLMM swap_v2 ExactIn hop.
type Params struct {
	ProgramID     solana.PublicKey
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	TickArrays    []solana.PublicKey // remaining tick-array accounts (caller-supplied)
}

// ExactIn is a Byreal CLMM ExactInHop.
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
		return nil, fmt.Errorf("byrealclmm: user, pool, and ATAs required")
	}
	if len(p.TickArrays) == 0 {
		return nil, fmt.Errorf("byrealclmm: at least one tick array required")
	}
	return &ExactIn{p: p, prog: prog, side: s}, nil
}

func (e *ExactIn) VenueID() string { return "byreal_clmm" }
func (e *ExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.InputMint, e.p.UserInputATA)
}
func (e *ExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.OutputMint, e.p.UserOutputATA)
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, swapV2DataLen)
	copy(data[:8], discSwapV2[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)
	data[40] = 1 // is_base_input = true

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
