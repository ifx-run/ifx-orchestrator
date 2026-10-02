package feature

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
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
