// Package flashrent borrows temporary SOL for ATA rent peaks (Feature).
package flashrent

import (
	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
)

// JupiterFlashFillProgramID is the mainnet Jupiter Flash Fill program.
var JupiterFlashFillProgramID = solana.MustPublicKeyFromBase58("JUPLdTqUdKztWJ1isGMV92W2QvmEmzs9WTJjhZe4QdJ")

// RentLiquidityBackend produces borrow/repay instructions for FlashRent.
type RentLiquidityBackend interface {
	BorrowIx(cx *feature.Ctx) (solana.Instruction, error)
	RepayIx(cx *feature.Ctx) (solana.Instruction, error)
}

// JupiterFlashFill is the default backend (ix builders land in Phase 2).
type JupiterFlashFill struct {
	Borrower solana.PublicKey
}

func (JupiterFlashFill) BorrowIx(*feature.Ctx) (solana.Instruction, error) {
	return nil, errNotImplemented("JupiterFlashFill.BorrowIx")
}
func (JupiterFlashFill) RepayIx(*feature.Ctx) (solana.Instruction, error) {
	return nil, errNotImplemented("JupiterFlashFill.RepayIx")
}

// CustomRentLiquidity uses caller-supplied borrow/repay instructions.
type CustomRentLiquidity struct {
	Borrow solana.Instruction
	Repay  solana.Instruction
}

func (c CustomRentLiquidity) BorrowIx(*feature.Ctx) (solana.Instruction, error) {
	return c.Borrow, nil
}
func (c CustomRentLiquidity) RepayIx(*feature.Ctx) (solana.Instruction, error) {
	return c.Repay, nil
}

type notImpl string

func (e notImpl) Error() string { return string(e) + ": not implemented (Phase 2)" }
func errNotImplemented(name string) error { return notImpl(name) }

// Feature wraps a RentLiquidityBackend. Phase 0 is a stub (no auto peak gating yet).
type Feature struct {
	feature.Base
	Backend RentLiquidityBackend
	Always  bool // if false, Phase 2 will gate on rent peak gap
}

// Auto returns FlashRent with JupiterFlashFill (stub until Phase 2 emits ixs).
func Auto() *Feature {
	return &Feature{Backend: JupiterFlashFill{}}
}

// AutoWith returns FlashRent with a custom backend.
func AutoWith(b RentLiquidityBackend) *Feature {
	return &Feature{Backend: b}
}
