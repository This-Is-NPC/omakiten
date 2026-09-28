package table

import (
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The Table is one fixed-column row per task inside a scrolling panel, so the
// two things a layout migration moves are the column arithmetic — the title
// column is whatever is left after a 44-cell fixed block, which is 28 cells at
// the 80-column floor, 68 at 120 and 148 at 200 — and the slice window the
// panel paints its rows through.
//
// The fixture therefore carries 44 rows against 9, 25 and 35 body rows (every
// recording is a window, never a whole list) and titles long enough to be
// truncated at 80, truncated at 120 and printed whole at 200, so a single
// recording's three fixtures differ in what they can show rather than only in
// their padding.
//
// The screen also owns a projection: Deps.View carries the filter and the sort
// the host persisted, and the rows the panel prints are the result. Two of the
// three recordings below pin a different projection of the same task set, which
// is the part of Table a card/row migration is most likely to change silently.

// tableGoldenTasks is the fixture snapshot: 44 tasks over four buckets, with a
// title length that alternates between "fits at 80" and "is truncated even at
// 200" so the truncation column is visible in every fixture.
func tableGoldenTasks() []domain.Task {
	buckets := []string{"backlog", "dev", "review", "done"}
	long := []string{
		"Record the three-geometry characterization baseline for every uncovered screen package",
		"Short row",
		"Re-home the fixed 44-cell column block into a shared column budget helper",
		"Rule audit",
		"Prove the title column is the only column that absorbs a width change",
		"Badge counts",
	}
	tasks := make([]domain.Task, 0, 44)
	for i := 0; i < 44; i++ {
		id := int64(i + 1)
		tasks = append(tasks, domain.Task{
			ID:        id,
			Title:     fmt.Sprintf("%02d %s", i+1, long[i%len(long)]),
			BucketKey: buckets[i%len(buckets)],
			Priority:  domain.Priority(i%3 + 1),
			CreatedAt: fmt.Sprintf("2026-01-%02dT09:00:00Z", i%28+1),
		})
	}
	return tasks
}

// tableGoldenDeps is the host snapshot, sorted by title ascending the way the
// persisted view settings would carry it. It returns a fresh value on every
// call because the recorder materialises each recording twice and compares the
// bytes, which only means anything if the two runs share no state.
func tableGoldenDeps() Deps {
	return Deps{
		Tasks: tableGoldenTasks(),
		Dependencies: []domain.TaskDependency{
			{TaskID: 1, DependsOnTaskID: 9}, {TaskID: 1, DependsOnTaskID: 13},
			{TaskID: 5, DependsOnTaskID: 9}, {TaskID: 18, DependsOnTaskID: 5},
			{TaskID: 30, DependsOnTaskID: 18}, {TaskID: 30, DependsOnTaskID: 1},
		},
		Comments: []domain.Comment{
			{ID: 1, TaskID: 1}, {ID: 2, TaskID: 1}, {ID: 3, TaskID: 4},
			{ID: 4, TaskID: 18}, {ID: 5, TaskID: 18}, {ID: 6, TaskID: 18},
			{ID: 7, TaskID: 41},
		},
		View:       config.TableViewSettings{Sort: config.SortSettings{Field: "title", Order: "asc"}},
		Priorities: []config.PriorityDefinition{{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"}},
	}
}

// tableGoldenFilteredDeps is the same snapshot under the other projection the
// view settings can carry: two buckets kept, sorted by priority descending.
func tableGoldenFilteredDeps() Deps {
	deps := tableGoldenDeps()
	deps.View = config.TableViewSettings{
		Sort: config.SortSettings{Field: "priority", Order: "desc"},
	}
	deps.View.Filter.Bucket = []string{"dev", "review"}
	return deps
}

// tableScenario wires one recorded state.
//
// Table's Bind takes the frame alongside the deps — the host re-binds the live
// snapshot before every message, and Bind is where the cursor is clamped to the
// new row count and the scroll window re-synced — but Drive hands Bind only the
// screen. The frame Build was called with is captured here and reused: Drive
// always calls Build before any Bind at a given geometry, so the captured frame
// is the geometry the recording is being painted at.
func tableScenario(name string, deps func() Deps, keys []string) screenfixture.Scenario {
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

// FixtureScenarios returns every recorded state for the Tasks › Table screen.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		tableScenario(
			// Mid-list: two page steps and a single one, which land on a
			// different row at each geometry because a page step is half the
			// viewport. Rows hidden above AND below the slice, so both scroll
			// hints are recorded alongside the truncated titles.
			"sorted-middle", tableGoldenDeps, []string{"pgdown", "pgdown", "j"},
		),
		tableScenario(
			// The bottom of the list, the one scroll position a clamp
			// regression moves without moving anything else. 44 rows against a
			// 35-row viewport at the widest geometry, so the window is off the
			// top everywhere.
			"sorted-bottom", tableGoldenDeps, []string{"G"},
		),
		tableScenario(
			// The screen's other projection: the bucket filter keeps two of the
			// four buckets and the sort runs on priority descending, so the row
			// ORDER differs from every other fixture rather than just the
			// window into it. The cursor is parked three rows down so the marker
			// is off its default cell without the panel having scrolled — the
			// one recorded state where a cursor moved and a slice did not.
			"filtered-priority-desc", tableGoldenFilteredDeps, []string{"j", "j", "j"},
		),
	}
}
