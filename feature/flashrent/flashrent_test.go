package flashrent_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/flashrent"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/rentpeak"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestFlashRentSkipsWhenUserCoversPeak(t *testing.T) {
	plan, err := buildTwoHop(t, rentpeak.DefaultTokenAccountRent*10, feature.AtaCreateAndCloseCreated)
	if err != nil {
		t.Fatal(err)
	}
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(flashrent.JupiterFlashFillProgramID) {
			t.Fatal("unexpected flash-fill ix when user covers peak")
		}
	}
}

func TestFlashRentRejectsWhenPeakExceedsJupiter(t *testing.T) {
	// Two missing ATAs → peak 2*rent > Jupiter max lend.
	_, err := buildTwoHop(t, 0, feature.AtaCreateAndCloseCreated)
	if err == nil {
		t.Fatal("expected error when peak exceeds Jupiter max lend")
	}
}

func TestFlashRentInsertsBorrowRepaySingleHop(t *testing.T) {
	plan, err := buildSingleHop(t, 0, feature.AtaCreateAndCloseCreated)
	if err != nil {
		t.Fatal(err)
	}
	var flashIxs int
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(flashrent.JupiterFlashFillProgramID) {
			flashIxs++
		}
	}
	if flashIxs != 2 {
		t.Fatalf("flash ixs=%d want borrow+repay", flashIxs)
	}
	last := plan.Instructions[len(plan.Instructions)-1]
	if !last.ProgramID().Equals(flashrent.JupiterFlashFillProgramID) {
		t.Fatalf("last ix should be repay, got %s", last.ProgramID())
	}
}

func TestCustomBackendAlways(t *testing.T) {
	borrowProg := solana.NewWallet().PublicKey()
	repayProg := solana.NewWallet().PublicKey()
	borrow := solana.NewInstruction(borrowProg, nil, []byte{1})
	repay := solana.NewInstruction(repayProg, nil, []byte{2})

	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	h := mockHop(t, user)

	fr := flashrent.AutoWith(flashrent.CustomRentLiquidity{Borrow: borrow, Repay: repay, MaxLamports: 1 << 60})
	fr.Always = true
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		UserLamports(0).
		Feature(fr).
		AtaPolicy(feature.AtaUseOnly).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Instructions[1].ProgramID().Equals(borrowProg) {
		t.Fatalf("expected custom borrow early, got %s", plan.Instructions[1].ProgramID())
	}
	if !plan.Instructions[len(plan.Instructions)-1].ProgramID().Equals(repayProg) {
		t.Fatalf("expected custom repay last")
	}
}

type planResult struct {
	Instructions []solana.Instruction
}

func buildTwoHop(t *testing.T, userLamports uint64, policy feature.AtaPolicy) (*planResult, error) {
	t.Helper()
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	mintA, mintB, mintC := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, _, err := solana.FindAssociatedTokenAddress(user, mintB)
	if err != nil {
		t.Fatal(err)
	}
	ataC, _, err := solana.FindAssociatedTokenAddress(user, mintC)
	if err != nil {
		t.Fatal(err)
	}
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		UserLamports(userLamports).
		TokenAccountRent(rentpeak.DefaultTokenAccountRent).
		Feature(flashrent.Auto()).
		AtaPolicy(policy).
		Hop(mock.New("a", prog, mintA, mintB, ataB, acc)).
		Hop(mock.New("b", prog, mintB, mintC, ataC, acc)).
		Build()
	if err != nil {
		return nil, err
	}
	return &planResult{Instructions: plan.Instructions}, nil
}

func buildSingleHop(t *testing.T, userLamports uint64, policy feature.AtaPolicy) (*planResult, error) {
	t.Helper()
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	h := mockHop(t, user)
	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		UserLamports(userLamports).
		Feature(flashrent.Auto()).
		AtaPolicy(policy).
		Hop(h).
		Build()
	if err != nil {
		return nil, err
	}
	return &planResult{Instructions: plan.Instructions}, nil
}

func mockHop(t *testing.T, user solana.PublicKey) *mock.ExactIn {
	t.Helper()
	mintA, mintB := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, _, err := solana.FindAssociatedTokenAddress(user, mintB)
	if err != nil {
		t.Fatal(err)
	}
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	return mock.New("m", prog, mintA, mintB, ataB, acc)
}
