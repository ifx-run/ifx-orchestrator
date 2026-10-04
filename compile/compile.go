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
	// SolIn/SolOut adapt wrapped vs native SOL at the route boundary (SolAsEmitted = leave as the hop CPI).
	SolIn            hop.SolForm
	SolOut           hop.SolForm
	WSOLAccount      solana.PublicKey // wrap destination when SolOut(WSOL) and not implied by a hop
	WSOLTokenProgram solana.PublicKey // zero => Tokenkeg
}

// Plan is the compiled instruction list.
type Plan struct {
	Instructions []solana.Instruction
}

// Compile builds reset → features → ExactIn edge loop (measure + patched CPI) → after_route.
//
// Hop0 patches amount_in from a let const(AmountIn) (or AmountFlow Full resolution).
// Later hops patch amount_in from the previous hop's output delta (after SOL/WSOL
// lane adapt and Feature.MapForwardAmount).
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
		EdgeIndex:        -1,
	}

	for _, f := range p.Features {
		if err := f.BeforeRoute(cx); err != nil {
			return nil, fmt.Errorf("before_route: %w", err)
		}
	}

	var forward *typed.ScratchValue // amount binding for next hop when chaining

	for i, edge := range p.Route.Edges {
		cx.EdgeIndex = i
		for _, f := range p.Features {
			if err := f.BeforeEdge(cx, i); err != nil {
				return nil, fmt.Errorf("before_edge[%d]: %w", i, err)
			}
		}

		step, err := flow.StartStep(edge.From, edge.To, edge.Split)
		if err != nil {
			return nil, fmt.Errorf("start_step[%d]: %w", i, err)
		}

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

		// Per-edge MinOut (0 allowed); route MinAmountOut fills the last edge if unset.
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

		needChain := i < len(p.Route.Edges)-1
		inPort := edge.Hop.Input()
		outPort := edge.Hop.Output()
		if err := inPort.Validate(); err != nil {
			return nil, fmt.Errorf("input port[%d]: %w", i, err)
		}
		if err := outPort.Validate(); err != nil {
			return nil, fmt.Errorf("output port[%d]: %w", i, err)
		}

		var nextIn hop.Port
		if needChain {
			if outPort.Native && countOutEdges(p.Route, edge.To) > 1 {
				return nil, fmt.Errorf("native SOL node cannot split; wrap to WSOL first (edge[%d])", i)
			}
			nextIn = p.Route.Edges[i+1].Hop.Input()
			if err := nextIn.Validate(); err != nil {
				return nil, fmt.Errorf("next input port[%d]: %w", i, err)
			}
			if !outPort.SameMint(nextIn) {
				return nil, fmt.Errorf("cannot chain edge[%d]: mint %s → %s", i, outPort.Mint, nextIn.Mint)
			}
		}

		settleWSOL := !needChain && p.SolOut == hop.SolWSOL
		settleNative := !needChain && p.SolOut == hop.SolNative
		if settleWSOL && !outPort.Native && !outPort.Mint.Equals(hop.WrappedSOLMint) {
			return nil, fmt.Errorf("SolOut(WSOL) requires last hop to emit SOL/WSOL")
		}
		if settleNative && !outPort.Native && !outPort.Mint.Equals(hop.WrappedSOLMint) {
			return nil, fmt.Errorf("SolOut(Native) requires last hop to emit SOL/WSOL")
		}
		sameATA := !inPort.Native && !outPort.Native && !inPort.Account.IsZero() && inPort.Account.Equals(outPort.Account)
		needAdapt := needChain || (settleWSOL && outPort.Native) || (settleNative && !outPort.Native && outPort.Mint.Equals(hop.WrappedSOLMint))
		needOut := (needAdapt || cx.HopConserve) && !sameATA
		needIn := cx.HopConserve && !cx.HopConserveSkipInput && (inPort.Native || !inPort.Account.IsZero())

		if i == 0 {
			switch p.SolIn {
			case hop.SolAsEmitted:
			case hop.SolWSOL:
				if inPort.Native {
					return nil, fmt.Errorf("SolIn(WSOL) but first hop spends native SOL")
				}
			case hop.SolNative:
				if !inPort.Native {
					if !inPort.Mint.Equals(hop.WrappedSOLMint) || inPort.Account.IsZero() {
						return nil, fmt.Errorf("SolIn(Native) requires a native SOL or WSOL first hop")
					}
					if err := wrapLamportsToWSOL(cx, amountBinding, inPort.Account, p.wsolTokenProgram()); err != nil {
						return nil, fmt.Errorf("sol_in wrap: %w", err)
					}
				}
			default:
				return nil, fmt.Errorf("unknown SolIn %d", p.SolIn)
			}
		}

		var inBefore, outBefore typed.ScratchValue
		if needIn || needOut {
			lb := p.Scratch.LetBuilder()
			if needIn {
				inBefore, err = measurePort(lb, p.User, inPort)
				if err != nil {
					return nil, fmt.Errorf("measure input before[%d]: %w", i, err)
				}
			}
			if needOut {
				outBefore, err = measurePort(lb, p.User, outPort)
				if err != nil {
					return nil, fmt.Errorf("measure output before[%d]: %w", i, err)
				}
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

		if needIn {
			lb := p.Scratch.LetBuilder()
			inAfter, err := measurePort(lb, p.User, inPort)
			if err != nil {
				return nil, fmt.Errorf("measure input after[%d]: %w", i, err)
			}
			spent, err := lb.LetEval(expr.SaturatingSub(expr.Ref(inBefore.Index), expr.Ref(inAfter.Index)))
			if err != nil {
				return nil, err
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)
			geIn, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(inBefore.Index), expr.Ref(inAfter.Index)))
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, geIn)
			geSpent, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(spent.Index), expr.Ref(amountBinding.Index)))
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, geSpent)
		}

		if needOut {
			lb := p.Scratch.LetBuilder()
			outAfter, err := measurePort(lb, p.User, outPort)
			if err != nil {
				return nil, fmt.Errorf("measure output after[%d]: %w", i, err)
			}
			minOutAmt, err := lb.LetEval(expr.Add(expr.Ref(outBefore.Index), expr.U64(1)))
			if err != nil {
				return nil, err
			}
			var delta typed.ScratchValue
			if needAdapt {
				if cx.HopConserve {
					delta, err = lb.LetEval(expr.SaturatingSub(expr.Ref(outAfter.Index), expr.Ref(outBefore.Index)))
				} else {
					delta, err = lb.LetEval(expr.Sub(expr.Ref(outAfter.Index), expr.Ref(outBefore.Index)))
				}
				if err != nil {
					return nil, err
				}
			}
			letIx, err := lb.BuildIx()
			if err != nil {
				return nil, err
			}
			ixs = append(ixs, letIx)
			if cx.HopConserve {
				geOut, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(outAfter.Index), expr.Ref(minOutAmt.Index)))
				if err != nil {
					return nil, err
				}
				ixs = append(ixs, geOut)
			}
			if needChain {
				fwd, err := adaptForward(cx, outPort, nextIn, delta, p.wsolTokenProgram())
				if err != nil {
					return nil, fmt.Errorf("adapt[%d]: %w", i, err)
				}
				for _, f := range p.Features {
					fwd, err = f.MapForwardAmount(cx, fwd)
					if err != nil {
						return nil, fmt.Errorf("map_forward[%d]: %w", i, err)
					}
				}
				forward = &fwd
			} else if settleWSOL && outPort.Native {
				dest, err := resolveWSOLATA(p, p.User)
				if err != nil {
					return nil, err
				}
				if err := wrapLamportsToWSOL(cx, delta, dest, p.wsolTokenProgram()); err != nil {
					return nil, fmt.Errorf("sol_out wrap: %w", err)
				}
			} else if settleNative && !outPort.Native {
				if err := unwrapWSOLToNative(cx, delta, outPort.Account, p.wsolTokenProgram()); err != nil {
					return nil, fmt.Errorf("sol_out unwrap: %w", err)
				}
			}
		} else if needChain {
			return nil, fmt.Errorf("cannot chain edge[%d]: no distinct output to measure", i)
		}

		if needChain {
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
