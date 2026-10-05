package raydiumcpmm

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestExactInBlueprint(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	poolID := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	inATA := solana.NewWallet().PublicKey()
	outATA := solana.NewWallet().PublicKey()
	tokenProg := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	pool := PoolState{
		ConfigID:      solana.NewWallet().PublicKey(),
		VaultA:        solana.NewWallet().PublicKey(),
		VaultB:        solana.NewWallet().PublicKey(),
		MintA:         inMint,
		MintB:         outMint,
		MintProgramA:  tokenProg,
		MintProgramB:  tokenProg,
		ObservationID: solana.NewWallet().PublicKey(),
	}

	hopImpl, err := NewExactIn(Params{
		User:          user,
		PoolID:        poolID,
		Pool:          pool,
		InputMint:     inMint,
		OutputMint:    outMint,
		UserInputATA:  inATA,
		UserOutputATA: outATA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hopImpl.VenueID() != "raydium_cpmm" {
		t.Fatalf("venue id %s", hopImpl.VenueID())
	}

	bp, err := hopImpl.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut == nil || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("unexpected patch sites: %+v", bp)
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 24 {
		t.Fatalf("data len %d", len(data))
	}
	if binary.LittleEndian.Uint64(data[8:16]) != 0 || binary.LittleEndian.Uint64(data[16:24]) != 0 {
		t.Fatalf("template amounts should be zero")
	}
	if got := len(bp.Template.Accounts()); got != 13 {
		t.Fatalf("accounts=%d want 13", got)
	}
	if bp.Template.ProgramID() != ProgramID {
		t.Fatalf("program %s", bp.Template.ProgramID())
	}
}

func TestExactInCompilesInOrchestrator(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	tokenProg := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	pool := PoolState{
		ConfigID: solana.NewWallet().PublicKey(), VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
		MintA: inMint, MintB: outMint, MintProgramA: tokenProg, MintProgramB: tokenProg,
		ObservationID: solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(), Pool: pool,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := orchestrator.New(s, user).AmountIn(1_000_000).MinAmountOut(1).Hop(h).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 1 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}

func TestDecodePoolStateRoundTripLayout(t *testing.T) {
	// Minimal synthetic account body: disc + fields at documented offsets.
	data := make([]byte, 8+320)
	cfg := solana.NewWallet().PublicKey()
	copy(data[8+0:], cfg[:])
	vaultA := solana.NewWallet().PublicKey()
	copy(data[8+64:], vaultA[:])
	obs := solana.NewWallet().PublicKey()
	copy(data[8+288:], obs[:])
	got, err := DecodePoolState(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ConfigID.Equals(cfg) || !got.VaultA.Equals(vaultA) || !got.ObservationID.Equals(obs) {
		t.Fatalf("decode mismatch: %+v", got)
	}
}
