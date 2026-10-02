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

// Feature takes a fixed lamports fee and/or bps of SOL/token proceeds.
//
// Timing:
//   - FeeNode unset: settle Fixed/ProceedsBps in AfterRoute.
//   - FeeNode set (Exact fee_node_index): charge once after that node's in-edges settle.
//     FixedLamports fires in AfterEdge; TokenBps deducts via MapForwardAmount on the
//     completing inbound edge (so the next hop sees net), or via ATA delta if the fee
//     node is terminal (no further chaining).
type Feature struct {
	feature.Base
	Recipient solana.PublicKey

	// FeeNode, when non-nil, charges when that node's in-edges complete (mid-graph).
	// nil => AfterRoute settlement only.
	FeeNode *hop.NodeID

	// FixedLamports is transferred literally when > 0 (System transfer from user).
	FixedLamports uint64
	// ProceedsBps takes floor(bps * (user_lamports_after - baseline) / 10000) when > 0.
	// Only applied at AfterRoute (needs full-route SOL delta).
	ProceedsBps uint16
	// TokenBps takes floor(bps * amount / 10000). Requires RecipientATA.
	TokenBps uint16
	// RecipientATA receives TokenBps transfers (required when TokenBps > 0).
	RecipientATA solana.PublicKey
	// SourceATA overrides Route.Nodes[*FeeNode].TokenAccount when TokenBps > 0.
	SourceATA solana.PublicKey

	userBefore   *typed.ScratchValue
	tokenBefore  *typed.ScratchValue
	inRemaining  int
	settled      bool
	tokenCharged bool
}

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

// WithFixed sets FixedLamports.
func (f *Feature) WithFixed(lamports uint64) *Feature {
	f.FixedLamports = lamports
	return f
}

// WithTokenBPS sets TokenBps + recipient ATA for mid-graph / terminal token fees.
func (f *Feature) WithTokenBPS(bps uint16, recipientATA solana.PublicKey) *Feature {
	f.TokenBps = bps
	f.RecipientATA = recipientATA
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

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.Recipient.IsZero() {
		return fmt.Errorf("feehook: Recipient is required")
	}
	if f.FixedLamports == 0 && f.ProceedsBps == 0 && f.TokenBps == 0 {
		return fmt.Errorf("feehook: FixedLamports, ProceedsBps, or TokenBps is required")
	}
	if f.TokenBps > 0 && f.RecipientATA.IsZero() {
		return fmt.Errorf("feehook: RecipientATA is required for TokenBps")
	}
	f.settled = false
	f.tokenCharged = false
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

	lb := cx.Scratch.LetBuilder()
	needLet := false
	if f.ProceedsBps > 0 {
		before, err := lb.Lamports(cx.User)
		if err != nil {
			return err
		}
		f.userBefore = &before
		needLet = true
	}
	if f.TokenBps > 0 && f.FeeNode != nil {
		src, err := f.sourceATA(cx)
		if err != nil {
			return err
		}
		tb, err := lb.SplTokenAmount(src)
		if err != nil {
			return err
		}
		f.tokenBefore = &tb
		needLet = true
	}
	if needLet {
		ix, err := lb.BuildIx()
		if err != nil {
			return err
		}
		cx.Emit(ix)
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
	if f.FixedLamports > 0 {
		feature.EmitFixedSystemTransfer(cx, cx.User, f.Recipient, f.FixedLamports)
	}
	// Terminal FeeNode: MapForwardAmount did not run — take TokenBps of ATA delta.
	if f.TokenBps > 0 && !f.tokenCharged {
		if err := f.chargeTokenFromDelta(cx); err != nil {
			return err
		}
	}
	f.settled = true
	return nil
}

func (f *Feature) MapForwardAmount(cx *feature.Ctx, amount feature.ForwardAmount) (feature.ForwardAmount, error) {
	if f.TokenBps == 0 || f.tokenCharged || f.FeeNode == nil {
		return amount, nil
	}
	if cx.EdgeIndex < 0 || cx.EdgeIndex >= len(cx.Route.Edges) {
		return amount, nil
	}
	edge := cx.Route.Edges[cx.EdgeIndex]
	if edge.To != *f.FeeNode {
		return amount, nil
	}
	// MapForward runs before AfterEdge decrements; inRemaining==1 means this is the last inbound.
	if f.inRemaining != 1 {
		return amount, nil
	}

	src, err := f.sourceATA(cx)
	if err != nil {
		return amount, err
	}

	lb := cx.Scratch.LetBuilder()
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
	ix, err := lb.BuildIx()
	if err != nil {
		return amount, err
	}
	cx.Emit(ix)

	if err := feature.EmitPatchedTokenTransfer(cx, src, f.RecipientATA, cx.User, fee); err != nil {
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
	lb := cx.Scratch.LetBuilder()
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
	ix, err := lb.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(ix)
	if err := feature.EmitPatchedTokenTransfer(cx, src, f.RecipientATA, cx.User, fee); err != nil {
		return err
	}
	f.tokenCharged = true
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	// Mid-graph Fixed already charged in AfterEdge; avoid double-charge.
	if f.FeeNode == nil && f.FixedLamports > 0 {
		feature.EmitFixedSystemTransfer(cx, cx.User, f.Recipient, f.FixedLamports)
	}
	if f.ProceedsBps == 0 {
		return nil
	}
	if f.userBefore == nil {
		return fmt.Errorf("feehook: missing baseline for ProceedsBps")
	}

	post := cx.Scratch.LetBuilder()
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
	postIx, err := post.BuildIx()
	if err != nil {
		return err
	}
	cx.Emit(postIx)

	return feature.EmitPatchedSystemTransfer(cx, cx.User, f.Recipient, fee)
}

var _ = typed.ScratchValue{}
