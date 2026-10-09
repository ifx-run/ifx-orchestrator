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
	err              error
}

// New starts a builder. Scratch may be nil when the compiled plan will not use
// Frame (single hop, compile-time amounts). Chained hops and measuring Features
// still need a Frame planner.
func New(s *scratch.FrameScratch, user solana.PublicKey) *Builder {
	return &Builder{scratch: s, user: user}
}

// AmountIn sets the route source ExactIn amount.
func (b *Builder) AmountIn(v uint64) *Builder {
	b.amountIn = v
	return b
}

// MinAmountOut is a last-hop fallback when that Hop() call omitted min_out. 0 is valid.
func (b *Builder) MinAmountOut(v uint64) *Builder {
	b.minAmountOut = &v
	return b
}

// Feature appends a lifecycle Feature. Cross-phase order is resolved by
// feature.Phase (FlashRent → Ata → route → settlement); within a phase,
// registration order is preserved.
func (b *Builder) Feature(f feature.Feature) *Builder {
	if b.err != nil {
		return b
	}
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

// Hop appends an ExactIn hop as a Full path edge (no min_out unless h is hop.WithMinOut).
// The first hop creates source+dest nodes; later hops must match previous output mint
// (wrapped SOL and native SOL share the So111 mint).
func (b *Builder) Hop(h hop.ExactInHop) *Builder {
	return b.appendHop(h)
}

// HopWithMinOut is Hop with that hop's min_amount_out (0 allowed).
func (b *Builder) HopWithMinOut(h hop.ExactInHop, minOut uint64) *Builder {
	return b.appendHop(hop.WithMinOut(h, minOut))
}

func (b *Builder) appendHop(h hop.ExactInHop) *Builder {
	if b.err != nil {
		return b
	}
	min, bare := hop.SplitMinOut(h)
	in, out := bare.Input(), bare.Output()
	if len(b.nodes) == 0 {
		b.nodes = append(b.nodes, hop.NodeFromPort(in))
		b.nodes = append(b.nodes, hop.NodeFromPort(out))
		b.edges = append(b.edges, hop.RouteEdge{
			From: 0, To: 1, Split: hop.Full(), Hop: bare, MinOut: min,
		})
		return b
	}
	prev := b.nodes[len(b.nodes)-1]
	if prev.Mint != in.Mint {
		b.err = fmt.Errorf("hop: input mint %s != previous output %s", in.Mint, prev.Mint)
		return b
	}
	from := hop.NodeID(len(b.nodes) - 1)
	b.nodes = append(b.nodes, hop.NodeFromPort(out))
	to := hop.NodeID(len(b.nodes) - 1)
	b.edges = append(b.edges, hop.RouteEdge{
		From: from, To: to, Split: hop.Full(), Hop: bare, MinOut: min,
	})
	return b
}

// FromGraph sets an explicit graph. Clears any prior Hop() path.
// Partial splits are only supported from the source node (see hop.Route.Validate).
// Hops may be hop.WithMinOut(...); MinOut is peeled onto the edge (edge.MinOut wins if already set).
func (b *Builder) FromGraph(nodes []hop.RouteNode, edges []hop.RouteEdge) *Builder {
	if b.err != nil {
		return b
	}
	b.nodes = append([]hop.RouteNode(nil), nodes...)
	b.edges = make([]hop.RouteEdge, len(edges))
	for i, e := range edges {
		min, bare := hop.SplitMinOut(e.Hop)
		e.Hop = bare
		if e.MinOut == nil {
			e.MinOut = min
		}
		b.edges[i] = e
	}
	return b
}

// Build compiles the route into an instruction plan.
func (b *Builder) Build() (*compile.Plan, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.amountIn == 0 {
		return nil, fmt.Errorf("AmountIn is required")
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
