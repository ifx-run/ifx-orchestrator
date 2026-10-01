package compile_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/router"
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
	plan, err := router.New(s, user).
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

	_, err := router.New(s, user).
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
