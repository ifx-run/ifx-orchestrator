package meteoradammv2

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/raydiumcpmm"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	tokenProg := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	h, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: PoolState{
			MintA: inMint, MintB: outMint,
			VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
		},
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
		TokenProgramA: tokenProg, TokenProgramB: tokenProg,
	})
	if err != nil {
		t.Fatal(err)
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset {
		t.Fatalf("amount offset %d", bp.AmountIn.Offset)
	}
	data, _ := bp.Template.Data()
	if len(data) != 25 || data[24] != swapModeExactIn {
		t.Fatalf("bad data %v", data)
	}
	if binary.LittleEndian.Uint64(data[8:16]) != 0 {
		t.Fatal("amount should be zero in template")
	}
	if got := len(bp.Template.Accounts()); got != 14 {
		t.Fatalf("accounts=%d want 14", got)
	}
}

func TestTwoVenuePathCompiles(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	tokenProg := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	ataAB := solana.NewWallet().PublicKey()
	ataBC := solana.NewWallet().PublicKey()
	ataA := solana.NewWallet().PublicKey()

	ray, err := raydiumcpmm.NewExactIn(raydiumcpmm.Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: raydiumcpmm.PoolState{
			ConfigID: solana.NewWallet().PublicKey(),
			VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
			MintA: mintA, MintB: mintB, MintProgramA: tokenProg, MintProgramB: tokenProg,
			ObservationID: solana.NewWallet().PublicKey(),
		},
		InputMint: mintA, OutputMint: mintB,
		UserInputATA: ataA, UserOutputATA: ataAB,
	})
	if err != nil {
		t.Fatal(err)
	}
	damm, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: PoolState{
			MintA: mintB, MintB: mintC,
			VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
		},
		InputMint: mintB, OutputMint: mintC,
		UserInputATA: ataAB, UserOutputATA: ataBC,
		TokenProgramA: tokenProg, TokenProgramB: tokenProg,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(1).
		Hop(ray).
		Hop(damm).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 5 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}
