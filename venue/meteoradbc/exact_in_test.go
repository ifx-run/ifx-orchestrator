package meteoradbc

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	poolID := solana.NewWallet().PublicKey()
	base := solana.NewWallet().PublicKey()
	quote := solana.NewWallet().PublicKey()
	h, err := NewExactIn(Params{
		User:          user,
		PoolID:        poolID,
		InputMint:     quote,
		OutputMint:    base,
		UserInputATA:  solana.NewWallet().PublicKey(),
		UserOutputATA: solana.NewWallet().PublicKey(),
		Pool: PoolMeta{
			ConfigID:   solana.NewWallet().PublicKey(),
			BaseMint:   base,
			QuoteMint:  quote,
			BaseVault:  solana.NewWallet().PublicKey(),
			QuoteVault: solana.NewWallet().PublicKey(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.VenueID() != "meteora_dbc" {
		t.Fatalf("venue id %s", h.VenueID())
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
	if len(data) != 25 || data[24] != 0 {
		t.Fatalf("data len/mode %d %v", len(data), data)
	}
	for i := 0; i < 8; i++ {
		if data[i] != discSwap2[i] {
			t.Fatalf("disc mismatch at %d", i)
		}
	}
	accs := bp.Template.Accounts()
	if len(accs) != accountCount {
		t.Fatalf("accounts %d", len(accs))
	}
	if !bp.Template.ProgramID().Equals(ProgramID) {
		t.Fatalf("program %s", bp.Template.ProgramID())
	}
}
