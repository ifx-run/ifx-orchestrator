package raydiumlaunchpad

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func testPool() PoolMeta {
	return PoolMeta{
		ConfigID: solana.NewWallet().PublicKey(), PlatformID: solana.NewWallet().PublicKey(),
		BaseVault: solana.NewWallet().PublicKey(), QuoteVault: solana.NewWallet().PublicKey(),
		BaseMint: solana.NewWallet().PublicKey(), QuoteMint: solana.NewWallet().PublicKey(),
	}
}

func TestBuyExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	pool := testPool()
	h, err := NewBuyExactIn(BuyParams{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		UserQuoteATA: solana.NewWallet().PublicKey(), UserBaseATA: solana.NewWallet().PublicKey(),
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
	if len(bp.Template.Accounts()) != launchAccountCount {
		t.Fatalf("accounts=%d", len(bp.Template.Accounts()))
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatal("program")
	}
}

func TestSellExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	pool := testPool()
	h, err := NewSellExactIn(SellParams{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		UserBaseATA: solana.NewWallet().PublicKey(), UserQuoteATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(bp.Template.Accounts()) != launchAccountCount {
		t.Fatalf("accounts=%d", len(bp.Template.Accounts()))
	}
}
