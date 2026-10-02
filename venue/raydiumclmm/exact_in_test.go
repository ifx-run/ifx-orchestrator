package raydiumclmm

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	pool := PoolState{
		AmmConfig: solana.NewWallet().PublicKey(), TokenMint0: inMint, TokenMint1: outMint,
		TokenVault0: solana.NewWallet().PublicKey(), TokenVault1: solana.NewWallet().PublicKey(),
		ObservationKey: solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
		TickArrays: []solana.PublicKey{solana.NewWallet().PublicKey()},
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut == nil || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("patches %+v", bp)
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != swapV2DataLen {
		t.Fatalf("data len %d", len(data))
	}
	if len(bp.Template.Accounts()) != 13+1 {
		t.Fatalf("accounts=%d", len(bp.Template.Accounts()))
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatal("program")
	}
}
