// Package hop defines ExactIn route graph primitives and venue blueprints.
package hop

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// NodeID indexes a RouteNode in a Route.
type NodeID int

// SplitBps is Exact-style edge funding: Full (100%) or Partial bps in 1..9999.
// Multiple partial out-edges from one node must sum to Full (10000).
type SplitBps struct {
	partial uint16 // 0 => Full; else Partial(partial)
}

// Full returns a 100% split.
func Full() SplitBps { return SplitBps{} }

// Partial returns a partial split. bps must be in 1..9999.
func Partial(bps uint16) (SplitBps, error) {
	if bps < 1 || bps > 9999 {
		return SplitBps{}, fmt.Errorf("split bps must be 1..9999, got %d", bps)
	}
	return SplitBps{partial: bps}, nil
}

// MustPartial panics if bps is invalid.
func MustPartial(bps uint16) SplitBps {
	s, err := Partial(bps)
	if err != nil {
		panic(err)
	}
	return s
}

func (s SplitBps) IsFull() bool    { return s.partial == 0 }
func (s SplitBps) IsPartial() bool { return s.partial != 0 }

// Bps returns the partial value, or 10000 when Full.
func (s SplitBps) Bps() uint16 {
	if s.IsFull() {
		return 10000
	}
	return s.partial
}

// TryAdd sums two partials; Full+anything is invalid.
func (s SplitBps) TryAdd(other SplitBps) (SplitBps, error) {
	if s.IsFull() || other.IsFull() {
		return SplitBps{}, fmt.Errorf("cannot add Full split")
	}
	sum := uint32(s.partial) + uint32(other.partial)
	if sum > 10000 {
		return SplitBps{}, fmt.Errorf("split bps sum %d exceeds 10000", sum)
	}
	if sum == 10000 {
		return Full(), nil
	}
	return SplitBps{partial: uint16(sum)}, nil
}

// RouteNode is a mint + user token account endpoint on the route graph.
type RouteNode struct {
	Mint         solana.PublicKey
	TokenAccount solana.PublicKey
	// TokenProgram is the SPL Token program for this ATA (zero => classic Tokenkeg).
	TokenProgram solana.PublicKey
	// Exists is true when the ATA is already initialized on-chain before the plan runs.
	Exists bool
	// Native is true when this endpoint is user-wallet lamports (mint must be Wrapped SOL).
	Native bool
}

// PatchSite is a u64 LE write into instruction data (Exact CustomInstruction offset).
type PatchSite struct {
	Offset uint16
}

// HopBlueprint is a venue ExactIn instruction template plus patch sites.
type HopBlueprint struct {
	Template solana.Instruction // amount fields typically zeroed
	AmountIn PatchSite
	MinOut   *PatchSite
}

// HopBuildCtx is passed to ExactInHop when building a blueprint.
type HopBuildCtx struct {
	User solana.PublicKey
}

// ExactInHop produces one ExactIn DEX instruction blueprint.
// Venues stay thin at this boundary: accounts + template + patch offsets only.
// Input/Output describe what the venue CPI actually consumes and produces
// (native SOL vs WSOL ATA). Compile adapts lanes when chaining.
type ExactInHop interface {
	VenueID() string
	Input() Port
	Output() Port
	BuildBlueprint(cx *HopBuildCtx) (HopBlueprint, error)
}

// RouteEdge is one ExactIn step on the graph.
type RouteEdge struct {
	From   NodeID
	To     NodeID
	Split  SplitBps
	Hop    ExactInHop
	MinOut *uint64
}

// Route is a validated node/edge graph (caller supplies topo order of Edges).
type Route struct {
	Nodes []RouteNode
	Edges []RouteEdge
}
