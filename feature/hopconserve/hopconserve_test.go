package hopconserve_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/hopconserve"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestTwoHopWithInputAndOutput(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 4096
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	ataA := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		Feature(hopconserve.New()).
		Hop(mock.New("a", prog, mintA, mintB, ataB, acc).WithInput(ataA)).
		Hop(mock.New("b", prog, mintB, mintC, ataC, acc).WithInput(ataB)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 8 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}

func TestSkipInput(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	a, b := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	if _, err := orchestrator.New(s, user).
		AmountIn(100).
		Feature(hopconserve.New().SkipInput()).
		Hop(mock.New("m", p, a, b, ata, acc)).
		Build(); err != nil {
		t.Fatal(err)
	}
}
