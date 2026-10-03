// Package hopconserve asserts per-hop token conservation in compile (Feature).
//
// Register with .Feature(hopconserve.New()). Compile then, for each edge:
//   - output after ≥ before + 1 (SPL ATA or native lamports)
//   - input debit ≥ patched amount_in (skipped when the input port has no ATA and is not native)
//
// Chained hops use SaturatingSub for the forwarded amount after the output assert.
package hopconserve

import "github.com/ifx-run/ifx-orchestrator/feature"

// Feature enables compile-time HopConserve flags.
type Feature struct {
	feature.Base
	skipInput bool
}

// New enables output increase + input debit checks.
func New() *Feature { return &Feature{} }

// SkipInput only asserts the output ATA increased.
func (f *Feature) SkipInput() *Feature {
	f.skipInput = true
	return f
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	cx.HopConserve = true
	cx.HopConserveSkipInput = f.skipInput
	return nil
}
