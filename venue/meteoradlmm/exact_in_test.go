package meteoradlmm

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
		TokenXMint: inMint, TokenYMint: outMint,
		ReserveX: solana.NewWallet().PublicKey(), ReserveY: solana.NewWallet().PublicKey(),
		Oracle: solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, LbPair: solana.NewWallet().PublicKey(), Pool: pool,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
		TokenXProgram: solana.TokenProgramID, TokenYProgram: solana.TokenProgramID,
		BinArrays: []solana.PublicKey{solana.NewWallet().PublicKey()},
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
	if len(bp.Template.Accounts()) != fixedAccountCount+1 {
		t.Fatalf("accounts=%d want %d", len(bp.Template.Accounts()), fixedAccountCount+1)
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatal("program")
	}
}
