// Package meteoradammv2 implements ExactInHop for Meteora DAMM v2 (cp-amm) swap2 ExactIn.
//
// Authoritative: https://github.com/MeteoraAg/damm-v2/blob/main/programs/cp-amm/src/instructions/swap/ix_swap.rs (SwapCtx + swap2).
package meteoradammv2

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Meteora DAMM v2 (cp-amm) on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("cpamdpZCGKUy5JxQXB4dcpGPiikHawvSWAd6mEn1sGG")

// PoolAuthority is the mainnet pool authority PDA (seed "pool_authority").
var PoolAuthority = solana.MustPublicKeyFromBase58("HLnpSz9h2S4hiLQ43rnSD9XkcUThA7B8hQMKmDaiTLcC")

var discSwap2 = [8]byte{65, 75, 63, 76, 235, 91, 91, 136}

const swapModeExactIn uint8 = 0

// AmountInOffset / MinOutOffset in swap2 data.
const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const poolFeesSize = 160

// PoolState holds fields needed for ExactIn account metas.
type PoolState struct {
	MintA  solana.PublicKey
	MintB  solana.PublicKey
	VaultA solana.PublicKey
	VaultB solana.PublicKey
}

// DecodePoolState parses a DAMM v2 pool account.
func DecodePoolState(data []byte) (PoolState, error) {
	const minLen = 8 + poolFeesSize + 32*4
	if len(data) < minLen {
		return PoolState{}, fmt.Errorf("damm v2 pool data too short: %d", len(data))
	}
	body := data[8:]
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(body[off : off+32])
	}
	return PoolState{
		MintA:  readPK(poolFeesSize),
		MintB:  readPK(poolFeesSize + 32),
		VaultA: readPK(poolFeesSize + 64),
		VaultB: readPK(poolFeesSize + 96),
	}, nil
}

func eventAuthority(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("__event_authority")}, programID)
	return pda, err
}

func (p PoolState) validateDirection(inputMint, outputMint solana.PublicKey) error {
	ok := (inputMint.Equals(p.MintA) && outputMint.Equals(p.MintB)) ||
		(inputMint.Equals(p.MintB) && outputMint.Equals(p.MintA))
	if !ok {
		return fmt.Errorf("damm v2 pool mints %s/%s do not match swap %s→%s",
			p.MintA, p.MintB, inputMint, outputMint)
	}
	return nil
}

// Params configures one ExactIn DAMM v2 hop.
type Params struct {
	ProgramID      solana.PublicKey // zero => ProgramID
	User           solana.PublicKey
	PoolID         solana.PublicKey
	Pool           PoolState
	InputMint      solana.PublicKey
	OutputMint     solana.PublicKey
	UserInputATA   solana.PublicKey
	UserOutputATA  solana.PublicKey
	TokenProgramA  solana.PublicKey // pool mint A token program
	TokenProgramB  solana.PublicKey // pool mint B token program
}

// ExactIn is a Meteora DAMM v2 ExactInHop.
type ExactIn struct {
	p         Params
	prog      solana.PublicKey
	eventAuth solana.PublicKey
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
		return nil, fmt.Errorf("meteoradammv2: user, pool, and ATAs are required")
	}
	if p.TokenProgramA.IsZero() || p.TokenProgramB.IsZero() {
		return nil, fmt.Errorf("meteoradammv2: token programs are required")
	}
	ea, err := eventAuthority(prog)
	if err != nil {
		return nil, err
	}
	return &ExactIn{p: p, prog: prog, eventAuth: ea}, nil
}

func (e *ExactIn) VenueID() string                        { return "meteora_damm_v2" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 25)
	copy(data[:8], discSwap2[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)
	data[24] = swapModeExactIn

	referral := e.prog // unused optional account → program id

	ix := solana.NewInstruction(e.prog, solana.AccountMetaSlice{
		{PublicKey: PoolAuthority, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.VaultA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.VaultB, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.MintA, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.MintB, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.TokenProgramA, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.TokenProgramB, IsWritable: false, IsSigner: false},
		{PublicKey: referral, IsWritable: true, IsSigner: false},
		{PublicKey: e.eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: e.prog, IsWritable: false, IsSigner: false},
	}, data)

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
