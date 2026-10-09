package feature

import "sort"

// Phase orders Features so registration order is not a silent cross-phase dependency.
//
// BeforeRoute / BeforeEdge run in ascending Phase (stable within a phase).
// AfterRoute runs in reverse of that sorted list (Funding repay last).
//
// Within the same Phase, registration order is preserved (stable sort).
type Phase int

const (
	// PhaseFunding — borrow / front capital (FlashRent).
	PhaseFunding Phase = iota
	// PhaseSetup — ATA create, SolFunding wrap, GasSponsored ATA-rent baseline.
	PhaseSetup
	// PhaseRoute — route-local hooks (default), e.g. HopConserve.
	PhaseRoute
	// PhaseSettlement — fees, tips, arb profit assert, sponsor repay.
	PhaseSettlement
)

// Phaser is an optional Feature capability. Features without it are PhaseRoute.
type Phaser interface {
	Phase() Phase
}

// PhaseOf returns f.Phase() when f implements Phaser, else PhaseRoute.
func PhaseOf(f Feature) Phase {
	if p, ok := f.(Phaser); ok {
		return p.Phase()
	}
	return PhaseRoute
}

// SortByPhase returns a new slice sorted by Phase (stable).
func SortByPhase(features []Feature) []Feature {
	out := append([]Feature(nil), features...)
	sort.SliceStable(out, func(i, j int) bool {
		return PhaseOf(out[i]) < PhaseOf(out[j])
	})
	return out
}
