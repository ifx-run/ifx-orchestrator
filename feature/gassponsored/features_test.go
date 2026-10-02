package gassponsored_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/feature/gassponsored"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestFromNativeWithProtection(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	sponsor := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(gassponsored.FromNative(sponsor, 5_000, 12_000)). // 1.2×
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 4 {
		t.Fatalf("expected reset+baseline+cpi+assert+repay, got %d", len(plan.Instructions))
	}
	last := plan.Instructions[len(plan.Instructions)-1]
	if !last.ProgramID().Equals(constants.DefaultProgramID) {
		t.Fatalf("last ix want ifx, got %s", last.ProgramID())
	}
}

func TestFromNativeWithWSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	sponsor := solana.NewWallet().PublicKey()
	wsolATA := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(gassponsored.FromNative(sponsor, 5_000, 11_000).WithWSOL(wsolATA)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var syncNative int
	for _, ix := range plan.Instructions {
		if !ix.ProgramID().Equals(solana.TokenProgramID) {
			continue
		}
		data, err := ix.Data()
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 0 && data[0] == 17 {
			syncNative++
		}
	}
	if syncNative < 1 {
		t.Fatal("expected SyncNative before WSOL unwrap repay")
	}
}

func TestFromTokenFixedAmount(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	userATA := solana.NewWallet().PublicKey()
	sponsorATA := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(gassponsored.FromToken(userATA, sponsorATA, 1_000_000)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 4 {
		t.Fatalf("too few ixs: %d", len(plan.Instructions))
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
