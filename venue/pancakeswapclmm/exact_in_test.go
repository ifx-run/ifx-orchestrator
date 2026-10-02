package pancakeswapclmm

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	mint0 := solana.NewWallet().PublicKey()
	mint1 := solana.NewWallet().PublicKey()
	pool := PoolState{
		AmmConfig:      solana.NewWallet().PublicKey(),
		TokenMint0:     mint0,
		TokenMint1:     mint1,
		TokenVault0:    solana.NewWallet().PublicKey(),
		TokenVault1:    solana.NewWallet().PublicKey(),
		ObservationKey: solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		InputMint: mint0, OutputMint: mint1,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
		TickArrays: []solana.PublicKey{solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.VenueID() != "pancakeswap_clmm" {
		t.Fatalf("venue %s", h.VenueID())
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if !bp.Template.ProgramID().Equals(ProgramID) {
		t.Fatal("program id")
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != swapV2DataLen || data[40] != 1 {
		t.Fatalf("data len/mode")
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut == nil || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("patches %+v", bp)
	}
	if len(bp.Template.Accounts()) != 15 {
		t.Fatalf("accounts %d", len(bp.Template.Accounts()))
	}
}
