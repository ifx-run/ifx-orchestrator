// Package titan wraps Titan Exchange router ExactIn swap_route as an ExactInHop.
//
// Use this to sandwich a Titan aggregator leg with our own venues:
//
//	.Hop(ownVenue).Hop(titanHop)
//	.Hop(titanHop).Hop(ownVenue)
//
// Authoritative:
//   - Program: T1TANpTeScyeqVzzgNViGDNrkQ6qHz9KrSBS4aNXvGT
//   - IDL / CPI: https://github.com/Titan-Pathfinder/titan-v2-cpi-example
//   - Docs: https://titan-exchange.gitbook.io/titan/developer-doc/swap-api
//
// Caller quotes Titan (Gateway or Direct) and passes the T1TAN swap instruction
// from route.instructions (use SelectSwap). Prefer titanSwapVersion=3,
// outputWsol=true when chaining, and transactionTemplate so Titan sizes the
// route next to our ifx ixs. ATA / SOL wrap belong to Features.
package titan

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// ProgramID is the Titan Exchange router on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58("T1TANpTeScyeqVzzgNViGDNrkQ6qHz9KrSBS4aNXvGT")

// Anchor discriminators (sha256("global:<name>")[:8]); v2 matches official CPI example.
var (
	discSwapRoute   = [8]byte{86, 183, 163, 144, 0, 50, 173, 28}
	discSwapRouteV2 = [8]byte{249, 91, 84, 33, 69, 22, 0, 135}
	discSwapRouteV3 = [8]byte{108, 81, 117, 138, 114, 206, 238, 14}
)

const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

const minDataLen = 8 + 8 + 8 // disc + amount + minimum_amount_out

// Params configures a Titan ExactIn hop from a prebuilt swap_route instruction.
type Params struct {
	InputMint     solana.PublicKey
	OutputMint    solana.PublicKey
	UserOutputATA solana.PublicKey
	Swap          solana.Instruction
}

// ExactIn is a Titan router ExactInHop.
type ExactIn struct {
	p        Params
	data     []byte
	accounts []*solana.AccountMeta
	prog     solana.PublicKey
}

// NewExactIn validates a T1TAN swap_route / swap_route_v2 ix.
// swap_route_v3 is accepted when the 8-byte sighash matches; amount/min_out are
// assumed to stay at offsets 8/16 like v1/v2 (official CPI IDL only documents v2).
func NewExactIn(p Params) (*ExactIn, error) {
	if p.Swap == nil {
		return nil, fmt.Errorf("titan: swap instruction is required")
	}
	if p.InputMint.IsZero() || p.OutputMint.IsZero() {
		return nil, fmt.Errorf("titan: input and output mints are required")
	}
	if p.UserOutputATA.IsZero() {
		return nil, fmt.Errorf("titan: UserOutputATA is required")
	}
	prog := p.Swap.ProgramID()
	if prog != ProgramID {
		return nil, fmt.Errorf("titan: program %s is not T1TAN", prog)
	}
	raw, err := p.Swap.Data()
	if err != nil {
		return nil, fmt.Errorf("titan: ix data: %w", err)
	}
	if err := checkExactIn(raw); err != nil {
		return nil, err
	}
	return &ExactIn{
		p:        p,
		data:     append([]byte(nil), raw...),
		accounts: cloneMetas(p.Swap.Accounts()),
		prog:     prog,
	}, nil
}

// SelectSwap returns the first T1TAN swap_route* instruction in a quote's ix list.
func SelectSwap(ixs []solana.Instruction) (solana.Instruction, error) {
	for _, ix := range ixs {
		if ix == nil || ix.ProgramID() != ProgramID {
			continue
		}
		raw, err := ix.Data()
		if err != nil {
			continue
		}
		if err := checkExactIn(raw); err != nil {
			continue
		}
		return ix, nil
	}
	return nil, fmt.Errorf("titan: no swap_route instruction in route")
}

func checkExactIn(data []byte) error {
	if len(data) < minDataLen {
		return fmt.Errorf("titan: instruction data too short: %d", len(data))
	}
	var disc [8]byte
	copy(disc[:], data[:8])
	switch disc {
	case discSwapRoute, discSwapRouteV2, discSwapRouteV3:
		return nil
	default:
		return fmt.Errorf("titan: unknown instruction discriminator %x", disc[:])
	}
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

func (e *ExactIn) VenueID() string                        { return "titan" }
func (e *ExactIn) InputMint() solana.PublicKey            { return e.p.InputMint }
func (e *ExactIn) OutputMint() solana.PublicKey           { return e.p.OutputMint }
func (e *ExactIn) OutputMeasureAccount() solana.PublicKey { return e.p.UserOutputATA }

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := append([]byte(nil), e.data...)
	binary.LittleEndian.PutUint64(data[AmountInOffset:AmountInOffset+8], 0)
	min := hop.PatchSite{Offset: MinOutOffset}
	return hop.HopBlueprint{
		Template: solana.NewInstruction(e.prog, cloneMetas(e.accounts), data),
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &min,
	}, nil
}

var _ hop.ExactInHop = (*ExactIn)(nil)
