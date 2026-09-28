package studio

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// studioDrive feeds one key through the real Update path and returns the screen
// the outcome carries, so the caller's own copy stays untouched. Async preview
// resolve cmds are pumped the way the host runs tea.Cmd, or tab-to-preview
// tests would paint the composing placeholder forever.
func studioDrive(tb testing.TB, screen Screen, frame screenhost.Frame, key string) Screen {
	tb.Helper()
	outcome := screen.Update(frame, screentest.Key(key))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		tb.Fatalf("Update(%q) carried %T, not a studio.Screen", key, outcome.Screen)
	}
	return studioPump(tb, next, frame, outcome.Command)
}

func studioPaintedLeaves(res screengrid.Result) []screenlayout.ID {
	var out []screenlayout.ID
	for _, p := range res.Placements {
		if p.Leaf && !p.Dropped {
			out = append(out, p.ID)
		}
	}
	return out
}

// A tall NARROW terminal is past the width breakpoint and still has rows to
// spare, so both panes stack and `tab` moves the focus without moving a box.
//
// Reported against Studio at 80×55: focusing the read-only inspector deleted the
// list beside it, and the inspector then padded twenty-six rows it had nothing
// to put in. Hooks made it worse — with the list gone, its own reserve arithmetic
// cut the bordered field table mid-box to make room for HISTORY.
//
// The two geometries are the pair, not one case: 80×24 cannot pay two panes and
// still gives the focused one everything, which is the rule this must not undo.
func TestStudioPanesStayStackedWhileTheTerminalCanAffordThem(t *testing.T) {
	t.Parallel()

	screens := map[string]struct {
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
	for name, sub := range screens {
		name, sub := name, sub
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertStudioStackingScreen(t, sub)
		})
	}
}

func assertStudioStackingScreen(t *testing.T, sub struct {
	open   func(testing.TB, int, int, State) (Screen, screenhost.Frame)
	result func(Screen) screengrid.Result
	list   screenlayout.ID
	zones  screenlayout.InspectorIDs
}) {
	t.Helper()
	screen, frame := sub.open(t, 80, 55, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	before := sub.result(screen.withFrame(frame))
	zones := studioPaintedLeaves(before)
	if len(zones) < 2 {
		t.Fatalf("at 80x55 painted %v, want the list and the inspector's zones stacked", zones)
	}
	for i, want := range append(append([]screenlayout.ID(nil), zones[1:]...), zones[0]) {
		screen = studioDrive(t, screen, frame, "tab")
		if got := screen.grid.Focus(); got != want {
			t.Fatalf("tab %d focused %q, want %q (painted %v)", i+1, got, want, zones)
		}
		assertStudioZonePlacements(t, sub, screen, frame, before, zones, i+1)
	}

	short, shortFrame := sub.open(t, 80, 24, State{})
	short = short.Lifecycle(shortFrame, screenhost.LifecycleEnter).Screen.(Screen)
	assertStudioPaintedLeaf(t, sub.result(short.withFrame(shortFrame)), sub.list, 80, 24)
}

func assertStudioZonePlacements(t *testing.T, sub struct {
	open   func(testing.TB, int, int, State) (Screen, screenhost.Frame)
	result func(Screen) screengrid.Result
	list   screenlayout.ID
	zones  screenlayout.InspectorIDs
}, screen Screen, frame screenhost.Frame, before screengrid.Result, zones []screenlayout.ID, tab int) {
	t.Helper()
	after := sub.result(screen.withFrame(frame))
	for _, id := range zones {
		was, _ := before.Placement(id)
		now, _ := after.Placement(id)
		if was.Box != now.Box || was.Dropped != now.Dropped {
			t.Errorf("after tab %d, %s was %+v (dropped=%v) and became %+v (dropped=%v)",
				tab, id, was.Box, was.Dropped, now.Box, now.Dropped)
		}
	}
}

// assertStudioListIsOffScreen is the stacked rule seen from the inspector's
// side: the list is not on screen, the focused zone is, and nothing that is on
// screen belongs to the list's half of the body.
func assertStudioListIsOffScreen(t *testing.T, res screengrid.Result, list, focused screenlayout.ID, width, height int) {
	t.Helper()
	painted := studioPaintedLeaves(res)
	have := map[screenlayout.ID]bool{}
	for _, id := range painted {
		have[id] = true
	}
	if have[list] {
		t.Fatalf("at %dx%d painted %v; the list should be off screen", width, height, painted)
	}
	if !have[focused] {
		t.Fatalf("at %dx%d painted %v, want the focused zone %s", width, height, painted, focused)
	}
}

func assertStudioPaintedLeaf(t *testing.T, res screengrid.Result, want screenlayout.ID, width, height int) {
	t.Helper()
	painted := studioPaintedLeaves(res)
	if len(painted) != 1 || painted[0] != want {
		t.Fatalf("at %dx%d painted %v, want only %s", width, height, painted, want)
	}
}

// assertStudioColsBreakpoint checks the body's arrangement and what it painted.
//
// Side by side is the LIST plus the inspector's zones — the inspector is a Rows
// of a field table over a detail box, and the grid dissolves that grouping, so
// what reaches the screen (and the focus ring) is the leaves and never the
// container. A row that has only one of the two paints only that one, which is
// why the check is "the list, plus at least one inspector zone, and nothing
// else" rather than a fixed count.
func assertStudioColsBreakpoint(t *testing.T, res screengrid.Result, root, list screenlayout.ID, zones screenlayout.InspectorIDs, width, height int, want screenlayout.Arrangement) {
	t.Helper()
	place, ok := res.Placement(root)
	if !ok {
		t.Fatal("root reported no placement")
	}
	if place.Arrangement != want {
		t.Fatalf("at %dx%d arrangement = %s, want %s", width, height, place.Arrangement, want)
	}
	for i, line := range strings.Split(res.View, "\n") {
		if got := lipgloss.Width(line); got > place.Box.Width {
			t.Fatalf("at %dx%d line %d is %d cols, box has %d", width, height, i, got, place.Box.Width)
		}
	}
	painted := studioPaintedLeaves(res)
	switch want {
	case screenlayout.SideBySide:
		have := map[screenlayout.ID]bool{}
		for _, id := range painted {
			have[id] = true
		}
		if !have[list] {
			t.Fatalf("at %dx%d painted %v, want the list", width, height, painted)
		}
		if !have[zones.Fields] && !have[zones.Detail] {
			t.Fatalf("at %dx%d painted %v, want at least one inspector zone", width, height, painted)
		}
		if have[zones.Inspector] {
			t.Fatalf("at %dx%d painted the inspector CONTAINER %q; it groups zones, it does not paint", width, height, zones.Inspector)
		}
	case screenlayout.Stacked:
		assertStudioPaintedLeaf(t, res, list, width, height)
	default:
		t.Fatalf("unexpected arrangement %s", want)
	}
}

func studioPump(tb testing.TB, screen Screen, frame screenhost.Frame, cmd tea.Cmd) Screen {
	tb.Helper()
	for i := 0; cmd != nil && i < 8; i++ {
		msg := cmd()
		if msg == nil {
			return screen
		}
		if next, ok := msg.(tea.Cmd); ok {
			cmd = next
			continue
		}
		outcome := screen.Update(frame, msg)
		next, ok := outcome.Screen.(Screen)
		if !ok {
			tb.Fatalf("async update carried %T, not a studio.Screen", outcome.Screen)
		}
		screen = next
		cmd = outcome.Command
	}
	return screen
}

func commandsScreen(tb testing.TB, width, height int, state State) (Screen, screenhost.Frame) {
	tb.Helper()
	frame := screentest.FrameAt(tb, width, height)
	screen := New().
		Bind(screenhost.StudioCommands, Deps{Ctx: context.Background()}).
		WithState(state)
	return screen.withFrame(frame), frame
}

func commandsPreviewScreen(tb testing.TB, width, height int, preview string) (Screen, screenhost.Frame) {
	tb.Helper()
	frame := screentest.FrameAt(tb, width, height)
	screen := New().
		Bind(screenhost.StudioCommands, Deps{
			Ctx: context.Background(),
			ResolveCommand: func(_ config.Bundle, _ string) (string, error) {
				return preview, nil
			},
		})
	return screen.withFrame(frame), frame
}

func longStudioPromptPreview() string {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("preview-line-%02d unique-token", i)
	}
	return strings.Join(lines, "\n")
}

func assertNoCommandFieldCursor(t *testing.T, view string) {
	t.Helper()
	for _, field := range studioCommandFields {
		marker := "> " + field
		if strings.Contains(view, marker) {
			t.Fatalf("metadata table still paints field cursor %q\n%s", marker, view)
		}
	}
}

// TestStudioBodyScrollDoesNotReAnchorOnTheCursor is the regression test for the
// first half of the defect: every branch of scrollStudioLines re-applied the
// screen's cursor before nudging the offset, and scrollwindow.Resync preserves
// the caller's scroll ONLY for the -1 no-selection sentinel. With a cursor set,
// each scroll step was snapped back onto the cursor first, so the body could
// never travel further than one step away from it — the user presses pgup and
// the screen fights back.
//
// The fixture parks the cursor at the FOOT of a long matrix, which is what makes
// the snap visible: the offset can never fall below the line that keeps the last
// matrix row on screen, so the top of the body is unreachable no matter how many
// times the key is pressed.
func TestStudioCommandsKeepsTheSelectedCommandVisible(t *testing.T) {
	t.Parallel()

	rows := studioCommandRows(nil, nil)
	if len(rows) < 20 {
		t.Fatalf("the command table holds %d rows; the fixture no longer overflows a viewport", len(rows))
	}

	screen, frame := commandsScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for i := 1; i < len(rows); i++ {
		screen = studioDrive(t, screen, frame, "j")
		marker := fmt.Sprintf("› %02d // %s", i+1, rows[i].Key)
		view := screentest.StripANSI(screen.View(frame))
		if !strings.Contains(view, marker) {
			t.Fatalf("after %d j press(es) the selected command row %q is off-screen\n%s", i, marker, view)
		}
	}
}

func TestStudioCommandsPreviewScrollsBoxedMarkdown(t *testing.T) {
	t.Parallel()

	preview := longStudioPromptPreview()
	screen, frame := commandsPreviewScreen(t, 120, 40, preview)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsPreview {
		t.Fatalf("tab focus = %q, want preview leaf %q", got, sectionCommandsPreview)
	}
	before := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(before, "preview-line-00 unique-token") {
		t.Fatalf("preview focus missing the first markdown line\n%s", before)
	}
	for _, token := range []string{"persona", "skills", "laws", "laws_disabled", "templates", "global laws"} {
		if !strings.Contains(before, token) {
			t.Fatalf("preview focus missing pinned metadata %q\n%s", token, before)
		}
	}
	assertNoCommandFieldCursor(t, before)
	index := screen.studioCommandIndex
	offset := screen.grid.Layout().Offset(sectionCommandsPreview)

	screen = studioDrive(t, screen, frame, "j")
	screen = studioDrive(t, screen, frame, "j")
	screen = studioDrive(t, screen, frame, "pgdown")
	after := screentest.StripANSI(screen.View(frame))
	if screen.studioCommandIndex != index {
		t.Fatalf("preview j/k moved the command index from %d to %d", index, screen.studioCommandIndex)
	}
	if got := screen.grid.Layout().Offset(sectionCommandsPreview); got <= offset {
		t.Fatalf("preview j/pgdn left inspector offset %d; want it to window the boxed markdown", got)
	}
	if strings.Contains(after, "preview-line-00 unique-token") {
		t.Fatalf("preview j/pgdn did not scroll the first markdown line out of the box\n%s", after)
	}
	for _, token := range []string{"persona", "skills", "laws"} {
		if !strings.Contains(after, token) {
			t.Fatalf("preview scroll moved pinned metadata %q\n%s", token, after)
		}
	}
	assertNoCommandFieldCursor(t, after)
}

// At 80×55 the list, the field table and PREVIEW all sit on screen, stacked.
// Side-by-side (120×40) already scrolls the focused preview; this proves every
// advertised vertical key reaches the same leaf past the width breakpoint and
// cannot spend itself on an unfocused zone.
func TestStudioCommandsPreviewNavigationWhenStacked(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		key       string
		prime     string
		direction int
	}{
		"j moves down":      {key: "j", direction: 1},
		"k moves up":        {key: "k", prime: "j", direction: -1},
		"pgdown moves down": {key: "pgdown", direction: 1},
		"pgup moves up":     {key: "pgup", prime: "pgdown", direction: -1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assertStackedPreviewNavigation(t, tc.key, tc.prime, tc.direction)
		})
	}
}

func assertStackedPreviewNavigation(t *testing.T, key, prime string, direction int) {
	t.Helper()
	lines := make([]string, 3000)
	for i := range lines {
		lines[i] = fmt.Sprintf("stacked-preview-%04d unique-token", i)
	}
	screen, frame := commandsPreviewScreen(t, 80, 55, strings.Join(lines, "\n"))
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	assertStackedCommandsBody(t, screen, frame)
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsPreview {
		t.Fatalf("tab focus = %q, want preview leaf %q", got, sectionCommandsPreview)
	}
	if prime != "" {
		screen = studioDrive(t, screen, frame, prime)
	}

	before := screen.grid.Layout()
	index := screen.studioCommandIndex
	screen = studioDrive(t, screen, frame, key)
	after := screen.grid.Layout()
	delta := after.Offset(sectionCommandsPreview) - before.Offset(sectionCommandsPreview)
	if delta*direction <= 0 {
		t.Fatalf("%s changed preview offset by %d (from %d to %d)", key, delta, before.Offset(sectionCommandsPreview), after.Offset(sectionCommandsPreview))
	}
	if screen.studioCommandIndex != index {
		t.Fatalf("%s moved command index from %d to %d", key, index, screen.studioCommandIndex)
	}
	for _, id := range []screenlayout.ID{sectionCommandsList, sectionCommandsFields} {
		if got, want := after.Offset(id), before.Offset(id); got != want {
			t.Errorf("%s changed unfocused zone %q offset from %d to %d", key, id, want, got)
		}
	}
}

func assertStackedCommandsBody(t *testing.T, screen Screen, frame screenhost.Frame) {
	t.Helper()
	res := screen.withFrame(frame).commandsGridResult()
	painted := studioPaintedLeaves(res)
	place, ok := res.Placement(sectionCommands)
	if !ok {
		t.Fatal("commands root reported no placement")
	}
	if place.Arrangement != screenlayout.Stacked {
		t.Fatalf("at 80x55 arrangement = %s, want Stacked (painted %v)", place.Arrangement, painted)
	}
	have := map[screenlayout.ID]bool{}
	for _, id := range painted {
		have[id] = true
	}
	if !have[sectionCommandsList] || !have[sectionCommandsFields] || !have[sectionCommandsPreview] {
		t.Fatalf("at 80x55 painted %v, want list, fields and preview stacked together", painted)
	}
}

func TestStudioCommandsPreviewResolvesOffThePaintPath(t *testing.T) {
	t.Parallel()

	var n int
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(screenhost.StudioCommands, Deps{
		Ctx: context.Background(),
		ResolveCommand: func(_ config.Bundle, _ string) (string, error) {
			n++
			return longStudioPromptPreview(), nil
		},
	}).withFrame(frame)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if n != 0 {
		t.Fatalf("Lifecycle/View resolved on the paint path %d time(s)", n)
	}
	view := screentest.StripANSI(screen.View(frame))
	if n != 0 {
		t.Fatalf("View resolved on the paint path %d time(s)", n)
	}
	if !strings.Contains(view, "Composing preview") {
		t.Fatalf("unresolved preview missing composing placeholder\n%s", view)
	}

	screen = studioDrive(t, screen, frame, "tab")
	if n != 1 {
		t.Fatalf("tab+pump resolved %d time(s), want 1", n)
	}
	view = screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "preview-line-00 unique-token") {
		t.Fatalf("pumped preview missing markdown\n%s", view)
	}
	if strings.Contains(view, "Composing preview") {
		t.Fatalf("pumped preview kept the composing placeholder\n%s", view)
	}

	studioDrive(t, screen, frame, "j")
	if n != 1 {
		t.Fatalf("preview scroll re-resolved; got %d", n)
	}
}

func TestStudioCommandsBodyScrollReachesTheTop(t *testing.T) {
	t.Parallel()

	screen, frame := commandsScreen(t, 120, 24, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for i := 0; i < 20; i++ {
		screen = studioDrive(t, screen, frame, "j")
	}
	screen = studioDrive(t, screen, frame, "end")
	if screen.studioCommandIndex == 0 {
		t.Fatal("end left the list on the first command; the fixture no longer overflows its viewport")
	}
	for press := 0; press < 200; press++ {
		before := screen.studioCommandIndex
		screen = studioDrive(t, screen, frame, "pgup")
		if screen.studioCommandIndex == 0 && screen.Scroll() == 0 {
			return
		}
		if screen.studioCommandIndex > before {
			t.Fatalf("pgup moved the list cursor down from %d to %d after %d press(es)", before, screen.studioCommandIndex, press+1)
		}
		if screen.studioCommandIndex == before && screen.Scroll() == 0 {
			t.Fatalf("pgup stalled at command %d after %d press(es)", before, press+1)
		}
	}
	t.Fatalf("pgup never reached the top of the Commands list: index=%d scroll=%d", screen.studioCommandIndex, screen.Scroll())
}

// TestStudioCommandsAnchorIsTheItemItMarked is the guard for the third
// acceptance criterion of the wrapping fix, restated for the section contract.
//
// Wrapping changes a body's line COUNT, so before the migration it changed the
// meaning of every line index: a renderer could only report its anchor as an
// index into the SOURCE body it had just built, while the viewport, the scroll
// offset and the linelist cursor lived in WRAPPED-line space. The translation
// between the two was the thing that could silently drift.
//
// There is one space now — a renderer appends the wrapped rows and states the
// anchor as it appends — so the property to hold is that the arranger's cursor
// and the SCREEN's marker are the same line, twice over: the item the arranger
// selected must carry the marker, and the terminal line it reports must be the
// line of the painted body that carries it. Pinned by CONTENT, never by number.
func TestStudioCommandsAnchorIsTheItemItMarked(t *testing.T) {
	t.Parallel()

	screen, frame := commandsScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for i := 0; i < 12; i++ {
		screen = studioDrive(t, screen, frame, "j")
	}
	idx := screen.studioCommandIndex
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "down")
	rows := studioCommandRows(nil, nil)
	commandMarker := fmt.Sprintf("› %02d // %s", idx+1, rows[idx].Key)
	view := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, commandMarker) {
		t.Fatalf("preview focus: selected command %q is off-screen\n%s", commandMarker, view)
	}
	assertNoCommandFieldCursor(t, view)
	if got := screen.grid.Layout().Cursor(sectionCommandsList); got != idx {
		t.Fatalf("preview focus: list cursor %d, want command index %d", got, idx)
	}
	if screen.studioCommandIndex != idx {
		t.Fatalf("preview down moved command index from %d to %d", idx, screen.studioCommandIndex)
	}
}

// TestEverySelectionIsTheArrangersCursor covers the half of the selection that
// a golden diff cannot see.
//
// A Studio fixture records the list `›` marker, because it is literal text.
// The metadata table is display-only and must not grow a `>` field cursor.
// What a fixture CANNOT see is the arranger's side of the same selection:
// Placement.Cursor can point at the wrong item, or the section can decline to
// read its cursor at all, and in both cases every recorded byte is unchanged
// while the window has stopped following the user.
func TestEverySelectionIsTheArrangersCursor(t *testing.T) {
	t.Parallel()

	screen, frame := commandsScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for _, key := range []string{"j", "j", "tab", "down"} {
		screen = studioDrive(t, screen, frame, key)
	}
	view := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "› ") {
		t.Fatalf("Commands view carries no list selection marker after cursor keys\n%s", view)
	}
	assertNoCommandFieldCursor(t, view)
	if got := screen.grid.Layout().Cursor(sectionCommandsList); got != screen.studioCommandIndex {
		t.Fatalf("list cursor %d, want command index %d", got, screen.studioCommandIndex)
	}
}

// TestStudioViewportWrapsEveryRenderedLine is the containment half of the same
// fix, asserted at the screen the user reported: no line the Studio viewport
// paints — body or hint — may be wider than the panel it is painted into.
func TestStudioViewportWrapsEveryRenderedLine(t *testing.T) {
	t.Parallel()

	for _, width := range []int{34, 60, 90} {
		width := width
		t.Run(fmt.Sprintf("w%d", width), func(t *testing.T) {
			t.Parallel()

			screen, frame := commandsScreen(t, width, 24, State{})
			// The panel is as wide as the arranger says the section is, and the
			// body is indented two columns inside the terminal. Asked, not
			// re-derived: a test that recomputed the width from the terminal
			// would be asserting its own arithmetic.
			panel := screen.studioSectionWidth() + 2
			for _, line := range strings.Split(screentest.StripANSI(screen.View(frame)), "\n") {
				if got := lipgloss.Width(line); got > panel {
					t.Fatalf("rendered line width %d exceeds the panel width %d at terminal=%d:\n%q",
						got, panel, width, line)
				}
			}
		})
	}
}
