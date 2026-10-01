// Package raydiumcpmm implements ExactInHop for Raydium CPMM swap_base_input.
package raydiumcpmm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is the Raydium CPMM program on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("CPMMoo8L3F4NbTegBCKVNunggL7H1ZpdTHKxQB5qKP1C")

// swap_base_input Anchor discriminator.
var discSwapBaseInput = [8]byte{0x8f, 0xbe, 0x5a, 0xda, 0xc4, 0x1e, 0x33, 0xde}

// Patch offsets (bytes from start of ix data).
const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const authSeed = "vault_and_lp_mint_auth_seed"

// PoolState mirrors Raydium CPMM pool account fields needed for swap accounts.
type PoolState struct {
	ConfigID      solana.PublicKey
	VaultA        solana.PublicKey
	VaultB        solana.PublicKey
	MintA         solana.PublicKey
	MintB         solana.PublicKey
	MintProgramA  solana.PublicKey
	MintProgramB  solana.PublicKey
	ObservationID solana.PublicKey
}

// DecodePoolState parses a CPMM pool account (skips 8-byte discriminator).
func DecodePoolState(data []byte) (PoolState, error) {
	const minLen = 8 + 32*9 + 3
	if len(data) < minLen {
		return PoolState{}, fmt.Errorf("cpmm pool data too short: %d", len(data))
	}
	body := data[8:]
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(body[off : off+32])
	}
	return PoolState{
		ConfigID:      readPK(0),
		VaultA:        readPK(64),
		VaultB:        readPK(96),
		MintA:         readPK(160),
		MintB:         readPK(192),
		MintProgramA:  readPK(224),
		MintProgramB:  readPK(256),
		ObservationID: readPK(288),
	}, nil
}

// Authority derives the CPMM vault authority PDA.
func Authority(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte(authSeed)}, programID)
	return pda, err
}

type side struct {
	InputMint, OutputMint         solana.PublicKey
	InputVault, OutputVault       solana.PublicKey
	InputProgram, OutputProgram   solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint solana.PublicKey) (side, error) {
	switch {
	case inputMint.Equals(p.MintA) && outputMint.Equals(p.MintB):
		return side{
			InputMint: p.MintA, OutputMint: p.MintB,
			InputVault: p.VaultA, OutputVault: p.VaultB,
			InputProgram: p.MintProgramA, OutputProgram: p.MintProgramB,
		}, nil
	case inputMint.Equals(p.MintB) && outputMint.Equals(p.MintA):
		return side{
			InputMint: p.MintB, OutputMint: p.MintA,
			InputVault: p.VaultB, OutputVault: p.VaultA,
			InputProgram: p.MintProgramB, OutputProgram: p.MintProgramA,
		}, nil
	default:
		return side{}, fmt.Errorf("cpmm pool mints %s/%s do not match swap %s→%s",
			p.MintA, p.MintB, inputMint, outputMint)
	}
}

// Params configures one ExactIn CPMM hop.
type Params struct {
	ProgramID     solana.PublicKey // zero => ProgramID
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
}

// ExactIn is a Raydium CPMM ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
	auth solana.PublicKey
	side side
}

// NewExactIn validates pool direction and returns an ExactInHop.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	auth, err := Authority(prog)
	if err != nil {
		return nil, err
	}
	s, err := p.Pool.side(p.InputMint, p.OutputMint)
	if err != nil {
		return nil, err
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("raydiumcpmm: user, pool, and ATAs are required")
	}
	return &ExactIn{p: p, prog: prog, auth: auth, side: s}, nil
}

func (e *ExactIn) VenueID() string                        { return "raydium_cpmm" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 24)
	copy(data[:8], discSwapBaseInput[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)

	ix := solana.NewInstruction(e.prog, solana.AccountMetaSlice{
		{PublicKey: e.p.User, IsSigner: true, IsWritable: false},
		{PublicKey: e.auth, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.ConfigID, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.InputVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.OutputVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.InputProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.side.OutputProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.side.InputMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.side.OutputMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.ObservationID, IsWritable: true, IsSigner: false},
	}, data)

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

// Ensure ExactIn implements hop.ExactInHop.
var _ hop.ExactInHop = (*ExactIn)(nil)
