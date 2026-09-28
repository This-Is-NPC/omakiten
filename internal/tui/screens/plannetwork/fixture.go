package plannetwork

import (
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// planNetworkGoldenPlan is the plan under test. GoalBody is what the `e` editor
// opens on, so it carries a paragraph well past the editor's 70-column inner
// width at the floor and inside its 190-column width at 200.
func planNetworkGoldenPlan() domain.Plan {
	return domain.Plan{
		ID:     12,
		Slug:   "layout-migration",
		Name:   "Terminal layout migration",
		Status: domain.PlanStatusActive,
		GoalBody: strings.Join([]string{
			"Move every screen off the ad-hoc column arithmetic and onto the shared layout kit without changing a single rendered byte.",
			"",
			"The characterization goldens recorded in wave one are the contract: a migration is behavior-preserving exactly when they do not move.",
		}, "\n"),
	}
}

// planNetworkGoldenWaves is the recorded outline: five waves and thirty-four
// tasks, spread across every bucket the row projection branches on (the first
// bucket, the working pipeline in between, and the final bucket) with assignees
// on some rows so the assigned badge renders alongside the ready and done ones.
//
// Done/total counts are derived from the bucket keys rather than written out,
// so a fixture edit cannot leave the wave header disagreeing with its rows.
func planNetworkGoldenWaves() []domain.PlanWaveView {
	waves := []domain.PlanWaveView{
		{Wave: domain.PlanWave{ID: 10, PlanID: 12, Name: "Foundation", Position: 1}, Tasks: []domain.PlanTaskRow{
			// A title well past the Title column at the floor and inside it
			// at 200, so the truncation is part of what the fixtures pin.
			{TaskID: 201, WaveID: 10, Title: "Freeze the screenhost frame contract before any column arithmetic is allowed to move", BucketKey: "done"},
			{TaskID: 202, WaveID: 10, Title: "Extract the shared golden recorder", BucketKey: "done"},
			{TaskID: 203, WaveID: 10, Title: "Wire the bundled English catalog into every fixture frame", BucketKey: "done", AssignedTo: "mira"},
			{TaskID: 204, WaveID: 10, Title: "Pin the three recorded terminal geometries", BucketKey: "review", AssignedTo: "otto"},
			{TaskID: 205, WaveID: 10, Title: "Document the non-vacuity gate", BucketKey: "dev"},
			{TaskID: 206, WaveID: 10, Title: "Record the description reader baseline", BucketKey: "backlog"},
			{TaskID: 207, WaveID: 10, Title: "Record the studio sub-screen baseline", BucketKey: "backlog"},
		}},
		{Wave: domain.PlanWave{ID: 20, PlanID: 12, Name: "Instrumentation", Position: 2}, Tasks: []domain.PlanTaskRow{
			{TaskID: 211, WaveID: 20, Title: "Count the rows every panel spends on chrome", BucketKey: "done"},
			{TaskID: 212, WaveID: 20, Title: "Measure the flex title column against the fixed bucket and deps columns", BucketKey: "review"},
			{TaskID: 213, WaveID: 20, Title: "Trace the scroll offset through the cardlist", BucketKey: "dev", AssignedTo: "priya"},
			{TaskID: 214, WaveID: 20, Title: "Log the filament lane allocations", BucketKey: "backlog"},
			{TaskID: 215, WaveID: 20, Title: "Log the intra-wave rail depth", BucketKey: "backlog"},
			{TaskID: 216, WaveID: 20, Title: "Snapshot the separator junction table", BucketKey: "backlog"},
		}},
		{Wave: domain.PlanWave{ID: 30, PlanID: 12, Name: "Migration", Position: 3}, Tasks: []domain.PlanTaskRow{
			{TaskID: 221, WaveID: 30, Title: "Port the outline table onto the shared column solver and delete the local budget math", BucketKey: "dev", AssignedTo: "otto"},
			{TaskID: 222, WaveID: 30, Title: "Port the wave header row", BucketKey: "dev"},
			{TaskID: 223, WaveID: 30, Title: "Port the task card row", BucketKey: "backlog"},
			{TaskID: 224, WaveID: 30, Title: "Port the goal editor panel", BucketKey: "backlog"},
			{TaskID: 225, WaveID: 30, Title: "Port the assign editor panel", BucketKey: "backlog"},
			{TaskID: 226, WaveID: 30, Title: "Port the dependencies footer", BucketKey: "backlog"},
			{TaskID: 227, WaveID: 30, Title: "Port the next-claimable hint", BucketKey: "backlog"},
			{TaskID: 228, WaveID: 30, Title: "Delete the per-screen padding helpers", BucketKey: "backlog"},
		}},
		{Wave: domain.PlanWave{ID: 40, PlanID: 12, Name: "Verification", Position: 4}, Tasks: []domain.PlanTaskRow{
			{TaskID: 231, WaveID: 40, Title: "Replay every recorded geometry against the ported outline", BucketKey: "backlog"},
			{TaskID: 232, WaveID: 40, Title: "Diff the 80-column fixtures first", BucketKey: "backlog"},
			{TaskID: 233, WaveID: 40, Title: "Diff the 120-column fixtures", BucketKey: "backlog"},
			{TaskID: 234, WaveID: 40, Title: "Diff the 200-column fixtures", BucketKey: "backlog"},
			{TaskID: 235, WaveID: 40, Title: "Explain every surviving byte of difference in the migration note", BucketKey: "backlog"},
			{TaskID: 236, WaveID: 40, Title: "Re-run the determinism proof", BucketKey: "backlog"},
		}},
		{Wave: domain.PlanWave{ID: 50, PlanID: 12, Name: "Rollout", Position: 5}, Tasks: []domain.PlanTaskRow{
			{TaskID: 241, WaveID: 50, Title: "Land the ported outline behind the existing key model", BucketKey: "backlog"},
			{TaskID: 242, WaveID: 50, Title: "Land the ported editors", BucketKey: "backlog"},
			{TaskID: 243, WaveID: 50, Title: "Retire the compatibility shims", BucketKey: "backlog"},
			{TaskID: 244, WaveID: 50, Title: "Refresh the architecture note", BucketKey: "backlog"},
			{TaskID: 245, WaveID: 50, Title: "Close the umbrella", BucketKey: "backlog"},
			{TaskID: 246, WaveID: 50, Title: "Archive the migration branch", BucketKey: "backlog"},
			{TaskID: 247, WaveID: 50, Title: "Announce the new layout contract to every screen owner before the next cohort starts", BucketKey: "backlog"},
		}},
	}
	for i, wave := range waves {
		done := 0
		for _, task := range wave.Tasks {
			if task.BucketKey == planNetworkGoldenFinalBucket {
				done++
			}
		}
		waves[i].DoneCount, waves[i].TotalCount = done, len(wave.Tasks)
	}
	return waves
}

const (
	planNetworkGoldenFirstBucket = "backlog"
	planNetworkGoldenFinalBucket = "done"

	// planNetworkGoldenActiveWaveID is the wave Open parks the cursor on.
	// The host opens the network on the plan's active wave, so a fixture
	// that entered at row 0 would record a state the host does not produce.
	planNetworkGoldenActiveWaveID = 30

	// planNetworkGoldenNextClaimable is the task the next-claimable hint
	// names. A zero id drops the hint line entirely, which would cost the
	// fixtures a row of the screen's own chrome.
	planNetworkGoldenNextClaimable = 214
)

// planNetworkGoldenDeps carries both edge kinds because they route through
// completely different renderers: intra-wave edges become the ├─ / └─ / │ rail
// prefix inside a wave, cross-wave edges become the left-margin filament lanes
// whose count widens the whole table. A fixture with only one kind would leave
// half the outline's horizontal budget unrecorded.
func planNetworkGoldenDeps() []domain.TaskDependency {
	return []domain.TaskDependency{
		// Intra-wave: a two-level rail tree inside Foundation, plus a flat
		// pair inside Migration.
		{TaskID: 202, DependsOnTaskID: 201},
		{TaskID: 203, DependsOnTaskID: 201},
		{TaskID: 204, DependsOnTaskID: 202},
		{TaskID: 222, DependsOnTaskID: 221},
		{TaskID: 223, DependsOnTaskID: 221},
		// Cross-wave: overlapping ranges, so the lane allocator has to open
		// a second lane, and a hub whose three dependents share one lane.
		{TaskID: 212, DependsOnTaskID: 201},
		{TaskID: 221, DependsOnTaskID: 212},
		{TaskID: 231, DependsOnTaskID: 221},
		{TaskID: 241, DependsOnTaskID: 221},
		{TaskID: 242, DependsOnTaskID: 231},
	}
}

func planNetworkGoldenPayload() Payload {
	waves := planNetworkGoldenWaves()
	done, total := 0, 0
	for _, wave := range waves {
		done, total = done+wave.DoneCount, total+wave.TotalCount
	}
	return Payload{
		Show: domain.PlanShow{
			Plan:         planNetworkGoldenPlan(),
			Waves:        waves,
			DoneCount:    done,
			TotalCount:   total,
			ActiveWaveID: planNetworkGoldenActiveWaveID,
			Dependencies: planNetworkGoldenDeps(),
		},
		NextClaimableID: planNetworkGoldenNextClaimable,
		FirstBucket:     planNetworkGoldenFirstBucket,
		FinalBucket:     planNetworkGoldenFinalBucket,
	}
}

// planNetworkGoldenBuild materialises the screen the way the host does — Open
// on a freshly loaded payload, then the LifecycleEnter that syncs the scroll
// against the live geometry. Without Enter the outline paints its entry cursor
// against an unclamped window.
func planNetworkGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Open(planNetworkGoldenPayload()), frame)
}

// planNetworkGoldenEmptyBuild is the no-waves plan. The render short-circuits
// into a two-line panel before any of the table arithmetic runs, which is a
// genuinely separate layout and the one the retired legacy fixture held.
func planNetworkGoldenEmptyBuild(frame screenhost.Frame) screenhost.Screen {
	payload := Payload{Show: domain.PlanShow{Plan: domain.Plan{ID: 7, Slug: planNetworkGoldenEmptySlug}}}
	return screenfixture.Enter(New().Open(payload), frame)
}

// planNetworkGoldenEmptySlug is printed twice by the no-waves branch — once in
// the header line and once in the body — so it is the whole of what that
// fixture's bytes carry beyond chrome.
const planNetworkGoldenEmptySlug = "unstarted-migration"

// planNetworkGoldenTyped is the text the editors are recorded carrying. It is
// long enough to wrap inside the editor's inner width at the 80-column floor
// and to sit on one line at 200.
const planNetworkGoldenTyped = "  Reviewed against the recorded baseline; every surviving diff is explained in the migration note."

// FixtureScenarios returns every recorded state for the Plan Network screen.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name:  "outline",
			Build: planNetworkGoldenBuild,
			Keys:  []string{"j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j"},
		},
		{
			Name:  "goal-editor",
			Build: planNetworkGoldenBuild,
			Keys:  []string{"e", planNetworkGoldenTyped},
		},
		{
			Name:  "assign-editor",
			Build: planNetworkGoldenBuild,
			Keys:  []string{"j", "c", "renata"},
		},
		{
			Name:  "empty-plan",
			Build: planNetworkGoldenEmptyBuild,
		},
	}
}
