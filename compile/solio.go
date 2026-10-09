package compile

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/solfunding"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx/go-sdk/scratch"
	"github.com/ifx-run/ifx/go-sdk/typed"
)

func (p Params) wsolTokenProgram() solana.PublicKey {
	if p.WSOLTokenProgram.IsZero() {
		return solana.TokenProgramID
	}
	return p.WSOLTokenProgram
}

func measurePort(lb *scratch.LetBuilder, user solana.PublicKey, port hop.Port) (typed.ScratchValue, error) {
	if port.Native {
		return lb.Lamports(user)
	}
	if port.Account.IsZero() {
		return typed.ScratchValue{}, fmt.Errorf("token port has no ATA to measure")
	}
	return lb.SplTokenAmount(port.Account)
}

func wrapLamportsToWSOL(cx *feature.Ctx, dest, tokenProgram solana.PublicKey, lit uint64, slot *typed.ScratchValue) error {
	create, err := solfunding.CreateATAIdempotent(cx.User, cx.User, hop.WrappedSOLMint, tokenProgram)
	if err != nil {
		return err
	}
	cx.Emit(create)
	if slot != nil {
		if err := feature.EmitPatchedSystemTransfer(cx, cx.User, dest, *slot); err != nil {
			return err
		}
	} else {
		feature.EmitFixedSystemTransfer(cx, cx.User, dest, lit)
	}
	cx.Emit(solfunding.SyncNativeInstruction(dest, tokenProgram))
	return cx.Err()
}

func unwrapWSOLToNative(cx *feature.Ctx, amount typed.ScratchValue, source, tokenProgram solana.PublicKey) error {
	cx.Emit(solfunding.SyncNativeInstruction(source, tokenProgram))
	zero := uint64(0)
	template := solfunding.UnwrapLamportsInstruction(source, cx.User, cx.User, tokenProgram, &zero)
	return feature.EmitRawPatchedCPI(cx, template, feature.RawCpiU64Patch(solfunding.UnwrapLamportsAmountOffset, amount))
}

func adaptForward(cx *feature.Ctx, from, to hop.Port, delta typed.ScratchValue, tokenProgram solana.PublicKey) (typed.ScratchValue, error) {
	switch {
	case from.Native && to.Native:
		return delta, nil
	case !from.Native && !to.Native:
		if from.Account.IsZero() {
			return typed.ScratchValue{}, fmt.Errorf("cannot chain without a user output ATA")
		}
		if !to.Account.IsZero() && !from.Account.Equals(to.Account) {
			return typed.ScratchValue{}, fmt.Errorf("token chain requires the same user ATA (%s → %s)", from.Account, to.Account)
		}
		return delta, nil
	case from.Native && !to.Native:
		if to.Account.IsZero() || !to.Mint.Equals(hop.WrappedSOLMint) {
			return typed.ScratchValue{}, fmt.Errorf("native SOL can only wrap into a WSOL ATA")
		}
		if err := wrapLamportsToWSOL(cx, to.Account, tokenProgram, 0, &delta); err != nil {
			return typed.ScratchValue{}, err
		}
		return delta, nil
	default:
		if from.Account.IsZero() || !from.Mint.Equals(hop.WrappedSOLMint) {
			return typed.ScratchValue{}, fmt.Errorf("unwrap to native SOL requires a WSOL ATA")
		}
		if err := unwrapWSOLToNative(cx, delta, from.Account, tokenProgram); err != nil {
			return typed.ScratchValue{}, err
		}
		return delta, nil
	}
}

func countOutEdges(route hop.Route, from hop.NodeID) int {
	n := 0
	for _, e := range route.Edges {
		if e.From == from {
			n++
		}
	}
	return n
}

func firstOutEdge(route hop.Route, from hop.NodeID) *hop.RouteEdge {
	for i := range route.Edges {
		if route.Edges[i].From == from {
			return &route.Edges[i]
		}
	}
	return nil
}

func resolveWSOLATA(p Params, user solana.PublicKey) (solana.PublicKey, error) {
	if !p.WSOLAccount.IsZero() {
		return p.WSOLAccount, nil
	}
	return solfunding.AssociatedTokenAddress(user, hop.WrappedSOLMint, p.wsolTokenProgram())
}
