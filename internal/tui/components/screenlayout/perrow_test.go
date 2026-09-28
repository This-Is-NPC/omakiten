package screenlayout

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"omakiten/internal/tui/components/cardtable"
)

// perRowGrid is the fixture this file is about: n already-painted "cards" laid
// out perRow to a terminal line. Heights vary on a three-cycle so a line is the
// TALLEST of its cards rather than a constant, which is the case a fold that
// simply multiplied would get right by accident.
const perRowGridID = ID("grid")

func perRowCard(i int) string {
	rows := 2 + i%3
	out := make([]string, rows)
	for r := range out {
		out[r] = fmt.Sprintf("c%02d.%d", i, r)
	}
	return strings.Join(out, "\n")
}

func perRowCards(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = perRowCard(i)
	}
	return out
}

// perRowSection is the grid as a section. perRow of 0 or 1 is the declaration a
// section that lays one item to a line makes.
func perRowSection(n, perRow int) Func {
	cards := perRowCards(n)
	return Func{
		Def: Spec{ID: perRowGridID, MinRows: 1, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block {
			return Block{Items: cards, PerRow: perRow}
		},
	}
}

func perRowBox() Box { return Box{Width: 80, Rows: 14} }

// perRowDrive replays keys through the same HandleKeyIn a screen uses and hands
// back the state plus the placement the next frame would paint.
func perRowDrive(t *testing.T, n, perRow int, keys ...string) (State, Placement) {
	t.Helper()
	kit := testKit(100, 40, 2)
	box := perRowBox()
	section := perRowSection(n, perRow)
	state := NewState().WithFocus(perRowGridID).ResyncIn(kit, box, section)
	for _, key := range keys {
		next, handled := state.HandleKeyIn(kit, box, key, section)
		if !handled {
			t.Fatalf("key %q was not handled by the arranger", key)
		}
		state = next
	}
	place, ok := ArrangeIn(kit, box, state, section).Placement(perRowGridID)
	if !ok {
		t.Fatal("no placement for the grid section")
	}
	return state, place
}

// assertPerRowWindow is every invariant a folded section owes, in one place so
// the three tests below assert the same thing and cannot drift.
//
// The PERSISTED pair is checked beside the painted one on purpose: Arrange
// re-clamps whatever it is handed, so asserting only on the placement would be
// asserting that the paint cleans up after the key.
func assertPerRowWindow(t *testing.T, where string, state State, place Placement, n, perRow int) {
	t.Helper()
	if stored := state.Cursor(perRowGridID); stored < 0 || stored >= n {
		t.Fatalf("%s: the keystroke persisted cursor %d, outside the %d cards", where, stored, n)
	}
	if stored := state.Offset(perRowGridID); stored%perRow != 0 {
		t.Fatalf("%s: the keystroke persisted offset %d, not the first card of a line", where, stored)
	}
	if place.Dropped {
		return
	}
	if place.PerRow != perRow {
		t.Fatalf("%s: PerRow reported %d, want %d", where, place.PerRow, perRow)
	}
	if place.Cursor < 0 || place.Cursor >= n {
		t.Fatalf("%s: cursor %d is outside the %d cards", where, place.Cursor, n)
	}
	if place.Offset%perRow != 0 || place.First != place.Offset {
		t.Fatalf("%s: offset %d / First %d is not the first card of a line", where, place.Offset, place.First)
	}
	visible := 0
	if place.Last >= place.First {
		visible = place.Last - place.First + 1
	}
	if place.Above+visible+place.Below != n {
		t.Fatalf("%s: %d above + %d visible + %d below != %d cards", where, place.Above, visible, place.Below, n)
	}
	if visible > 0 && (place.Cursor < place.First || place.Cursor > place.Last) {
		t.Fatalf("%s: cursor %d fell outside the painted window [%d,%d]", where, place.Cursor, place.First, place.Last)
	}
}

// TestPerRowZeroAndOneAreTheSameFrameAsSayingNothing is the no-op half.
//
// A Block that declares PerRow 0 or 1 must produce the frame a Block that never
// mentioned the field produces — the same view, the same window, the same pair
// — because twenty of the twenty-one screens lay one item to a line and none of
// them opted into anything.
func TestPerRowZeroAndOneAreTheSameFrameAsSayingNothing(t *testing.T) {
	t.Parallel()
	kit := testKit(100, 40, 2)
	box := perRowBox()
	cards := perRowCards(17)
	silent := Func{
		Def:  Spec{ID: perRowGridID, MinRows: 1, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block { return Block{Items: cards} },
	}
	for _, declared := range []int{0, 1} {
		spoken := perRowSection(len(cards), declared)
		for _, keys := range [][]string{nil, {"j"}, {"j", "j", "j"}, {"G"}, {"pgdown"}, {"pgdown", "pgup"}, {"G", "g"}} {
			base := NewState().WithFocus(perRowGridID).ResyncIn(kit, box, silent)
			said := NewState().WithFocus(perRowGridID).ResyncIn(kit, box, spoken)
			for _, key := range keys {
				base, _ = base.HandleKeyIn(kit, box, key, silent)
				said, _ = said.HandleKeyIn(kit, box, key, spoken)
			}
			assertSameFrame(t, fmt.Sprintf("PerRow %d after %v", declared, keys),
				ArrangeIn(kit, box, base, silent), ArrangeIn(kit, box, said, spoken))
		}
	}
}

// assertSameFrame is byte equality of the body plus number equality of every
// window figure — the whole observable surface of a resolve.
func assertSameFrame(t *testing.T, where string, want, got Result) {
	t.Helper()
	if got.View != want.View {
		t.Fatalf("%s painted a different body than saying nothing", where)
	}
	wp, _ := want.Placement(perRowGridID)
	gp, _ := got.Placement(perRowGridID)
	window := func(p Placement) []int {
		return []int{p.Cursor, p.Offset, p.First, p.Last, p.Above, p.Below, p.CursorLine, p.PerRow}
	}
	if fmt.Sprint(window(gp)) != fmt.Sprint(window(wp)) {
		t.Fatalf("%s: window %v, want %v", where, window(gp), window(wp))
	}
	if gp.PerRow != 1 {
		t.Fatalf("%s: PerRow was reported as %d; anything below two is one item per line", where, gp.PerRow)
	}
}

// TestPerRowStepsTheCursorOneCardAndWrapsAcrossLines is the granularity the
// whole field exists for: `j` is the next CARD, not the next line, and it
// crosses a line boundary without skipping the cards on it.
func TestPerRowStepsTheCursorOneCardAndWrapsAcrossLines(t *testing.T) {
	t.Parallel()
	const n, perRow = 20, 3
	for step := 0; step < n+3; step++ {
		keys := make([]string, step)
		for i := range keys {
			keys[i] = "j"
		}
		_, place := perRowDrive(t, n, perRow, keys...)
		want := min(step, n-1)
		if place.Cursor != want {
			t.Fatalf("%d presses of j put the cursor on card %d, want %d", step, place.Cursor, want)
		}
	}
}

// TestPerRowWindowsByLineWhileTheCursorCountsCards is the other half: the
// offset only ever names the FIRST CARD OF A LINE, and the reported window
// accounts for every card exactly once.
func TestPerRowWindowsByLineWhileTheCursorCountsCards(t *testing.T) {
	t.Parallel()
	const n, perRow = 41, 4
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, "j")
		state, place := perRowDrive(t, n, perRow, keys...)
		assertPerRowWindow(t, fmt.Sprintf("after %d j", i+1), state, place, n, perRow)
	}
}

// TestPerRowFirstAndLastReachTheFirstAndLastCard pins g/G on the CARD ends. A
// fold that jumped to the last LINE would land on the first card of it, which
// is a different card whenever the last line is full.
func TestPerRowFirstAndLastReachTheFirstAndLastCard(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 5, 12, 13, 41} {
		for _, perRow := range []int{2, 3, 6} {
			if _, place := perRowDrive(t, n, perRow, "G"); place.Cursor != n-1 {
				t.Fatalf("n=%d perRow=%d: G reached card %d, want %d", n, perRow, place.Cursor, n-1)
			}
			if _, place := perRowDrive(t, n, perRow, "G", "g"); place.Cursor != 0 {
				t.Fatalf("n=%d perRow=%d: g reached card %d, want 0", n, perRow, place.Cursor)
			}
		}
	}
}

// TestPerRowPagesWholeLinesAndLandsOnACard is the page-key contract: a page
// travels a whole number of LINES — so the cursor keeps its column — and never
// leaves the cards that exist.
func TestPerRowPagesWholeLinesAndLandsOnACard(t *testing.T) {
	t.Parallel()
	const n, perRow = 41, 4
	for _, key := range []string{"pgdown", "ctrl+d"} {
		var keys []string
		for press := 1; press <= 6; press++ {
			keys = append(keys, key)
			_, place := perRowDrive(t, n, perRow, keys...)
			if place.Cursor < 0 || place.Cursor >= n {
				t.Fatalf("%d presses of %q left the cursor on %d, outside the %d cards", press, key, place.Cursor, n)
			}
			// Either the page travelled whole lines (same column), or it hit the
			// end of the list and clamped onto the last card.
			if place.Cursor%perRow != 0 && place.Cursor != n-1 {
				t.Fatalf("%d presses of %q landed on card %d — column %d, not the column it left, and not the last card",
					press, key, place.Cursor, place.Cursor%perRow)
			}
		}
	}
	if _, place := perRowDrive(t, n, perRow, "G", "pgup"); place.Cursor%perRow != (n-1)%perRow {
		t.Fatalf("pgup off the last card landed on column %d, want column %d",
			place.Cursor%perRow, (n-1)%perRow)
	}
}

// TestPerRowNeverLandsPastAPartialLastLine is the off-the-end case. A list
// whose last line is short has cursor positions the fold's own arithmetic
// would happily produce — `lastLine*perRow + column` — and none of them exist.
func TestPerRowNeverLandsPastAPartialLastLine(t *testing.T) {
	t.Parallel()
	for _, n := range []int{7, 10, 13, 17} {
		for _, perRow := range []int{3, 4, 6} {
			if n%perRow == 0 {
				continue
			}
			for _, keys := range [][]string{{"G"}, {"G", "j"}, {"pgdown", "pgdown", "pgdown", "pgdown"}, {"G", "pgdown"}} {
				state, place := perRowDrive(t, n, perRow, keys...)
				assertPerRowWindow(t, fmt.Sprintf("n=%d perRow=%d after %v", n, perRow, keys), state, place, n, perRow)
			}
		}
	}
}

// TestPerRowPaintsTheLineCardtableWouldHaveJoined is the byte half: the fold
// produces exactly what a screen packing its own cards produced, so migrating
// onto the field is a move rather than a rewrite.
func TestPerRowPaintsTheLineCardtableWouldHaveJoined(t *testing.T) {
	t.Parallel()
	const n, perRow = 9, 3
	kit := testKit(100, 40, 2)
	box := Box{Width: 80, Rows: 40}
	section := perRowSection(n, perRow)
	state := NewState().WithFocus(perRowGridID).ResyncIn(kit, box, section)
	view := ArrangeIn(kit, box, state, section).View
	cards := perRowCards(n)
	for line := 0; line < n/perRow; line++ {
		joined := cardtable.Row(cards[line*perRow : (line+1)*perRow])
		if !strings.Contains(view, joined) {
			t.Fatalf("line %d is not the cardtable.Row join of its cards:\n%s", line, view)
		}
	}
}

// TestPerRowHoldsItsInvariantsUnderARandomWalk is the stateful-component form
// CONTRIBUTING asks for: assert after EVERY op, not just at the end.
func TestPerRowHoldsItsInvariantsUnderARandomWalk(t *testing.T) {
	t.Parallel()
	seed := int64(20260820)
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))
	keys := []string{"j", "k", "down", "up", "pgdown", "pgup", "g", "G"}
	kit := testKit(100, 40, 2)
	for trial := 0; trial < 40; trial++ {
		n := 1 + rng.Intn(45)
		perRow := 2 + rng.Intn(5)
		box := Box{Width: 80, Rows: 6 + rng.Intn(20)}
		section := perRowSection(n, perRow)
		state := NewState().WithFocus(perRowGridID).ResyncIn(kit, box, section)
		var history []string
		for op := 0; op < 30; op++ {
			key := keys[rng.Intn(len(keys))]
			history = append(history, key)
			state, _ = state.HandleKeyIn(kit, box, key, section)
			result := ArrangeIn(kit, box, state, section)
			place, ok := result.Placement(perRowGridID)
			if !ok {
				continue
			}
			where := fmt.Sprintf("n=%d perRow=%d box=%+v keys=%v", n, perRow, box, history)
			assertPerRowWindow(t, where, state, place, n, perRow)
			if rows := strings.Count(result.View, "\n") + 1; rows > box.Rows {
				t.Fatalf("%s: painted %d rows in a %d-row box", where, rows, box.Rows)
			}
		}
	}
}
