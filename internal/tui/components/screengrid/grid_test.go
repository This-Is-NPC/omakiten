package screengrid

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

func testKit() screenkit.Kit { return screenkit.Kit{Width: 120, Height: 40} }

// fill is a body that paints `count` single-row items, each as wide as it is
// allowed to be — the shape that makes an overflow or a gap impossible to miss.
func fill(label string, count int) func(screenlayout.Canvas) screenlayout.Block {
	return func(canvas screenlayout.Canvas) screenlayout.Block {
		items := make([]string, count)
		for i := range items {
			items[i] = strings.Repeat(label, max(canvas.Width(), 1))
		}
		return screenlayout.Block{Items: items}
	}
}

func cell(id string, minWidth, minRows, count int) Node {
	return Cell(screenlayout.Spec{
		ID: screenlayout.ID(id), MinWidth: minWidth, MinRows: minRows,
		Weight: 1, Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}, fill(id[:1], count))
}

func box(width, rows int) screenlayout.Box {
	return screenlayout.Box{Width: width, Rows: rows}
}

// taskDetailShape is `[details over subtasks] | activity` — the body both
// migrated screens already have, expressed as a tree.
func taskDetailShape() Node {
	return Cols(screenlayout.Spec{ID: "root"},
		Rows(screenlayout.Spec{ID: "left", MinWidth: 30, MinRows: 6, Weight: 1},
			cell("details", 30, 3, 12),
			cell("subtasks", 30, 3, 12),
		),
		cell("activity", 30, 6, 30),
	)
}

// The whole point of the package: whatever the tree, whatever the terminal, the
// body fits the box on BOTH axes. A nested arranger that charged its own
// leading blank row, or a hint painted out of the leftover rows, breaks this.
func TestABodyNeverLeavesItsBoxAtAnyGeometry(t *testing.T) {
	shapes := map[string]Node{
		"task detail": taskDetailShape(),
		"single cell": cell("body", 20, 3, 40),
		"bands":       Rows(screenlayout.Spec{ID: "root"}, cell("a", 20, 2, 8), cell("b", 20, 2, 8), cell("c", 20, 2, 8)),
		"grid 2x2": Rows(screenlayout.Spec{ID: "root"},
			Cols(screenlayout.Spec{ID: "top", MinWidth: 40, MinRows: 4, Weight: 1}, cell("a", 18, 2, 6), cell("b", 18, 2, 6)),
			Cols(screenlayout.Spec{ID: "bottom", MinWidth: 40, MinRows: 4, Weight: 1}, cell("c", 18, 2, 6), cell("d", 18, 2, 6)),
		),
		"board":       lanes(7).Windowed(),
		"ragged cols": raggedCols(),
		"deep": Cols(screenlayout.Spec{ID: "root"},
			Rows(screenlayout.Spec{ID: "l", MinWidth: 24, MinRows: 6, Weight: 1},
				cell("a", 24, 2, 5),
				Cols(screenlayout.Spec{ID: "l2", MinWidth: 24, MinRows: 4, Weight: 1}, cell("b", 11, 2, 5), cell("c", 11, 2, 5)),
			),
			cell("d", 24, 4, 20),
		),
	}
	// Widths include narrow sections: screenlayout caps ScrollWindowSplit hints
	// to the section width, so an 8-column column no longer overflows on "▲ N above".
	sizes := []struct{ w, h int }{
		{200, 50}, {120, 40}, {96, 30}, {80, 24}, {50, 24}, {60, 40}, {60, 16}, {40, 10}, {24, 6}, {24, 2}, {24, 1},
		{12, 8}, {8, 6}, {8, 2}, {8, 1},
	}
	for name, root := range shapes {
		for _, size := range sizes {
			b := box(size.w, size.h)
			res := Render(testKit(), NewState(), b, root)
			if got := res.Rows(); res.View != "" && got > b.Rows {
				t.Errorf("%s at %dx%d: painted %d rows, box has %d", name, size.w, size.h, got, b.Rows)
			}
			for i, line := range strings.Split(res.View, "\n") {
				if got := lipgloss.Width(line); got > b.Width {
					t.Errorf("%s at %dx%d: line %d is %d cols, box has %d", name, size.w, size.h, i, got, b.Width)
					break
				}
			}
		}
	}
}

func lanes(n int) Node {
	children := make([]Node, n)
	for i := range children {
		children[i] = cell(string(rune('a'+i))+"-lane", 20, 3, 6)
	}
	return Cols(screenlayout.Spec{ID: "board"}, children...)
}

// raggedCols is list + inspector: short scroll hints beside full-width pipe
// rows. fill() cannot catch a join that only misaligns ragged lines.
func raggedCols() Node {
	list := Cell(screenlayout.Spec{
		ID: "list", MinWidth: 40, MinRows: 6, Weight: 1,
		Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}, func(canvas screenlayout.Canvas) screenlayout.Block {
		items := make([]string, 40)
		for i := range items {
			items[i] = "item"
		}
		return screenlayout.Block{Items: items}
	})
	inspector := Cell(screenlayout.Spec{
		ID: "inspector", MinWidth: 40, MinRows: 6, Weight: 2,
		Scroll: screenlayout.ScrollItems,
	}, func(canvas screenlayout.Canvas) screenlayout.Block {
		w := max(canvas.Width(), 1)
		inner := max(w-2, 0)
		bar := "├" + strings.Repeat("─", inner) + "┤"
		items := make([]string, 20)
		for i := range items {
			items[i] = bar
		}
		return screenlayout.Block{Items: items}
	})
	return Cols(screenlayout.Spec{ID: "root", ColumnGap: 2}, list, inspector)
}

func TestRaggedColsKeepInspectorPipesAlignedWhenSideBySide(t *testing.T) {
	root := raggedCols()
	for _, size := range []struct{ w, h int }{{50, 24}, {80, 24}, {96, 24}, {120, 40}, {200, 50}} {
		assertRaggedSize(t, root, size.w, size.h)
	}
	narrow := Render(testKit(), NewState(), box(50, 24), root)
	if p, _ := narrow.Placement("root"); p.Arrangement != screenlayout.Stacked {
		t.Errorf("at 50 columns arrangement = %s, want stacked", p.Arrangement)
	}
	wide := Render(testKit(), NewState(), box(120, 40), root)
	if p, _ := wide.Placement("root"); p.Arrangement != screenlayout.SideBySide {
		t.Errorf("at 120 columns arrangement = %s, want side by side", p.Arrangement)
	}
}

func assertRaggedSize(t *testing.T, root Node, width, height int) {
	b := box(width, height)
	res := Render(testKit(), NewState(), b, root)
	if got := res.Rows(); res.View != "" && got > b.Rows {
		t.Errorf("%dx%d: painted %d rows, box has %d", width, height, got, b.Rows)
	}
	x := -1
	for i, line := range strings.Split(res.View, "\n") {
		if got := lipgloss.Width(line); got > b.Width {
			t.Errorf("%dx%d: line %d is %d cols, box has %d", width, height, i, got, b.Width)
			break
		}
		at := strings.IndexRune(line, '├')
		if at < 0 {
			continue
		}
		col := lipgloss.Width(line[:at])
		if x >= 0 && col != x {
			t.Errorf("%dx%d: inspector pipe drifted: first at %d, line %d at %d", width, height, x, i, col)
			break
		}
		x = col
	}
}

// Side by side is a breakpoint, not a promise: a row of columns that cannot fit
// its minimums stacks, at every level of the tree.
func TestARowOfColumnsStacksWhenItsMinimumsDoNotFit(t *testing.T) {
	root := taskDetailShape()
	wide := Render(testKit(), NewState(), box(120, 40), root)
	narrow := Render(testKit(), NewState(), box(50, 40), root)

	if p, _ := wide.Placement("root"); p.Arrangement != screenlayout.SideBySide {
		t.Errorf("at 120 columns the body should be side by side, got %s", p.Arrangement)
	}
	if p, _ := narrow.Placement("root"); p.Arrangement != screenlayout.Stacked {
		t.Errorf("at 50 columns the body should stack, got %s", p.Arrangement)
	}
}

// A windowed row SLIDES rather than stacking — the board's answer, which is a
// different answer from the breakpoint's and has to survive beside it.
func TestAWindowedRowSlidesInsteadOfStacking(t *testing.T) {
	root := lanes(7).Windowed()
	res := Render(testKit(), NewState(), box(80, 24), root)

	p, ok := res.Placement("board")
	if !ok {
		t.Fatal("the board reported no placement")
	}
	if !p.Windowed {
		t.Fatal("the board should have windowed its lanes")
	}
	if p.Arrangement != screenlayout.SideBySide {
		t.Errorf("the lanes it DID show should be side by side, got %s", p.Arrangement)
	}
	if p.HiddenAfter == 0 {
		t.Error("seven lanes at eighty columns should leave some off-screen")
	}
	if visible := p.Last - p.First + 1; visible+p.HiddenAfter+p.HiddenBefore != 7 {
		t.Errorf("visible %d + hidden %d/%d should account for all 7 lanes",
			visible, p.HiddenBefore, p.HiddenAfter)
	}
	if !strings.Contains(res.View, "›") {
		t.Error("the hint should say how many lanes are off to the right")
	}
}

// A windowed container may say what it hid in its OWN words.
//
// The glyphs are right for anything, and anonymous. A body whose children are
// named things the user counts — a board's lanes — has a sentence its users
// already read, and adopting the grid by deleting that sentence would be a
// migration that removed information. So the wording is a hook, and everything
// around it stays the grid's: whether a hint is painted at all, the row it is
// charged, and the width it is cut to.
func TestAWindowedRowCanSayWhatItHidInItsOwnWords(t *testing.T) {
	root := lanes(9).Windowed().WithHint(func(first, last, total int) string {
		return fmt.Sprintf("lanes %d-%d / %d", first, last, total)
	})
	b := box(70, 24)
	res := Render(testKit(), NewState(), b, root)

	p, ok := res.Placement("board")
	if !ok || !p.Windowed {
		t.Fatalf("the board reported placement %+v (found %v); this case needs a windowed one", p, ok)
	}
	rows := strings.Split(res.View, "\n")
	got := rows[len(rows)-1]
	want := fmt.Sprintf("lanes %d-%d / %d", p.First+1, p.Last+1, 9)
	if got != want {
		t.Errorf("hint row = %q, want the node's own wording %q", got, want)
	}
	if strings.Contains(res.View, "›") || strings.Contains(res.View, "‹") {
		t.Error("the declared wording did not replace the glyph hint; the body carries both")
	}

	// The seeded break: the same tree WITHOUT the hook must go back to the
	// glyphs, or this test would pass against a hook that is never consulted.
	plain := Render(testKit(), NewState(), b, lanes(9).Windowed())
	if !strings.Contains(plain.View, "›") {
		t.Fatal("a windowed row with no declared wording lost its glyph hint")
	}
	if strings.Contains(plain.View, "lanes ") {
		t.Fatal("a windowed row with no declared wording painted the other tree's sentence")
	}

	// A hint is still one row of the box, whatever it says. A closure that
	// returns more than the container is wide is cut to the container.
	long := lanes(9).Windowed().WithHint(func(int, int, int) string {
		return strings.Repeat("x", 400)
	})
	res = Render(testKit(), NewState(), b, long)
	for i, row := range strings.Split(res.View, "\n") {
		if w := lipgloss.Width(row); w > b.Width {
			t.Fatalf("row %d is %d columns wide in a %d-column box", i, w, b.Width)
		}
	}
	if res.Rows() > b.Rows {
		t.Fatalf("the declared hint painted %d rows in a %d-row box", res.Rows(), b.Rows)
	}
}

// The hint is charged BEFORE the lanes are sized. A hint paid for out of the
// leftover rows is a hint that pushes the last lane past the bottom.
func TestTheLaneHintIsChargedInsideTheBox(t *testing.T) {
	root := lanes(9).Windowed()
	for rows := 4; rows <= 30; rows++ {
		res := Render(testKit(), NewState(), box(70, rows), root)
		if got := res.Rows(); res.View != "" && got > rows {
			t.Fatalf("%d rows of box painted %d rows", rows, got)
		}
	}
}

// `tab` walks the ZONES, and a pure grouping container is not one. The left
// column arranges two zones and paints nothing itself — no header, no border,
// no cursor — so making it a stop would put the focus somewhere the eye cannot
// find it and leave the two real zones unreachable.
//
// Side by side and stacked must agree about this. They used to not: stacked
// dissolved the grouping and side by side kept it, so the same body offered
// three stops at one width and two at another.
func TestTabWalksTheZonesAndNotTheGrouping(t *testing.T) {
	root := taskDetailShape()
	want := []screenlayout.ID{"details", "subtasks", "activity", "details"}

	for _, b := range []screenlayout.Box{box(120, 40), box(50, 40)} {
		state := NewState()
		seen := make([]screenlayout.ID, 0, len(want))
		for i := range want {
			next, handled := state.HandleKey(testKit(), b, "tab", root)
			if !handled {
				t.Fatalf("%dx%d: tab %d was not handled", b.Width, b.Rows, i)
			}
			state = next
			seen = append(seen, state.Focus())
		}
		for i := range want {
			if seen[i] != want[i] {
				t.Fatalf("%dx%d: tab walked %v, want %v", b.Width, b.Rows, seen, want)
			}
		}
		if indexOf(seen, "left") >= 0 {
			t.Errorf("%dx%d: tab stopped on the grouping column, which paints nothing", b.Width, b.Rows)
		}
	}
}

// ONE definition of what is a thing and what is only an arrangement of things.
// Both the layout and the navigation read it, and a body must not offer a
// different set of stops at two widths because two copies of the rule drifted.
func TestTheRingIsTheSameZonesAtEveryWidth(t *testing.T) {
	shapes := map[string][]screenlayout.ID{
		"task detail":  {"details", "subtasks", "activity"},
		"rows of cols": {"1·1", "1·2", "2·1", "2·2"},
	}
	trees := map[string]Node{
		"task detail": taskDetailShape(),
		"rows of cols": Rows(screenlayout.Spec{ID: "root"},
			Cols(screenlayout.Spec{ID: "band 1", MinRows: 4, Weight: 1},
				cell("1·1", 20, 4, 8), cell("1·2", 20, 4, 8)),
			Cols(screenlayout.Spec{ID: "band 2", MinRows: 4, Weight: 1},
				cell("2·1", 20, 4, 8), cell("2·2", 20, 4, 8)),
		),
	}
	for name, want := range shapes {
		assertRingAtWidths(t, name, trees[name], want)
	}
}

func assertRingAtWidths(t *testing.T, name string, root Node, want []screenlayout.ID) {
	for _, b := range []screenlayout.Box{box(160, 50), box(120, 40), box(50, 40)} {
		state := NewState()
		seen := make([]screenlayout.ID, 0, len(want))
		for range want {
			next, handled := state.HandleKey(testKit(), b, "tab", root)
			if !handled {
				t.Fatalf("%s at %dx%d: tab was not handled", name, b.Width, b.Rows)
			}
			state = next
			seen = append(seen, state.Focus())
		}
		if !slices.Equal(seen, want) {
			t.Fatalf("%s at %dx%d: tab walked %v, want %v", name, b.Width, b.Rows, seen, want)
		}
	}
}

// A body declared as BANDS has no breakpoint to be past, so nothing is hidden
// while the bands fit. Handing the focused band the whole body there would be a
// mode nothing asked for — it is the answer to a row of columns giving up, not
// to a screen that was always a stack.
func TestABodyOfBandsKeepsItsBandsWhileTheyFit(t *testing.T) {
	root := Rows(screenlayout.Spec{ID: "root"},
		cell("chips", 20, 3, 6), cell("summary", 20, 8, 20), cell("panel", 20, 6, 40),
	)
	for _, focus := range []screenlayout.ID{"chips", "summary", "panel"} {
		res := Render(testKit(), NewState().WithFocus(focus), box(116, 40), root)
		if got := len(paintedLeaves(res)); got != 3 {
			t.Errorf("focused on %q the body paints %d bands, want all three", focus, got)
		}
	}
}

// Navigation follows what was DRAWN. A stacked body dissolves its columns, so
// the three zones on screen are three stops — not the two members the tree
// declares, one of which is a column that is no longer there.
func TestTabWalksTheZonesTheLayoutActuallyDrew(t *testing.T) {
	root := taskDetailShape()
	b := box(50, 24) // two 30-column minimums plus the gap do not fit: the body stacks

	if res := Render(testKit(), NewState(), b, root); res.Rows() == 0 {
		t.Fatal("the body painted nothing")
	}
	state := NewState().Resync(testKit(), b, root)
	if got := state.Focus(); got != "details" {
		t.Fatalf("a stacked body starts on %q, want the first ZONE", got)
	}

	seen := []screenlayout.ID{state.Focus()}
	for i := 0; i < 3; i++ {
		next, handled := state.HandleKey(testKit(), b, "tab", root)
		if !handled {
			t.Fatalf("tab %d was not handled", i)
		}
		state = next
		seen = append(seen, state.Focus())
	}
	want := []screenlayout.ID{"details", "subtasks", "activity", "details"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("stacked, tab walked %v, want %v", seen, want)
		}
	}
}

// A container the layout dissolved cannot be entered: its children ARE the
// level you are on, so `f` has nothing to descend into and falls back to
// filling the body.
func TestFDoesNotEnterAColumnTheBreakpointDissolved(t *testing.T) {
	root := taskDetailShape()
	b := box(50, 24)
	state := NewState().Resync(testKit(), b, root)

	state, handled := state.HandleKey(testKit(), b, "f", root)
	if !handled {
		t.Fatal("f was not handled")
	}
	if len(state.Path()) != 0 {
		t.Errorf("f descended into %v, but the stacked body has no columns left to enter", state.Path())
	}
	if !state.Fullscreen() {
		t.Error("with nothing to enter, f should have filled the body")
	}
}

// `f` enters the focused container and `tab` then walks ITS children; `esc`
// comes back out to where it was.
// A container earns a stop — and an inside — only when it is a UNIT: one that
// cannot show all its children at once, so entering it is the only way to reach
// the rest. A nested board is the case.
func TestFEntersAUnitAndEscComesBackOut(t *testing.T) {
	root := Rows(screenlayout.Spec{ID: "root"},
		cell("meta", 20, 4, 8),
		lanes(7).Windowed(),
	)
	b := box(80, 30)
	state := NewState().Resync(testKit(), b, root)
	if state.Focus() != "meta" {
		t.Fatalf("focus starts on %q, want the first zone", state.Focus())
	}

	state, handled := state.HandleKey(testKit(), b, "tab", root)
	if !handled || state.Focus() != "board" {
		t.Fatalf("tab reached %q, want the board — a unit IS a stop", state.Focus())
	}

	state, handled = state.HandleKey(testKit(), b, "f", root)
	if !handled {
		t.Fatal("f on a unit was not handled")
	}
	if got := state.Focus(); got != "a-lane" {
		t.Fatalf("entering the board focused %q, want its first lane", got)
	}
	if len(state.Path()) != 1 || state.Path()[0] != "board" {
		t.Fatalf("path is %v, want the board", state.Path())
	}

	state, _ = state.HandleKey(testKit(), b, "tab", root)
	if got := state.Focus(); got != "b-lane" {
		t.Errorf("inside the board tab reached %q, want the next lane", got)
	}

	state, handled = state.HandleKey(testKit(), b, "esc", root)
	if !handled {
		t.Fatal("esc inside a unit was not handled")
	}
	if len(state.Path()) != 0 || state.Focus() != "board" {
		t.Errorf("esc left to path %v focus %q, want the body with the board focused",
			state.Path(), state.Focus())
	}
}

// `f` on a LEAF has nothing to descend into, so it fills the body instead —
// the `f · esc` task detail and project already have. `esc` gives the layout
// back.
func TestFOnALeafFillsTheBodyAndEscRestoresTheLayout(t *testing.T) {
	root := taskDetailShape()
	b := box(120, 40)
	state := NewState().Resync(testKit(), b, root).WithFocus("activity")

	if got := len(paintedLeaves(Render(testKit(), state, b, root))); got < 2 {
		t.Fatalf("the layout paints %d zones before fullscreen, want the whole body", got)
	}

	state, handled := state.HandleKey(testKit(), b, "f", root)
	if !handled || !state.Fullscreen() {
		t.Fatal("f on a leaf should have filled the body")
	}
	if got := paintedLeaves(Render(testKit(), state, b, root)); len(got) != 1 || got[0] != "activity" {
		t.Errorf("fullscreen paints %v, want only the focused leaf", got)
	}

	state, handled = state.HandleKey(testKit(), b, "esc", root)
	if !handled || state.Fullscreen() {
		t.Fatal("esc should have dropped fullscreen")
	}
	if got := len(paintedLeaves(Render(testKit(), state, b, root))); got < 2 {
		t.Errorf("after esc the body paints %d zones, want the layout back", got)
	}
}

// At the top level there is nothing left to leave, so `esc` is handed back to
// the caller — closing an overlay or going back a screen is theirs to decide,
// not something this package may swallow.
func TestEscAtTheTopLevelIsNotConsumed(t *testing.T) {
	root := taskDetailShape()
	b := box(120, 40)
	state := NewState().Resync(testKit(), b, root)
	if _, handled := state.HandleKey(testKit(), b, "esc", root); handled {
		t.Error("esc at the body level should be left for the caller")
	}
}

// A body renders focused from its first frame, without a keystroke to ask for
// it — otherwise the first thing on screen is a layout with no zone in it, and
// the window that follows the focus has nothing to follow.
func TestAFreshStateFocusesTheFirstZoneOnResync(t *testing.T) {
	root := taskDetailShape()
	state := NewState().Resync(testKit(), box(120, 40), root)
	if got := state.Focus(); got != "details" {
		t.Errorf("a resynced grid is focused on %q, want the first zone", got)
	}
}

// A motion key on an unfocused grid still moves something: the seed repairs the
// state and the key does its own job. Only the RING keys are spent by the seed,
// because for them the seed already is the move.
func TestAMotionKeyOnAFreshGridSeedsAndMoves(t *testing.T) {
	root := taskDetailShape()
	state, handled := NewState().HandleKey(testKit(), box(120, 40), "j", root)
	if !handled {
		t.Fatal("j on a fresh grid was not handled")
	}
	if state.Focus() != "details" {
		t.Fatalf("focus is %q, want the first leaf", state.Focus())
	}
	if got := state.Layout().Cursor("details"); got != 1 {
		t.Errorf("the first leaf's cursor is %d, want 1 — the seed swallowed the key", got)
	}
}

// A motion key belongs to the focused leaf, wherever it is nested. It reaches
// the arranger that placed that leaf this frame, which is the only call that
// knows that leaf's box.
func TestAMotionKeyMovesTheFocusedLeafOnly(t *testing.T) {
	root := taskDetailShape()
	b := box(120, 40)
	state := NewState().WithFocus("activity")

	for i := 0; i < 5; i++ {
		next, handled := state.HandleKey(testKit(), b, "j", root)
		if !handled {
			t.Fatalf("j %d was not handled", i)
		}
		state = next
	}
	if got := state.Layout().Cursor("activity"); got != 5 {
		t.Errorf("the focused leaf's cursor is %d, want 5", got)
	}
	if got := state.Layout().Cursor("details"); got > 0 {
		t.Errorf("an unfocused leaf moved to %d", got)
	}
}

// A stacked body dissolves the grouping Rows, so the three zones on screen
// belong to the ROOT's arranger. The declared parent of "details" is "left",
// which is not arranged this frame. A motion key that looks up that parent
// finds no frame and eats the keystroke — the zone is focused, it shows
// overflow, and j does nothing.
func TestAMotionKeyMovesALeafAfterItsParentDissolved(t *testing.T) {
	root := taskDetailShape()
	b := box(50, 40) // two 30-column minimums plus the gap do not fit: the body stacks
	res := Render(testKit(), NewState(), b, root)
	place, ok := res.Placement("root")
	if !ok {
		t.Fatal("root reported no placement")
	}
	if place.Arrangement != screenlayout.Stacked {
		t.Fatalf("arrangement = %s, want Stacked so the grouping has dissolved", place.Arrangement)
	}
	painted := paintedLeaves(res)
	if len(painted) < 3 {
		t.Fatalf("stacked body painted %v, want details, subtasks and activity together", painted)
	}

	state := NewState().WithFocus("details")
	next, handled := state.HandleKey(testKit(), b, "j", root)
	if !handled {
		t.Fatal("j on a focused stacked leaf was not handled")
	}
	if got := next.Layout().Cursor("details"); got != 1 {
		t.Fatalf("details cursor = %d, want 1", got)
	}
}

// h and l are the second axis. They move the window and take focus with them,
// so the lane you slid to is the lane you are in.
func TestSlidingMovesTheWindowAndTheFocus(t *testing.T) {
	root := lanes(7).Windowed()
	b := box(80, 24)
	state := NewState()

	state, handled := state.HandleKey(testKit(), b, "l", root)
	if !handled {
		t.Fatal("l was not handled")
	}
	if state.Focus() != "b-lane" {
		t.Errorf("focus is %q, want the next lane", state.Focus())
	}

	// Slide to the far end and the window must have moved with it.
	for i := 0; i < 6; i++ {
		state, _ = state.HandleKey(testKit(), b, "l", root)
	}
	if state.Focus() != "g-lane" {
		t.Fatalf("focus is %q after sliding to the end", state.Focus())
	}
	res := Render(testKit(), state, b, root)
	p, _ := res.Placement("board")
	if p.Last != 6 {
		t.Errorf("the window ends at lane %d, want the last one", p.Last)
	}
	if p.HiddenAfter != 0 {
		t.Errorf("%d lanes still hidden to the right of the last one", p.HiddenAfter)
	}
}

// A lane the user tabbed into and cannot see is worse than no window at all.
func TestTheWindowFollowsFocusEvenWhenNothingSlidIt(t *testing.T) {
	root := lanes(7).Windowed()
	state := NewState().WithFocus("g-lane")
	res := Render(testKit(), state, box(80, 24), root)
	p, _ := res.Placement("board")
	if p.First > 6 || p.Last < 6 {
		t.Errorf("the focused lane 6 is outside the window %d..%d", p.First, p.Last)
	}
}

// Past the breakpoint AND out of rows, the focused zone gets the WHOLE body
// and its siblings come off the screen. This is not a rule invented here — it
// is what internal/tui/layout already does for task detail (layout.go:169-212).
func TestAStackedBodyOutOfRowsGivesTheFocusedZoneEverything(t *testing.T) {
	root := Cols(screenlayout.Spec{ID: "root"},
		cell("a", 60, 6, 40), cell("b", 60, 6, 40), cell("c", 60, 6, 40),
	)
	// Three 60-column minimums cannot fit: the columns give up. And 3×6 rows
	// cannot fit in 14 either, so there is no layout left to keep.
	b := box(80, 14)
	res := Render(testKit(), NewState().WithFocus("b"), b, root)

	shown := map[screenlayout.ID]int{}
	for _, p := range res.Placements {
		if p.Leaf && !p.Dropped {
			shown[p.ID] = p.Box.Rows
		}
	}
	if len(shown) != 1 {
		t.Fatalf("%d zones on screen, want only the focused one: %v", len(shown), shown)
	}
	if _, ok := shown["b"]; !ok {
		t.Fatalf("the zone on screen is %v, want the focused one", shown)
	}
	// Everything the box has, less the row the hint spends saying what is hidden.
	if got := shown["b"]; got < b.Rows-1 {
		t.Errorf("the focused zone got %d of %d rows — full screen means full screen", got, b.Rows)
	}

	p, _ := res.Placement("root")
	if p.HiddenBefore != 1 || p.HiddenAfter != 1 {
		t.Errorf("hid %d above and %d below, want one each", p.HiddenBefore, p.HiddenAfter)
	}
	if !strings.Contains(res.View, "▲") || !strings.Contains(res.View, "▼") {
		t.Error("a stack that hid zones on both sides should say so on both sides")
	}
	if got := res.Rows(); got > b.Rows {
		t.Errorf("painted %d rows into a box of %d", got, b.Rows)
	}
}

// A FIXED zone is the exception, and the same one the app makes for the form: it
// declared exactly how many rows it wants, so it cannot use a whole body. It
// keeps its rows and the siblings after it cascade into what is left.
func TestAFixedZoneKeepsItsRowsAndTheRestCascade(t *testing.T) {
	form := Cell(screenlayout.Spec{
		ID: "form", MinWidth: 20, MinRows: 4, MaxRows: 4, Weight: 1,
		Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}, fill("f", 40))
	root := Rows(screenlayout.Spec{ID: "root"}, form, cell("b", 20, 4, 40), cell("c", 20, 4, 40))

	res := Render(testKit(), NewState().WithFocus("form"), box(80, 24), root)
	shown := map[screenlayout.ID]int{}
	for _, p := range res.Placements {
		if p.Leaf && !p.Dropped {
			shown[p.ID] = p.Box.Rows
		}
	}
	if len(shown) < 2 {
		t.Fatalf("a fixed zone cannot fill a body on its own; its siblings should have cascaded in: %v", shown)
	}
	if got := shown["form"]; got != 4 {
		t.Errorf("the fixed zone took %d rows, want the 4 it declared", got)
	}
}

// Spec.ColumnMaxRows is a ceiling that means something whenever a zone is
// sharing what it is placed in with a sibling — the field table over the
// detail box, the task grid over the sub-task board, or a fixed zone
// cascading beside the siblings a stacked body kept. It caps the zone in all
// three; the one place it stops applying is a zone that has the whole body to
// itself, with no sibling left for the ceiling to be a share of.
func TestAColumnCeilingAppliesOnlyWhileSharingAColumn(t *testing.T) {
	shape := func() Node {
		details := Cell(screenlayout.Spec{
			ID: "details", MinWidth: 30, MinRows: 3, ColumnMaxRows: 6,
			Weight: 1, Scroll: screenlayout.ScrollItems, SelectFirst: true,
		}, fill("d", 40))
		return Cols(screenlayout.Spec{ID: "root"},
			Rows(screenlayout.Spec{ID: "left", MinWidth: 30, MinRows: 6, Weight: 1},
				details, cell("subtasks", 30, 3, 40),
			),
			cell("activity", 30, 6, 40),
		)
	}

	// Side by side, details and subtasks share the left column: the ceiling
	// caps details and subtasks takes the room it refused.
	sideBySide := Render(testKit(), NewState().WithFocus("details"), box(120, 30), shape())
	details, _ := sideBySide.Placement("details")
	subtasks, _ := sideBySide.Placement("subtasks")
	if details.Box.Rows > 6 {
		t.Errorf("details got %d rows sharing the column, want its ColumnMaxRows ceiling of 6", details.Box.Rows)
	}
	if subtasks.Box.Rows <= 6 {
		t.Errorf("subtasks got %d rows, want more than details' 6-row ceiling once details stopped eating them", subtasks.Box.Rows)
	}

	// Narrow, and tall enough for details to reach its ceiling beside
	// sub-tasks but not for activity to join them too. Details is the FIXED
	// zone — it declared a ceiling of its own — so its focus cascades rather
	// than fullscreens: the cascade drops activity first (declaration order),
	// and keeps sub-tasks only once details can have the full ceiling it
	// declared — not merely a floor-sum guess that ceiling would fit.
	stacked := Render(testKit(), NewState().WithFocus("details"), box(32, 14), shape())
	shown := map[screenlayout.ID]int{}
	for _, p := range stacked.Placements {
		if p.Leaf && !p.Dropped {
			shown[p.ID] = p.Box.Rows
		}
	}
	if len(shown) != 2 {
		t.Fatalf("%d zones on screen, want details and subtasks cascading: %v", len(shown), shown)
	}
	if _, ok := shown["activity"]; ok {
		t.Fatalf("activity is on screen at %v, want it dropped first", shown)
	}
	if got := shown["details"]; got != 6 {
		t.Errorf("details got %d rows cascading beside subtasks, want its full column ceiling of 6", got)
	}
	if got := shown["subtasks"]; got < 3 {
		t.Errorf("subtasks got %d rows, under its MinRows floor of 3", got)
	}
}

// A stack's children share its width, so a box narrower than a child's MinWidth
// cannot be repaired by showing fewer of them. The floor is one zone with the
// whole box — which is what task detail's three 60-column zones have to do on a
// 33-column terminal.
func TestABoxTooNarrowForAnyChildGivesTheFocusedOneEverything(t *testing.T) {
	root := taskDetailShape() // details and subtasks declare MinWidth 30
	b := box(20, 23)

	state := NewState().Resync(testKit(), b, root)
	res := Render(testKit(), state, b, root)

	p, ok := res.Placement("root")
	if !ok || !p.Windowed {
		t.Fatal("no child can have its minimum width; the body should have collapsed to one zone")
	}
	if p.First != p.Last {
		t.Errorf("the window shows children %d..%d, want exactly one", p.First, p.Last)
	}

	shown := 0
	for _, q := range res.Placements {
		if q.Leaf && !q.Dropped {
			shown++
		}
	}
	if shown != 1 {
		t.Errorf("%d leaves painted, want only the focused one", shown)
	}
	if got := res.Rows(); got > b.Rows {
		t.Errorf("painted %d rows into a box of %d", got, b.Rows)
	}

	// And `tab` is what the other zones are behind, not a dead end.
	next, handled := state.HandleKey(testKit(), b, "tab", root)
	if !handled || next.Focus() == state.Focus() {
		t.Fatal("tab should move to the next zone even when only one is on screen")
	}
	if got := paintedLeaves(Render(testKit(), next, b, root)); len(got) != 1 || got[0] != next.Focus() {
		t.Errorf("after tab the body paints %v, want only the newly focused zone", got)
	}
}

// A Cols past its width breakpoint paints BOTH children stacked while the box
// can afford the minimums they declared. Hiding a zone the terminal has room for
// is throwing rows away, and the user pays for it twice — once in the zone that
// vanished, once in the blank rows the survivor cannot fill.
//
// This used to be opt-in, as StackWhenNarrow(), and no screen ever opted in. The
// default was the wrong way round: Studio's two-pane bodies hid one pane on a
// forty-five-row terminal where each pane wanted seven rows.
func TestAStackedBodyKeepsEveryZoneItCanAfford(t *testing.T) {
	root := Cols(screenlayout.Spec{ID: "root"},
		cell("list", 40, 6, 20), cell("inspector", 40, 8, 20),
	)
	b := box(76, 14) // 40+40+gap misses 76; 6+8 fits 14 stacked
	res := Render(testKit(), NewState().WithFocus("list"), b, root)

	p, ok := res.Placement("root")
	if !ok {
		t.Fatal("root reported no placement")
	}
	if p.Arrangement != screenlayout.Stacked {
		t.Fatalf("arrangement = %s, want stacked", p.Arrangement)
	}
	shown := paintedLeaves(res)
	if len(shown) != 2 {
		t.Fatalf("painted %v, want both leaves stacked", shown)
	}
	if p.HiddenBefore != 0 || p.HiddenAfter != 0 {
		t.Errorf("hid %d/%d, want both children on screen", p.HiddenBefore, p.HiddenAfter)
	}

	wide := Render(testKit(), NewState(), box(116, 30), root)
	if got, _ := wide.Placement("root"); got.Arrangement != screenlayout.SideBySide {
		t.Fatalf("at 116 columns arrangement = %s, want side by side", got.Arrangement)
	}
}

// `tab` moves the focus. It does not move a single box.
//
// The defect this pins is the one the rule above removes, stated as the property
// a user actually observes: focusing the read-only half of a two-pane body used
// to delete the other half. Focus decides who takes the keys and who paints the
// ▸; filling the body with one zone is what `f` is for, and it is asked for.
func TestFocusDoesNotChangeTheLayout(t *testing.T) {
	root := Cols(screenlayout.Spec{ID: "root"},
		cell("list", 40, 7, 20), cell("inspector", 40, 7, 20),
	)
	b := box(76, 45) // stacked (40+40+gap misses 76), and rows to spare

	before := Render(testKit(), NewState().WithFocus("list"), b, root)
	after := Render(testKit(), NewState().WithFocus("inspector"), b, root)

	for _, id := range []screenlayout.ID{"list", "inspector"} {
		was, _ := before.Placement(id)
		now, _ := after.Placement(id)
		if was.Box != now.Box || was.Dropped != now.Dropped {
			t.Errorf("%s was %+v (dropped=%v) and became %+v (dropped=%v) because the focus moved",
				id, was.Box, was.Dropped, now.Box, now.Dropped)
		}
	}
	if got := paintedLeaves(after); len(got) != 2 {
		t.Fatalf("focusing the second zone painted %v, want both", got)
	}

	// `f` is the explicit way to fill the body, and it still works.
	full, handled := NewState().WithFocus("inspector").HandleKey(testKit(), b, EnterKey, root)
	if !handled {
		t.Fatal("f was not consumed")
	}
	if got := paintedLeaves(Render(testKit(), full, b, root)); len(got) != 1 || got[0] != "inspector" {
		t.Errorf("after f the body paints %v, want only the focused zone", got)
	}
}

// paintedLeaves are the leaves a frame actually put on screen.
func paintedLeaves(res Result) []screenlayout.ID {
	var out []screenlayout.ID
	for _, p := range res.Placements {
		if p.Leaf && !p.Dropped {
			out = append(out, p.ID)
		}
	}
	return out
}

// Hidden is not unreachable: the ring walks every leaf and the window follows
// the focus, so `tab` is what brings a hidden section on screen.
func TestTabBringsAHiddenSectionIntoView(t *testing.T) {
	root := Rows(screenlayout.Spec{ID: "root"},
		cell("a", 20, 6, 40), cell("b", 20, 6, 40), cell("c", 20, 6, 40),
	)
	b := box(80, 10)
	state := NewState().Resync(testKit(), b, root)
	if p, _ := Render(testKit(), state, b, root).Placement("root"); p.First != 0 {
		t.Fatalf("the window starts at %d, want the focused first band", p.First)
	}

	for i := 0; i < 2; i++ {
		next, handled := state.HandleKey(testKit(), b, "tab", root)
		if !handled {
			t.Fatalf("tab %d was not handled", i)
		}
		state = next
	}
	if state.Focus() != "c" {
		t.Fatalf("focus is %q after two tabs, want the third band", state.Focus())
	}
	p, _ := Render(testKit(), state, b, root).Placement("root")
	if p.First > 2 || p.Last < 2 {
		t.Errorf("the focused band 2 is outside the window %d..%d", p.First, p.Last)
	}
	if p.HiddenBefore == 0 {
		t.Error("having scrolled to the last band, the earlier ones should be counted above")
	}
}

// A root that is a single cell goes through the same clip as a nested one, and
// its keys still reach the arranger.
func TestARootCellScrollsLikeAnyOtherLeaf(t *testing.T) {
	root := cell("body", 20, 3, 50)
	b := box(80, 12)
	state := NewState().WithFocus("body")
	state, handled := state.HandleKey(testKit(), b, "G", root)
	if !handled {
		t.Fatal("G was not handled on a root cell")
	}
	if got := state.Layout().Cursor("body"); got != 49 {
		t.Errorf("cursor is %d, want the last item", got)
	}
	if got := Render(testKit(), state, b, root).Rows(); got > b.Rows {
		t.Errorf("painted %d rows into a box of %d", got, b.Rows)
	}
}

// A root cell has no parent level to fullscreen into, so its screen keeps f
// (and esc must not clear an invisible fullscreen flag afterward).
func TestARootCellLeavesFullscreenKeysToItsScreen(t *testing.T) {
	root := cell("body", 20, 3, 12)
	b := box(80, 12)
	state := NewState().WithFocus("body")

	next, handled := state.HandleKey(testKit(), b, "f", root)
	if handled {
		t.Fatal("f on a root cell was consumed instead of reaching the screen")
	}
	if next.Fullscreen() {
		t.Fatal("an unhandled root-cell f must not set fullscreen")
	}

	if _, handled = next.HandleKey(testKit(), b, "esc", root); handled {
		t.Fatal("esc after root-cell f was consumed by invisible fullscreen")
	}
}

// The constructors normalise their children's specs, so they must not be able
// to normalise a slice the caller still holds.
func TestAConstructorNeverRewritesTheCallersNodes(t *testing.T) {
	children := []Node{cell("a", 10, 2, 3), cell("b", 10, 2, 3)}
	_ = Cols(screenlayout.Spec{ID: "row"}, children...)
	for _, child := range children {
		if child.Spec.Column {
			t.Fatalf("Cols reached back and rewrote %q in the caller's slice", child.Spec.ID)
		}
	}
	sideBySide := Cols(screenlayout.Spec{ID: "row"}, children...)
	_ = Rows(screenlayout.Spec{ID: "stack"}, sideBySide.children...)
	for _, child := range sideBySide.children {
		if !child.Spec.Column {
			t.Fatalf("Rows reached back and rewrote %q in another node's children", child.Spec.ID)
		}
	}
}

// A container is placed like a cell and scrolls like nothing. A caller that
// hands one an item policy must not turn the nested view's LINES into scrollable
// items — that is the unit mismatch (lines versus items) the arranger exists to
// make unspellable, arriving one level up.
func TestAContainerNeverBecomesAScrollSurface(t *testing.T) {
	inner := screenlayout.Spec{
		ID: "left", MinWidth: 30, MinRows: 6, Weight: 1,
		Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}
	root := Cols(screenlayout.Spec{ID: "root"},
		Rows(inner, cell("details", 30, 3, 12), cell("subtasks", 30, 3, 12)),
		cell("activity", 30, 6, 30),
	)
	for _, id := range navMembers(root.children) {
		if id == "left" {
			t.Fatal("a container reached the focus ring; only leaves hold a cursor")
		}
	}

	// And the geometry it DID declare is honoured, so dropping the item half is
	// not dropping the container's placement.
	res := Render(testKit(), NewState(), box(120, 30), root)
	if p, ok := res.Placement("left"); !ok || p.Dropped {
		t.Fatal("the container declared a placeable geometry and was not placed")
	}
	if got := res.Rows(); got > 30 {
		t.Errorf("painted %d rows into a box of 30", got)
	}
}

// Resync settles a stored selection against data that changed under it.
func TestResyncClampsASelectionThatOutlivedItsItems(t *testing.T) {
	b := box(120, 40)
	big := taskDetailShape()
	state := NewState().WithFocus("activity")
	for i := 0; i < 20; i++ {
		state, _ = state.HandleKey(testKit(), b, "j", big)
	}
	if got := state.Layout().Cursor("activity"); got != 20 {
		t.Fatalf("cursor is %d before the data shrank, want 20", got)
	}

	small := Cols(screenlayout.Spec{ID: "root"},
		Rows(screenlayout.Spec{ID: "left", MinWidth: 30, MinRows: 6, Weight: 1},
			cell("details", 30, 3, 12), cell("subtasks", 30, 3, 12),
		),
		cell("activity", 30, 6, 4),
	)
	state = state.Resync(testKit(), b, small)
	if got := state.Layout().Cursor("activity"); got != 3 {
		t.Errorf("cursor is %d after resync, want the last of four items", got)
	}
}
