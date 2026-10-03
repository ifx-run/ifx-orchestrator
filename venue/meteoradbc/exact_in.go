// Package meteoradbc implements ExactInHop for Meteora Dynamic Bonding Curve swap2 ExactIn.
//
// Authoritative: https://github.com/MeteoraAg/dynamic-bonding-curve/blob/main/scripts/idl/release_0.1.6.json (instruction "swap2").
package meteoradbc

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Meteora DBC on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("dbcij3LWUppWqq96dh6gJWwBifmcGfLSB5D4DuSMaqN")

// PoolAuthority is the fixed DBC pool authority.
var PoolAuthority = solana.MustPublicKeyFromBase58("FhVo3mqL8PW5pH5U2CN4XE33DokiyZnUwuGpH2hmHLuM")

// swap2 discriminator = sha256("global:swap2")[:8]
var discSwap2 = [8]byte{65, 75, 63, 76, 235, 91, 91, 136}

const swapModeExactIn uint8 = 0

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const accountCount = 15

// PoolMeta holds DBC virtual-pool routing fields.
type PoolMeta struct {
	ConfigID          solana.PublicKey
	BaseMint          solana.PublicKey
	QuoteMint         solana.PublicKey
	BaseVault         solana.PublicKey
	QuoteVault        solana.PublicKey
	TokenBaseProgram  solana.PublicKey // zero => Tokenkeg
	TokenQuoteProgram solana.PublicKey // zero => Tokenkeg
}

// Params configures one ExactIn DBC hop (either direction).
type Params struct {
	ProgramID     solana.PublicKey // zero => ProgramID
	User          solana.PublicKey
	PoolID        solana.PublicKey
	Pool          PoolMeta
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserInputATA  solana.PublicKey
	UserOutputATA solana.PublicKey
	// ReferralTokenAccount optional; zero => program id placeholder (no referral).
	ReferralTokenAccount solana.PublicKey
	// RemainingAccounts e.g. SysvarInstructions when rate-limiter fees require it.
	RemainingAccounts []solana.PublicKey
}

// ExactIn is a Meteora DBC ExactInHop.
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
	if p.User.IsZero() || p.PoolID.IsZero() || p.UserInputATA.IsZero() || p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("meteoradbc: user, pool, and ATAs required")
	}
	if p.Pool.ConfigID.IsZero() || p.Pool.BaseMint.IsZero() || p.Pool.QuoteMint.IsZero() {
		return nil, fmt.Errorf("meteoradbc: config and mints required")
	}
	ok := (p.InputMint.Equals(p.Pool.BaseMint) && p.OutputMint.Equals(p.Pool.QuoteMint)) ||
		(p.InputMint.Equals(p.Pool.QuoteMint) && p.OutputMint.Equals(p.Pool.BaseMint))
	if !ok {
		return nil, fmt.Errorf("meteoradbc: pool mints %s/%s do not match swap %s→%s",
			p.Pool.BaseMint, p.Pool.QuoteMint, p.InputMint, p.OutputMint)
	}
	if p.Pool.TokenBaseProgram.IsZero() {
		p.Pool.TokenBaseProgram = solana.TokenProgramID
	}
	if p.Pool.TokenQuoteProgram.IsZero() {
		p.Pool.TokenQuoteProgram = solana.TokenProgramID
	}
	if p.ReferralTokenAccount.IsZero() {
		p.ReferralTokenAccount = prog
	}
	ea, err := eventAuthority(prog)
	if err != nil {
		return nil, err
	}
	return &ExactIn{p: p, prog: prog, eventAuth: ea}, nil
}

func eventAuthority(programID solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("__event_authority")}, programID)
	return pda, err
}

func (e *ExactIn) VenueID() string { return "meteora_dbc" }
func (e *ExactIn) Input() hop.Port {
	return hop.TokenPort(e.p.InputMint, e.p.UserInputATA)
}
func (e *ExactIn) Output() hop.Port {
	return hop.TokenPort(e.p.OutputMint, e.p.UserOutputATA)
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 25)
	copy(data[:8], discSwap2[:])
	binary.LittleEndian.PutUint64(data[8:16], 0)
	binary.LittleEndian.PutUint64(data[16:24], 0)
	data[24] = swapModeExactIn

	metas := solana.AccountMetaSlice{
		{PublicKey: PoolAuthority, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.ConfigID, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.PoolID, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserInputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.UserOutputATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.BaseVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.QuoteVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.Pool.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.QuoteMint, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: false, IsSigner: true},
		{PublicKey: e.p.Pool.TokenBaseProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.Pool.TokenQuoteProgram, IsWritable: false, IsSigner: false},
		{PublicKey: e.p.ReferralTokenAccount, IsWritable: true, IsSigner: false},
		{PublicKey: e.eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: e.prog, IsWritable: false, IsSigner: false},
	}
	for _, rem := range e.p.RemainingAccounts {
		metas = append(metas, &solana.AccountMeta{PublicKey: rem, IsWritable: false, IsSigner: false})
	}

	minOff := MinOutOffset
	return hop.HopBlueprint{
		Template: solana.NewInstruction(e.prog, metas, data),
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
