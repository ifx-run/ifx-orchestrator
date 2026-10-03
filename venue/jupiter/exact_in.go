// Package jupiter wraps Jupiter v6 ExactIn route instructions as an ExactInHop.
//
// Use this to sandwich a JUP6 aggregator leg with our own venues:
//
//	.Hop(ownVenue).Hop(jupiterHop)  // own hop, then Jupiter rest-of-path
//	.Hop(jupiterHop).Hop(ownVenue)  // Jupiter, then a custom tail
//
// Authoritative program: JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4
// IDL: https://github.com/jup-ag/instruction-parser/blob/main/src/idl/jupiter.ts
//
// Caller fetches POST /swap/v1/swap-instructions and passes only swapInstruction
// (not the full /swap transaction). Prefer wrapAndUnwrapSol=false and a concrete
// destinationTokenAccount; ATA / SOL wrap belong to Features. Do not set
// useTokenLedger — those ixs have no patchable in_amount.
package jupiter

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is Jupiter v6 on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4")

// Anchor discriminators (sha256("global:<name>")[:8]).
var (
	discRoute               = [8]byte{229, 23, 203, 151, 122, 227, 173, 42}
	discSharedAccountsRoute = [8]byte{193, 32, 155, 51, 65, 214, 156, 129}
	discRouteTokenLedger    = [8]byte{150, 86, 71, 116, 167, 93, 14, 104}
	discSharedTokenLedger   = [8]byte{230, 121, 143, 80, 119, 159, 106, 170}
	discExactOutRoute       = [8]byte{208, 51, 239, 151, 123, 43, 237, 92}
	discSharedExactOut      = [8]byte{176, 209, 105, 168, 154, 125, 69, 62}
)

// ExactIn route args end with in_amount (u64) + quoted_out_amount (u64) + slippage_bps (u16) + platform_fee_bps (u8).
const exactInTail = 8 + 8 + 2 + 1

// APIAccount is one account meta from Jupiter /swap-instructions JSON.
type APIAccount struct {
	Pubkey     string `json:"pubkey"`
	IsSigner   bool   `json:"isSigner"`
	IsWritable bool   `json:"isWritable"`
}

// APIInstruction is Jupiter Instruction JSON (swapInstruction).
type APIInstruction struct {
	ProgramID string       `json:"programId"`
	Accounts  []APIAccount `json:"accounts"`
	Data      string       `json:"data"`
}

// Params configures a Jupiter ExactIn hop from a prebuilt swap instruction.
type Params struct {
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserOutputATA solana.PublicKey
	Swap          solana.Instruction
}

// ExactIn is a Jupiter v6 ExactInHop.
type ExactIn struct {
	p        Params
	inOff    uint16
	minOff   uint16
	data     []byte
	accounts []*solana.AccountMeta
	prog     solana.PublicKey
}

// NewExactIn validates a JUP6 ExactIn route/shared_accounts_route ix.
func NewExactIn(p Params) (*ExactIn, error) {
	if p.Swap == nil {
		return nil, fmt.Errorf("jupiter: swap instruction is required")
	}
	if p.InputMint.IsZero() || p.OutputMint.IsZero() {
		return nil, fmt.Errorf("jupiter: input and output mints are required")
	}
	if p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("jupiter: UserOutputATA is required")
	}
	prog := p.Swap.ProgramID()
	if prog != ProgramID {
		return nil, fmt.Errorf("jupiter: program %s is not JUP6", prog)
	}
	raw, err := p.Swap.Data()
	if err != nil {
		return nil, fmt.Errorf("jupiter: ix data: %w", err)
	}
	inOff, minOff, err := ExactInOffsets(raw)
	if err != nil {
		return nil, err
	}
	data := append([]byte(nil), raw...)
	copyAccounts := cloneMetas(p.Swap.Accounts())
	return &ExactIn{p: p, inOff: inOff, minOff: minOff, data: data, accounts: copyAccounts, prog: prog}, nil
}

// ExactInOffsets locates in_amount / quoted_out_amount in a JUP6 ExactIn ix.
func ExactInOffsets(data []byte) (amountIn, minOut uint16, err error) {
	if len(data) < 8 {
		return 0, 0, fmt.Errorf("jupiter: instruction data too short")
	}
	var disc [8]byte
	copy(disc[:], data[:8])
	switch disc {
	case discRouteTokenLedger, discSharedTokenLedger:
		return 0, 0, fmt.Errorf("jupiter: token-ledger route has no patchable in_amount; omit useTokenLedger")
	case discExactOutRoute, discSharedExactOut:
		return 0, 0, fmt.Errorf("jupiter: ExactOut route is not supported")
	case discRoute:
		if len(data) < 8+4+exactInTail {
			return 0, 0, fmt.Errorf("jupiter: route data too short: %d", len(data))
		}
	case discSharedAccountsRoute:
		if len(data) < 8+1+4+exactInTail {
			return 0, 0, fmt.Errorf("jupiter: shared_accounts_route data too short: %d", len(data))
		}
	default:
		return 0, 0, fmt.Errorf("jupiter: unknown instruction discriminator %x", disc[:])
	}
	in := len(data) - exactInTail
	if in > 0xffff {
		return 0, 0, fmt.Errorf("jupiter: amount offset overflow")
	}
	return uint16(in), uint16(in + 8), nil
}

// InstructionFromAPI decodes a Jupiter swapInstruction JSON object.
func InstructionFromAPI(raw []byte) (solana.Instruction, error) {
	var api APIInstruction
	if err := json.Unmarshal(raw, &api); err != nil {
		return nil, fmt.Errorf("jupiter: swap instruction json: %w", err)
	}
	return InstructionFromAPIStruct(api)
}

// InstructionFromAPIStruct builds a solana instruction from Jupiter API fields.
func InstructionFromAPIStruct(api APIInstruction) (solana.Instruction, error) {
	prog, err := solana.PublicKeyFromBase58(api.ProgramID)
	if err != nil {
		return nil, fmt.Errorf("jupiter: program id: %w", err)
	}
	data, err := decodeIxData(api.Data)
	if err != nil {
		return nil, err
	}
	accs := make([]*solana.AccountMeta, 0, len(api.Accounts))
	for i, a := range api.Accounts {
		pk, err := solana.PublicKeyFromBase58(a.Pubkey)
		if err != nil {
			return nil, fmt.Errorf("jupiter: account[%d]: %w", i, err)
		}
		accs = append(accs, &solana.AccountMeta{
			PublicKey:  pk,
			IsSigner:   a.IsSigner,
			IsWritable: a.IsWritable,
		})
	}
	return solana.NewInstruction(prog, accs, data), nil
}

func decodeIxData(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("jupiter: empty instruction data")
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) >= 8 {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil && len(b) >= 8 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) >= 8 {
		return b, nil
	}
	return nil, fmt.Errorf("jupiter: instruction data is not base64")
}

func cloneMetas(in []*solana.AccountMeta) []*solana.AccountMeta {
	out := make([]*solana.AccountMeta, len(in))
	for i, a := range in {
		if a == nil {
			continue
		}
		cp := *a
		out[i] = &cp
	}
	return out
}

func (e *ExactIn) VenueID() string                        { return "jupiter_v6" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := append([]byte(nil), e.data...)
	binary.LittleEndian.PutUint64(data[int(e.inOff):int(e.inOff)+8], 0)
	min := hop.PatchSite{Offset: e.minOff}
	return hop.HopBlueprint{
		Template: solana.NewInstruction(e.prog, cloneMetas(e.accounts), data),
		AmountIn: hop.PatchSite{Offset: e.inOff},
		MinOut:   &min,
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
