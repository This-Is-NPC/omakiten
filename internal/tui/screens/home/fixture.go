package home

import (
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// homeGoldenProjects is the recorded project list: twenty-four cards against
// the 36-row column the tallest recorded terminal gives Home, so no recorded
// cursor position can reach a window that also holds the head or the tail.
// Project ids run 101 + index, which is what lets the side tables below be read
// against a position in this list.
//
// The wrap-sensitive entries come in pairs — a title that wraps only at the
// floor, then a meta line that truncates only at the floor — and one pair sits
// at each of the three windows the recordings park in (head, middle, tail), so
// every fixture carries the width-dependent behaviour rather than only the
// first one.
//
// Every root path is a fixed literal. Home prints project root paths verbatim,
// so a path derived from t.TempDir(), os.Getwd() or $HOME would put the
// recording machine into the fixture and change the column at which the meta
// line truncates.
func homeGoldenProjects() []domain.Project {
	return []domain.Project{
		// 0-1: the head pair. The 80-column window only holds two cards, so
		// a wrap any further down never reaches the floor's head fixture.
		//
		// 72-column title: wraps inside the 70-column card at the floor,
		// fits on one line in the 80-column card at 120 and 200.
		{ID: 101, Name: "Charlie observability pipeline rewrite for the terminal layout migration", Slug: "charlie", RootPath: "/srv/omakiten/charlie"},
		// 75-column meta line (slug + " · " + root): truncatePath eats the
		// head of the path at the floor and leaves it whole at 120 and 200.
		{ID: 102, Name: "Delta Ingest", Slug: "delta-ingest", RootPath: "/srv/omakiten/checkouts/delta-ingest/services/rows-and-lanes"},
		{ID: 103, Name: "Alpha", Slug: "alpha", RootPath: "/srv/omakiten/alpha"},
		{ID: 104, Name: "Bravo Control Plane", Slug: "bravo", RootPath: "/srv/omakiten/bravo"},
		{ID: 105, Name: "Echo", Slug: "echo", RootPath: "/srv/omakiten/echo"},
		{ID: 106, Name: "Foxtrot Reporting", Slug: "foxtrot", RootPath: "/srv/omakiten/foxtrot"},
		{ID: 107, Name: "Golf", Slug: "golf", RootPath: "/srv/omakiten/golf"},
		{ID: 108, Name: "Hotel Migration Toolkit", Slug: "hotel", RootPath: "/srv/omakiten/hotel"},
		{ID: 109, Name: "India", Slug: "india", RootPath: "/srv/omakiten/india"},
		{ID: 110, Name: "Juliett Archive", Slug: "juliett", RootPath: "/srv/omakiten/juliett"},
		{ID: 111, Name: "Mike", Slug: "mike", RootPath: "/srv/omakiten/mike"},
		{ID: 112, Name: "November Gateway", Slug: "november", RootPath: "/srv/omakiten/november"},
		// 12-13: the middle pair, where the "scrolled" window lands.
		//
		// 77-column meta line.
		{ID: 113, Name: "Lima Scheduler", Slug: "lima-scheduler", RootPath: "/srv/omakiten/checkouts/lima-scheduler/internal/wave-planner"},
		// 73-column title.
		{ID: 114, Name: "Kilo cross-project characterization baseline recorder and fixture archive", Slug: "kilo", RootPath: "/srv/omakiten/kilo"},
		{ID: 115, Name: "Oscar", Slug: "oscar", RootPath: "/srv/omakiten/oscar"},
		{ID: 116, Name: "Papa Telemetry Sink", Slug: "papa", RootPath: "/srv/omakiten/papa"},
		{ID: 117, Name: "Quebec", Slug: "quebec", RootPath: "/srv/omakiten/quebec"},
		{ID: 118, Name: "Romeo Indexer", Slug: "romeo", RootPath: "/srv/omakiten/romeo"},
		{ID: 119, Name: "Sierra", Slug: "sierra", RootPath: "/srv/omakiten/sierra"},
		{ID: 120, Name: "Tango Bridge", Slug: "tango", RootPath: "/srv/omakiten/tango"},
		{ID: 121, Name: "Uniform", Slug: "uniform", RootPath: "/srv/omakiten/uniform"},
		// 21-22: the tail pair, where the "armed-delete" and "bottom"
		// windows land.
		//
		// 74-column meta line.
		{ID: 122, Name: "Victor Ledger", Slug: "victor-ledger", RootPath: "/srv/omakiten/checkouts/victor-ledger/internal/append-only"},
		// 71-column title.
		{ID: 123, Name: "Whiskey deep-link resolver and cross-surface navigation index rebuilder", Slug: "whiskey", RootPath: "/srv/omakiten/whiskey"},
		{ID: 124, Name: "Xray Sink", Slug: "xray", RootPath: "/srv/omakiten/xray"},
	}
}

// homeGoldenPayload adds the two host-resolved side tables the badge row is
// built from. Both are maps, and Home indexes them per project rather than
// ranging them, so their iteration order must never reach the render — the
// recorder's double paint is what proves it does not.
//
// The pending counts deliberately cover all three badge branches (0, 1, many)
// and the tag lists run from none to three so the badge row wraps on some cards
// and not on others.
func homeGoldenPayload() Payload {
	return Payload{
		Projects: homeGoldenProjects(),
		Tags: map[int64][]domain.Tag{
			101: {{ID: 1, Name: "tui", Label: "Terminal UI"}, {ID: 2, Name: "migration", Label: "Layout Migration"}, {ID: 3, Name: "chars", Label: "Characterization"}},
			102: {{ID: 4, Name: "ingest"}},
			103: {{ID: 5, Name: "go", Label: "Go"}},
			113: {{ID: 6, Name: "scheduler", Label: "Scheduler"}},
			114: {{ID: 7, Name: "goldens", Label: "Goldens"}, {ID: 8, Name: "fixtures", Label: "Fixtures"}},
			116: {{ID: 9, Name: "telemetry"}},
			122: {{ID: 10, Name: "ledger", Label: "Append Only Ledger"}, {ID: 11, Name: "storage", Label: "Storage"}},
			123: {{ID: 12, Name: "nav", Label: "Navigation"}},
		},
		Pending: map[int64]int{
			101: 17, 102: 0, 103: 3, 104: 1, 105: 2,
			107: 8, 109: 1, 113: 5, 114: 42, 116: 6,
			118: 0, 120: 9, 122: 31, 123: 1, 124: 4,
		},
	}
}

// homeGoldenCounters is the delete blast radius the host resolves between
// PrepareProjectDelete and ArmDelete. Home holds it verbatim for the
// confirmation, so the fixture carries a non-zero one.
func homeGoldenCounters() domain.ProjectDeleteCounters {
	return domain.ProjectDeleteCounters{Tasks: 24, Comments: 91, Plans: 3, Tags: 6, ActivityLogEntries: 412}
}

// homeGoldenBuild materialises the screen the way the host does: a fresh Screen
// with the loaded payload applied, then the LifecycleEnter the host issues
// before the first paint. Enter is where Home clamps the picker against the
// live viewport, so a recording built without it would record a scroll the host
// never shows.
func homeGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(New().Apply(Result{Payload: homeGoldenPayload()}), frame)
}

// homeGoldenArm models the host's half of the delete confirmation: Home answers
// `d` with a PrepareProjectDelete intent, the host resolves the counters, and
// hands them back through ArmDelete before the next paint. Bind is the only
// hook that runs after the last replayed key, which is exactly where that round
// trip lands in the real host.
func homeGoldenArm(screen screenhost.Screen) screenhost.Screen {
	s := screen.(Screen)
	projects := s.Projects()
	if s.Cursor() < 0 || s.Cursor() >= len(projects) {
		return s
	}
	return s.ArmDelete(projects[s.Cursor()].ID, homeGoldenCounters())
}

// homeGoldenDown is n real `j` presses. The recordings walk the list down
// rather than jumping, because scrollwindow.Follow only ever advances the
// offset — a cursor that arrives from below keeps the window it arrived with,
// so "walk down to 21" and "G then back up to 21" are different recorded
// states even though the cursor matches.
func homeGoldenDown(n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "j"
	}
	return keys
}

// FixtureScenarios returns every recorded state for the Home project picker.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			// The head of the list, straight out of LifecycleEnter. This is
			// the only recording that shows the top of the column — the
			// kicker, the rule and the first cards — together with the
			// "N below" overflow hint, which is the pair a column migration
			// is most likely to move.
			Name:  "head",
			Build: homeGoldenBuild,
		},
		{
			// Twelve real `j` presses. Twelve is past the last card that
			// fits at 200x50, so every geometry records a window that has
			// scrolled off the head, with the middle wrap-sensitive pair
			// (lima-scheduler, kilo) inside it.
			Name:  "scrolled",
			Build: homeGoldenBuild,
			Keys:  homeGoldenDown(12),
		},
		{
			// `G` parks the selection on the last card. The bottom is the
			// one scroll position a clamp regression moves without moving
			// anything else, and it is the only state where the trailing
			// card sits flush against the end of the window.
			Name:  "bottom",
			Build: homeGoldenBuild,
			Keys:  []string{"G"},
		},
		{
			// The armed-delete state, walked down onto the tail pair with
			// the host's resolved blast radius held for a confirming `d`.
			//
			// Home keeps the arm out of View entirely — it reaches the
			// chrome through Footer — and the selected card is a bold and
			// border-colour delta that ANSI-strip removes, so scroll is the
			// only part of this state the recorded bytes can carry. The
			// position therefore has to be one no other recording windows
			// onto, or this fixture is a copy of another under a new name.
			Name:  "armed-delete",
			Build: homeGoldenBuild,
			Bind:  homeGoldenArm,
			Keys:  homeGoldenDown(21),
		},
	}
}
