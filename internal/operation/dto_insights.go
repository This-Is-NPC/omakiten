package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// InsightsSummarySchemaVersion is the frozen contract marker for the
// insights.summary agent response. The agent self-consults this surface to
// self-correct (reactive → proactive), so its shape is a published API: any
// breaking change to field names, nesting, or semantics MUST bump this
// version and ship a migration note. Additive optional fields keep the same
// version. The number is echoed in every response as `schema_version` so a
// consuming agent can pin behaviour and reject a shape it does not understand.
//
// v2 (task 1353 — per-model partial-state gate): the per-model `sample_size`
// field was REPOINTED — it now reports the count of stamped task events behind
// the row (the partial-state gate input), not the dwell-interval count it
// aliased in v1 (that count moves to the new `dwell_samples` field). Each
// per-model row additionally gains `partial`, `first_stamped_at`, and
// `guards_per_task`. Because `sample_size` changed meaning, this is a breaking
// bump (v1 → v2), not an additive one.
const InsightsSummarySchemaVersion = 2

// toInsightsSummaryBoard projects the internal domain.Insights aggregation onto
// the frozen wire contract (v2), repointing the per-model SampleSize onto the
// stamped-event gate input and carrying the partial-state fields through.
func toInsightsSummaryBoard(in domain.Insights) contract.InsightsSummaryBoard {
	models := make([]contract.InsightsModelSummary, 0, len(in.PerModel.Models))
	for _, m := range in.PerModel.Models {
		models = append(models, contract.InsightsModelSummary{
			AgentModel:      m.AgentModel,
			AvgDwellDays:    m.AvgDwellDays,
			DwellSamples:    m.DwellSamples,
			GuardViolations: m.GuardViolations,
			GuardsPerTask:   m.GuardsPerTask,
			SampleSize:      m.SampleSize,
			FirstStampedAt:  m.FirstStampedAt,
			Partial:         m.Partial,
		})
	}
	return contract.InsightsSummaryBoard{
		StuckDays: in.StuckDays,
		Stuck:     in.Stuck,
		CycleTime: in.CycleTime,
		WIP:       in.WIP,
		Guards:    in.Guards,
		ErrorLoop: in.ErrorLoop,
		PerModel: contract.InsightsPerModelSummary{
			HasData: in.PerModel.HasData,
			Models:  models,
		},
	}
}
