// Package orchestrator provides the batteries-included ExactIn plan builder.
package orchestrator

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/compile"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

// Builder is a fluent facade over compile.Compile.
type Builder struct {
	scratch      *scratch.FrameScratch
	user         solana.PublicKey
	amountIn     uint64
	minAmountOut *uint64
	nodes        []hop.RouteNode
	edges        []hop.RouteEdge
	features     []feature.Feature
}

// New starts a builder for an existing public Frame scratch planner.
func New(s *scratch.FrameScratch, user solana.PublicKey) *Builder {
	return &Builder{scratch: s, user: user}
}

// AmountIn sets the route source ExactIn amount.
func (b *Builder) AmountIn(v uint64) *Builder {
	b.amountIn = v
	return b
}

// MinAmountOut sets the final min-out hint (patched when the last hop exposes MinOut site).
func (b *Builder) MinAmountOut(v uint64) *Builder {
	b.minAmountOut = &v
	return b
}

// Feature appends a lifecycle Feature.
func (b *Builder) Feature(f feature.Feature) *Builder {
	b.features = append(b.features, f)
	return b
}

// Hop appends an ExactIn hop as a Full path edge.
// The first hop creates source+dest nodes; later hops must match previous output mint.
func (b *Builder) Hop(h hop.ExactInHop) *Builder {
	if len(b.nodes) == 0 {
		// Placeholder source ATA unknown until hop provides input — use zero ATA for source measure
		// (source is not measured for chaining). Venues should still set accounts in the blueprint.
		b.nodes = append(b.nodes, hop.RouteNode{
			Mint:         h.InputMint(),
			TokenAccount: solana.PublicKey{}, // unused for measure on node 0 in path compile
		})
		b.nodes = append(b.nodes, hop.RouteNode{
			Mint:         h.OutputMint(),
			TokenAccount: h.OutputMeasureAccount(),
		})
		b.edges = append(b.edges, hop.RouteEdge{
			From:  0,
			To:    1,
			Split: hop.Full(),
			Hop:   h,
		})
		return b
	}
	prev := b.nodes[len(b.nodes)-1]
	if prev.Mint != h.InputMint() {
		// Deferred error at Build time via invalid graph / mint mismatch check
		b.edges = append(b.edges, hop.RouteEdge{
			From:  hop.NodeID(len(b.nodes) - 1),
			To:    hop.NodeID(len(b.nodes)),
			Split: hop.Full(),
			Hop:   mintMismatchHop{inner: h, want: prev.Mint},
		})
		b.nodes = append(b.nodes, hop.RouteNode{
			Mint:         h.OutputMint(),
			TokenAccount: h.OutputMeasureAccount(),
		})
		return b
	}
	from := hop.NodeID(len(b.nodes) - 1)
	b.nodes = append(b.nodes, hop.RouteNode{
		Mint:         h.OutputMint(),
		TokenAccount: h.OutputMeasureAccount(),
	})
	to := hop.NodeID(len(b.nodes) - 1)
	b.edges = append(b.edges, hop.RouteEdge{
		From:  from,
		To:    to,
		Split: hop.Full(),
		Hop:   h,
	})
	return b
}

// FromGraph sets an explicit graph (supports splits). Clears any prior Hop() path.
func (b *Builder) FromGraph(nodes []hop.RouteNode, edges []hop.RouteEdge) *Builder {
	b.nodes = append([]hop.RouteNode(nil), nodes...)
	b.edges = append([]hop.RouteEdge(nil), edges...)
	return b
}

// Build compiles the route into an ifx instruction plan.
func (b *Builder) Build() (*compile.Plan, error) {
	if b.scratch == nil {
		return nil, fmt.Errorf("scratch is required")
	}
	if b.amountIn == 0 {
		return nil, fmt.Errorf("AmountIn is required")
	}
	for i, e := range b.edges {
		if mm, ok := e.Hop.(mintMismatchHop); ok {
			return nil, fmt.Errorf("hop[%d]: input mint %s != previous output %s", i, mm.inner.InputMint(), mm.want)
		}
	}
	return compile.Compile(compile.Params{
		Scratch:      b.scratch,
		User:         b.user,
		AmountIn:     b.amountIn,
		MinAmountOut: b.minAmountOut,
		Route: hop.Route{
			Nodes: b.nodes,
			Edges: b.edges,
		},
		Features: b.features,
	})
}

type mintMismatchHop struct {
	inner hop.ExactInHop
	want  solana.PublicKey
}

func (m mintMismatchHop) VenueID() string                         { return m.inner.VenueID() }
func (m mintMismatchHop) InputMint() solana.PublicKey             { return m.inner.InputMint() }
func (m mintMismatchHop) OutputMint() solana.PublicKey            { return m.inner.OutputMint() }
func (m mintMismatchHop) OutputMeasureAccount() solana.PublicKey  { return m.inner.OutputMeasureAccount() }
func (m mintMismatchHop) BuildBlueprint(cx *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	return m.inner.BuildBlueprint(cx)
}
