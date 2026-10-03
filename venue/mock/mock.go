// Package mock provides a fake ExactInHop for framework tests.
package mock

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// DefaultAmountInOffset places amount_in as the first 8 bytes of data (no discriminator).
const DefaultAmountInOffset uint16 = 0

// DefaultMinOutOffset places min_out at bytes [8..16).
const DefaultMinOutOffset uint16 = 8

// ExactIn is a configurable mock venue hop.
type ExactIn struct {
	ID           string
	InMint       solana.PublicKey
	OutMint      solana.PublicKey
	MeasureATA   solana.PublicKey
	InputATA     solana.PublicKey
	NativeIn     bool
	NativeOut    bool
	ProgramID    solana.PublicKey
	Accounts     []*solana.AccountMeta
	AmountOffset uint16
	MinOutOffset *uint16
}

// New builds a mock hop with default offsets (amount@0, min_out@8).
func New(id string, program, inMint, outMint, measureATA solana.PublicKey, accounts []*solana.AccountMeta) *ExactIn {
	minOff := DefaultMinOutOffset
	return &ExactIn{
		ID:           id,
		InMint:       inMint,
		OutMint:      outMint,
		MeasureATA:   measureATA,
		ProgramID:    program,
		Accounts:     accounts,
		AmountOffset: DefaultAmountInOffset,
		MinOutOffset: &minOff,
	}
}

func (e *ExactIn) VenueID() string { return e.ID }
func (e *ExactIn) Input() hop.Port {
	if e.NativeIn {
		return hop.NativeSOLPort()
	}
	return hop.TokenPort(e.InMint, e.InputATA)
}
func (e *ExactIn) Output() hop.Port {
	if e.NativeOut {
		return hop.NativeSOLPort()
	}
	return hop.TokenPort(e.OutMint, e.MeasureATA)
}

// WithInput sets the source ATA used by HopConserve / native→token bridging.
func (e *ExactIn) WithInput(ata solana.PublicKey) *ExactIn {
	e.InputATA = ata
	return e
}

// WithNativeIn marks the hop as spending user-wallet lamports.
func (e *ExactIn) WithNativeIn() *ExactIn {
	e.NativeIn = true
	e.InMint = hop.WrappedSOLMint
	return e
}

// WithNativeOut marks the hop as crediting user-wallet lamports.
func (e *ExactIn) WithNativeOut() *ExactIn {
	e.NativeOut = true
	e.OutMint = hop.WrappedSOLMint
	return e
}

func (e *ExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint64(data[0:8], 0)
	binary.LittleEndian.PutUint64(data[8:16], 0)
	ix := solana.NewInstruction(e.ProgramID, e.Accounts, data)
	bp := hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: e.AmountOffset},
	}
	if e.MinOutOffset != nil {
		off := *e.MinOutOffset
		bp.MinOut = &hop.PatchSite{Offset: off}
	}
	return bp, nil
}
