package lifinityv2

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

	pool := PoolState{
		TokenAAccount:     solana.NewWallet().PublicKey(),
		TokenBAccount:     solana.NewWallet().PublicKey(),
		PoolMint:          solana.NewWallet().PublicKey(),
		TokenAMint:        inMint,
		TokenBMint:        outMint,
		FeeAccount:        solana.NewWallet().PublicKey(),
		OracleMainAccount: solana.NewWallet().PublicKey(),
		OracleSubAccount:  solana.NewWallet().PublicKey(),
		OraclePcAccount:   solana.NewWallet().PublicKey(),
	}
	h, err := NewExactIn(Params{
		User: user, PoolID: poolID, Pool: pool,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: inATA, UserOutputATA: outATA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.VenueID() != "lifinity_v2" {
		t.Fatalf("venue %s", h.VenueID())
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != AmountInOffset || bp.MinOut == nil || bp.MinOut.Offset != MinOutOffset {
		t.Fatalf("offsets %+v", bp)
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 24 || binary.LittleEndian.Uint64(data[8:16]) != 0 {
		t.Fatalf("data len=%d amount=%d", len(data), binary.LittleEndian.Uint64(data[8:16]))
	}
	if got := len(bp.Template.Accounts()); got != 13 {
		t.Fatalf("accounts=%d want 13", got)
	}
	auth, err := Authority(ProgramID, poolID)
	if err != nil {
		t.Fatal(err)
	}
	if bp.Template.Accounts()[0].PublicKey != auth {
		t.Fatal("authority mismatch")
	}
}

func TestCompile(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	h, err := NewExactIn(Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: PoolState{
			TokenAAccount: solana.NewWallet().PublicKey(), TokenBAccount: solana.NewWallet().PublicKey(),
			PoolMint: solana.NewWallet().PublicKey(), TokenAMint: inMint, TokenBMint: outMint,
			FeeAccount:        solana.NewWallet().PublicKey(),
			OracleMainAccount: solana.NewWallet().PublicKey(), OracleSubAccount: solana.NewWallet().PublicKey(),
			OraclePcAccount: solana.NewWallet().PublicKey(),
		},
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: solana.NewWallet().PublicKey(), UserOutputATA: solana.NewWallet().PublicKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.New(s, user).AmountIn(1_000).Hop(h).Build(); err != nil {
		t.Fatal(err)
	}
}

func TestDecodePoolStateShort(t *testing.T) {
	if _, err := DecodePoolState(make([]byte, 10)); err == nil {
		t.Fatal("expected short data error")
	}
}
