package feehook

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Settler emits the on-chain fee collection instruction(s).
// FeeHook computes the fee amount; Settler decides how it is collected
// (System transfer, Token transfer, or a custom fee program).
type Settler interface {
	// EmitFixed collects a compile-time-known lamports/token amount.
	EmitFixed(cx *feature.Ctx, amount uint64) error
	// EmitPatched collects an amount bound in Frame scratch (bps / measured delta).
	EmitPatched(cx *feature.Ctx, amount typed.ScratchValue) error
}

// SystemTransferSettler is the default SOL fee collector.
type SystemTransferSettler struct {
	From solana.PublicKey // zero => Ctx.User
	To   solana.PublicKey
}

func (s SystemTransferSettler) from(cx *feature.Ctx) solana.PublicKey {
	if s.From.IsZero() {
		return cx.User
	}
	return s.From
}

func (s SystemTransferSettler) EmitFixed(cx *feature.Ctx, amount uint64) error {
	if s.To.IsZero() {
		return fmt.Errorf("feehook: SystemTransferSettler.To is required")
	}
	feature.EmitFixedSystemTransfer(cx, s.from(cx), s.To, amount)
	return nil
}

func (s SystemTransferSettler) EmitPatched(cx *feature.Ctx, amount typed.ScratchValue) error {
	if s.To.IsZero() {
		return fmt.Errorf("feehook: SystemTransferSettler.To is required")
	}
	return feature.EmitPatchedSystemTransfer(cx, s.from(cx), s.To, amount)
}

// TokenTransferSettler is the default SPL token fee collector.
type TokenTransferSettler struct {
	Source      solana.PublicKey
	Destination solana.PublicKey
	Owner       solana.PublicKey // zero => Ctx.User
}

func (s TokenTransferSettler) owner(cx *feature.Ctx) solana.PublicKey {
	if s.Owner.IsZero() {
		return cx.User
	}
	return s.Owner
}

func (s TokenTransferSettler) EmitFixed(cx *feature.Ctx, amount uint64) error {
	if s.Source.IsZero() || s.Destination.IsZero() {
		return fmt.Errorf("feehook: TokenTransferSettler Source and Destination are required")
	}
	lb := cx.Scratch.LetBuilder()
	bind, err := lb.LetConstU64(amount)
	if err != nil {
		return err
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	return feature.EmitPatchedTokenTransfer(cx, s.Source, s.Destination, s.owner(cx), bind)
}

func (s TokenTransferSettler) EmitPatched(cx *feature.Ctx, amount typed.ScratchValue) error {
	if s.Source.IsZero() || s.Destination.IsZero() {
		return fmt.Errorf("feehook: TokenTransferSettler Source and Destination are required")
	}
	return feature.EmitPatchedTokenTransfer(cx, s.Source, s.Destination, s.owner(cx), amount)
}

// CustomSettler calls a user fee program via ifx raw_cpi, patching a u64 amount into Template data.
//
// Template should have the amount field zeroed (or any placeholder); AmountOffset is the byte
// offset of that u64 LE field — same shape as hop.PatchSite / Exact CustomInstruction.
type CustomSettler struct {
	Template     solana.Instruction
	AmountOffset uint16
}

// CustomIx returns a Settler that patches amount into a custom fee-program instruction.
func CustomIx(template solana.Instruction, amountOffset uint16) Settler {
	return CustomSettler{Template: template, AmountOffset: amountOffset}
}

func (s CustomSettler) EmitFixed(cx *feature.Ctx, amount uint64) error {
	lb := cx.Scratch.LetBuilder()
	bind, err := lb.LetConstU64(amount)
	if err != nil {
		return err
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	return s.EmitPatched(cx, bind)
}

func (s CustomSettler) EmitPatched(cx *feature.Ctx, amount typed.ScratchValue) error {
	if s.Template == nil {
		return fmt.Errorf("feehook: CustomSettler.Template is required")
	}
	return feature.EmitRawPatchedCPI(cx, s.Template, feature.RawCpiU64Patch(s.AmountOffset, amount))
}

// SettlerFunc adapts closures to Settler (full control, including multi-ix fee programs).
type SettlerFunc struct {
	Fixed   func(cx *feature.Ctx, amount uint64) error
	Patched func(cx *feature.Ctx, amount typed.ScratchValue) error
}

func (s SettlerFunc) EmitFixed(cx *feature.Ctx, amount uint64) error {
	if s.Fixed == nil {
		// Fall back: bind const and use Patched.
		if s.Patched == nil {
			return fmt.Errorf("feehook: SettlerFunc has no Fixed or Patched handler")
		}
		lb := cx.Scratch.LetBuilder()
		bind, err := lb.LetConstU64(amount)
		if err != nil {
			return err
		}
		ix, err := lb.BuildIx()
		if err != nil {
			return err
		}
		cx.Emit(ix)
		return s.Patched(cx, bind)
	}
	return s.Fixed(cx, amount)
}

func (s SettlerFunc) EmitPatched(cx *feature.Ctx, amount typed.ScratchValue) error {
	if s.Patched == nil {
		return fmt.Errorf("feehook: SettlerFunc.Patched is required")
	}
	return s.Patched(cx, amount)
}
