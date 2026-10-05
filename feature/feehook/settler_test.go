package feehook_test

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestCustomSolSettler(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	feeProg := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()

	// Fake fee program: [disc:8][amount:u64] — amount at offset 8.
	data := make([]byte, 16)
	copy(data[:8], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	binary.LittleEndian.PutUint64(data[8:], 0)
	template := solana.NewInstruction(feeProg, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
		{PublicKey: treasury, IsWritable: true, IsSigner: false},
	}, data)

	h := mockHop(t, user)
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.Fixed(treasury, 42_000).WithSolSettler(feehook.CustomIx(template, 8))).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	foundFee := false
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			t.Fatal("fixed custom fee must not use ifx")
		}
		if ix.ProgramID().Equals(feeProg) {
			foundFee = true
			got, err := ix.Data()
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint64(got[8:]) != 42_000 {
				t.Fatalf("baked fee %d", binary.LittleEndian.Uint64(got[8:]))
			}
		}
	}
	if !foundFee {
		t.Fatal("expected baked custom fee instruction")
	}
}

func TestSingleHopProceedsBpsUsesIfx(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(treasury, 30)).
		Hop(mockHop(t, user)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasIfx(plan.Instructions) {
		t.Fatal("single-hop bps fee must emit ifx (measure Δ then patch transfer)")
	}
	if plan.Instructions[0].ProgramID() != constants.DefaultProgramID {
		t.Fatal("ifx reset should be first when Frame is used")
	}
}

func TestSingleHopProceedsBpsNeedsScratch(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	_, err := orchestrator.New(nil, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(solana.NewWallet().PublicKey(), 30)).
		Hop(mockHop(t, user)).
		Build()
	if err == nil {
		t.Fatal("bps fee without scratch should fail")
	}
}

func TestSingleHopTokenBpsUsesIfx(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	recv := solana.NewWallet().PublicKey()
	h := mockHop(t, user)
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.AtNode(1, recv).WithTokenBPS(50, recv)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasIfx(plan.Instructions) {
		t.Fatal("single-hop token bps must emit ifx")
	}
}

func hasIfx(ixs []solana.Instruction) bool {
	for _, ix := range ixs {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			return true
		}
	}
	return false
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
