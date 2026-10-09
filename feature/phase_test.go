package feature_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/flashrent"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
)

func TestSortByPhaseFlashBeforeAta(t *testing.T) {
	ata := feature.WithAta(feature.AtaUseOnly)
	fr := flashrent.Auto()
	tip := mevtip.New(solana.NewWallet().PublicKey(), 1)
	// Deliberately reverse registration order.
	sorted := feature.SortByPhase([]feature.Feature{tip, ata, fr})
	if feature.PhaseOf(sorted[0]) != feature.PhaseFunding {
		t.Fatalf("want Funding first, got %d", feature.PhaseOf(sorted[0]))
	}
	if feature.PhaseOf(sorted[1]) != feature.PhaseSetup {
		t.Fatalf("want Setup second, got %d", feature.PhaseOf(sorted[1]))
	}
	if feature.PhaseOf(sorted[2]) != feature.PhaseSettlement {
		t.Fatalf("want Settlement last, got %d", feature.PhaseOf(sorted[2]))
	}
}
