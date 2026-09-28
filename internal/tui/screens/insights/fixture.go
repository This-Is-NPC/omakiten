package insights

import (
	"fmt"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// insightsGoldenWorkflow resolves the bucket ids the stuck and WIP readings
// print. It carries a deliberately long bucket name ("Review and handoff") so
// the two callsites can be told apart in the fixture: the stuck reading prints a
// bucket label in full, while the WIP reading truncates it to 12 cells. Bucket
// 99 is deliberately ABSENT so the `#<id>` fallback for a historical bucket is
// recorded too.
func insightsGoldenWorkflow() domain.Workflow {
	return domain.Workflow{Buckets: []domain.Bucket{
		{ID: 1, Name: "Backlog"},
		{ID: 2, Name: "Development"},
		{ID: 3, Name: "Review and handoff"},
		{ID: 4, Name: "Blocked"},
		{ID: 5, Name: "Done"},
	}}
}

// insightsGoldenDeps is the host snapshot every recording is bound to. The
// service is a stub: the render path reads the cached reading and never queries,
// so the port only has to be non-nil for the screen to leave its "unavailable"
// branch.
func insightsGoldenDeps() Deps {
	return Deps{Available: true}
}

func insightsGoldenBucketNames() []domain.Bucket {
	return insightsGoldenWorkflow().Buckets
}

// insightsGoldenModels builds `count` confident per-model rows. The names are
// long enough that the last few cross the renderer's 24-cell ceiling and record
// the ellipsis, and the dwell / guard figures vary per row so a fixture in which
// a column went constant is visible on sight.
func insightsGoldenModels(count int) []domain.ModelContrast {
	models := make([]domain.ModelContrast, 0, count)
	for i := 0; i < count; i++ {
		models = append(models, domain.ModelContrast{
			AgentModel:      fmt.Sprintf("anthropic/claude-opus-5-%02d", i),
			AvgDwellDays:    0.4 + float64(i)*0.35,
			DwellSamples:    3 + i,
			GuardViolations: i * 2,
			GuardsPerTask:   float64(i) * 0.15,
			SampleSize:      12 + i,
			FirstStampedAt:  "2026-01-04 07:15:00",
		})
	}
	return models
}

// insightsGoldenReading is the fully populated board: all six sub-insights carry
// data, and the body runs to 65 lines so it overflows the tallest recorded
// terminal (a 200x50 frame budgets 38 body rows) and the recorded frame is a
// genuine window rather than the whole of it.
func insightsGoldenReading() domain.Insights {
	return domain.Insights{
		StuckDays: 7,
		Stuck: domain.StuckInsight{HasData: true, Tasks: []domain.StuckTask{
			// Over the 40-cell title ceiling: records the ellipsis.
			{TaskID: 2421, BucketID: 2, DaysStuck: 34, Title: "Record 3-width characterization goldens for every remaining uncovered screen"},
			{TaskID: 1833, BucketID: 3, DaysStuck: 21, Title: "Architecture review closeout"},
			{TaskID: 1901, BucketID: 4, DaysStuck: 18, Title: "Installer trust verification parity"},
			{TaskID: 2044, BucketID: 2, DaysStuck: 15, Title: "ClaimNext benchmark ceiling"},
			{TaskID: 2107, BucketID: 5, DaysStuck: 12, Title: "Task Detail ownership cleanup"},
			{TaskID: 2188, BucketID: 3, DaysStuck: 11, Title: "Studio screen extraction"},
			// Bucket 99 is not in the workflow: records the `#99` fallback.
			{TaskID: 2203, BucketID: 99, DaysStuck: 9, Title: "Orphaned bucket task"},
			{TaskID: 2290, BucketID: 2, DaysStuck: 8, Title: "i18n parity across the twenty locale packs"},
		}},
		CycleTime: domain.CycleTimeInsight{HasData: true, Bottleneck: "review-and-handoff", Buckets: []domain.BucketDwell{
			{FromBucket: "backlog", Samples: 18, AvgDwellDays: 3.25},
			{FromBucket: "development", Samples: 22, AvgDwellDays: 1.5},
			// Over the 12-cell from-bucket ceiling.
			{FromBucket: "review-and-handoff", Samples: 14, AvgDwellDays: 6.75},
			{FromBucket: "blocked", Samples: 4, AvgDwellDays: 12.5},
			{FromBucket: "done", Samples: 31, AvgDwellDays: 0.5},
		}},
		WIP: domain.WIPInsight{HasData: true, Buckets: []domain.BucketWIP{
			{BucketID: 2, Count: 14},
			// "Review and handoff" is 18 cells: truncated here, printed whole by
			// the stuck reading above.
			{BucketID: 3, Count: 6},
			{BucketID: 4, Count: 3},
			{BucketID: 99, Count: 1},
		}},
		Guards: domain.GuardInsight{HasData: true, Hotspots: []domain.GuardHotspot{
			{Rule: "comments_tagged", Tag: "self-branch", Hits: 41, Recent7d: 12},
			// rule/tag is 29 cells: over the 24-cell ceiling.
			{Rule: "comments_tagged", Tag: "tests-passing", Hits: 33, Recent7d: 9},
			{Rule: "subtasks_complete", Hits: 18, Recent7d: 4},
			{Rule: "blockers_in", Tag: "done", Hits: 12, Recent7d: 3},
			{Rule: "wave_gate", Hits: 7, Recent7d: 1},
			{Rule: "comments_min", Hits: 5, Recent7d: 0},
		}},
		ErrorLoop: domain.ErrorLoopInsight{HasData: true, Total: 148, Resolved: 121, Open: 27},
		PerModel:  domain.PerModelInsight{HasData: true, Models: insightsGoldenModels(20)},
	}
}

// insightsGoldenMixedReading is the same board with every renderer on its OTHER
// branch: a section with no data at all next to populated ones, a cycle reading
// with no bottleneck line, guard hotspots with no tag (so the label is the bare
// rule), an error loop with nothing open, and per-model rows that are partial,
// dwell-less or rate-less.
//
// It runs to 45 lines, so it still overflows every recorded geometry and the
// three-row offset the recording drives is reachable at all of them.
func insightsGoldenMixedReading() domain.Insights {
	models := make([]domain.ModelContrast, 0, 16)
	for i := 0; i < 16; i++ {
		switch i % 4 {
		case 0:
			// Below the confidence gate: "sample since <date>, N rows", never an
			// average. The stamp is a fixed literal, not a formatted clock read.
			models = append(models, domain.ModelContrast{
				AgentModel:     fmt.Sprintf("openai/gpt-5-mini-%02d", i),
				SampleSize:     1 + i%3,
				FirstStampedAt: "2026-06-20 08:00:00",
				Partial:        true,
			})
		case 1:
			// Above the gate but with no completed dwell: the muted "no dwell"
			// label stands in for the average.
			models = append(models, domain.ModelContrast{
				AgentModel:      fmt.Sprintf("google/gemini-3-pro-%02d", i),
				DwellSamples:    0,
				GuardViolations: i,
				GuardsPerTask:   0,
				SampleSize:      14 + i,
				FirstStampedAt:  "2026-02-11 12:00:00",
			})
		case 2:
			// A confident row with no per-task rate: the guards label carries no
			// "/task" suffix.
			models = append(models, domain.ModelContrast{
				AgentModel:      fmt.Sprintf("anthropic/claude-sonnet-4-6-%02d", i),
				AvgDwellDays:    1.25 + float64(i)*0.2,
				DwellSamples:    2 + i,
				GuardViolations: 0,
				GuardsPerTask:   0,
				SampleSize:      20 + i,
				FirstStampedAt:  "2026-03-09 18:45:00",
			})
		default:
			models = append(models, domain.ModelContrast{
				AgentModel:      fmt.Sprintf("local/qwen-3-coder-%02d", i),
				AvgDwellDays:    5.5 - float64(i)*0.25,
				DwellSamples:    9 + i,
				GuardViolations: 30 - i,
				GuardsPerTask:   2.75,
				SampleSize:      40 + i,
				FirstStampedAt:  "2026-04-22 06:30:00",
			})
		}
	}
	return domain.Insights{
		StuckDays: 14,
		// Nothing stuck: the muted placeholder, never a zero.
		Stuck: domain.StuckInsight{},
		// No bottleneck: the warning line is omitted entirely.
		CycleTime: domain.CycleTimeInsight{HasData: true, Buckets: []domain.BucketDwell{
			{FromBucket: "backlog", Samples: 3, AvgDwellDays: 0.0},
			{FromBucket: "development", Samples: 1, AvgDwellDays: 44.75},
			{FromBucket: "done", Samples: 7, AvgDwellDays: 2.0},
		}},
		WIP: domain.WIPInsight{HasData: true, Buckets: []domain.BucketWIP{
			{BucketID: 4, Count: 1},
			{BucketID: 99, Count: 2},
		}},
		// No tags: the hotspot label is the bare rule name.
		Guards: domain.GuardInsight{HasData: true, Hotspots: []domain.GuardHotspot{
			{Rule: "subtasks_complete", Hits: 2, Recent7d: 0},
			{Rule: "wave_gate", Hits: 1, Recent7d: 0},
			{Rule: "comments_min", Hits: 1, Recent7d: 1},
		}},
		// Everything resolved: the accented headline figure is 0, which is a
		// reading, not an empty state.
		ErrorLoop: domain.ErrorLoopInsight{HasData: true, Total: 9, Resolved: 9, Open: 0},
		PerModel:  domain.PerModelInsight{HasData: true, Models: models},
	}
}

// insightsGoldenBuild materialises a screen on `reading` FRESH: a new value, a
// new deps snapshot, and the LifecycleEnter the host issues before the first
// paint (which is what sizes the scroll window — without it the body paints
// unclamped).
func insightsGoldenBuild(reading domain.Insights) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New().Bind(insightsGoldenDeps()).Apply(Payload{Insights: reading, BucketNames: insightsGoldenBucketNames()}), frame)
	}
}

// insightsGoldenBind re-applies the host-owned deps between messages, exactly as
// the host rebuilds and re-binds them on every dispatch.
func insightsGoldenBind(screen screenhost.Screen) screenhost.Screen {
	return screen.(Screen).Bind(insightsGoldenDeps())
}

// FixtureScenarios returns every recorded state for Stats › Insights.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name:  "board-top",
			Build: insightsGoldenBuild(insightsGoldenReading()),
			Bind:  insightsGoldenBind,
		},
		{
			Name:  "board-scrolled",
			Build: insightsGoldenBuild(insightsGoldenReading()),
			Bind:  insightsGoldenBind,
			// Line steps (not pgdown): after the screenlayout migration the
			// arranger's page is nearly a full item viewport, and pinning the
			// kicker shortens the scrollable run, so one pgdown hits the
			// ceiling at 80×24 and collapses onto board-bottom.
			Keys: []string{"j", "j", "j", "j", "j", "j", "j", "j"},
		},
		{
			Name:  "board-bottom",
			Build: insightsGoldenBuild(insightsGoldenReading()),
			Bind:  insightsGoldenBind,
			Keys:  []string{"G"},
		},
		{
			Name:  "board-mixed",
			Build: insightsGoldenBuild(insightsGoldenMixedReading()),
			Bind:  insightsGoldenBind,
			Keys:  []string{"j", "j", "j"},
		},
		{
			Name:  "board-empty",
			Build: insightsGoldenBuild(domain.Insights{StuckDays: 7}),
			Bind:  insightsGoldenBind,
		},
	}
}
