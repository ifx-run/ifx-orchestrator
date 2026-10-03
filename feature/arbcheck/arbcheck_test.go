package arbcheck_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/arbcheck"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestTokenCycleTwoHop(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	ataA := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	tipTo := solana.NewWallet().PublicKey()

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		Feature(arbcheck.Token(ataA, 1)).
		Feature(mevtip.New(tipTo, 1_000)).
		Hop(mock.New("leg0", prog, mintA, mintB, ataB, acc)).
		Hop(mock.New("leg1", prog, mintB, mintA, ataA, acc)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 6 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}

func TestNativeWithWSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	wsol := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	_, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(arbcheck.Native(0).WithWSOL(wsol)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
}

func TestShareNativeTip(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	tipTo := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	_, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(arbcheck.Native(1)).
		Feature(mevtip.ShareNative(tipTo, 500).WithMax(1_000_000)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
}

func mockHop(t *testing.T, user solana.PublicKey) *mock.ExactIn {
	t.Helper()
	p := solana.NewWallet().PublicKey()
	a := solana.NewWallet().PublicKey()
	b := solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	return mock.New("m", p, a, b, ata, acc)
}
