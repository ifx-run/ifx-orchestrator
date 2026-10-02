package gassponsored_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/gassponsored"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestGasSponsoredEmitsAssertAndRepay(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	sponsor := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(gassponsored.New(sponsor, 5000)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 4 {
		t.Fatalf("expected reset+baseline+cpi+assert+repay, got %d", len(plan.Instructions))
	}
	// Last ix should be ifx CPI (repay), second-to-last assert — both via ifx program.
	last := plan.Instructions[len(plan.Instructions)-1]
	if !last.ProgramID().Equals(constants.DefaultProgramID) {
		t.Fatalf("last ix want ifx, got %s", last.ProgramID())
	}
}

func TestMevTipAndFeeHookFixed(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	tipTo := solana.NewWallet().PublicKey()
	feeTo := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(mevtip.New(tipTo, 10_000)).
		Feature(feehook.Fixed(feeTo, 20_000)).
		Hop(h).
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
	if sysXfers < 2 {
		t.Fatalf("want >=2 system transfers (tip+fee), got %d", sysXfers)
	}
}

func TestFeeHookProceedsBps(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	feeTo := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(feeTo, 50)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 3 {
		t.Fatalf("too few ixs: %d", len(plan.Instructions))
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
