package titan

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func swapData(disc [8]byte, amount, minOut uint64) []byte {
	data := make([]byte, minDataLen+8)
	copy(data, disc[:])
	binary.LittleEndian.PutUint64(data[8:16], amount)
	binary.LittleEndian.PutUint64(data[16:24], minOut)
	return data
}

func TestNewExactInV2ZerosAmount(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	outATA := solana.NewWallet().PublicKey()
	ix := solana.NewInstruction(ProgramID, []*solana.AccountMeta{
		{PublicKey: user, IsSigner: true, IsWritable: true},
		{PublicKey: outATA, IsWritable: true},
	}, swapData(discSwapRouteV2, 99, 1000))

	h, err := NewExactIn(Params{
		InputMint: inMint, OutputMint: outMint, UserOutputATA: outATA, Swap: ix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.VenueID() != "titan" {
		t.Fatalf("venue %s", h.VenueID())
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut == nil || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("patch sites %+v", bp)
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(data[8:16]) != 0 {
		t.Fatal("amount should be zeroed")
	}
	if binary.LittleEndian.Uint64(data[16:24]) != 1000 {
		t.Fatal("min_out should remain until compile patch")
	}
}

func TestSelectSwapSkipsNonTitan(t *testing.T) {
	other := solana.NewInstruction(solana.NewWallet().PublicKey(), nil, []byte{1, 2, 3})
	want := solana.NewInstruction(ProgramID, nil, swapData(discSwapRoute, 1, 2))
	got, err := SelectSwap([]solana.Instruction{other, want})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := got.Data()
	if binary.LittleEndian.Uint64(data[8:16]) != 1 {
		t.Fatal("selected wrong ix")
	}
}

func TestRejectUnknownDisc(t *testing.T) {
	ix := solana.NewInstruction(ProgramID, nil, make([]byte, 32))
	_, err := NewExactIn(Params{
		InputMint:     solana.NewWallet().PublicKey(),
		OutputMint:    solana.NewWallet().PublicKey(),
		UserOutputATA: solana.NewWallet().PublicKey(),
		Swap:          ix,
	})
	if err == nil {
		t.Fatal("expected unknown discriminator")
	}
}

func TestCompileOwnHopThenTitan(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	titanIx := solana.NewInstruction(ProgramID, []*solana.AccountMeta{
		{PublicKey: user, IsSigner: true, IsWritable: true},
		{PublicKey: ataC, IsWritable: true},
	}, swapData(discSwapRouteV2, 1, 2))
	th, err := NewExactIn(Params{
		InputMint: mintB, OutputMint: mintC, UserOutputATA: ataC, Swap: titanIx,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(1).
		Hop(mock.New("own_leg", prog, mintA, mintB, ataB, acc)).
		Hop(th).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 5 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}
