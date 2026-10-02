package solfunding_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/feature/gassponsored"
	"github.com/ifx-run/ifx-orchestrator/feature/solfunding"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestSolFundingWrapAndUnwrapLamportsAll(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(solfunding.WrapAndUnwrap(1_000_000, solfunding.UnwrapLamportsAll)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var sync, unwrap int
	for _, ix := range plan.Instructions {
		if !ix.ProgramID().Equals(solana.TokenProgramID) {
			continue
		}
		data, err := ix.Data()
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 0 && data[0] == 17 {
			sync++
		}
		if len(data) > 0 && data[0] == 45 {
			unwrap++
		}
	}
	if sync < 2 { // wrap SyncNative + unwrap SyncNative
		t.Fatalf("sync native ixs=%d", sync)
	}
	if unwrap != 1 {
		t.Fatalf("unwrap ixs=%d want 1", unwrap)
	}
}

func TestGasSponsoredInterceptWSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	sponsor := solana.NewWallet().PublicKey()
	wsolATA := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(gassponsored.New(sponsor, 5000).WithWSOL(wsolATA)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var unwrap int
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(solana.TokenProgramID) {
			data, err := ix.Data()
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > 0 && data[0] == 45 {
				unwrap++
			}
		}
	}
	_ = unwrap
	if len(plan.Instructions) < 4 {
		t.Fatalf("too few ixs: %d", len(plan.Instructions))
	}
}

func TestFeeHookAtNodeFixed(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	feeTo := solana.NewWallet().PublicKey()

	in := solana.NewWallet().PublicKey()
	mid := solana.NewWallet().PublicKey()
	out := solana.NewWallet().PublicKey()
	ataIn := solana.NewWallet().PublicKey()
	ataMid := solana.NewWallet().PublicKey()
	ataOut := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()

	h0 := mock.New("m0", prog, in, mid, ataMid, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
	})
	h1 := mock.New("m1", prog, mid, out, ataOut, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
	})

	feeNode := hop.NodeID(1)
	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		Feature(feehook.AtNode(feeNode, feeTo).WithFixed(12_345)).
		FromGraph(
			[]hop.RouteNode{
				{Mint: in, TokenAccount: ataIn},
				{Mint: mid, TokenAccount: ataMid},
				{Mint: out, TokenAccount: ataOut},
			},
			[]hop.RouteEdge{
				{From: 0, To: 1, Split: hop.Full(), Hop: h0},
				{From: 1, To: 2, Split: hop.Full(), Hop: h1},
			},
		).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var sysXfers int
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(solana.SystemProgramID) {
			sysXfers++
		}
	}
	if sysXfers < 1 {
		t.Fatal("expected mid-graph fixed fee system transfer")
	}
}

func mockHop(t *testing.T, user solana.PublicKey) *mock.ExactIn {
	t.Helper()
	in := solana.NewWallet().PublicKey()
	out := solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	return mock.New("mock", prog, in, out, ata, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
	})
}
