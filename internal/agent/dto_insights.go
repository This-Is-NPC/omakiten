package agent

import "omakiten/internal/domain"

// InsightsSummarySchemaVersion is the frozen contract marker for the
// insights.summary MCP response. The agent self-consults this surface to
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

// InsightsSummaryInput drives the insights.summary MCP endpoint.
//
// StuckDays parameterises insight 1 (the stuck-task staleness threshold);
// pass 0 (or omit) to take the service default (app.DefaultStuckDays = 7).
// ProjectID > 0 pins the reading to that project explicitly. When omitted
// (0), the reading scopes to the project resolved from the selector (cwd /
// slug / id) — contextual by default, mirroring the TUI screen; the
// cross-project global view only applies when no project resolves either.
//
// This endpoint is read-only and consultivo: it ONLY returns insight data. It
// never moves a task, relaxes a guard, or gates a workflow transition.
//
// ProjectID is the embedded ProjectSelector.ProjectID (promoted), NOT a
// second field: declaring a sibling ProjectID here would collide on the
// `project_id` JSON tag and, worse, the two would diverge — encoding/json
// fills only the shallowest field, so a redeclared outer ProjectID would be
// populated while resolveProject reads the still-zero embedded one. Reusing
// the promoted field keeps decode and resolution reading the same value.
type InsightsSummaryInput struct {
	ProjectSelector
	StuckDays int `json:"stuck_days,omitempty"`
}

// InsightsSummaryResponse is the frozen, versioned output contract.
//
// SchemaVersion pins the shape (see InsightsSummarySchemaVersion). Insights is
// the verbatim domain.Insights aggregation — every sub-report carries its own
// explicit `has_data` flag so a consumer distinguishes "no history to compute
// from" from a genuine zero reading (never a silent zero). Per-model rows
// additionally carry an explicit `sample_size` (stamped-event count) plus a
// `partial` flag and `first_stamped_at` date, so the agent treats a below-gate
// model as not-yet-measured rather than reading a confident average on a tiny n.
//
// Project is a pointer so the global view (no project scope) genuinely omits
// the field instead of emitting a zero-valued object — `omitempty` on a
// struct value is a no-op.
type InsightsSummaryResponse struct {
	SchemaVersion int                  `json:"schema_version"`
	Project       *ProjectSummary      `json:"project,omitempty"`
	Insights      InsightsSummaryBoard `json:"insights"`
}

// InsightsSummaryBoard mirrors domain.Insights field-for-field. It exists as a
// dedicated wire DTO (rather than embedding domain.Insights directly) so the
// frozen MCP contract is decoupled from the internal domain struct: the domain
// type can grow internal fields without leaking onto the published schema, and
// the per-model rows gain the explicit `sample_size` alias the contract
// promises without mutating the domain shape.
type InsightsSummaryBoard struct {
	StuckDays int                     `json:"stuck_days"`
	Stuck     domain.StuckInsight     `json:"stuck"`
	CycleTime domain.CycleTimeInsight `json:"cycle_time"`
	WIP       domain.WIPInsight       `json:"wip"`
	Guards    domain.GuardInsight     `json:"guards"`
	ErrorLoop domain.ErrorLoopInsight `json:"error_loop"`
	PerModel  InsightsPerModelSummary `json:"per_model"`
}

// InsightsPerModelSummary wraps the per-model contrast with its explicit
// has_data flag and rows that carry a frozen `sample_size` field.
type InsightsPerModelSummary struct {
	HasData bool                   `json:"has_data"`
	Models  []InsightsModelSummary `json:"models"`
}

// InsightsModelSummary is one per-model row in the frozen contract (v2).
//
// SampleSize is the count of stamped task events behind the row — the
// partial-state gate input. When it is below domain.MinModelSampleSize the row
// is flagged Partial and FirstStampedAt names the "sample since" date, so a
// consumer treats the row as not-yet-measured rather than reading a confident
// average on a tiny n. DwellSamples is the (separate) number of completed move
// intervals that fed AvgDwellDays. GuardsPerTask is GuardViolations normalised
// by the distinct tasks the model touched, comparable across models of
// different volume.
type InsightsModelSummary struct {
	AgentModel      string  `json:"agent_model"`
	AvgDwellDays    float64 `json:"avg_dwell_days"`
	DwellSamples    int     `json:"dwell_samples"`
	GuardViolations int     `json:"guard_violations"`
	GuardsPerTask   float64 `json:"guards_per_task"`
	SampleSize      int     `json:"sample_size"`
	FirstStampedAt  string  `json:"first_stamped_at"`
	Partial         bool    `json:"partial"`
}

// toInsightsSummaryBoard projects the internal domain.Insights aggregation onto
// the frozen wire contract (v2), repointing the per-model SampleSize onto the
// stamped-event gate input and carrying the partial-state fields through.
func toInsightsSummaryBoard(in domain.Insights) InsightsSummaryBoard {
	models := make([]InsightsModelSummary, 0, len(in.PerModel.Models))
	for _, m := range in.PerModel.Models {
		models = append(models, InsightsModelSummary{
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
	return InsightsSummaryBoard{
		StuckDays: in.StuckDays,
		Stuck:     in.Stuck,
		CycleTime: in.CycleTime,
		WIP:       in.WIP,
		Guards:    in.Guards,
		ErrorLoop: in.ErrorLoop,
		PerModel: InsightsPerModelSummary{
			HasData: in.PerModel.HasData,
			Models:  models,
		},
	}
}
