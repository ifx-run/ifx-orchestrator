package whirlpool

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	inATA := solana.NewWallet().PublicKey()
	outATA := solana.NewWallet().PublicKey()
	pool := PoolState{
		TokenMintA: inMint, TokenMintB: outMint,
		TokenVaultA: solana.NewWallet().PublicKey(), TokenVaultB: solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, Whirlpool: solana.NewWallet().PublicKey(), Pool: pool,
		InputMint: inMint, OutputMint: outMint, UserInputATA: inATA, UserOutputATA: outATA,
		TickArray0: solana.NewWallet().PublicKey(), TickArray1: solana.NewWallet().PublicKey(),
		TickArray2: solana.NewWallet().PublicKey(), Oracle: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("patches %+v", bp)
	}
	data, _ := bp.Template.Data()
	if len(data) != swapDataLen {
		t.Fatalf("data len %d", len(data))
	}
	if len(bp.Template.Accounts()) != 11 {
		t.Fatalf("accounts=%d", len(bp.Template.Accounts()))
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatal("program")
	}
}
