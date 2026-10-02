// Package feature defines pluggable route lifecycle hooks.
package feature

import (
	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/scratch"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Ctx is the hook context Features may read/mutate while a plan is compiled.
type Ctx struct {
	Scratch *scratch.FrameScratch
	User    solana.PublicKey
	Ixs     *[]solana.Instruction

	AmountIn     uint64
	MinAmountOut *uint64

	// Route is the graph being compiled (set by compile).
	Route hop.Route
	// UserLamports is the user's available SOL before the plan (for FlashRent gating).
	UserLamports uint64
	// TokenAccountRent overrides classic ATA rent (0 => rentpeak default).
	TokenAccountRent uint64

	// RentPeak is filled by Ata (or callers) for FlashRent to read.
	RentPeakPeakReserve uint64
	RentPeakReady       bool

	// EdgeIndex is the edge currently being compiled (-1 outside the edge loop).
	// MapForwardAmount / AfterEdge may read it.
	EdgeIndex int
}

// Emit appends instructions to the plan.
func (c *Ctx) Emit(ixs ...solana.Instruction) {
	*c.Ixs = append(*c.Ixs, ixs...)
}

// ForwardAmount is a frame binding forwarded between hops.
type ForwardAmount = typed.ScratchValue

// Feature injects behavior around route compilation.
type Feature interface {
	BeforeRoute(cx *Ctx) error
	BeforeEdge(cx *Ctx, edgeIndex int) error
	AfterEdge(cx *Ctx, edgeIndex int) error
	AfterRoute(cx *Ctx) error
	MapForwardAmount(cx *Ctx, amount ForwardAmount) (ForwardAmount, error)
}

// Base embeds no-op Feature methods.
type Base struct{}

func (Base) BeforeRoute(*Ctx) error     { return nil }
func (Base) BeforeEdge(*Ctx, int) error { return nil }
func (Base) AfterEdge(*Ctx, int) error  { return nil }
func (Base) AfterRoute(*Ctx) error      { return nil }
func (Base) MapForwardAmount(_ *Ctx, a ForwardAmount) (ForwardAmount, error) {
	return a, nil
}
