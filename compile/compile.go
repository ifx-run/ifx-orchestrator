// Package compile turns a Route + Features into an ifx instruction plan.
package compile

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/amountflow"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/codec"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/patch"
	"github.com/ifx-run/ifx/go-sdk/patchedcpi"
	"github.com/ifx-run/ifx/go-sdk/scratch"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

// Params configures a route compilation.
type Params struct {
	Scratch          *scratch.FrameScratch
	User             solana.PublicKey
	AmountIn         uint64
	MinAmountOut     *uint64
	Route            hop.Route
	Features         []feature.Feature
	UserLamports     uint64
	TokenAccountRent uint64
}

// Plan is the compiled instruction list.
type Plan struct {
	Instructions []solana.Instruction
}

// Compile builds reset → features → ExactIn edge loop (measure + patched CPI) → after_route.
//
// Hop0 patches amount_in from a let const(AmountIn) (or AmountFlow Full resolution).
// Later hops patch amount_in from the previous hop's output ATA delta (after Feature.MapForwardAmount).
func Compile(p Params) (*Plan, error) {
	if p.Scratch == nil {
		return nil, fmt.Errorf("scratch is required")
	}
	if len(p.Route.Nodes) < 2 || len(p.Route.Edges) == 0 {
		return nil, fmt.Errorf("route needs at least one edge")
	}
	if p.AmountIn == 0 {
		return nil, fmt.Errorf("amount_in must be > 0")
	}

	flow, err := amountflow.New(p.AmountIn, len(p.Route.Nodes), p.Route.Edges)
	if err != nil {
		return nil, fmt.Errorf("amount flow: %w", err)
	}

	ixs := []solana.Instruction{p.Scratch.IxReset()}
	cx := &feature.Ctx{
		Scratch:          p.Scratch,
		User:             p.User,
		Ixs:              &ixs,
		AmountIn:         p.AmountIn,
		MinAmountOut:     p.MinAmountOut,
		Route:            p.Route,
		UserLamports:     p.UserLamports,
		TokenAccountRent: p.TokenAccountRent,
	}

	for _, f := range p.Features {
		if err := f.BeforeRoute(cx); err != nil {
			return nil, fmt.Errorf("before_route: %w", err)
		}
	}

	var forward *typed.ScratchValue // amount binding for next hop when chaining

	for i, edge := range p.Route.Edges {
		for _, f := range p.Features {
			if err := f.BeforeEdge(cx, i); err != nil {
				return nil, fmt.Errorf("before_edge[%d]: %w", i, err)
			}
		}

		step, err := flow.StartStep(edge.From, edge.To, edge.Split)
		if err != nil {
			return nil, fmt.Errorf("start_step[%d]: %w", i, err)
		}

		toNode := p.Route.Nodes[edge.To]
		buildHop := hop.HopBuildCtx{User: p.User}
		bp, err := edge.Hop.BuildBlueprint(&buildHop)
		if err != nil {
			return nil, fmt.Errorf("blueprint[%d]: %w", i, err)
		}

		var amountBinding typed.ScratchValue
		if i == 0 {
			// First hop: bind AmountFlow-resolved amount (Full path => AmountIn).
			lb := p.Scratch.LetBuilder()
			amountBinding, err = lb.LetEval(expr.U64(step.AmountIn))
			if err != nil {
				return nil, err
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)
		} else {
			if forward == nil {
				return nil, fmt.Errorf("missing forward amount at edge %d", i)
			}
			amountBinding = *forward
		}

		// Optional min_out patch on last edge when provided on edge or route params.
		minOut := edge.MinOut
		if minOut == nil && i == len(p.Route.Edges)-1 {
			minOut = p.MinAmountOut
		}

		patches := []codec.RawCpiPatch{
			patch.RawCpiPatch(bp.AmountIn.Offset, amountBinding),
		}
		if minOut != nil && bp.MinOut != nil {
			lb := p.Scratch.LetBuilder()
			minBind, err := lb.LetEval(expr.U64(*minOut))
			if err != nil {
				return nil, err
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)
			patches = append(patches, patch.RawCpiPatch(bp.MinOut.Offset, minBind))
		}

		// Measure output ATA before hop when we will chain further.
		var before typed.ScratchValue
		needChain := i < len(p.Route.Edges)-1
		if needChain {
			lb := p.Scratch.LetBuilder()
			before, err = lb.SplTokenAmount(toNode.TokenAccount)
			if err != nil {
				return nil, fmt.Errorf("measure before[%d]: %w", i, err)
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)
		}

		cpiIx, err := rawCpi(p.Scratch, bp.Template, patches...)
		if err != nil {
			return nil, fmt.Errorf("cpi[%d]: %w", i, err)
		}
		ixs = append(ixs, cpiIx)

		if needChain {
			lb := p.Scratch.LetBuilder()
			after, err := lb.SplTokenAmount(toNode.TokenAccount)
			if err != nil {
				return nil, fmt.Errorf("measure after[%d]: %w", i, err)
			}
			delta, err := lb.LetEval(expr.Sub(expr.Ref(after.Index), expr.Ref(before.Index)))
			if err != nil {
				return nil, err
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)

			fwd := delta
			for _, f := range p.Features {
				fwd, err = f.MapForwardAmount(cx, fwd)
				if err != nil {
					return nil, fmt.Errorf("map_forward[%d]: %w", i, err)
				}
			}
			forward = &fwd

			// AmountFlow needs an off-chain output estimate for readiness of later Full edges.
			// Use step.AmountIn as a placeholder out when chaining (exact out is on-chain).
			// For Full path this keeps node totals consistent enough for StartStep checks.
			if _, err := step.Complete(step.AmountIn); err != nil {
				return nil, err
			}
		} else {
			if _, err := step.Complete(0); err != nil {
				return nil, err
			}
		}

		for _, f := range p.Features {
			if err := f.AfterEdge(cx, i); err != nil {
				return nil, fmt.Errorf("after_edge[%d]: %w", i, err)
			}
		}
	}

	// AfterRoute runs in reverse so sandwich Features (FlashRent) repay after ATA closes.
	for i := len(p.Features) - 1; i >= 0; i-- {
		if err := p.Features[i].AfterRoute(cx); err != nil {
			return nil, fmt.Errorf("after_route: %w", err)
		}
	}

	return &Plan{Instructions: ixs}, nil
}

func rawCpi(s *scratch.FrameScratch, template solana.Instruction, patches ...codec.RawCpiPatch) (solana.Instruction, error) {
	built, err := patchedcpi.RawCpi(template, patches...).Build(nil)
	if err != nil {
		return nil, err
	}
	return s.IxCpi(built.WireBuild())
}

// WriteU64LE is a helper for venue templates.
func WriteU64LE(dst []byte, offset int, v uint64) {
	binary.LittleEndian.PutUint64(dst[offset:offset+8], v)
}
