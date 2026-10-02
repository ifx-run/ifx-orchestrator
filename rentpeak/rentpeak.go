// Package rentpeak estimates ATA rent peak / net for a route (Exact PdaLamportsBudget).
package rentpeak

import (
	"github.com/ifx-run/ifx-orchestrator/amountflow"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// DefaultTokenAccountRent is rent-exempt lamports for a classic 165-byte SPL token account.
const DefaultTokenAccountRent uint64 = 2_039_280

// ChangeType is the net direction after the transaction.
type ChangeType uint8

const (
	Neutral ChangeType = iota
	Spent
	Returned
)

// Estimate is the rent budget for a route under an AtaPolicy.
type Estimate struct {
	PeakReserve   uint64
	TotalSpent    uint64
	TotalReturned uint64
	NetChange     uint64
	ChangeType    ChangeType
}

// Unchanged is a zero budget.
func Unchanged() Estimate { return Estimate{ChangeType: Neutral} }

func newEstimate(peak, spent, returned uint64) Estimate {
	e := Estimate{PeakReserve: peak, TotalSpent: spent, TotalReturned: returned}
	switch {
	case spent > returned:
		e.ChangeType = Spent
		e.NetChange = spent - returned
	case returned > spent:
		e.ChangeType = Returned
		e.NetChange = returned - spent
	default:
		e.ChangeType = Neutral
	}
	return e
}

type recorder struct {
	peak, spent, returned uint64
}

func (r *recorder) addSpent(amount uint64) {
	r.spent += amount
	if r.spent > r.returned {
		if net := r.spent - r.returned; net > r.peak {
			r.peak = net
		}
	}
}

func (r *recorder) addReturned(amount uint64) {
	r.returned += amount
}

// Params configures a rent-peak estimate.
type Params struct {
	AmountIn         uint64
	Policy           feature.AtaPolicy
	Route            hop.Route
	TokenAccountRent uint64 // zero => DefaultTokenAccountRent
}

// EstimateForRoute walks the graph like Exact's CreateAndClose* budget.
//
// CreateAndCloseCreated: create missing ATAs as edges touch them; when a node's
// out-edges finish, return rent for ATAs created this tx (destination may stay open).
// CreateOnly: create all missing ATAs; no returns (peak = total spent).
// UseOnly / UseAndClose: no creates → peak 0 (UseAndClose returns need live balances).
func EstimateForRoute(p Params) (Estimate, error) {
	rent := p.TokenAccountRent
	if rent == 0 {
		rent = DefaultTokenAccountRent
	}
	switch p.Policy {
	case feature.AtaUseOnly, feature.AtaUseAndClose:
		return Unchanged(), nil
	case feature.AtaCreateOnly:
		var spent uint64
		for _, n := range p.Route.Nodes {
			if !n.Exists && !n.TokenAccount.IsZero() {
				spent += rent
			}
		}
		return newEstimate(spent, spent, 0), nil
	case feature.AtaCreateAndCloseCreated, feature.AtaCreateAndCloseAll:
		return estimateCreateAndClose(p.AmountIn, p.Route, rent, p.Policy == feature.AtaCreateAndCloseAll)
	default:
		return Unchanged(), nil
	}
}

func estimateCreateAndClose(amountIn uint64, route hop.Route, rent uint64, closeAll bool) (Estimate, error) {
	flow, err := amountflow.New(amountIn, len(route.Nodes), route.Edges)
	if err != nil {
		return Estimate{}, err
	}
	rec := recorder{}
	createdRent := make([]uint64, len(route.Nodes))
	exists := make([]bool, len(route.Nodes))
	for i, n := range route.Nodes {
		exists[i] = n.Exists
	}

	for _, edge := range route.Edges {
		to := int(edge.To)
		from := int(edge.From)
		if !exists[to] && !route.Nodes[to].TokenAccount.IsZero() {
			rec.addSpent(rent)
			createdRent[to] = rent
			exists[to] = true
		}
		step, err := flow.StartStep(edge.From, edge.To, edge.Split)
		if err != nil {
			return Estimate{}, err
		}
		inputDone, err := step.Complete(step.AmountIn)
		if err != nil {
			return Estimate{}, err
		}
		if inputDone {
			if createdRent[from] > 0 {
				rec.addReturned(createdRent[from])
				createdRent[from] = 0
				exists[from] = false
			} else if closeAll && route.Nodes[from].Exists {
				rec.addReturned(rent)
			}
		}
	}

	if closeAll {
		end := len(route.Nodes) - 1
		if createdRent[end] > 0 {
			rec.addReturned(createdRent[end])
			createdRent[end] = 0
		} else if route.Nodes[end].Exists {
			rec.addReturned(rent)
		}
	}

	return newEstimate(rec.peak, rec.spent, rec.returned), nil
}
