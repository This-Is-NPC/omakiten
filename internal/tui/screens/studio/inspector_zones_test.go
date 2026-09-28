package studio

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// studioSubScreens is the four Studio sub-screens, for the properties that hold
// on all of them.
func studioSubScreens() map[string]struct {
	open   func(testing.TB, int, int, State) (Screen, screenhost.Frame)
	result func(Screen) screengrid.Result
	list   screenlayout.ID
	zones  screenlayout.InspectorIDs
} {
	return map[string]struct {
		open   func(testing.TB, int, int, State) (Screen, screenhost.Frame)
		result func(Screen) screengrid.Result
		list   screenlayout.ID
		zones  screenlayout.InspectorIDs
	}{
		"hooks":    {hooksScreen, Screen.hooksGridResult, sectionHooksList, hooksZones},
		"workflow": {workflowScreen, Screen.workflowGridResult, sectionWorkflowList, workflowZones},
		"commands": {commandsScreen, Screen.commandsGridResult, sectionCommandsList, commandsZones},
		"personas": {personasScreen, Screen.personasGridResult, sectionPersonasList, personasZones},
	}
}

// `tab` stops on every zone the body painted, in reading order, and never on the
// container that groups them.
//
// This is the defect reported against Studio › Commands: "pressing tab I leave
// section 1 and go to 3". The inspector was one leaf painting two framed
// sections, so the metadata table between the list and PREVIEW was not a stop —
// there was no zone there to be one.
func TestStudioTabStopsOnEveryPaintedZone(t *testing.T) {
	t.Parallel()

	for name, sub := range studioSubScreens() {
		name, sub := name, sub
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertStudioTabStops(t, sub)
		})
	}
}

func assertStudioTabStops(t *testing.T, sub struct {
	open   func(testing.TB, int, int, State) (Screen, screenhost.Frame)
	result func(Screen) screengrid.Result
	list   screenlayout.ID
	zones  screenlayout.InspectorIDs
}) {
	t.Helper()
	screen, frame := sub.open(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	painted := map[screenlayout.ID]bool{}
	for _, id := range studioPaintedLeaves(sub.result(screen.withFrame(frame))) {
		painted[id] = true
	}
	for _, want := range []screenlayout.ID{sub.list, sub.zones.Fields} {
		if !painted[want] {
			t.Fatalf("zone %q was not painted; painted %v", want, painted)
		}
	}
	if painted[sub.zones.Inspector] {
		t.Fatalf("painted the inspector CONTAINER %q; it groups zones, it does not paint", sub.zones.Inspector)
	}

	seen := map[screenlayout.ID]bool{sub.list: true}
	for i := 1; i <= len(painted); i++ {
		screen = studioDrive(t, screen, frame, "tab")
		if !assertStudioTabFocus(t, screen.grid.Focus(), painted, seen, i, sub.list) {
			break
		}
	}
}

func assertStudioTabFocus(t *testing.T, got screenlayout.ID, painted map[screenlayout.ID]bool, seen map[screenlayout.ID]bool, tab int, list screenlayout.ID) bool {
	t.Helper()
	if tab < len(painted) {
		if !painted[got] {
			t.Fatalf("tab %d focused %q, which the body did not paint", tab, got)
		}
		if seen[got] {
			t.Fatalf("tab %d returned to %q before visiting every zone (seen %v)", tab, got, seen)
		}
		seen[got] = true
		return true
	}
	if got != list {
		t.Fatalf("after %d tabs focus = %q, want the ring back on the list", tab, got)
	}
	return false
}

// Exactly one zone is accented at a time, and the view-only one is not exempt.
//
// This is the other half of the report: a Workflow bucket's inspector is a field
// table and nothing else, and focusing it used to paint no ▸ anywhere, because
// the accent was spent on a detail kicker that row does not have. "Read-only" is
// a statement about what the keys do inside a zone, never about whether it can
// say you are in it.
func TestStudioExactlyOneZoneIsAccented(t *testing.T) {
	t.Parallel()

	for name, sub := range studioSubScreens() {
		name, sub := name, sub
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			screen, frame := sub.open(t, 120, 40, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)

			zones := len(studioPaintedLeaves(sub.result(screen.withFrame(frame))))
			for i := 0; i <= zones; i++ {
				view := screentest.StripANSI(screen.View(frame))
				if got := strings.Count(view, "▸ "); got != 1 {
					t.Fatalf("after %d tabs the body carries %d focus accents, want exactly one\n%s", i, got, view)
				}
				screen = studioDrive(t, screen, frame, "tab")
			}
		})
	}
}

// The view-only zone takes no cursor, so the standard keys move its OFFSET and
// there is no selection to walk. That is the whole of "focus it, but do not
// navigate it", and it is a property of the Spec rather than of a branch in each
// screen's key handler.
func TestStudioFieldsZoneHasNoSelection(t *testing.T) {
	t.Parallel()

	for name, sub := range studioSubScreens() {
		name, sub := name, sub
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			screen, frame := sub.open(t, 120, 40, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			screen = studioDrive(t, screen, frame, "tab")
			if got := screen.grid.Focus(); got != sub.zones.Fields {
				t.Fatalf("tab focused %q, want the fields zone %q", got, sub.zones.Fields)
			}
			if cur := screen.grid.Layout().Cursor(sub.zones.Fields); cur >= 0 {
				t.Fatalf("the view-only zone holds cursor %d; it must hold none", cur)
			}
			screen = studioDrive(t, screen, frame, "j")
			if cur := screen.grid.Layout().Cursor(sub.zones.Fields); cur >= 0 {
				t.Fatalf("j gave the view-only zone cursor %d; it must only scroll", cur)
			}
			if got := screen.grid.Focus(); got != sub.zones.Fields {
				t.Fatalf("j moved the focus to %q; it must stay on the zone it was aimed at", got)
			}
		})
	}
}

// A bordered table is never left open: whatever the fields zone paints, its
// first line opens a box and its last closes one, at every inspector height.
func TestStudioFieldTableIsAlwaysAClosedBox(t *testing.T) {
	t.Parallel()

	screen, _ := hooksScreen(t, 80, 55, State{})
	hooks := screen.studioHookSpecs()
	if len(hooks) == 0 {
		t.Fatal("fixture carries no hooks")
	}
	fields := hookInspectorFields(screen, hooks[0], nil)

	for rows := 0; rows <= 40; rows++ {
		table := screen.studioFieldTable(76, rows, hooksFieldOpts,
			screen.studioFieldRows("01 // task.created", false, fields...))
		if len(table) == 0 {
			continue
		}
		if rows > 0 && len(table) > rows {
			t.Fatalf("at %d rows the table is %d lines", rows, len(table))
		}
		if opens, closes := strings.Count(table[0], "┌"), strings.Count(table[len(table)-1], "└"); opens != 1 || closes != 1 {
			t.Fatalf("at %d rows the table opens %d and closes %d:\n%s", rows, opens, closes, strings.Join(table, "\n"))
		}
	}
}

// A table's column heading is CHROME: it stays put while the rows under it
// scroll, in HISTORY and in RELATED alike.
//
// Both used to lose it. RELATED painted it as selectlist's Subtitle, which sits
// above the rule and reads as a second line of the kicker rather than as the
// first line of the table; HISTORY passed it as the first ITEM, so the arranger
// windowed it away the moment the user scrolled two rows in and the columns
// stopped being labelled at exactly the point a long table needs labels.
func TestStudioTableHeadingsStayPinnedWhileTheRowsScroll(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		open    func(testing.TB, int, int, State) (Screen, screenhost.Frame)
		reach   []string
		heading []string
	}{
		// The hook the fixture gives a history to; index 0 has none, and a table
		// with no rows has nothing to scroll.
		"hooks":    {hooksScreen, hooksFocusKeys(studioHookIsGuardTaskDelete), []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"}},
		"personas": {personasScreen, nil, []string{"NAME", "TYPE", "DETAIL"}},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertStudioHeadingStaysPinned(t, tc)
		})
	}
}

func assertStudioHeadingStaysPinned(t *testing.T, tc struct {
	open    func(testing.TB, int, int, State) (Screen, screenhost.Frame)
	reach   []string
	heading []string
}) {
	t.Helper()
	screen, frame := tc.open(t, 200, 30, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range tc.reach {
		screen = studioDrive(t, screen, frame, "down")
	}
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")

	first := studioHeadingLine(screentest.StripANSI(screen.View(frame)), tc.heading)
	if first < 0 {
		t.Fatalf("no column heading %v on screen\n%s", tc.heading, screentest.StripANSI(screen.View(frame)))
	}
	for i := 0; i < 12; i++ {
		screen = studioDrive(t, screen, frame, "j")
		view := screentest.StripANSI(screen.View(frame))
		at := studioHeadingLine(view, tc.heading)
		if at < 0 {
			t.Fatalf("the column heading scrolled away after %d presses\n%s", i+1, view)
		}
		if at != first {
			t.Fatalf("the column heading moved from row %d to %d after %d presses\n%s", first, at, i+1, view)
		}
	}
}

// studioHeadingLine is the row a column heading is on, or -1.
func studioHeadingLine(view string, heading []string) int {
	for i, line := range strings.Split(view, "\n") {
		all := true
		for _, want := range heading {
			if !strings.Contains(line, want) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}
