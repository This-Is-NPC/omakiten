package board

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// boardGoldenBuckets is the four-lane workflow every board recording runs
// against. The names are deliberately uneven in length because the lane header
// prints them upper-cased next to a count.
func boardGoldenBuckets() []domain.Bucket {
	return []domain.Bucket{
		{Key: "backlog", Name: "Backlog"},
		{Key: "dev", Name: "Development"},
		{Key: "review", Name: "Review"},
		{Key: "done", Name: "Done"},
	}
}

// boardGoldenTasks is the fixture snapshot: 29 top-level tasks spread unevenly
// over the four lanes, plus two subtasks that the projection must drop from the
// lanes and count as badges on their parent instead.
//
// Titles alternate between short labels and sentences long enough to wrap two
// or three times inside a card, so a card-height regression moves the lane's
// scroll window rather than only its last row.
func boardGoldenTasks() []domain.Task {
	titles := []string{
		"Record the three-geometry characterization baseline before the layout migration starts",
		"Short one",
		"Teach the lane carousel to keep the focused column on screen when the terminal narrows",
		"Rule audit",
		"Re-home the hardcoded column padding into the shared column frame component",
		"Badge overflow",
		"Pin the card wrap column so a migration diff shows the wrap moving instead of hiding it",
		"Tiny",
		"Carry the selection across a refresh that shrinks the lane the cursor was parked in",
		"Lane header counts",
		"Split the board render cache key so a vertical resize invalidates it on its own",
		"Empty lane copy",
		"Move mode should survive a snapshot rebind that leaves the selected task in place",
		"Priority palette",
		"Keep the horizontal offset stable while the cursor walks a single lane top to bottom",
		"Rule glyphs",
		"Document the lane capacity arithmetic next to the constant it is derived from",
		"Card marker",
		"Fold the duplicated badge assembly into one helper shared by board and table",
		"Scroll hint",
		"Guard the focused-lane sync against a bucket key the workflow no longer declares",
		"Wrap probe",
		"Prove the cursor stays visible across five hundred random moves and resizes",
		"Offset clamp",
		"Reserve the two hint rows the scroll window paints above and below the card slice",
		"Hint wording",
		"Drop the per-lane empty copy into the catalog so every locale pack carries it",
		"Offset probe",
		"Measure the lane header rule against the column inner width at the narrowest terminal",
	}
	buckets := []string{"backlog", "dev", "review", "done"}
	// counts spreads the 29 tasks unevenly: 16 backlog, 7 dev, 4 review, 2 done.
	// The backlog lane is deliberately taller than the tallest recorded
	// viewport (36 body rows against roughly 55 rows of cards) so every lane
	// recording is a window rather than a lane that happened to fit.
	counts := []int{16, 7, 4, 2}
	tasks := make([]domain.Task, 0, len(titles)+2)
	index := 0
	for bucketIndex, count := range counts {
		for i := 0; i < count; i++ {
			id := int64(index + 1)
			tasks = append(tasks, domain.Task{
				ID:        id,
				Title:     titles[index],
				BucketKey: buckets[bucketIndex],
				Priority:  domain.Priority(index%3 + 1),
			})
			index++
		}
	}
	parent := tasks[2].ID
	tasks = append(tasks,
		domain.Task{ID: 900, Title: "subtask kept out of the lanes", BucketKey: "dev", Priority: 2, ParentID: &parent},
		domain.Task{ID: 901, Title: "second subtask kept out of the lanes", BucketKey: "dev", Priority: 2, ParentID: &parent},
	)
	return tasks
}

// boardGoldenDeps is the host snapshot the fixtures bind. It returns a fresh
// value on every call: the recorder materialises each recording twice and
// compares the bytes, which only means anything if the two runs share no state.
func boardGoldenDeps() Deps {
	tasks := boardGoldenTasks()
	return Deps{
		Tasks:    tasks,
		Workflow: domain.Workflow{Buckets: boardGoldenBuckets()},
		Dependencies: []domain.TaskDependency{
			{TaskID: 1, DependsOnTaskID: 13},
			{TaskID: 1, DependsOnTaskID: 14},
			{TaskID: 5, DependsOnTaskID: 13},
			{TaskID: 15, DependsOnTaskID: 1},
			{TaskID: 21, DependsOnTaskID: 15},
		},
		Comments: []domain.Comment{
			{ID: 1, TaskID: 1}, {ID: 2, TaskID: 1}, {ID: 3, TaskID: 1},
			{ID: 4, TaskID: 7}, {ID: 5, TaskID: 13}, {ID: 6, TaskID: 20},
			{ID: 7, TaskID: 24}, {ID: 8, TaskID: 24},
		},
		Priorities: []config.PriorityDefinition{
			{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"},
		},
	}
}

// boardGoldenFilteredDeps is the same snapshot with the priority filter the
// board view settings own switched on, so the lanes carry the filtered
// projection instead of the full one.
func boardGoldenFilteredDeps() Deps {
	deps := boardGoldenDeps()
	deps.View.Filter.Priority = []string{"high"}
	return deps
}

// boardGoldenScenario wires one fixture scenario.
//
// Board's Bind takes the frame alongside the deps — the host re-binds the live
// snapshot before every message and Bind is where the lane cursor is clamped
// and the focused lane's window re-synced — but screenfixture hands Bind only
// the screen. The frame Build was called with is captured here and reused: the
// recorder always calls Build before any Bind at a given geometry, so the
// captured frame is the geometry the recording is being painted at.
func boardGoldenScenario(name string, deps func() Deps, keys []string) screenfixture.Scenario {
	var bound screenhost.Frame
	return screenfixture.Scenario{
		Name: name,
		Build: func(frame screenhost.Frame) screenhost.Screen {
			bound = frame
			return screenfixture.Enter(New().Bind(deps(), frame), frame)
		},
		Bind: func(screen screenhost.Screen) screenhost.Screen {
			return screen.(Screen).Bind(deps(), bound)
		},
		Keys: keys,
	}
}

// FixtureScenarios returns every recorded state for the Tasks › Board screen.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		boardGoldenScenario(
			// Lane zero, walked to its last card: sixteen cards two to five rows
			// tall against 10, 26 and 36 body rows, so the window has left the
			// top of the lane at every geometry. This is the fixture that pins
			// the card wrap, the lane header count and the "▲ N above" hint
			// together.
			"lane-bottom", boardGoldenDeps, []string{"G"},
		),
		boardGoldenScenario(
			// Mid-lane rather than at either end: three page steps and a single
			// one, which lands on a different card at each geometry because a
			// page step is a function of the viewport. Cards above AND below the
			// window, so both scroll hints are recorded.
			"lane-middle", boardGoldenDeps, []string{"pgdown", "pgdown", "pgdown", "j"},
		),
		boardGoldenScenario(
			// The carousel and move mode together. Three lanes right puts the
			// cursor on Done, which is off-screen at 80 and 120 until the column
			// offset slides — so this fixture is the one that records a non-zero
			// horizontal offset and the "lanes N-M of 4" hint at two widths and
			// neither at 200. `m` then arms the move, which swaps the whole
			// footer contract and is a state no other recording carries.
			"move-mode-last-lane", boardGoldenDeps, []string{"right", "right", "right", "j", "m"},
		),
		boardGoldenScenario(
			// The board view's priority filter, which is the screen's own
			// projection mode: only `high` tasks survive, which empties two
			// lanes entirely and leaves the rest short. The cursor sits on the
			// third lane so an emptied lane is on screen next to a populated one
			// at every width.
			"priority-filtered", boardGoldenFilteredDeps, []string{"right", "right", "j"},
		),
	}
}
