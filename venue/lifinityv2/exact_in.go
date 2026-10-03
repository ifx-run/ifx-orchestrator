// Package lifinityv2 implements ExactInHop for Lifinity AMM V2 swap (ExactIn).
//
// Authoritative: @lifinity/sdk-v2 IDL lifinity_amm_v2 instruction "swap"
// (amountIn + minimumAmountOut) and 13 accounts. Program ID from sdk-v2 state.js.
// https://www.npmjs.com/package/@lifinity/sdk-v2
package lifinityv2

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Lifinity AMM V2 on Solana mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("2wT8Yq49kHgDzXuPxZSaeLaH1qbmGXtEyPy64bL7aD3c")

// swap discriminator sha256("global:swap")[:8]
var discSwap = [8]byte{248, 198, 158, 145, 225, 117, 135, 200}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

// Pool account offsets include 8-byte Anchor discriminator.
const (
	offTokenAAccount     = 158
	offTokenBAccount     = 190
	offPoolMint          = 222
	offTokenAMint        = 254
	offTokenBMint        = 286
	offFeeAccount        = 318
	offOracleMainAccount = 350
	offOracleSubAccount  = 382
	offOraclePcAccount   = 414
)

// PoolState holds AMM fields needed to build swap accounts.
type PoolState struct {
	TokenAAccount     solana.PublicKey
	TokenBAccount     solana.PublicKey
	PoolMint          solana.PublicKey
	TokenAMint        solana.PublicKey
	TokenBMint        solana.PublicKey
	FeeAccount        solana.PublicKey
	OracleMainAccount solana.PublicKey
	OracleSubAccount  solana.PublicKey
	OraclePcAccount   solana.PublicKey
}

// DecodePoolState parses a Lifinity V2 AMM account (Anchor layout from SDK IDL).
func DecodePoolState(data []byte) (PoolState, error) {
	if len(data) < offOraclePcAccount+32 {
		return PoolState{}, fmt.Errorf("lifinity v2 amm data too short: %d", len(data))
	}
	readPK := func(off int) solana.PublicKey {
		return solana.PublicKeyFromBytes(data[off : off+32])
	}
	return PoolState{
		TokenAAccount:     readPK(offTokenAAccount),
		TokenBAccount:     readPK(offTokenBAccount),
		PoolMint:          readPK(offPoolMint),
		TokenAMint:        readPK(offTokenAMint),
		TokenBMint:        readPK(offTokenBMint),
		FeeAccount:        readPK(offFeeAccount),
		OracleMainAccount: readPK(offOracleMainAccount),
		OracleSubAccount:  readPK(offOracleSubAccount),
		OraclePcAccount:   readPK(offOraclePcAccount),
	}, nil
}

// Authority is PDA [amm] on the Lifinity program (sdk-v2 getProgramAuthority).
func Authority(programID, amm solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{amm.Bytes()}, programID)
	return pda, err
}

type swapSide struct {
	swapSource, swapDest solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint solana.PublicKey) (swapSide, error) {
	switch {
	case inputMint.Equals(p.TokenAMint) && outputMint.Equals(p.TokenBMint):
		return swapSide{swapSource: p.TokenAAccount, swapDest: p.TokenBAccount}, nil
	case inputMint.Equals(p.TokenBMint) && outputMint.Equals(p.TokenAMint):
		return swapSide{swapSource: p.TokenBAccount, swapDest: p.TokenAAccount}, nil
	default:
		return swapSide{}, fmt.Errorf("lifinity v2 pool mints %s/%s do not match swap %s→%s",
			p.TokenAMint, p.TokenBMint, inputMint, outputMint)
	}
}

// Params configures one ExactIn Lifinity V2 hop.
type Params struct {
	ProgramID     solana.PublicKey
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolState
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	TokenProgram  solana.PublicKey // zero => Tokenkeg (SDK default)
}

// ExactIn is a Lifinity V2 ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
	auth solana.PublicKey
	side swapSide
}

// NewExactIn validates pool direction and returns an ExactInHop.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("lifinityv2: user, pool, and ATAs are required")
	}
	s, err := p.Pool.side(p.InputMint, p.OutputMint)
	if err != nil {
		return nil, err
	}
	auth, err := Authority(prog, p.PoolID)
	if err != nil {
		return nil, err
	}
	return &ExactIn{p: p, prog: prog, auth: auth, side: s}, nil
}

func (e *ExactIn) VenueID() string { return "lifinity_v2" }
func (e *ExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.InputMint, e.p.UserInputATA)
}
func (e *ExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.OutputMint, e.p.UserOutputATA)
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	tp := e.p.TokenProgram
	if tp.IsZero() {
		tp = solana.TokenProgramID
	}
	data := make([]byte, 24)
	copy(data[:8], discSwap[:])
	ix := solana.NewInstruction(e.prog, []*solana.AccountMeta{
		{PublicKey: e.auth, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.swapSource, IsWritable: true, IsSigner: false},
		{PublicKey: e.side.swapDest, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.PoolMint, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.FeeAccount, IsWritable: true, IsSigner: false},
		{PublicKey: tp, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.OracleMainAccount, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.OracleSubAccount, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.OraclePcAccount, IsWritable: false, IsSigner: false},
	}, data)
	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
