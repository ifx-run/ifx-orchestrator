package compile_test

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/compile"
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
	// reset must be first when Frame is used (two-hop chain)
	if plan.Instructions[0].ProgramID() != constants.DefaultProgramID {
		t.Fatalf("first ix should be ifx, got %s", plan.Instructions[0].ProgramID())
	}
	data, err := plan.Instructions[0].Data()
	if err != nil || len(data) == 0 || data[0] != constants.IxDiscResetFrame {
		t.Fatalf("first ifx ix should be reset")
	}
	if consecutiveIfxLets(plan) {
		t.Fatal("consecutive IfxLet instructions")
	}
	if counter.mapCalls != 1 {
		t.Fatalf("MapForwardAmount calls=%d want 1", counter.mapCalls)
	}
	if !counter.beforeRoute || !counter.afterRoute {
		t.Fatalf("feature route hooks not called")
	}
}

func TestPerHopMinOutIncludingZero(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 4096
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	a, b, c := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000).
		HopWithMinOut(mock.New("a", p, a, b, ataB, acc), 0).
		HopWithMinOut(mock.New("b", p, b, c, ataC, acc), 42).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if countIfx(plan) == 0 {
		t.Fatal("two-hop plan should use ifx for the forwarded amount")
	}
	if consecutiveIfxLets(plan) {
		t.Fatal("consecutive IfxLet instructions")
	}
	// Hop0 amount/min_out are compile-time: first non-ifx hop data is baked.
	foundBake := false
	for _, ix := range plan.Instructions {
		if ix.ProgramID() == p {
			data, err := ix.Data()
			if err != nil || len(data) < 16 {
				continue
			}
			if binary.LittleEndian.Uint64(data[0:8]) == 1000 && binary.LittleEndian.Uint64(data[8:16]) == 0 {
				foundBake = true
			}
			if binary.LittleEndian.Uint64(data[8:16]) == 42 {
				foundBake = true
			}
		}
	}
	if !foundBake {
		t.Fatal("expected baked min_out/amount on a hop template")
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
	plan, err := orchestrator.New(s, user).
		AmountIn(100).
		SolIn(hop.SolNative).
		Hop(mock.New("ray", p, hop.WrappedSOLMint, solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), acc).WithInput(wsolATA)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if n := countIfx(plan); n != 0 {
		t.Fatalf("SolIn wrap of compile-time amount must not use ifx, got %d ifx ixs", n)
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

func TestSingleHopZeroCostIfx(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	a, b := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	plan, err := orchestrator.New(nil, user).
		AmountIn(1_000).
		MinAmountOut(7).
		Hop(mock.New("m", p, a, b, ata, acc)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) != 1 {
		t.Fatalf("ix count %d want 1", len(plan.Instructions))
	}
	if plan.Instructions[0].ProgramID() != p {
		t.Fatalf("want venue program, got %s", plan.Instructions[0].ProgramID())
	}
	data, err := plan.Instructions[0].Data()
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(data[0:8]) != 1_000 {
		t.Fatalf("baked amount %d", binary.LittleEndian.Uint64(data[0:8]))
	}
	if binary.LittleEndian.Uint64(data[8:16]) != 7 {
		t.Fatalf("baked min_out %d", binary.LittleEndian.Uint64(data[8:16]))
	}
}

func TestTwoHopRequiresScratch(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	a, b, c := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	_, err := orchestrator.New(nil, user).
		AmountIn(100).
		Hop(mock.New("a", p, a, b, ataB, acc)).
		Hop(mock.New("b", p, b, c, ataC, acc)).
		Build()
	if err == nil {
		t.Fatal("expected scratch required for chained hops")
	}
}

func TestScratchPresentButUnusedHasNoIfx(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	a, b := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	plan, err := orchestrator.New(s, user).
		AmountIn(50).
		Hop(mock.New("m", p, a, b, ata, acc)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if n := countIfx(plan); n != 0 {
		t.Fatalf("unused scratch still emitted %d ifx ixs", n)
	}
}

func countIfx(plan *compile.Plan) int {
	n := 0
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			n++
		}
	}
	return n
}

func consecutiveIfxLets(plan *compile.Plan) bool {
	prevLet := false
	for _, ix := range plan.Instructions {
		if !ix.ProgramID().Equals(constants.DefaultProgramID) {
			prevLet = false
			continue
		}
		data, err := ix.Data()
		if err != nil || len(data) == 0 {
			prevLet = false
			continue
		}
		isLet := data[0] == constants.IxDiscLet
		if isLet && prevLet {
			return true
		}
		prevLet = isLet
	}
	return false
}
