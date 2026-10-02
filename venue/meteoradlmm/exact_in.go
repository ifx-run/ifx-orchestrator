// Package meteoradlmm implements ExactInHop for Meteora DLMM swap (ExactIn).
//
// Authoritative: https://github.com/MeteoraAg/dlmm-sdk/blob/main/idls/dlmm.json (instruction "swap").
package meteoradlmm

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Meteora DLMM on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo")

var discSwap = [8]byte{0xf8, 0xc6, 0x9e, 0x91, 0xe1, 0x75, 0x87, 0xc8}

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const fixedAccountCount = 15

// PoolState holds DLMM pair vaults and mints.
type PoolState struct {
	TokenXMint solana.PublicKey
	TokenYMint solana.PublicKey
	ReserveX   solana.PublicKey
	ReserveY   solana.PublicKey
	Oracle     solana.PublicKey
}

func (p PoolState) side(inputMint, outputMint solana.PublicKey) error {
	ok := (inputMint.Equals(p.TokenXMint) && outputMint.Equals(p.TokenYMint)) ||
		(inputMint.Equals(p.TokenYMint) && outputMint.Equals(p.TokenXMint))
	if !ok {
		return fmt.Errorf("dlmm mints %s/%s do not match %s→%s", p.TokenXMint, p.TokenYMint, inputMint, outputMint)
	}
	return nil
}

// Params configures one DLMM ExactIn hop.
type Params struct {
	ProgramID              solana.PublicKey
	User                   solana.PublicKey
	LbPair                 solana.PublicKey
	Pool                   PoolState
	InputMint              solana.PublicKey
	OutputMint             solana.PublicKey
	UserInputATA           solana.PublicKey
	UserOutputATA          solana.PublicKey
	TokenXProgram          solana.PublicKey
	TokenYProgram          solana.PublicKey
	BinArrayBitmapExtension solana.PublicKey // zero => program id (none)
	HostFeeIn              solana.PublicKey   // zero => program id (none)
	BinArrays              []solana.PublicKey // remaining bin arrays
}

// ExactIn is a Meteora DLMM ExactInHop.
type ExactIn struct {
	p    Params
	prog solana.PublicKey
}

// NewExactIn validates pool direction and bin arrays.
func NewExactIn(p Params) (*ExactIn, error) {
	prog := p.ProgramID
	if prog.IsZero() {
		prog = ProgramID
	}
	if err := p.Pool.side(p.InputMint, p.OutputMint); err != nil {
		return nil, err
	}
	if p.User.IsZero() || p.LbPair.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("meteoradlmm: user, pair, and ATAs required")
	}
	if len(p.BinArrays) == 0 {
		return nil, fmt.Errorf("meteoradlmm: at least one bin array required")
	}
	if p.TokenXProgram.IsZero() || p.TokenYProgram.IsZero() {
		return nil, fmt.Errorf("meteoradlmm: token programs required")
	}
	return &ExactIn{p: p, prog: prog}, nil
}

func (e *ExactIn) VenueID() string                        { return "meteora_dlmm" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func eventAuthority(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("__event_authority")}, programID)
	return pda, err
}

func optionalWritableMeta(account, placeholder solana.PublicKey) *solana.AccountMeta {
	w := !account.Equals(placeholder)
	return &solana.AccountMeta{PublicKey: account, IsWritable: w, IsSigner: false}
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 24)
	copy(data[:8], discSwap[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)

	bitmapExt := e.p.BinArrayBitmapExtension
	if bitmapExt.IsZero() {
		bitmapExt = e.prog
	}
	hostFee := e.p.HostFeeIn
	if hostFee.IsZero() {
		hostFee = e.prog
	}
	eventAuth, err := eventAuthority(e.prog)
	if err != nil {
		return hop.HopBlueprint{}, err
	}

	metas := solana.AccountMetaSlice{
		{PublicKey: e.p.LbPair, IsWritable: true, IsSigner: false},
		optionalWritableMeta(bitmapExt, e.prog),
		{PublicKey: e.p.Pool.ReserveX, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.ReserveY, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.TokenXMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.TokenYMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.Oracle, IsWritable: true, IsSigner: false},
		optionalWritableMeta(hostFee, e.prog),
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.TokenXProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.TokenYProgram, IsWritable: false, IsSigner: false},
		{PublicKey: eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: e.prog, IsWritable: false, IsSigner: false},
	}
	for _, ba := range e.p.BinArrays {
		metas = append(metas, &solana.AccountMeta{PublicKey: ba, IsWritable: true, IsSigner: false})
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
