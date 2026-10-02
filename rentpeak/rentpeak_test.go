package rentpeak

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestCreateAndCloseCreatedPeak(t *testing.T) {
	// Linear A(exists) -> B(missing) -> C(missing): create B+C, close B after hop0 → peak = 2*rent
	rent := uint64(100)
	route := hop.Route{
		Nodes: []hop.RouteNode{
			{Mint: pk(), TokenAccount: pk(), Exists: true},
			{Mint: pk(), TokenAccount: pk(), Exists: false},
			{Mint: pk(), TokenAccount: pk(), Exists: false},
		},
		Edges: []hop.RouteEdge{
			{From: 0, To: 1, Split: hop.Full()},
			{From: 1, To: 2, Split: hop.Full()},
		},
	}
	est, err := EstimateForRoute(Params{
		AmountIn: 1_000, Policy: feature.AtaCreateAndCloseCreated,
		Route: route, TokenAccountRent: rent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if est.PeakReserve != 2*rent {
		t.Fatalf("peak=%d want %d (spent=%d returned=%d)", est.PeakReserve, 2*rent, est.TotalSpent, est.TotalReturned)
	}
	if est.TotalSpent != 2*rent || est.TotalReturned != rent {
		t.Fatalf("spent=%d returned=%d", est.TotalSpent, est.TotalReturned)
	}
	if est.NetChange != rent || est.ChangeType != Spent {
		t.Fatalf("net=%d type=%v", est.NetChange, est.ChangeType)
	}
}

func TestCreateOnly(t *testing.T) {
	rent := uint64(50)
	route := hop.Route{
		Nodes: []hop.RouteNode{
			{TokenAccount: pk(), Exists: true},
			{TokenAccount: pk(), Exists: false},
			{TokenAccount: pk(), Exists: false},
		},
		Edges: []hop.RouteEdge{{From: 0, To: 1, Split: hop.Full()}, {From: 1, To: 2, Split: hop.Full()}},
	}
	est, err := EstimateForRoute(Params{AmountIn: 1, Policy: feature.AtaCreateOnly, Route: route, TokenAccountRent: rent})
	if err != nil {
		t.Fatal(err)
	}
	if est.PeakReserve != 2*rent || est.TotalReturned != 0 {
		t.Fatalf("%+v", est)
	}
}

func pk() solana.PublicKey { return solana.NewWallet().PublicKey() }
