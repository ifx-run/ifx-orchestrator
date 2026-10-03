// Package raydiumclmm implements ExactInHop for Raydium CLMM swap_v2 (ExactIn).
//
// Authoritative: https://github.com/raydium-io/raydium-idl/blob/master/raydium_clmm/raydium_clmm.json (instruction "swap_v2").
package raydiumclmm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Raydium CLMM on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK")

// MemoProgram is the SPL memo program required by swap_v2 (Raydium CLMM IDL).
var MemoProgram = solana.MustPublicKeyFromBase58("MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr")

// Token2022Program is the Token-2022 program account required by swap_v2.
var Token2022Program = solana.MustPublicKeyFromBase58("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")

var discSwapV2 = [8]byte{0x2b, 0x04, 0xed, 0x0b, 0x1a, 0xc9, 0x1e, 0x62}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const swapV2DataLen = 41 // disc + amount + threshold + sqrt_limit + is_base_input

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
	TokenProgram0  solana.PublicKey
	TokenProgram1  solana.PublicKey
}

// DecodePoolState parses a CLMM pool account (skips 8-byte disc).
func DecodePoolState(data []byte) (PoolState, error) {
	if len(data) < offObservationKey+32 {
		return PoolState{}, fmt.Errorf("clmm pool data too short: %d", len(data))
	}
	body := data[8:]
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(body[off : off+32])
	}
	return PoolState{
		AmmConfig:      readPK(offAmmConfig - 8),
		TokenMint0:     readPK(offTokenMint0 - 8),
		TokenMint1:     readPK(offTokenMint1 - 8),
		TokenVault0:    readPK(offTokenVault0 - 8),
		TokenVault1:    readPK(offTokenVault1 - 8),
		ObservationKey: readPK(offObservationKey - 8),
	}, nil
}

type swapSide struct {
	inputVault, outputVault     solana.PublicKey
	inputMint, outputMint       solana.PublicKey
	inputProgram, outputProgram solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint solana.PublicKey) (swapSide, error) {
	tp0, tp1 := p.TokenProgram0, p.TokenProgram1
	if tp0.IsZero() {
		tp0 = solana.TokenProgramID
	}
	if tp1.IsZero() {
		tp1 = solana.TokenProgramID
	}
	switch {
	case inputMint.Equals(p.TokenMint0) && outputMint.Equals(p.TokenMint1):
		return swapSide{p.TokenVault0, p.TokenVault1, p.TokenMint0, p.TokenMint1, tp0, tp1}, nil
	case inputMint.Equals(p.TokenMint1) && outputMint.Equals(p.TokenMint0):
		return swapSide{p.TokenVault1, p.TokenVault0, p.TokenMint1, p.TokenMint0, tp1, tp0}, nil
	default:
		return swapSide{}, fmt.Errorf("clmm mints %s/%s do not match %s→%s",
			p.TokenMint0, p.TokenMint1, inputMint, outputMint)
	}
}

// Params configures one CLMM swap_v2 ExactIn hop.
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

// ExactIn is a Raydium CLMM ExactInHop.
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
		return nil, fmt.Errorf("raydiumclmm: user, pool, and ATAs required")
	}
	if len(p.TickArrays) == 0 {
		return nil, fmt.Errorf("raydiumclmm: at least one tick array required")
	}
	return &ExactIn{p: p, prog: prog, side: s}, nil
}

func (e *ExactIn) VenueID() string { return "raydium_clmm" }
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
	// sqrt_price_limit_x64 = 0
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
	ix := solana.NewInstruction(e.prog, metas, data)
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
