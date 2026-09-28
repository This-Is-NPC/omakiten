package stats

import (
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// This file records the characterization baseline for Stats › General: two
// bordered summary tables (project totals + token budget) stacked above a
// per-AI-model breakdown panel whose kicker carries the 7d / 30d / all picker.
//
// What a layout change can break here is threefold, and the recordings are
// chosen to pin all three:
//
//   - THE RESPONSIVE SUMMARY BLOCK. Two 43-column tables go side by side only
//     when the panel affords 88 columns, so the 80x24 fixture stacks them and
//     the 120x40 / 200x50 fixtures sit them beside each other. Every recording
//     carries that split; it is the one thing in this screen that genuinely
//     re-lays-out with width.
//   - THE PICKER AND ITS DATASET. The picker is the screen's only sub-tab, and
//     it is bound to the model table beneath it. Each period is recorded on its
//     own, driven with the real arrow keys, against a dataset that DIFFERS per
//     period — so a fixture in which the table stopped tracking the chip is a
//     visible diff rather than an invisible one.
//   - THE COLUMN GRID. The six fixed columns (26/8/8/9/8/7) have to hold a model
//     id past its truncation ceiling, a six-figure count, an unmeasurable ratio
//     rendering as an em-dash rather than a misleading 0%, and the rule + total
//     row that close the table.
//
// THE 80x24 RECORDINGS LOST THEIR SUMMARY BLOCK, and that is the #2441 fix
// rather than a dropped block. At the 80-column floor MergeNarrow folds the two
// 43-column tables into a single 17-row stack, against a HostBox of 14 rows
// (BodyRows=15 less the leading blank). After the screenlayout migration the
// summary is outer chrome (plannetwork-style) above a ScrollItems model
// section with MinRows=3: floors 17+3=20 > HostBox 14, so the budget caption
// yields. The screen exists to show the model/period table; the budget caption
// is supplemental. The model table is windowed by the arranger with live scroll
// hints. The 120x40 and 200x50 recordings are unchanged: there the terminal
// affords both.
//
// DETERMINISM. Nothing on this screen reads a clock: the only date it prints is
// MetricsSummary.Since, which is a pinned literal per period, and every count,
// ratio and token figure is a fixed value in the fixture below. The per-model
// bucket maps are only ever indexed by a constant key, never ranged over, so no
// map iteration order reaches a rendered row.

// statsGoldenSummaries is the per-period dataset the fixture service answers
// with. The three periods deliberately differ in shape as well as in numbers —
// two models at 7d, four at 30d, seven at all — so a recording that lost its
// binding to the picker cannot look plausible.
func statsGoldenSummaries() map[string]domain.MetricsSummary {
	buckets := func(recorded, researched, solutions int) map[domain.EventMetricBucket]int {
		return map[domain.EventMetricBucket]int{
			domain.MetricBucketErrorRecorded:    recorded,
			domain.MetricBucketErrorsResearched: researched,
			domain.MetricBucketSolutionAdded:    solutions,
		}
	}
	return map[string]domain.MetricsSummary{
		"7d": {
			Period: "7d",
			Since:  "2026-07-31",
			ByModel: []domain.AgentMetrics{
				{AgentModel: "anthropic/claude-opus-5", SessionCorrelatedSample: 4, SearchBeforeRecordRatio: 0.5, LikeRate: 0.25, Buckets: buckets(6, 3, 4)},
				// No correlated sample and no solution: BOTH ratio columns must
				// render an em-dash rather than a misleading 0%.
				{AgentModel: "openai/gpt-5-codex", Buckets: buckets(2, 0, 0)},
			},
			Total: domain.AgentMetrics{AgentModel: "total", SessionCorrelatedSample: 4, SearchBeforeRecordRatio: 0.5, LikeRate: 0.25, Buckets: buckets(8, 3, 4)},
		},
		"30d": {
			Period: "30d",
			Since:  "2026-07-08",
			ByModel: []domain.AgentMetrics{
				// 27 cells against a 26-cell column: records the ellipsis.
				{AgentModel: "anthropic/claude-opus-5[1m]", SessionCorrelatedSample: 96, SearchBeforeRecordRatio: 0.77, LikeRate: 0.61, Buckets: buckets(118, 74, 41)},
				{AgentModel: "claude-sonnet-4-6", SessionCorrelatedSample: 30, SearchBeforeRecordRatio: 0.7, LikeRate: 0.5, Buckets: buckets(43, 21, 12)},
				// Measurable search ratio, no solutions: only the like column
				// falls back to the em-dash.
				{AgentModel: "openai/gpt-5-codex", SessionCorrelatedSample: 5, SearchBeforeRecordRatio: 0.4, Buckets: buckets(9, 2, 0)},
				// 31 cells: the widest id in the fixture.
				{AgentModel: "local/qwen-3-coder-30b-instruct", Buckets: buckets(4, 0, 0)},
			},
			Total: domain.AgentMetrics{AgentModel: "total", SessionCorrelatedSample: 131, SearchBeforeRecordRatio: 0.74, LikeRate: 0.58, Buckets: buckets(174, 97, 53)},
		},
		"all": {
			Period: "all",
			Since:  "2025-11-02",
			ByModel: []domain.AgentMetrics{
				{AgentModel: "anthropic/claude-opus-5[1m]", SessionCorrelatedSample: 110455, SearchBeforeRecordRatio: 0.75, LikeRate: 0.63, Buckets: buckets(128704, 96331, 41208)},
				{AgentModel: "claude-sonnet-4-6", SessionCorrelatedSample: 35008, SearchBeforeRecordRatio: 0.8, LikeRate: 0.55, Buckets: buckets(41022, 28110, 9004)},
				{AgentModel: "openai/gpt-5-codex", SessionCorrelatedSample: 9001, SearchBeforeRecordRatio: 0.32, LikeRate: 0.41, Buckets: buckets(12877, 4102, 1330)},
				{AgentModel: "google/gemini-3-pro-preview", SessionCorrelatedSample: 4880, SearchBeforeRecordRatio: 0.21, LikeRate: 0.38, Buckets: buckets(6210, 1044, 402)},
				{AgentModel: "local/qwen-3-coder-30b-instruct", Buckets: buckets(3145, 0, 0)},
				{AgentModel: "mistral/devstral-2", SessionCorrelatedSample: 700, SearchBeforeRecordRatio: 0.44, LikeRate: 0.09, Buckets: buckets(902, 311, 44)},
				{AgentModel: "unknown", Buckets: buckets(55, 0, 0)},
			},
			// The column sums of the seven rows above, so the total row is a
			// reading rather than an unrelated literal.
			Total: domain.AgentMetrics{AgentModel: "total", SessionCorrelatedSample: 160044, SearchBeforeRecordRatio: 0.71, LikeRate: 0.6, Buckets: buckets(192915, 129898, 51988)},
		},
	}
}

// statsGoldenTotals is the host-owned bundle projection the summary block
// reports. The figures are wide enough that the value column is exercised and
// the token estimate sits under its ceiling, so the `[BUDGET EXCEEDED]` badge is
// absent from every recording but the one that records it.
func statsGoldenTotals() Totals {
	return Totals{
		Tasks:    2421,
		Comments: 18337,
		Tags:     96,
		Tokens:   domain.TokenMetrics{EstimatedTotal: 1284000, MaxTokens: 2000000},
	}
}

// statsGoldenDeps is the host snapshot a recording is bound to.
func statsGoldenDeps(totals Totals) Deps {
	return Deps{
		Available: true,
		Totals:    totals,
	}
}

// statsGoldenPopulatedDeps is the standard snapshot: the period-keyed service
// over the standard totals.
func statsGoldenPopulatedDeps() Deps {
	return statsGoldenDeps(statsGoldenTotals())
}

// statsGoldenBuild materialises a screen on `deps` FRESH — a new value with
// NOTHING loaded — and issues the LifecycleEnter the host issues before the
// first paint. Nothing is pre-applied on purpose: every recording reaches its
// dataset by pressing the picker, so each fixture is evidence that the chip and
// the table beneath it are still bound to each other.
func statsGoldenBuild(deps Deps, summaries map[string]domain.MetricsSummary) func(frame screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New().Bind(deps).Apply(Payload{Summary: summaries[DefaultPeriod]}), frame)
	}
}

// statsGoldenBind re-applies the host-owned deps between messages, exactly as
// the host rebuilds them on every dispatch — a bundle swap or a fresh token
// count lands the same way.
func statsGoldenBind(deps Deps, summaries map[string]domain.MetricsSummary) func(screenhost.Screen) screenhost.Screen {
	return func(screen screenhost.Screen) screenhost.Screen {
		s := screen.(Screen).Bind(deps)
		if s.Summary().Period == s.Period() {
			return s
		}
		return s.Apply(Payload{Summary: summaries[s.Period()]})
	}
}

func statsFixtureScenario(name string, deps Deps, summaries map[string]domain.MetricsSummary, keys []string) screenfixture.Scenario {
	return screenfixture.Scenario{
		Name:  name,
		Build: statsGoldenBuild(deps, summaries),
		Bind:  statsGoldenBind(deps, summaries),
		Keys:  keys,
	}
}

// FixtureScenarios returns every recorded state for Stats › General.
func FixtureScenarios() []screenfixture.Scenario {
	populated := statsGoldenPopulatedDeps()
	populatedSummaries := statsGoldenSummaries()
	exceeded := statsGoldenDeps(Totals{
		Tasks:    2421,
		Comments: 18337,
		Tags:     96,
		Tokens:   domain.TokenMetrics{EstimatedTotal: 2140355, MaxTokens: 2000000, Truncated: true},
	})
	empty := statsGoldenDeps(statsGoldenTotals())
	emptySummaries := map[string]domain.MetricsSummary{}

	return []screenfixture.Scenario{
		statsFixtureScenario(
			// The picker one step BACK from its default, on the narrowest
			// dataset: two model rows, one of which cannot measure either ratio
			// and renders both as em-dashes.
			"period-7d", populated, populatedSummaries, []string{"left"},
		),
		statsFixtureScenario(
			// The picker one step FORWARD, on the widest dataset: seven model
			// rows, six-figure counts against the 8-cell count columns, two ids
			// past the 26-cell ceiling, and a total row that is the column sum of
			// the rows above it.
			"period-all", populated, populatedSummaries, []string{"right"},
		),
		statsFixtureScenario(
			// A full forward cycle back onto 30d. It renders where the default
			// renders, but it gets there through three reloads — so this is the
			// fixture that records the wraparound AND the middle dataset, and its
			// assertion is what separates "cycled home" from "never moved".
			"period-30d-wrapped", populated, populatedSummaries, []string{"right", "right", "right"},
		),
		statsFixtureScenario(
			// The widest dataset with the window driven to its ceiling. At the
			// 80-column floor the summary block has yielded and seven model rows
			// do not fit the remainder, so this is the recording that pins the
			// scrolled window and its "▲ N above" hint; at 120 and 200 the table
			// fits whole and `G` is a no-op, which is the point — the same key
			// has to be safe on a table that does not overflow.
			"model-scrolled", populated, populatedSummaries, []string{"right", "G"},
		),
		statsFixtureScenario(
			// The token block over its ceiling: the max row appears and the
			// error-styled `[BUDGET EXCEEDED]` badge takes a row of its own,
			// which grows the tokens table by one row relative to every other
			// recording — the case where the two summary tables stop being the
			// same height.
			"budget-exceeded", exceeded, populatedSummaries, []string{"left"},
		),
		statsFixtureScenario(
			// A wired-but-silent metrics port. The summary block still reports
			// the host's bundle totals, while the model panel collapses to its
			// empty-state hint with no rule, no total row and no since note —
			// a different panel shape, not a shorter version of the same one.
			"empty-dataset", empty, emptySummaries, []string{"right"},
		),
	}
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	if id == screenhost.StatsGeneral {
		return FixtureScenarios()
	}
	return nil
}
