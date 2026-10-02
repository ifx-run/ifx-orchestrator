package raydiumammv4

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	poolID := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	pool := PoolState{
		BaseVault:  solana.NewWallet().PublicKey(),
		QuoteVault: solana.NewWallet().PublicKey(),
		BaseMint:   inMint,
		QuoteMint:  outMint,
	}
	h, err := NewExactIn(Params{
		User: user, PoolID: poolID, Pool: pool,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
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
	if len(data) != 17 || data[0] != discSwapBaseInV2 {
		t.Fatalf("data %x", data)
	}
	if binary.LittleEndian.Uint64(data[1:9]) != 0 || binary.LittleEndian.Uint64(data[9:17]) != 0 {
		t.Fatal("amounts should be zero")
	}
	if len(bp.Template.Accounts()) != 8 {
		t.Fatalf("accounts=%d", len(bp.Template.Accounts()))
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatalf("program %s", bp.Template.ProgramID())
	}
}
