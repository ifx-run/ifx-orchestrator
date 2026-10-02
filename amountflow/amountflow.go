// Package amountflow is an off-chain port of Exact's ExecutionContext.
// It validates route graphs and resolves ExactIn split amounts (topo order assumed).
package amountflow

import (
	"fmt"

	"github.com/ifx-run/ifx-orchestrator/hop"
)

// EdgeView is the minimal edge surface AmountFlow needs.
type EdgeView interface {
	From() hop.NodeID
	To() hop.NodeID
	Split() hop.SplitBps
}

type routeEdgeView struct {
	e hop.RouteEdge
}

func (v routeEdgeView) From() hop.NodeID    { return v.e.From }
func (v routeEdgeView) To() hop.NodeID      { return v.e.To }
func (v routeEdgeView) Split() hop.SplitBps { return v.e.Split }

type nodeState struct {
	inputCount    uint8
	outputCount   uint8
	currentAmount uint64
	totalAmount   uint64
}

func (n *nodeState) inputCompleted() bool  { return n.inputCount == 0 }
func (n *nodeState) outputCompleted() bool { return n.outputCount == 0 }

// AmountFlow tracks node balances and split readiness while walking edges in order.
type AmountFlow struct {
	nodes []nodeState
}

// New builds an AmountFlow for amountIn at node 0 over the given edges.
func New(amountIn uint64, nodesCount int, edges []hop.RouteEdge) (*AmountFlow, error) {
	views := make([]EdgeView, len(edges))
	for i := range edges {
		views[i] = routeEdgeView{e: edges[i]}
	}
	return NewFromViews(amountIn, nodesCount, views)
}

// NewFromViews is the generic constructor (useful in tests).
func NewFromViews(amountIn uint64, nodesCount int, edges []EdgeView) (*AmountFlow, error) {
	if nodesCount < 2 {
		return nil, fmt.Errorf("nodesCount must be >= 2")
	}
	nodes := make([]nodeState, nodesCount)
	nodes[0] = nodeState{currentAmount: amountIn, totalAmount: amountIn}

	splitAcc := map[int]hop.SplitBps{}
	for _, e := range edges {
		from, to := int(e.From()), int(e.To())
		if from < 0 || from >= nodesCount || to < 0 || to >= nodesCount {
			return nil, fmt.Errorf("invalid node index from=%d to=%d", from, to)
		}
		if from == to {
			return nil, fmt.Errorf("edge from==to (%d)", from)
		}
		nodes[from].outputCount++
		nodes[to].inputCount++

		bps := e.Split()
		if bps.IsPartial() {
			if prev, ok := splitAcc[from]; ok {
				sum, err := prev.TryAdd(bps)
				if err != nil {
					return nil, err
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
	}

	for i, n := range nodes {
		switch i {
		case 0:
			if n.inputCount > 0 || n.outputCount == 0 {
				return nil, fmt.Errorf("invalid start node")
			}
		case nodesCount - 1:
			if n.outputCount > 0 || n.inputCount == 0 {
				return nil, fmt.Errorf("invalid end node")
			}
		default:
			if n.inputCount == 0 || n.outputCount == 0 {
				return nil, fmt.Errorf("invalid intermediate node %d", i)
			}
		}
	}
	if len(splitAcc) != 0 {
		return nil, fmt.Errorf("partial splits on a node do not sum to Full")
	}
	return &AmountFlow{nodes: nodes}, nil
}

// StepAmount is the resolved amount_in for one edge.
type StepAmount struct {
	AmountIn uint64
	from     int
	to       int
	flow     *AmountFlow
	consumed bool
}

// StartStep resolves amount_in for an edge and checks readiness.
func (f *AmountFlow) StartStep(from, to hop.NodeID, bps hop.SplitBps) (*StepAmount, error) {
	fi, ti := int(from), int(to)
	if fi < 0 || fi >= len(f.nodes) || ti < 0 || ti >= len(f.nodes) || fi == ti {
		return nil, fmt.Errorf("invalid step nodes %d -> %d", fi, ti)
	}
	in := &f.nodes[fi]
	out := &f.nodes[ti]
	if !in.inputCompleted() || in.outputCompleted() || out.inputCompleted() {
		return nil, fmt.Errorf("invalid step sequence at %d -> %d", fi, ti)
	}

	var amountIn uint64
	switch {
	case bps.IsFull():
		amountIn = in.totalAmount
	default:
		if in.outputCount == 1 {
			amountIn = in.currentAmount // last remaining out-edge takes remainder
		} else {
			amountIn = in.totalAmount * uint64(bps.Bps()) / 10000
		}
	}
	if amountIn > in.currentAmount {
		return nil, fmt.Errorf("insufficient funds: need %d have %d", amountIn, in.currentAmount)
	}
	return &StepAmount{AmountIn: amountIn, from: fi, to: ti, flow: f}, nil
}

// Complete moves amount_in from the input node and credits amountOut to the output node.
// Returns true when the input node has no remaining out-edges (eligible to close).
func (s *StepAmount) Complete(amountOut uint64) (bool, error) {
	if s.consumed {
		return false, fmt.Errorf("step already completed")
	}
	s.consumed = true
	in := &s.flow.nodes[s.from]
	out := &s.flow.nodes[s.to]
	in.currentAmount -= s.AmountIn
	out.currentAmount += amountOut
	in.outputCount--
	out.inputCount--
	if out.inputCount == 0 {
		out.totalAmount = out.currentAmount
	}
	return in.outputCount == 0, nil
}

// NodeCurrent returns the simulated current amount at a node (for tests).
func (f *AmountFlow) NodeCurrent(id hop.NodeID) uint64 {
	return f.nodes[int(id)].currentAmount
}

// NodeTotal returns the simulated total amount at a node (for tests).
func (f *AmountFlow) NodeTotal(id hop.NodeID) uint64 {
	return f.nodes[int(id)].totalAmount
}
