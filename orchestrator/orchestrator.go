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
	scratch          *scratch.FrameScratch
	user             solana.PublicKey
	amountIn         uint64
	minAmountOut     *uint64
	userLamports     uint64
	tokenAccountRent uint64
	nodes            []hop.RouteNode
	edges            []hop.RouteEdge
	features         []feature.Feature
	solIn            hop.SolForm
	solOut           hop.SolForm
	wsolAccount      solana.PublicKey
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
// Register FlashRent before Ata so borrow runs first; AfterRoute is reversed so repay runs last.
func (b *Builder) Feature(f feature.Feature) *Builder {
	b.features = append(b.features, f)
	return b
}

// AtaPolicy is sugar for Feature(feature.WithAta(p)).
func (b *Builder) AtaPolicy(p feature.AtaPolicy) *Builder {
	return b.Feature(feature.WithAta(p))
}

// UserLamports sets available SOL for FlashRent peak gating.
func (b *Builder) UserLamports(v uint64) *Builder {
	b.userLamports = v
	return b
}

// TokenAccountRent overrides classic ATA rent used by rent-peak estimates.
func (b *Builder) TokenAccountRent(v uint64) *Builder {
	b.tokenAccountRent = v
	return b
}

// SolIn selects how the first hop is funded when it wants SOL (Native wraps AmountIn into WSOL).
func (b *Builder) SolIn(form hop.SolForm) *Builder {
	b.solIn = form
	return b
}

// SolOut selects how the last hop's SOL is left (WSOL wraps native proceeds; Native unwraps WSOL proceeds).
func (b *Builder) SolOut(form hop.SolForm) *Builder {
	b.solOut = form
	return b
}

// WSOLAccount sets the wrap destination for SolOut(WSOL). Zero derives ATA(user, WSOL).
func (b *Builder) WSOLAccount(ata solana.PublicKey) *Builder {
	b.wsolAccount = ata
	return b
}

// Hop appends an ExactIn hop as a Full path edge.
// The first hop creates source+dest nodes; later hops must match previous output mint
// (wrapped SOL and native SOL share the So111 mint).
func (b *Builder) Hop(h hop.ExactInHop) *Builder {
	in, out := h.Input(), h.Output()
	if len(b.nodes) == 0 {
		b.nodes = append(b.nodes, hop.NodeFromPort(in))
		b.nodes = append(b.nodes, hop.NodeFromPort(out))
		b.edges = append(b.edges, hop.RouteEdge{
			From:  0,
			To:    1,
			Split: hop.Full(),
			Hop:   h,
		})
		return b
	}
	prev := b.nodes[len(b.nodes)-1]
	if prev.Mint != in.Mint {
		b.edges = append(b.edges, hop.RouteEdge{
			From:  hop.NodeID(len(b.nodes) - 1),
			To:    hop.NodeID(len(b.nodes)),
			Split: hop.Full(),
			Hop:   mintMismatchHop{inner: h, want: prev.Mint},
		})
		b.nodes = append(b.nodes, hop.NodeFromPort(out))
		return b
	}
	from := hop.NodeID(len(b.nodes) - 1)
	b.nodes = append(b.nodes, hop.NodeFromPort(out))
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
			return nil, fmt.Errorf("hop[%d]: input mint %s != previous output %s", i, mm.inner.Input().Mint, mm.want)
		}
	}
	return compile.Compile(compile.Params{
		Scratch:          b.scratch,
		User:             b.user,
		AmountIn:         b.amountIn,
		MinAmountOut:     b.minAmountOut,
		UserLamports:     b.userLamports,
		TokenAccountRent: b.tokenAccountRent,
		SolIn:            b.solIn,
		SolOut:           b.solOut,
		WSOLAccount:      b.wsolAccount,
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

func (m mintMismatchHop) VenueID() string  { return m.inner.VenueID() }
func (m mintMismatchHop) Input() hop.Port  { return m.inner.Input() }
func (m mintMismatchHop) Output() hop.Port { return m.inner.Output() }
func (m mintMismatchHop) BuildBlueprint(cx *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	return m.inner.BuildBlueprint(cx)
}
