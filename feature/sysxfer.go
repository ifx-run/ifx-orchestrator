package feature

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/ifx-run/ifx/go-sdk/codec"
	"github.com/ifx-run/ifx/go-sdk/patch"
	"github.com/ifx-run/ifx/go-sdk/patchedcpi"
	"github.com/ifx-run/ifx/go-sdk/structuredcpi"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// EmitPatchedSystemTransfer appends an ifx CPI that patches System Program transfer lamports
// from a Frame scratch binding.
func EmitPatchedSystemTransfer(cx *Ctx, from, to solana.PublicKey, amount typed.ScratchValue) error {
	if cx == nil || cx.Scratch == nil {
		return fmt.Errorf("feature: scratch is required for patched transfer")
	}
	built, err := structuredcpi.StructuredCpi(
		system.NewTransferInstruction(0, from, to).Build(),
		structuredcpi.StructuredCpiPatch.SystemTransfer(structuredcpi.AsFrameValue(amount)),
	)
	if err != nil {
		return err
	}
	xfer, err := built.Build(nil)
	if err != nil {
		return err
	}
	cpi, err := cx.Scratch.IxCpi(xfer)
	if err != nil {
		return err
	}
	cx.Emit(cpi)
	return nil
}

// EmitFixedSystemTransfer appends a plain System Program transfer (literal lamports).
func EmitFixedSystemTransfer(cx *Ctx, from, to solana.PublicKey, lamports uint64) {
	cx.Emit(system.NewTransferInstruction(lamports, from, to).Build())
}

// EmitFixedTokenTransfer appends a plain SPL Token transfer (literal amount).
func EmitFixedTokenTransfer(cx *Ctx, source, destination, owner solana.PublicKey, amount uint64) {
	cx.Emit(token.NewTransferInstruction(amount, source, destination, owner, []solana.PublicKey{}).Build())
}

// EmitPatchedTokenTransfer appends an ifx CPI that patches classic SPL Token transfer amount.
func EmitPatchedTokenTransfer(
	cx *Ctx,
	source, destination, owner solana.PublicKey,
	amount typed.ScratchValue,
) error {
	if cx == nil || cx.Scratch == nil {
		return fmt.Errorf("feature: scratch is required for patched token transfer")
	}
	built, err := structuredcpi.StructuredCpi(
		token.NewTransferInstruction(0, source, destination, owner, []solana.PublicKey{}).Build(),
		structuredcpi.StructuredCpiPatch.TokenTransfer(structuredcpi.AsFrameValue(amount)),
	)
	if err != nil {
		return err
	}
	xfer, err := built.Build(nil)
	if err != nil {
		return err
	}
	cpi, err := cx.Scratch.IxCpi(xfer)
	if err != nil {
		return err
	}
	cx.Emit(cpi)
	return nil
}

// EmitRawPatchedCPI appends an ifx raw_cpi with the given data patches.
func EmitRawPatchedCPI(cx *Ctx, template solana.Instruction, patches ...codec.RawCpiPatch) error {
	if cx == nil || cx.Scratch == nil {
		return fmt.Errorf("feature: scratch is required for raw patched cpi")
	}
	built, err := patchedcpi.RawCpi(template, patches...).Build(nil)
	if err != nil {
		return err
	}
	cpi, err := cx.Scratch.IxCpi(built.WireBuild())
	if err != nil {
		return err
	}
	cx.Emit(cpi)
	return nil
}

// RawCpiU64Patch is a convenience for patch.RawCpiPatch.
func RawCpiU64Patch(offset uint16, v typed.ScratchValue) codec.RawCpiPatch {
	return patch.RawCpiPatch(offset, v)
}
