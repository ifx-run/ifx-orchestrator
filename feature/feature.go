// Package feature defines pluggable route lifecycle hooks.
package feature

import (
	"fmt"

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

	// HopConserve, when true, compile asserts per-hop output increase and
	// (unless HopConserveSkipInput) input debit ≥ patched amount_in.
	HopConserve          bool
	HopConserveSkipInput bool

	letOpen *scratch.LetBuilder
	emitErr error
}

// Let returns the open ifx_let batch. Consecutive Let() calls share one instruction
// until FlushLet or Emit of a non-let ix. Requires Scratch.
func (c *Ctx) Let() (*scratch.LetBuilder, error) {
	if c == nil || c.Scratch == nil {
		return nil, fmt.Errorf("scratch is required for Frame bindings (chained hops, HopConserve, or Features that measure)")
	}
	if c.letOpen == nil {
		c.letOpen = c.Scratch.LetBuilder()
	}
	return c.letOpen, nil
}

// FlushLet emits the open ifx_let if it has bindings. Safe to call when empty.
func (c *Ctx) FlushLet() error {
	if c == nil || c.letOpen == nil {
		return nil
	}
	lb := c.letOpen
	c.letOpen = nil
	if len(lb.Finish().Bindings) == 0 {
		return nil
	}
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	*c.Ixs = append(*c.Ixs, ix)
	return nil
}

// Err is the first failure recorded by Emit (for example a Let flush error).
func (c *Ctx) Err() error {
	if c == nil {
		return nil
	}
	return c.emitErr
}

func (c *Ctx) noteErr(err error) {
	if err != nil && c.emitErr == nil {
		c.emitErr = err
	}
}

// Emit flushes any open ifx_let, then appends instructions.
func (c *Ctx) Emit(ixs ...solana.Instruction) {
	if err := c.FlushLet(); err != nil {
		c.noteErr(err)
		return
	}
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
