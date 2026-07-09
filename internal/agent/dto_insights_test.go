package agent

import (
	"testing"

	"omakiten/internal/domain"
)

// TestToInsightsSummaryBoardPerModelV2 pins the v2 per-model contract: the wire
// `sample_size` reports the stamped-event gate input (NOT the dwell-interval
// count, which it aliased in v1), and the partial-state fields (partial,
// first_stamped_at, guards_per_task) plus dwell_samples project through
// faithfully. A consumer must be able to tell a below-gate row from a confident
// one without re-deriving anything.
func TestToInsightsSummaryBoardPerModelV2(t *testing.T) {
	in := domain.Insights{
		PerModel: domain.PerModelInsight{
			HasData: true,
			Models: []domain.ModelContrast{
				{
					AgentModel: "claude-opus-4-8", AvgDwellDays: 1.4, DwellSamples: 6,
					GuardViolations: 3, GuardsPerTask: 1.5,
					SampleSize: 9, FirstStampedAt: "2026-05-01 10:00:00", Partial: false,
				},
				{
					AgentModel: "claude-sonnet-4-6", AvgDwellDays: 0, DwellSamples: 0,
					GuardViolations: 1, GuardsPerTask: 1.0,
					SampleSize: 2, FirstStampedAt: "2026-06-15 08:30:00", Partial: true,
				},
			},
		},
	}

	board := toInsightsSummaryBoard(in)
	if !board.PerModel.HasData {
		t.Fatalf("per-model has_data lost in projection")
	}
	if len(board.PerModel.Models) != 2 {
		t.Fatalf("per-model rows = %d, want 2", len(board.PerModel.Models))
	}

	opus := board.PerModel.Models[0]
	// sample_size must be the stamped-event count (9), NOT DwellSamples (6).
	if opus.SampleSize != 9 {
		t.Fatalf("opus sample_size = %d, want 9 (stamped-event gate input, not dwell count)", opus.SampleSize)
	}
	if opus.DwellSamples != 6 {
		t.Fatalf("opus dwell_samples = %d, want 6", opus.DwellSamples)
	}
	if opus.Partial {
		t.Fatalf("opus partial = true, want false (sample 9 >= gate)")
	}
	if opus.GuardsPerTask != 1.5 {
		t.Fatalf("opus guards_per_task = %.2f, want 1.5", opus.GuardsPerTask)
	}
	if opus.FirstStampedAt != "2026-05-01 10:00:00" {
		t.Fatalf("opus first_stamped_at = %q, want passthrough", opus.FirstStampedAt)
	}

	sonnet := board.PerModel.Models[1]
	if sonnet.SampleSize != 2 {
		t.Fatalf("sonnet sample_size = %d, want 2", sonnet.SampleSize)
	}
	if !sonnet.Partial {
		t.Fatalf("sonnet partial = false, want true (sample 2 < gate)")
	}
	if sonnet.FirstStampedAt != "2026-06-15 08:30:00" {
		t.Fatalf("sonnet first_stamped_at = %q, want passthrough", sonnet.FirstStampedAt)
	}
}

// TestInsightsSummarySchemaVersionV2 pins the frozen-contract version at 2:
// task 1353 repointed per-model sample_size semantics, a breaking change that
// MUST carry a version bump (see the const godoc).
func TestInsightsSummarySchemaVersionV2(t *testing.T) {
	if InsightsSummarySchemaVersion != 2 {
		t.Fatalf("InsightsSummarySchemaVersion = %d, want 2", InsightsSummarySchemaVersion)
	}
}
