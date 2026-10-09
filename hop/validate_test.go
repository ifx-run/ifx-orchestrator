package hop_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
)

func TestValidateLinearOK(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	a, b, c := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	r := hop.Route{
		Nodes: []hop.RouteNode{{Mint: a}, {Mint: b, TokenAccount: ataB}, {Mint: c, TokenAccount: ataC}},
		Edges: []hop.RouteEdge{
			{From: 0, To: 1, Split: hop.Full(), Hop: mock.New("a", p, a, b, ataB, acc)},
			{From: 1, To: 2, Split: hop.Full(), Hop: mock.New("b", p, b, c, ataC, acc)},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSourceSplitOK(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	a, b, c, d := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC, ataD := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	r := hop.Route{
		Nodes: []hop.RouteNode{
			{Mint: a}, {Mint: b, TokenAccount: ataB}, {Mint: c, TokenAccount: ataC}, {Mint: d, TokenAccount: ataD},
		},
		Edges: []hop.RouteEdge{
			{From: 0, To: 1, Split: hop.MustPartial(3000), Hop: mock.New("a", p, a, b, ataB, acc)},
			{From: 0, To: 2, Split: hop.MustPartial(7000), Hop: mock.New("b", p, a, c, ataC, acc)},
			{From: 1, To: 3, Split: hop.Full(), Hop: mock.New("c", p, b, d, ataD, acc)},
			{From: 2, To: 3, Split: hop.Full(), Hop: mock.New("d", p, c, d, ataD, acc)},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsMidGraphPartial(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	a, b, c, d := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC, ataD := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	r := hop.Route{
		Nodes: []hop.RouteNode{
			{Mint: a}, {Mint: b, TokenAccount: ataB}, {Mint: c, TokenAccount: ataC}, {Mint: d, TokenAccount: ataD},
		},
		Edges: []hop.RouteEdge{
			{From: 0, To: 1, Split: hop.Full(), Hop: mock.New("a", p, a, b, ataB, acc)},
			{From: 1, To: 2, Split: hop.MustPartial(3000), Hop: mock.New("b", p, b, c, ataC, acc)},
			{From: 1, To: 3, Split: hop.MustPartial(7000), Hop: mock.New("c", p, b, d, ataD, acc)},
		},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected mid-graph Partial reject")
	}
}

func TestValidateRejectsMidGraphFanIn(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	a, b, c, d := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC, ataD := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true}}
	// 0→1, 0→2, 1→3, 2→3 is sink fan-in (OK). Mid fan-in: 0→1, 0→2, 1→2, 2→3
	r := hop.Route{
		Nodes: []hop.RouteNode{
			{Mint: a}, {Mint: b, TokenAccount: ataB}, {Mint: c, TokenAccount: ataC}, {Mint: d, TokenAccount: ataD},
		},
		Edges: []hop.RouteEdge{
			{From: 0, To: 1, Split: hop.MustPartial(5000), Hop: mock.New("a", p, a, b, ataB, acc)},
			{From: 0, To: 2, Split: hop.MustPartial(5000), Hop: mock.New("b", p, a, c, ataC, acc)},
			{From: 1, To: 2, Split: hop.Full(), Hop: mock.New("c", p, b, c, ataC, acc)},
			{From: 2, To: 3, Split: hop.Full(), Hop: mock.New("d", p, c, d, ataD, acc)},
		},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected mid-graph fan-in reject")
	}
}
