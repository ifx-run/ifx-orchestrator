package compile_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestMockTwoHopPlanShape(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()
	prog1 := solana.NewWallet().PublicKey()
	prog2 := solana.NewWallet().PublicKey()

	acc := func(p solana.PublicKey) []*solana.AccountMeta {
		return []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}, {PublicKey: p}}
	}

	hop1 := mock.New("mock_a", prog1, mintA, mintB, ataB, acc(ataB))
	hop2 := mock.New("mock_b", prog2, mintB, mintC, ataC, acc(ataC))

	minOut := uint64(1)
	counter := &mapForwardCounter{}
	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(minOut).
		Feature(counter).
		Hop(hop1).
		Hop(hop2).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 5 {
		t.Fatalf("expected several ixs, got %d", len(plan.Instructions))
	}
	// reset must be first
	if plan.Instructions[0].ProgramID() != constants.DefaultProgramID {
		t.Fatalf("first ix should be ifx, got %s", plan.Instructions[0].ProgramID())
	}
	if counter.mapCalls != 1 {
		t.Fatalf("MapForwardAmount calls=%d want 1", counter.mapCalls)
	}
	if !counter.beforeRoute || !counter.afterRoute {
		t.Fatalf("feature route hooks not called")
	}
}

func TestMintMismatch(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	a, b, c := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	d := solana.NewWallet().PublicKey()
	ata1, ata2 := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	_, err := orchestrator.New(s, user).
		AmountIn(100).
		Hop(mock.New("a", p, a, b, ata1, acc)).
		Hop(mock.New("b", p, c, d, ata2, acc)). // c != b
		Build()
	if err == nil {
		t.Fatal("expected mint mismatch")
	}
}

type mapForwardCounter struct {
	feature.Base
	mapCalls    int
	beforeRoute bool
	afterRoute  bool
}

func (m *mapForwardCounter) BeforeRoute(*feature.Ctx) error {
	m.beforeRoute = true
	return nil
}
func (m *mapForwardCounter) AfterRoute(*feature.Ctx) error {
	m.afterRoute = true
	return nil
}
func (m *mapForwardCounter) MapForwardAmount(cx *feature.Ctx, a feature.ForwardAmount) (feature.ForwardAmount, error) {
	m.mapCalls++
	return m.Base.MapForwardAmount(cx, a)
}

func TestNativeThenWSOLChain(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 8192
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	meme := solana.NewWallet().PublicKey()
	usdc := solana.NewWallet().PublicKey()
	wsolATA := solana.NewWallet().PublicKey()
	usdcATA := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000).
		Hop(mock.New("sell", p, meme, hop.WrappedSOLMint, solana.PublicKey{}, acc).WithInput(solana.NewWallet().PublicKey()).WithNativeOut()).
		Hop(mock.New("ray", p, hop.WrappedSOLMint, usdc, usdcATA, acc).WithInput(wsolATA)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var syncNative int
	for _, ix := range plan.Instructions {
		data, err := ix.Data()
		if err != nil || len(data) == 0 {
			continue
		}
		if (ix.ProgramID() == solana.TokenProgramID || ix.ProgramID() == constants.DefaultProgramID) && data[0] == 17 {
			syncNative++
		}
		// ifx CPI wraps Token SyncNative
		if ix.ProgramID() == constants.DefaultProgramID && len(data) > 1 {
			// count SyncNative templates inside remaining accounts / ignore
		}
	}
	if len(plan.Instructions) < 8 {
		t.Fatalf("ix count %d, expected wrap + two hops", len(plan.Instructions))
	}
	_ = syncNative
}

func TestWSOLThenNativeChain(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 8192
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	usdc := solana.NewWallet().PublicKey()
	wsolATA := solana.NewWallet().PublicKey()
	memeATA := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	if _, err := orchestrator.New(s, user).
		AmountIn(1_000).
		Hop(mock.New("ray", p, usdc, hop.WrappedSOLMint, wsolATA, acc).WithInput(solana.NewWallet().PublicKey())).
		Hop(mock.New("buy", p, hop.WrappedSOLMint, solana.NewWallet().PublicKey(), memeATA, acc).WithNativeIn()).
		Build(); err != nil {
		t.Fatal(err)
	}
}

func TestSolOutWSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 8192
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	wsolATA := solana.NewWallet().PublicKey()
	if _, err := orchestrator.New(s, user).
		AmountIn(100).
		SolOut(hop.SolWSOL).
		WSOLAccount(wsolATA).
		Hop(mock.New("sell", p, solana.NewWallet().PublicKey(), hop.WrappedSOLMint, solana.PublicKey{}, acc).
			WithInput(solana.NewWallet().PublicKey()).WithNativeOut()).
		Build(); err != nil {
		t.Fatal(err)
	}
}

func TestSolInNativeWrapsWSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 8192
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	wsolATA := solana.NewWallet().PublicKey()
	if _, err := orchestrator.New(s, user).
		AmountIn(100).
		SolIn(hop.SolNative).
		Hop(mock.New("ray", p, hop.WrappedSOLMint, solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), acc).WithInput(wsolATA)).
		Build(); err != nil {
		t.Fatal(err)
	}
}

func TestSolOutWSOLRejectsNonSOL(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	_, err := orchestrator.New(s, user).
		AmountIn(100).
		SolOut(hop.SolWSOL).
		Hop(mock.New("m", p, solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), acc)).
		Build()
	if err == nil {
		t.Fatal("expected SolOut(WSOL) error")
	}
}
