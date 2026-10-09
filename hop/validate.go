package hop

import "fmt"

// Validate checks graph structure invariants expected by compile.
//
// Edges must be in a valid execution order (caller-supplied topo order):
// each StartStep readiness check in AmountFlow assumes that order.
//
// Current compile support (see also compile docs):
//   - linear Full paths
//   - Partial splits only from the source node (0); mid-graph fan-out needs
//     runtime balance expressions and is rejected until wired
//   - mid-graph fan-in (in-degree > 1 with out-edges) is rejected
func (r Route) Validate() error {
	n := len(r.Nodes)
	if n < 2 {
		return fmt.Errorf("route: need at least 2 nodes")
	}
	if len(r.Edges) == 0 {
		return fmt.Errorf("route: need at least one edge")
	}

	inDeg := make([]int, n)
	outDeg := make([]int, n)
	splitAcc := map[int]SplitBps{}

	for i, e := range r.Edges {
		from, to := int(e.From), int(e.To)
		if from < 0 || from >= n || to < 0 || to >= n {
			return fmt.Errorf("route: edge[%d] invalid nodes %d→%d", i, from, to)
		}
		if from == to {
			return fmt.Errorf("route: edge[%d] from==to (%d)", i, from)
		}
		if e.Hop == nil {
			return fmt.Errorf("route: edge[%d] missing Hop", i)
		}
		inDeg[to]++
		outDeg[from]++

		bps := e.Split
		if bps.IsPartial() {
			if from != 0 {
				return fmt.Errorf("route: edge[%d] Partial split from node %d: only source node 0 is supported until runtime mid-graph split is wired", i, from)
			}
			if prev, ok := splitAcc[from]; ok {
				sum, err := prev.TryAdd(bps)
				if err != nil {
					return fmt.Errorf("route: edge[%d] split: %w", i, err)
				}
				if sum.IsFull() {
					delete(splitAcc, from)
				} else {
					splitAcc[from] = sum
				}
			} else {
				splitAcc[from] = bps
			}
		}

		in, out := e.Hop.Input(), e.Hop.Output()
		if err := in.Validate(); err != nil {
			return fmt.Errorf("route: edge[%d] input: %w", i, err)
		}
		if err := out.Validate(); err != nil {
			return fmt.Errorf("route: edge[%d] output: %w", i, err)
		}
		if !r.Nodes[from].Mint.IsZero() && !r.Nodes[from].Mint.Equals(in.Mint) {
			return fmt.Errorf("route: edge[%d] hop input mint %s != node[%d] mint %s", i, in.Mint, from, r.Nodes[from].Mint)
		}
		if !r.Nodes[to].Mint.IsZero() && !r.Nodes[to].Mint.Equals(out.Mint) {
			return fmt.Errorf("route: edge[%d] hop output mint %s != node[%d] mint %s", i, out.Mint, to, r.Nodes[to].Mint)
		}
		if r.Nodes[from].Native && bps.IsPartial() {
			return fmt.Errorf("route: edge[%d] native SOL node cannot Partial-split; wrap to WSOL first", i)
		}
	}

	if len(splitAcc) != 0 {
		return fmt.Errorf("route: Partial splits on a node do not sum to Full")
	}

	for i := 0; i < n; i++ {
		switch i {
		case 0:
			if inDeg[i] != 0 || outDeg[i] == 0 {
				return fmt.Errorf("route: invalid source node 0 (in=%d out=%d)", inDeg[i], outDeg[i])
			}
		case n - 1:
			if outDeg[i] != 0 || inDeg[i] == 0 {
				return fmt.Errorf("route: invalid sink node %d (in=%d out=%d)", i, inDeg[i], outDeg[i])
			}
		default:
			if inDeg[i] == 0 || outDeg[i] == 0 {
				return fmt.Errorf("route: invalid intermediate node %d (in=%d out=%d)", i, inDeg[i], outDeg[i])
			}
			if inDeg[i] > 1 {
				return fmt.Errorf("route: node %d has fan-in (%d); mid-graph join is not supported until runtime merge is wired", i, inDeg[i])
			}
		}
	}

	// Mint continuity along each edge (hop ports).
	for i, e := range r.Edges {
		if i+1 >= len(r.Edges) {
			break
		}
		// Adjacent edges that chain (prev.To == next.From) must share mint.
		next := r.Edges[i+1]
		if e.To == next.From {
			if !e.Hop.Output().SameMint(next.Hop.Input()) {
				return fmt.Errorf("route: edge[%d]→[%d] mint mismatch %s → %s", i, i+1, e.Hop.Output().Mint, next.Hop.Input().Mint)
			}
		}
	}
	return nil
}
