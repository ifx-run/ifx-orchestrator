package feature

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// AtaPolicy mirrors Exact TokenAccountLifecycleStrategy.
type AtaPolicy uint8

const (
	AtaUseOnly AtaPolicy = iota
	AtaUseAndClose
	AtaCreateAndCloseCreated
	AtaCreateAndCloseAll
	AtaCreateOnly
)

// DefaultTokenProgram is classic SPL Token.
var DefaultTokenProgram = solana.TokenProgramID

// Ata emits ATA create/close instructions according to Policy.
type Ata struct {
	Base
	Policy AtaPolicy
	Payer  solana.PublicKey // zero => Ctx.User

	created []bool // nodes created in BeforeRoute
}

// WithAta returns an Ata Feature.
func WithAta(p AtaPolicy) *Ata { return &Ata{Policy: p} }

func (a *Ata) BeforeRoute(cx *Ctx) error {
	switch a.Policy {
	case AtaUseOnly, AtaUseAndClose:
		a.publishPeak(cx)
		return nil
	case AtaCreateOnly, AtaCreateAndCloseCreated, AtaCreateAndCloseAll:
		// continue
	default:
		return nil
	}

	payer := a.Payer
	if payer.IsZero() {
		payer = cx.User
	}
	a.created = make([]bool, len(cx.Route.Nodes))
	for i, n := range cx.Route.Nodes {
		if n.Exists || n.TokenAccount.IsZero() || n.Mint.IsZero() {
			continue
		}
		tp := n.TokenProgram
		if tp.IsZero() {
			tp = DefaultTokenProgram
		}
		ix, err := createATAIdempotent(payer, cx.User, n.Mint, tp, n.TokenAccount)
		if err != nil {
			return fmt.Errorf("ata create node[%d]: %w", i, err)
		}
		cx.Emit(ix)
		a.created[i] = true
	}

	a.publishPeak(cx)
	return nil
}

func (a *Ata) publishPeak(cx *Ctx) {
	// Optional: callers/FlashRent may compute independently. Left as no-op here to
	// avoid an import cycle with rentpeak; FlashRent owns peak estimation.
}

func (a *Ata) AfterRoute(cx *Ctx) error {
	switch a.Policy {
	case AtaUseOnly, AtaCreateOnly:
		return nil
	case AtaUseAndClose:
		// Close pre-existing intermediates when empty — without balance checks we only
		// close non-destination nodes that have a token account (caller responsibility).
		return a.closeNodes(cx, func(i int, n hop.RouteNode) bool {
			return i < len(cx.Route.Nodes)-1 && !n.TokenAccount.IsZero()
		})
	case AtaCreateAndCloseCreated:
		return a.closeNodes(cx, func(i int, _ hop.RouteNode) bool {
			return a.created != nil && i < len(a.created) && a.created[i] && i < len(cx.Route.Nodes)-1
		})
	case AtaCreateAndCloseAll:
		return a.closeNodes(cx, func(i int, n hop.RouteNode) bool {
			if n.TokenAccount.IsZero() {
				return false
			}
			if a.created != nil && i < len(a.created) && a.created[i] {
				return true
			}
			return n.Exists // close pre-existing too
		})
	default:
		return nil
	}
}

func (a *Ata) closeNodes(cx *Ctx, want func(int, hop.RouteNode) bool) error {
	for i, n := range cx.Route.Nodes {
		if !want(i, n) {
			continue
		}
		tp := n.TokenProgram
		if tp.IsZero() {
			tp = DefaultTokenProgram
		}
		ix := token.NewCloseAccountInstruction(
			n.TokenAccount,
			cx.User, // destination for rent
			cx.User, // owner
			[]solana.PublicKey{},
		).Build()
		// token.Instruction needs to target the right program for Token-2022;
		// classic Tokenkeg is the default Build() program.
		_ = tp
		cx.Emit(ix)
	}
	return nil
}

func createATAIdempotent(payer, owner, mint, tokenProgram, ata solana.PublicKey) (solana.Instruction, error) {
	// Prefer caller-supplied ATA address; verify it matches derivation when classic token.
	if tokenProgram.Equals(solana.TokenProgramID) || tokenProgram.IsZero() {
		derived, _, err := solana.FindAssociatedTokenAddress(owner, mint)
		if err != nil {
			return nil, err
		}
		if !ata.IsZero() && !ata.Equals(derived) {
			return nil, fmt.Errorf("token account %s != ATA(%s,%s)=%s", ata, owner, mint, derived)
		}
		ata = derived
		tokenProgram = solana.TokenProgramID
	}
	return solana.NewInstruction(
		solana.SPLAssociatedTokenAccountProgramID,
		solana.AccountMetaSlice{
			{PublicKey: payer, IsWritable: true, IsSigner: true},
			{PublicKey: ata, IsWritable: true, IsSigner: false},
			{PublicKey: owner, IsWritable: false, IsSigner: false},
			{PublicKey: mint, IsWritable: false, IsSigner: false},
			{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
			{PublicKey: tokenProgram, IsWritable: false, IsSigner: false},
		},
		[]byte{1}, // CreateIdempotent
	), nil
}
