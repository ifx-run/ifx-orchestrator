// Package feehook charges a platform fee from user SOL or token proceeds (Feature).
package feehook

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Feature charges exactly one fee kind: FixedLamports, ProceedsBps, or TokenBps.
//
// Combine fixed + bps by registering two Features; charge order is then controlled
// by registration order (see package comment on ordering below).
//
// Timing:
//   - FeeNode unset: Fixed / ProceedsBps settle in AfterRoute.
//   - FeeNode set (Exact fee_node_index): charge once when that node's in-edges complete.
//     Fixed and TokenBps both participate in MapForwardAmount (chained) or AfterEdge
//     (terminal), so registration order among mid-graph fee Features is honored.
//
// Ordering:
//   - AfterRoute runs in reverse of Phase-sorted Features. Within Settlement, a Feature
//     registered later runs earlier. Want Fixed then ProceedsBPS ⇒
//     Feature(ProceedsBPS(...)).Feature(Fixed(...)).
//   - Mid-graph (AtNode) fees run in MapForwardAmount / AfterEdge in Phase-sorted
//     registration order (not reversed). Want Fixed then TokenBps ⇒
//     Feature(AtNode(...).WithFixed(...)).Feature(AtNode(...).WithTokenBPS(...)).
//
// Settlement is pluggable via SolSettler / TokenSettler (default System/Token transfer).
type Feature struct {
	feature.Base
	Recipient solana.PublicKey

	// FeeNode, when non-nil, charges when that node's in-edges complete (mid-graph).
	// nil => AfterRoute settlement only.
	FeeNode *hop.NodeID

	// FixedLamports is transferred literally when > 0.
	FixedLamports uint64
	// ProceedsBps takes floor(bps * (user_lamports_after - baseline) / 10000) when > 0.
	// Only applied at AfterRoute (needs full-route SOL delta).
	ProceedsBps uint16
	// TokenBps takes floor(bps * amount / 10000).
	TokenBps uint16
	// RecipientATA is used by the default TokenTransferSettler (required when TokenBps > 0
	// and TokenSettler is nil).
	RecipientATA solana.PublicKey
	// SourceATA overrides Route.Nodes[*FeeNode].TokenAccount when TokenBps > 0.
	SourceATA solana.PublicKey

	// SolSettler collects FixedLamports / ProceedsBps. nil => SystemTransferSettler{To: Recipient}.
	SolSettler Settler
	// TokenSettler collects TokenBps. nil => TokenTransferSettler{Source, Destination: RecipientATA}.
	TokenSettler Settler

	userBefore   *typed.ScratchValue
	tokenBefore  *typed.ScratchValue
	inRemaining  int
	settled      bool
	tokenCharged bool
	fixedEmitted bool
}

// Phase settles fees after the route (with other Settlement features).
func (f *Feature) Phase() feature.Phase { return feature.PhaseSettlement }

// Fixed returns a FeeHook that transfers a constant lamports amount (AfterRoute).
func Fixed(recipient solana.PublicKey, lamports uint64) *Feature {
	return &Feature{Recipient: recipient, FixedLamports: lamports}
}

// ProceedsBPS returns a FeeHook that takes bps of user SOL delta after the route.
func ProceedsBPS(recipient solana.PublicKey, bps uint16) *Feature {
	return &Feature{Recipient: recipient, ProceedsBps: bps}
}

// AtNode returns a mid-graph FeeHook (Exact fee_node_index).
func AtNode(node hop.NodeID, recipient solana.PublicKey) *Feature {
	n := node
	return &Feature{Recipient: recipient, FeeNode: &n}
}

// WithFixed sets FixedLamports on an AtNode Feature (Fixed-only; do not also set bps).
func (f *Feature) WithFixed(lamports uint64) *Feature {
	f.FixedLamports = lamports
	return f
}

// WithTokenBPS sets TokenBps + recipient ATA on an AtNode Feature (TokenBps-only).
func (f *Feature) WithTokenBPS(bps uint16, recipientATA solana.PublicKey) *Feature {
	f.TokenBps = bps
	f.RecipientATA = recipientATA
	return f
}

// WithSolSettler overrides how Fixed / ProceedsBps fees are collected.
func (f *Feature) WithSolSettler(s Settler) *Feature {
	f.SolSettler = s
	return f
}

// WithTokenSettler overrides how TokenBps fees are collected.
func (f *Feature) WithTokenSettler(s Settler) *Feature {
	f.TokenSettler = s
	return f
}

func (f *Feature) sourceATA(cx *feature.Ctx) (solana.PublicKey, error) {
	if !f.SourceATA.IsZero() {
		return f.SourceATA, nil
	}
	if f.FeeNode == nil {
		return solana.PublicKey{}, fmt.Errorf("feehook: SourceATA or FeeNode required for TokenBps")
	}
	ata := cx.Route.Nodes[*f.FeeNode].TokenAccount
	if ata.IsZero() {
		return solana.PublicKey{}, fmt.Errorf("feehook: FeeNode token account is zero")
	}
	return ata, nil
}

func (f *Feature) solSettler() Settler {
	if f.SolSettler != nil {
		return f.SolSettler
	}
	return SystemTransferSettler{To: f.Recipient}
}

func (f *Feature) tokenSettler(cx *feature.Ctx) (Settler, error) {
	if f.TokenSettler != nil {
		return f.TokenSettler, nil
	}
	src, err := f.sourceATA(cx)
	if err != nil {
		return nil, err
	}
	if f.RecipientATA.IsZero() {
		return nil, fmt.Errorf("feehook: RecipientATA is required for default TokenTransferSettler")
	}
	return TokenTransferSettler{Source: src, Destination: f.RecipientATA}, nil
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	kinds := 0
	if f.FixedLamports > 0 {
		kinds++
	}
	if f.ProceedsBps > 0 {
		kinds++
	}
	if f.TokenBps > 0 {
		kinds++
	}
	if kinds == 0 {
		return fmt.Errorf("feehook: FixedLamports, ProceedsBps, or TokenBps is required")
	}
	if kinds > 1 {
		return fmt.Errorf("feehook: one Feature charges one fee kind; register separate Features to combine Fixed and bps (order = registration / AfterRoute reverse)")
	}
	if f.ProceedsBps > 0 && f.FeeNode != nil {
		return fmt.Errorf("feehook: ProceedsBps is AfterRoute-only; omit FeeNode")
	}
	if f.TokenBps > 0 && f.FeeNode == nil {
		return fmt.Errorf("feehook: TokenBps requires AtNode (FeeNode)")
	}
	if f.SolSettler == nil && f.Recipient.IsZero() && (f.FixedLamports > 0 || f.ProceedsBps > 0) {
		return fmt.Errorf("feehook: Recipient or SolSettler is required for SOL fees")
	}
	if f.TokenBps > 0 && f.TokenSettler == nil && f.RecipientATA.IsZero() {
		return fmt.Errorf("feehook: RecipientATA or TokenSettler is required for TokenBps")
	}
	f.settled = false
	f.tokenCharged = false
	f.fixedEmitted = false
	f.inRemaining = 0
	f.tokenBefore = nil
	if f.FeeNode != nil {
		n := int(*f.FeeNode)
		if n < 0 || n >= len(cx.Route.Nodes) {
			return fmt.Errorf("feehook: FeeNode %d out of range", n)
		}
		for _, e := range cx.Route.Edges {
			if e.To == *f.FeeNode {
				f.inRemaining++
			}
		}
		if f.inRemaining == 0 {
			return fmt.Errorf("feehook: FeeNode %d has no in-edges", n)
		}
	}

	needLet := f.ProceedsBps > 0 || (f.TokenBps > 0 && f.FeeNode != nil)
	if !needLet {
		return nil
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	if f.ProceedsBps > 0 {
		before, err := lb.Lamports(cx.User)
		if err != nil {
			return err
		}
		f.userBefore = &before
	}
	if f.TokenBps > 0 && f.FeeNode != nil && f.TokenSettler == nil {
		// Baseline only needed for default terminal TokenBps delta path.
		src, err := f.sourceATA(cx)
		if err != nil {
			return err
		}
		tb, err := lb.SplTokenAmount(src)
		if err != nil {
			return err
		}
		f.tokenBefore = &tb
	} else if f.TokenBps > 0 && f.FeeNode != nil && f.TokenSettler != nil {
		// Custom token settler still needs a baseline for terminal (non-chaining) FeeNode.
		src, err := f.sourceATA(cx)
		if err == nil && !src.IsZero() {
			tb, err := lb.SplTokenAmount(src)
			if err != nil {
				return err
			}
			f.tokenBefore = &tb
		}
	}
	return nil
}

func (f *Feature) AfterEdge(cx *feature.Ctx, edgeIndex int) error {
	if f.FeeNode == nil || f.settled {
		return nil
	}
	edge := cx.Route.Edges[edgeIndex]
	if edge.To != *f.FeeNode {
		return nil
	}
	f.inRemaining--
	if f.inRemaining > 0 {
		return nil
	}
	// Terminal FeeNode (no further hop): settle here in Feature registration order.
	// Chained Fixed/TokenBps already ran in MapForwardAmount.
	if f.FixedLamports > 0 && !f.fixedEmitted {
		if err := f.solSettler().EmitFixed(cx, f.FixedLamports); err != nil {
			return err
		}
		f.fixedEmitted = true
	}
	if f.TokenBps > 0 && !f.tokenCharged {
		if err := f.chargeTokenFromDelta(cx); err != nil {
			return err
		}
	}
	f.settled = true
	return nil
}

func (f *Feature) feeNodeReady(cx *feature.Ctx) bool {
	if f.FeeNode == nil || cx.EdgeIndex < 0 || cx.EdgeIndex >= len(cx.Route.Edges) {
		return false
	}
	edge := cx.Route.Edges[cx.EdgeIndex]
	if edge.To != *f.FeeNode {
		return false
	}
	return f.inRemaining == 1
}

func (f *Feature) MapForwardAmount(cx *feature.Ctx, amount feature.ForwardAmount) (feature.ForwardAmount, error) {
	if !f.feeNodeReady(cx) {
		return amount, nil
	}
	// Mid-graph chained path: honor Feature registration order among fee hooks.
	if f.FixedLamports > 0 && !f.fixedEmitted {
		if err := f.solSettler().EmitFixed(cx, f.FixedLamports); err != nil {
			return amount, err
		}
		f.fixedEmitted = true
	}
	if f.TokenBps == 0 || f.tokenCharged {
		return amount, nil
	}

	lb, err := cx.Let()
	if err != nil {
		return amount, err
	}
	bpsConst, err := lb.LetConstU64(uint64(f.TokenBps))
	if err != nil {
		return amount, err
	}
	fee, err := lb.LetEval(expr.BpsMulFloor(
		expr.Ref(amount.Index),
		expr.Ref(bpsConst.Index),
	))
	if err != nil {
		return amount, err
	}
	net, err := lb.LetEval(expr.Sub(
		expr.Ref(amount.Index),
		expr.Ref(fee.Index),
	))
	if err != nil {
		return amount, err
	}

	settler, err := f.tokenSettler(cx)
	if err != nil {
		return amount, err
	}
	if err := settler.EmitPatched(cx, fee); err != nil {
		return amount, err
	}
	f.tokenCharged = true
	return net, nil
}

func (f *Feature) chargeTokenFromDelta(cx *feature.Ctx) error {
	if f.tokenBefore == nil {
		return fmt.Errorf("feehook: missing token baseline for terminal TokenBps")
	}
	src, err := f.sourceATA(cx)
	if err != nil {
		return err
	}
	lb, err := cx.Let()
	if err != nil {
		return err
	}
	after, err := lb.SplTokenAmount(src)
	if err != nil {
		return err
	}
	delta, err := lb.LetEval(expr.Sub(
		expr.Ref(after.Index),
		expr.Ref(f.tokenBefore.Index),
	))
	if err != nil {
		return err
	}
	bpsConst, err := lb.LetConstU64(uint64(f.TokenBps))
	if err != nil {
		return err
	}
	fee, err := lb.LetEval(expr.BpsMulFloor(
		expr.Ref(delta.Index),
		expr.Ref(bpsConst.Index),
	))
	if err != nil {
		return err
	}
	settler, err := f.tokenSettler(cx)
	if err != nil {
		return err
	}
	if err := settler.EmitPatched(cx, fee); err != nil {
		return err
	}
	f.tokenCharged = true
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.FeeNode == nil && f.FixedLamports > 0 {
		if err := f.solSettler().EmitFixed(cx, f.FixedLamports); err != nil {
			return err
		}
	}
	if f.ProceedsBps == 0 {
		return nil
	}
	if f.userBefore == nil {
		return fmt.Errorf("feehook: missing baseline for ProceedsBps")
	}

	post, err := cx.Let()
	if err != nil {
		return err
	}
	userAfter, err := post.Lamports(cx.User)
	if err != nil {
		return err
	}
	proceeds, err := post.LetEval(expr.Sub(
		expr.Ref(userAfter.Index),
		expr.Ref(f.userBefore.Index),
	))
	if err != nil {
		return err
	}
	bpsConst, err := post.LetConstU64(uint64(f.ProceedsBps))
	if err != nil {
		return err
	}
	fee, err := post.LetEval(expr.BpsMulFloor(
		expr.Ref(proceeds.Index),
		expr.Ref(bpsConst.Index),
	))
	if err != nil {
		return err
	}

	return f.solSettler().EmitPatched(cx, fee)
}

var _ = typed.ScratchValue{}
