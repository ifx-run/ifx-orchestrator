package pumpfun_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/pumpfun"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestBuyExactSolInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	base := solana.NewWallet().PublicKey()
	creator := solana.NewWallet().PublicKey()
	buy, err := pumpfun.NewBuyExactSolIn(pumpfun.BuyParams{
		User: user, BaseMint: base,
		Curve: pumpfun.CurveMeta{Creator: creator},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !buy.Input().Native || buy.Input().Mint != pumpfun.NativeSOL {
		t.Fatalf("input port %+v", buy.Input())
	}
	bp, err := buy.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if !bp.Template.ProgramID().Equals(pumpfun.ProgramID) {
		t.Fatalf("program %s", bp.Template.ProgramID())
	}
	if bp.AmountIn.Offset != pumpfun.AmountInOffset {
		t.Fatalf("amount offset %d", bp.AmountIn.Offset)
	}
	if bp.MinOut == nil || bp.MinOut.Offset != pumpfun.MinOutOffset {
		t.Fatal("min_out site missing")
	}
}

func TestSellExactInCompiles(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	base := solana.NewWallet().PublicKey()
	creator := solana.NewWallet().PublicKey()

	sell, err := pumpfun.NewSellExactIn(pumpfun.SellParams{
		User: user, BaseMint: base,
		Curve: pumpfun.CurveMeta{Creator: creator, CashbackEnabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(1).
		Hop(sell).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ifx instructions in plan")
	}
}

func TestDecodeCurveMeta(t *testing.T) {
	creator := solana.NewWallet().PublicKey()
	data := make([]byte, 8+75)
	copy(data[8+41:8+73], creator[:])
	data[8+73] = 1
	data[8+74] = 1
	meta, err := pumpfun.DecodeCurveMeta(data)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Creator.Equals(creator) {
		t.Fatal("creator mismatch")
	}
	if !meta.IsMayhemMode || !meta.CashbackEnabled {
		t.Fatal("flags")
	}
}
