package pumpamm

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func testPool() PoolState {
	return PoolState{
		BaseMint:              solana.NewWallet().PublicKey(),
		QuoteMint:             solana.NewWallet().PublicKey(),
		PoolBaseTokenAccount:  solana.NewWallet().PublicKey(),
		PoolQuoteTokenAccount: solana.NewWallet().PublicKey(),
		CoinCreator:           solana.NewWallet().PublicKey(),
	}
}

func TestSellExactInBlueprint(t *testing.T) {
	pool := testPool()
	user := solana.NewWallet().PublicKey()
	feeRecip := solana.NewWallet().PublicKey()
	h, err := NewSellExactIn(SellParams{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		UserBaseATA: solana.NewWallet().PublicKey(), UserQuoteATA: solana.NewWallet().PublicKey(),
		ProtocolFeeRecipient: feeRecip, ProtocolFeeRecipientTokenATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != SellAmountInOffset || bp.MinOut.Offset != SellMinOutOffset {
		t.Fatalf("patches %+v", bp)
	}
	if len(bp.Template.Accounts()) != sellAccountCount {
		t.Fatalf("accounts=%d want %d", len(bp.Template.Accounts()), sellAccountCount)
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatal("program id")
	}
}

func TestBuyExactInBlueprint(t *testing.T) {
	pool := testPool()
	user := solana.NewWallet().PublicKey()
	h, err := NewBuyExactIn(BuyParams{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		UserBaseATA: solana.NewWallet().PublicKey(), UserQuoteATA: solana.NewWallet().PublicKey(),
		ProtocolFeeRecipient: solana.NewWallet().PublicKey(), ProtocolFeeRecipientTokenATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != BuyAmountInOffset || bp.MinOut.Offset != BuyMinOutOffset {
		t.Fatalf("patches %+v", bp)
	}
	if len(bp.Template.Accounts()) != buyAccountCount {
		t.Fatalf("accounts=%d want %d", len(bp.Template.Accounts()), buyAccountCount)
	}
}
