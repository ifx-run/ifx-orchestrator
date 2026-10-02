package pumpfun_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/venue/pumpfun"
)

func TestBuyExactQuoteInV2Blueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	creator := solana.NewWallet().PublicKey()
	quoteMint := solana.NewWallet().PublicKey()
	buy, err := pumpfun.NewBuyExactQuoteInV2(pumpfun.BuyQuoteV2Params{
		User: user, BaseMint: solana.NewWallet().PublicKey(), QuoteMint: quoteMint,
		Curve: pumpfun.CurveMeta{Creator: creator},
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := buy.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != pumpfun.AmountInOffset {
		t.Fatalf("amount offset %d", bp.AmountIn.Offset)
	}
	if len(bp.Template.Accounts()) != 28 {
		t.Fatalf("buy accounts=%d", len(bp.Template.Accounts()))
	}
}

func TestSellV2Blueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	sell, err := pumpfun.NewSellV2(pumpfun.SellQuoteV2Params{
		User: user, BaseMint: solana.NewWallet().PublicKey(), QuoteMint: solana.NewWallet().PublicKey(),
		Curve: pumpfun.CurveMeta{Creator: solana.NewWallet().PublicKey()},
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := sell.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(bp.Template.Accounts()) != 27 {
		t.Fatalf("sell accounts=%d", len(bp.Template.Accounts()))
	}
}
