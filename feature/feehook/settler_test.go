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
	foundIfx := false
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			foundIfx = true
		}
		// Custom fee is wrapped in ifx CPI, not emitted as bare feeProg.
		if ix.ProgramID().Equals(feeProg) {
			t.Fatal("expected custom fee via ifx CPI, not bare program id")
		}
	}
	if !foundIfx {
		t.Fatal("expected ifx instructions")
	}
	_ = plan
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
