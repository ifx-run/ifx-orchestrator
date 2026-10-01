package amountflow

import (
	"testing"

	"github.com/ifx-run/ifx-orchestrator/hop"
)

func TestPathFull(t *testing.T) {
	edges := []hop.RouteEdge{
		{From: 0, To: 1, Split: hop.Full()},
		{From: 1, To: 2, Split: hop.Full()},
	}
	f, err := New(1_000_000, 3, edges)
	if err != nil {
		t.Fatal(err)
	}
	s0, err := f.StartStep(0, 1, hop.Full())
	if err != nil {
		t.Fatal(err)
	}
	if s0.AmountIn != 1_000_000 {
		t.Fatalf("got %d", s0.AmountIn)
	}
	if err := s0.Complete(900_000); err != nil {
		t.Fatal(err)
	}
	s1, err := f.StartStep(1, 2, hop.Full())
	if err != nil {
		t.Fatal(err)
	}
	if s1.AmountIn != 900_000 {
		t.Fatalf("got %d", s1.AmountIn)
	}
	if err := s1.Complete(800_000); err != nil {
		t.Fatal(err)
	}
	if f.NodeTotal(2) != 800_000 {
		t.Fatalf("end total %d", f.NodeTotal(2))
	}
}

func TestSplitRemainder(t *testing.T) {
	// node0 --3000--> node1
	//      --7000--> node2  (last edge takes remainder of current)
	// both feed node3? Actually Exact end node has no out. Simpler diamond:
	// 0 -a-> 1, 0 -b-> 2, then we'd need 1->3 and 2->3. For remainder test only out-edges from 0:
	edges := []hop.RouteEdge{
		{From: 0, To: 1, Split: hop.MustPartial(3000)},
		{From: 0, To: 2, Split: hop.MustPartial(7000)},
	}
	// end nodes 1 and 2 — but New requires single end node at nodesCount-1.
	// Use: 0 splits to 1 and 2, then 1->3 Full, 2->3 Full.
	edges = []hop.RouteEdge{
		{From: 0, To: 1, Split: hop.MustPartial(3000)},
		{From: 0, To: 2, Split: hop.MustPartial(7000)},
		{From: 1, To: 3, Split: hop.Full()},
		{From: 2, To: 3, Split: hop.Full()},
	}
	f, err := New(10_000, 4, edges)
	if err != nil {
		t.Fatal(err)
	}
	sA, err := f.StartStep(0, 1, hop.MustPartial(3000))
	if err != nil {
		t.Fatal(err)
	}
	if sA.AmountIn != 3000 { // 10000 * 3000 / 10000
		t.Fatalf("leg A got %d", sA.AmountIn)
	}
	if err := sA.Complete(3000); err != nil {
		t.Fatal(err)
	}
	sB, err := f.StartStep(0, 2, hop.MustPartial(7000))
	if err != nil {
		t.Fatal(err)
	}
	// last out-edge from 0: remainder of current (7000)
	if sB.AmountIn != 7000 {
		t.Fatalf("leg B remainder got %d", sB.AmountIn)
	}
	if err := sB.Complete(7000); err != nil {
		t.Fatal(err)
	}
}

func TestRejectUnbalancedSplit(t *testing.T) {
	edges := []hop.RouteEdge{
		{From: 0, To: 1, Split: hop.MustPartial(3000)},
		{From: 0, To: 1, Split: hop.MustPartial(3000)},
	}
	_, err := New(100, 2, edges)
	if err == nil {
		t.Fatal("expected error for unbalanced split")
	}
}
