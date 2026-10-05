// Package compile turns a Route + Features into an instruction plan.
package compile

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/amountflow"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/expr"
	"github.com/ifx-run/ifx/go-sdk/ix"
	"github.com/ifx-run/ifx/go-sdk/patch"
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

// Compile builds features → ExactIn edge loop (measure + hop CPI) → after_route.
//
// Compile-time amount_in / min_out / wrap lamports are baked into templates.
// Frame (Reset / Let / patched CPI) is emitted only when a hop or Feature needs
// a runtime binding. Consecutive Lets share one instruction via Ctx.Let.
func Compile(p Params) (*Plan, error) {
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

	ixs := []solana.Instruction{}
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
		if err := cx.Err(); err != nil {
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
			if err := cx.Err(); err != nil {
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

		amountLit := step.AmountIn
		var amountSlot *typed.ScratchValue
		if i == 0 {
			// First hop: compile-time AmountFlow amount (Full path => AmountIn).
		} else {
			if forward == nil {
				return nil, fmt.Errorf("missing forward amount at edge %d", i)
			}
			amountSlot = forward
		}

		// Per-edge MinOut (0 allowed); route MinAmountOut fills the last edge if unset.
		minOut := edge.MinOut
		if minOut == nil && i == len(p.Route.Edges)-1 {
			minOut = p.MinAmountOut
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
					if err := wrapLamportsToWSOL(cx, inPort.Account, p.wsolTokenProgram(), amountLit, nil); err != nil {
						return nil, fmt.Errorf("sol_in wrap: %w", err)
					}
				}
			default:
				return nil, fmt.Errorf("unknown SolIn %d", p.SolIn)
			}
		}

		var inBefore, outBefore typed.ScratchValue
		if needIn || needOut {
			lb, err := cx.Let()
			if err != nil {
				return nil, err
			}
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
		}

		if err := emitExactIn(cx, bp, amountSlot, amountLit, minOut); err != nil {
			return nil, fmt.Errorf("cpi[%d]: %w", i, err)
		}
		if err := cx.Err(); err != nil {
			return nil, fmt.Errorf("cpi[%d]: %w", i, err)
		}

		if needIn {
			lb, err := cx.Let()
			if err != nil {
				return nil, err
			}
			inAfter, err := measurePort(lb, p.User, inPort)
			if err != nil {
				return nil, fmt.Errorf("measure input after[%d]: %w", i, err)
			}
			spent, err := lb.LetEval(expr.SaturatingSub(expr.Ref(inBefore.Index), expr.Ref(inAfter.Index)))
			if err != nil {
				return nil, err
			}
			amtRef := expr.U64(amountLit)
			if amountSlot != nil {
				amtRef = expr.Ref(amountSlot.Index)
			}
			geIn, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(inBefore.Index), expr.Ref(inAfter.Index)))
			if err != nil {
				return nil, err
			}
			cx.Emit(geIn)
			geSpent, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(spent.Index), amtRef))
			if err != nil {
				return nil, err
			}
			cx.Emit(geSpent)
		}

		if needOut {
			lb, err := cx.Let()
			if err != nil {
				return nil, err
			}
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
			if cx.HopConserve {
				geOut, err := p.Scratch.IxAssert(expr.Ge(expr.Ref(outAfter.Index), expr.Ref(minOutAmt.Index)))
				if err != nil {
					return nil, err
				}
				cx.Emit(geOut)
			} else if needAdapt {
				if err := cx.FlushLet(); err != nil {
					return nil, err
				}
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
					if err := cx.Err(); err != nil {
						return nil, fmt.Errorf("map_forward[%d]: %w", i, err)
					}
				}
				forward = &fwd
			} else if settleWSOL && outPort.Native {
				dest, err := resolveWSOLATA(p, p.User)
				if err != nil {
					return nil, err
				}
				if err := wrapLamportsToWSOL(cx, dest, p.wsolTokenProgram(), 0, &delta); err != nil {
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
			if err := cx.Err(); err != nil {
				return nil, fmt.Errorf("after_edge[%d]: %w", i, err)
			}
		}
	}

	// AfterRoute runs in reverse so sandwich Features (FlashRent) repay after ATA closes.
	for i := len(p.Features) - 1; i >= 0; i-- {
		if err := p.Features[i].AfterRoute(cx); err != nil {
			return nil, fmt.Errorf("after_route: %w", err)
		}
		if err := cx.Err(); err != nil {
			return nil, fmt.Errorf("after_route: %w", err)
		}
	}

	if err := cx.FlushLet(); err != nil {
		return nil, err
	}
	return &Plan{Instructions: prependResetIfUsed(p.Scratch, ixs)}, nil
}

func emitExactIn(cx *feature.Ctx, bp hop.HopBlueprint, amountSlot *typed.ScratchValue, amountLit uint64, minOut *uint64) error {
	tmpl := bp.Template
	var err error
	if amountSlot == nil {
		tmpl, err = feature.BakeU64(tmpl, bp.AmountIn.Offset, amountLit)
		if err != nil {
			return err
		}
	}
	if minOut != nil && bp.MinOut != nil {
		tmpl, err = feature.BakeU64(tmpl, bp.MinOut.Offset, *minOut)
		if err != nil {
			return err
		}
	}
	if amountSlot == nil {
		cx.Emit(tmpl)
		return cx.Err()
	}
	return feature.EmitRawPatchedCPI(cx, tmpl, patch.RawCpiPatch(bp.AmountIn.Offset, *amountSlot))
}

func prependResetIfUsed(s *scratch.FrameScratch, ixs []solana.Instruction) []solana.Instruction {
	if s == nil {
		return ixs
	}
	used := false
	for _, ins := range ixs {
		if ins != nil && ins.ProgramID().Equals(s.ProgramID) {
			used = true
			break
		}
	}
	if !used {
		return ixs
	}
	reset := ix.BuildResetFrame(s.Frame, s.Authority, &ix.Options{ProgramID: s.ProgramID})
	out := make([]solana.Instruction, 0, len(ixs)+1)
	out = append(out, reset)
	return append(out, ixs...)
}

// WriteU64LE is a helper for venue templates.
func WriteU64LE(dst []byte, offset int, v uint64) {
	binary.LittleEndian.PutUint64(dst[offset:offset+8], v)
}
