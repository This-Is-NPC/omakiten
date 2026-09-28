package graph

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
	depgraph "omakiten/internal/graph"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// The Tasks > Graph screen is a projected DAG drawn as one truncated line per
// node inside a scrolling panel, so what a layout migration can break here is
// the projection (indent glyphs, back-reference rows, root ordering), the
// truncation column, and the window the line list scrolls to keep the cursor
// visible. The recordings below pin all four: a body far taller than the
// tallest recorded terminal, titles long enough to be cut at the 80-column
// floor and to survive whole at 200, a cursor parked well off its default cell,
// a non-default root ordering, and the separate empty-state branch the screen
// renders when the project has no dependencies at all.

// graphGoldenTitles cycle across the projected nodes so the rows differ in
// length. The first and fourth are deliberately longer than the 80-column
// content area and shorter than the 200-column one, which is what makes the
// three geometries record three different things rather than the same rows
// three times.
var graphGoldenTitles = []string{
	"Record three-width characterization goldens for every remaining uncovered TUI screen package before the layout migration begins",
	"Extract the shared panel frame",
	"Pin the 80-column floor",
	"Rewire the footer accounting so the viewport budget stops drifting between the board, the table and the dependency graph",
	"Teach the scroll window about variable row heights",
	"Split the host cycle",
	"Fold the status badge into the chrome measurement",
	"Freeze the wrap column",
}

// graphGoldenView is the default projection order the host binds.
func graphGoldenView() config.GraphViewSettings {
	return config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}}
}

// graphGoldenDeps materialises the fixture graph FRESH on every call: three
// roots, four children each, three grandchildren each — 51 nodes over 54
// projected lines, which overflows the tallest recorded viewport (36 rows at
// 200x50) by half again.
//
// The last edge is deliberately cross-root: #120 already hangs off #110 under
// the first root, so reaching it again under the second root's #210 makes the
// projection print it a second time as a back-reference row listing both
// parents. That row is the one piece of the projection that no amount of
// scrolling would otherwise put in a fixture.
func graphGoldenDeps(view config.GraphViewSettings) Deps {
	tasks := make([]domain.Task, 0, 51)
	links := make([]domain.TaskDependency, 0, 49)
	next := 0
	add := func(id int64) {
		tasks = append(tasks, domain.Task{ID: id, Title: graphGoldenTitles[next%len(graphGoldenTitles)]})
		next++
	}
	for r := range 3 {
		base := int64(100 + r*100)
		add(base)
		for c := range 4 {
			child := base + 10 + int64(c)
			add(child)
			links = append(links, domain.TaskDependency{TaskID: child, DependsOnTaskID: base})
			for g := range 3 {
				grandchild := base + 20 + int64(c*3+g)
				add(grandchild)
				links = append(links, domain.TaskDependency{TaskID: grandchild, DependsOnTaskID: child})
			}
		}
	}
	links = append(links, domain.TaskDependency{TaskID: 120, DependsOnTaskID: 210})
	return projected(Deps{Tasks: tasks, Dependencies: links}, view)
}

// projected does for a recorded screen what internal/tui does for the live one:
// it builds the DAG projection the screen is handed. The fixture plays host
// here, so the recordings exercise the same Deps shape the host fills.
func projected(deps Deps, view config.GraphViewSettings) Deps {
	deps.Lines = depgraph.Lines(deps.Dependencies, deps.Tasks, depgraph.RootOrder{
		Field: view.Sort.Field, Order: view.Sort.Order,
	})
	return deps
}

// graphScenario wires one recorded state.
//
// Graph's Bind takes the frame alongside the deps — the host re-binds both
// before every message — but Drive is handed only the screen on Bind, so the
// frame Build was called with is stashed for the rebinds that follow it. Drive
// always calls Build before any Bind of the same painting, and paints one
// recording at one geometry at a time, so the stashed frame is always the one
// the screen is currently being painted at.
func graphScenario(name string, deps func() Deps, keys []string) screenfixture.Scenario {
	var painted screenhost.Frame
	return screenfixture.Scenario{
		Name: name,
		Build: func(frame screenhost.Frame) screenhost.Screen {
			painted = frame
			return screenfixture.Enter(New().Bind(deps(), frame), frame)
		},
		Bind: func(screen screenhost.Screen) screenhost.Screen {
			return screen.(Screen).Bind(deps(), painted)
		},
		Keys: keys,
	}
}

// FixtureScenarios returns every recorded state for the Tasks > Graph screen.
func FixtureScenarios() []screenfixture.Scenario {
	sorted := config.GraphViewSettings{Sort: config.SortSettings{Field: "title", Order: "asc"}}
	return []screenfixture.Scenario{
		graphScenario(
			// Two page steps and three single steps into the projection. A page
			// step is a function of the terminal height, so the extra single steps
			// keep the three geometries landing on different nodes — which is the
			// point of recording three of them. Both the selectable cursor and the
			// line window it drags along are off zero here.
			"tree-scrolled", func() Deps { return graphGoldenDeps(graphGoldenView()) },
			[]string{"pgdown", "pgdown", "j", "j", "j"},
		),
		graphScenario(
			// `G` parks the cursor on the last projected node — the one scroll
			// position a clamp regression moves without moving anything else, and
			// the only one that records the tail of the third root's subtree.
			"tree-bottom", func() Deps { return graphGoldenDeps(graphGoldenView()) },
			[]string{"G"},
		),
		graphScenario(
			// The same graph under the other root ordering the view settings can
			// ask for. Sorting is applied to ROOTS only, so this fixture is what
			// pins that the subtree walk below each root is unaffected by it.
			"sorted-by-title", func() Deps { return graphGoldenDeps(sorted) },
			[]string{"pgdown", "j", "j"},
		),
		graphScenario(
			// The screen's other render branch: with no dependencies at all it
			// draws a hint box instead of the panel, and that box is its own layout
			// with its own wrap. A migration that only ever ran the panel path
			// would sail past it.
			//
			// Its three fixtures are byte-identical, and that is the finding: the
			// box width is Clamp(AvailableWidth()-8, 32, 60), which saturates at 60
			// from 72 columns up, so the empty state is the one thing on this
			// screen the terminal width does not move. The three copies are what
			// would catch a migration changing that.
			"no-dependencies", func() Deps { return projected(Deps{}, graphGoldenView()) },
			nil,
		),
	}
}
