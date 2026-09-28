package screenlayout

import (
	"testing"
)

func TestSpecHelpersAndNormalisationHoldTheDocumentedDefaults(t *testing.T) {
	fixed := Spec{ID: "f", MinWidth: 30, MinRows: 9, MaxRows: 9}.normalize()
	if fixed.MinRows != 9 || fixed.MaxRows != 9 || fixed.MinWidth != 30 || fixed.Scroll.Scrolls() {
		t.Fatalf("fixed spec produced %+v", fixed)
	}
	flex := Spec{ID: "x", MinWidth: 20, MinRows: 4, Weight: 3, Scroll: ScrollItems}.normalize()
	if flex.MinRows != 4 || flex.Weight != 3 || flex.MaxRows != 0 || !flex.Scroll.Scrolls() {
		t.Fatalf("flex spec produced %+v", flex)
	}
	odd := Spec{ID: "odd", MinWidth: -5, MinRows: 0, MaxRows: 2, Weight: -1}.normalize()
	if odd.MinRows != 1 {
		t.Fatalf("MinRows normalised to %d, want at least 1", odd.MinRows)
	}
	if odd.MaxRows != 2 {
		t.Fatalf("MaxRows normalised to %d, want the declared 2", odd.MaxRows)
	}
	if odd.Weight != 1 {
		t.Fatalf("Weight normalised to %d, want 1", odd.Weight)
	}
	if odd.MinWidth != 0 {
		t.Fatalf("MinWidth normalised to %d, want 0", odd.MinWidth)
	}
	contradictory := Spec{ID: "c", MinRows: 8, MaxRows: 3}.normalize()
	if contradictory.MaxRows != 8 {
		t.Fatalf("a maximum below the minimum resolved to %d, want the minimum 8", contradictory.MaxRows)
	}
}

func TestArrangementAndActionNameThemselvesForFailureMessages(t *testing.T) {
	if got := Stacked.String(); got != "Stacked" {
		t.Errorf("Stacked.String() = %q", got)
	}
	if got := SideBySide.String(); got != "SideBySide" {
		t.Errorf("SideBySide.String() = %q", got)
	}
	if got := Arrangement(42).String(); got != "Stacked" {
		t.Errorf("an unknown arrangement named itself %q", got)
	}
	names := map[Action]string{
		ActionNone: "None", ActionUp: "Up", ActionDown: "Down",
		ActionPageUp: "PageUp", ActionPageDown: "PageDown",
		ActionFirst: "First", ActionLast: "Last",
		ActionNextSection: "NextSection", ActionPrevSection: "PrevSection",
	}
	for action, want := range names {
		if got := action.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", action, got, want)
		}
	}
	if got := Action(99).String(); got != "None" {
		t.Errorf("an unknown action named itself %q", got)
	}
}

func TestTheCanvasCarriesTheArrangersCursorToTheSection(t *testing.T) {
	kit := testKit(100, 30, 2)
	var seen int
	sec := Func{
		Def: Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(c Canvas) Block {
			seen = c.Cursor()
			return Block{Items: numbered("row", 20)}
		},
	}
	Arrange(kit, NewState().WithCursor("s", 7), sec)
	if seen != 7 {
		t.Fatalf("the section was handed cursor %d, want the stored 7", seen)
	}
	// And a fresh section with no SelectFirst sees the no-selection sentinel.
	Arrange(kit, NewState(), sec)
	if seen != -1 {
		t.Fatalf("a fresh section was handed cursor %d, want -1", seen)
	}
}

func TestABlockCanOverrideTheArrangersCursorAndTheZeroValueDoesNot(t *testing.T) {
	kit := testKit(100, 30, 2)
	spec := Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems}

	override := Func{Def: spec, Body: func(Canvas) Block {
		return Block{Items: numbered("row", 20), Cursor: At(5)}
	}}
	p, _ := Arrange(kit, NewState().WithCursor("s", 1), override).Placement("s")
	if p.Cursor != 5 {
		t.Fatalf("an authoritative selection resolved to %d, want the declared 5", p.Cursor)
	}

	// The zero-value Cursor inherits, which is what makes a Block literal that
	// says nothing about the cursor safe rather than silently asserting item 0.
	silent := Func{Def: spec, Body: func(Canvas) Block { return Block{Items: numbered("row", 20)} }}
	p, _ = Arrange(kit, NewState().WithCursor("s", 4), silent).Placement("s")
	if p.Cursor != 4 {
		t.Fatalf("a Block that said nothing resolved the cursor to %d, want the stored 4", p.Cursor)
	}
}

func TestAnOutOfRangeOverrideIsClamped(t *testing.T) {
	kit := testKit(100, 30, 2)
	sec := Func{
		Def:  Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: numbered("row", 3), Cursor: At(99)} },
	}
	p, _ := Arrange(kit, NewState(), sec).Placement("s")
	if p.Cursor != 2 {
		t.Fatalf("cursor = %d, want it clamped to the last of three items", p.Cursor)
	}
}

func TestASectionWithNoItemsHasNoCursorAndNoCursorLine(t *testing.T) {
	kit := testKit(100, 30, 2)
	sec := Func{
		Def:  Spec{ID: "empty", MinWidth: 20, MinRows: 4, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block { return Block{Header: []string{"// EMPTY"}} },
	}
	p, _ := Arrange(kit, NewState(), sec).Placement("empty")
	if p.Cursor != -1 {
		t.Fatalf("an empty section reported cursor %d, want -1", p.Cursor)
	}
	if p.CursorLine != -1 {
		t.Fatalf("an empty section reported cursor line %d, want -1", p.CursorLine)
	}
	if p.Last >= p.First {
		t.Fatalf("an empty section reported a visible range [%d,%d]", p.First, p.Last)
	}
}

func TestAFuncSectionWithNoBodyRendersNothingRatherThanPanicking(t *testing.T) {
	kit := testKit(100, 30, 2)
	res := Arrange(kit, NewState(), Func{Def: Spec{ID: "nil", MinWidth: 20, MinRows: 2}})
	p, ok := res.Placement("nil")
	if !ok {
		t.Fatal("no placement for the body-less section")
	}
	if len(p.Items) != 0 {
		t.Fatalf("a body-less section produced %d items", len(p.Items))
	}
}

func TestPlacementLookupReportsAnUnknownSection(t *testing.T) {
	res := Arrange(testKit(100, 30, 2), NewState(), feed("known", 10))
	if _, ok := res.Placement("unknown"); ok {
		t.Fatal("an unknown section id resolved to a placement")
	}
}

func TestArrangeWithNoSectionsPaintsNothing(t *testing.T) {
	res := Arrange(testKit(100, 30, 2), NewState())
	if res.View != "" || res.Rows() != 0 {
		t.Fatalf("an empty body painted %d rows: %q", res.Rows(), res.View)
	}
	if res.BodyRows != BodyRows(testKit(100, 30, 2)) {
		t.Fatalf("BodyRows reported %d", res.BodyRows)
	}
}

func TestASectionTheBodyCannotAffordIsDroppedNotRenderedShort(t *testing.T) {
	kit := testKit(100, 7, 2) // BodyRows 3, two to spend across three sections
	sections := []Section{feed("a", 20), feed("b", 20), feed("c", 20)}
	res := Arrange(kit, NewState(), sections...)
	dropped := 0
	for _, p := range res.Placements {
		if p.Dropped {
			dropped++
			if p.Rows != 0 {
				t.Fatalf("dropped section %s was still given %d rows", p.ID, p.Rows)
			}
		}
	}
	if dropped == 0 {
		t.Fatal("no section was dropped from a body that cannot hold three")
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestAMaximumStopsASectionTakingMoreOfTheSurplus(t *testing.T) {
	kit := testKit(100, 40, 2) // 33 rows to spend
	capped := Spec{ID: "capped", MinWidth: 20, MinRows: 2, MaxRows: 6, Weight: 5, Scroll: ScrollItems}
	open := Spec{ID: "open", MinWidth: 20, MinRows: 2, Weight: 1, Scroll: ScrollItems}
	res := Arrange(kit, NewState(),
		listSection("capped", capped, numbered("c", 100)),
		listSection("open", open, numbered("o", 100)),
	)
	got := map[ID]int{}
	for _, p := range res.Placements {
		got[p.ID] = p.Rows
	}
	if got["capped"] != 6 {
		t.Fatalf("the capped section took %d rows despite MaxRows 6", got["capped"])
	}
	if want := BodyRows(kit) - 1 - 6; got["open"] != want {
		t.Fatalf("the open section took %d rows, want the rest %d", got["open"], want)
	}
}

func TestEverySectionAtItsMaximumLeavesTheRemainderUnpainted(t *testing.T) {
	kit := testKit(100, 40, 2)
	res := Arrange(kit, NewState(),
		listSection("a", Spec{ID: "a", MinWidth: 20, MinRows: 3, MaxRows: 3}, numbered("a", 3)),
		listSection("b", Spec{ID: "b", MinWidth: 20, MinRows: 3, MaxRows: 3}, numbered("b", 3)),
	)
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
	if res.Rows() >= BodyRows(kit) {
		t.Fatalf("two three-row sections filled a %d-row body; the surplus should stay unpainted", BodyRows(kit))
	}
}

func TestDeclaredHeightsOfTheWrongLengthAreReported(t *testing.T) {
	kit := testKit(100, 30, 2)
	res := Arrange(kit, NewState(), Func{
		Def:  Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: []string{"a", "b", "c"}, Heights: []int{1, 1}} },
	})
	p, _ := res.Placement("s")
	if !p.DeclaredHeightsDisagree {
		t.Fatal("a heights slice of the wrong length was accepted")
	}
}

func TestHeightsDeclaredForNoItemsAreReported(t *testing.T) {
	kit := testKit(100, 30, 2)
	res := Arrange(kit, NewState(), Func{
		Def:  Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Heights: []int{1, 2}} },
	})
	p, _ := res.Placement("s")
	if !p.DeclaredHeightsDisagree {
		t.Fatal("heights declared for an empty item list were accepted")
	}
}

func TestChromeTallerThanTheSectionIsStillClippedToItsRows(t *testing.T) {
	kit := testKit(100, 14, 2)
	header := make([]string, 40)
	for i := range header {
		header[i] = "chrome"
	}
	res := Arrange(kit, NewState(), Func{
		Def:  Spec{ID: "s", MinWidth: 20, MinRows: 2, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Header: header, Items: numbered("row", 20)} },
	})
	p, _ := res.Placement("s")
	if p.ItemViewport != 0 {
		t.Fatalf("item viewport = %d, want 0 when the chrome alone exceeds the section", p.ItemViewport)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestAnItemTallerThanTheWindowIsClippedRatherThanOverdrawn(t *testing.T) {
	kit := testKit(100, 12, 2)
	tall := ""
	for i := 0; i < 40; i++ {
		tall += "line\n"
	}
	res := Arrange(kit, NewState(), Func{
		Def:  Spec{ID: "s", MinWidth: 20, MinRows: 2, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: []string{tall, "next"}} },
	})
	if got, budget := res.Rows(), BodyRows(kit); got > budget {
		t.Fatalf("painted %d rows into a %d-row body", got, budget)
	}
}

// --- cursorless offset movement -------------------------------------------

func bodyScroll(id ID, n int) Func {
	return Func{
		Def:  Spec{ID: id, MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: numbered("line", n), Cursor: NoSelection()} },
	}
}

func TestEveryStandardKeyMovesACursorlessSectionsOffset(t *testing.T) {
	kit := testKit(100, 24, 2)
	sec := bodyScroll("body", 400)
	offset := func(s State) int { p, _ := Arrange(kit, s, sec).Placement("body"); return p.Offset }

	down, _ := NewState().HandleKey(kit, "down", sec)
	if offset(down) != 1 {
		t.Fatalf("down moved the offset to %d, want 1", offset(down))
	}
	up, _ := down.HandleKey(kit, "up", sec)
	if offset(up) != 0 {
		t.Fatalf("up moved the offset to %d, want 0", offset(up))
	}
	if again, _ := up.HandleKey(kit, "up", sec); offset(again) != 0 {
		t.Fatalf("up at the top moved the offset to %d", offset(again))
	}
	page, _ := NewState().HandleKey(kit, "pgdown", sec)
	if offset(page) <= 1 {
		t.Fatalf("pgdown moved the offset to %d, want a whole window", offset(page))
	}
	back, _ := page.HandleKey(kit, "pgup", sec)
	if offset(back) != 0 {
		t.Fatalf("pgup from one window down landed on %d, want 0", offset(back))
	}
	last, _ := NewState().HandleKey(kit, "end", sec)
	end := offset(last)
	if end == 0 {
		t.Fatal("end left the offset at the top")
	}
	// The last item must be reachable: `total - viewport` would leave it behind
	// the "▼ N below" hint forever.
	p, _ := Arrange(kit, last, sec).Placement("body")
	if p.Last != len(p.Items)-1 {
		t.Fatalf("end left the window at [%d,%d] of %d items; the last item is unreachable", p.First, p.Last, len(p.Items))
	}
	first, _ := last.HandleKey(kit, "home", sec)
	if offset(first) != 0 {
		t.Fatalf("home landed on offset %d, want 0", offset(first))
	}
}

func TestPagingASectionOfTallItemsMovesFewerItemsThanASectionOfShortOnes(t *testing.T) {
	// The average item height is measured, not assumed: cardlist's 4 and
	// linelist's 1 are the same expression with a different guess.
	kit := testKit(100, 40, 2)
	short := feed("short", 400)
	tallItems := make([]string, 400)
	for i := range tallItems {
		tallItems[i] = "a\nb\nc\nd\ne\nf"
	}
	tall := listSection("tall", Spec{ID: "tall", MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, tallItems)

	shortState, _ := NewState().HandleKey(kit, "pgdown", short)
	tallState, _ := NewState().HandleKey(kit, "pgdown", tall)
	sp, _ := Arrange(kit, shortState, short).Placement("short")
	tp, _ := Arrange(kit, tallState, tall).Placement("tall")
	if tp.Cursor >= sp.Cursor {
		t.Fatalf("pgdown moved %d tall items and %d short ones; the tall page should be shorter", tp.Cursor, sp.Cursor)
	}
}

func TestHandleKeyIgnoresABodyWithNothingScrollable(t *testing.T) {
	kit := testKit(100, 30, 2)
	if _, handled := NewState().HandleKey(kit, "G", staticSection("a"), staticSection("b")); handled {
		t.Fatal("a body with nothing scrollable consumed a scroll key")
	}
}

func TestFocusOnADroppedSectionFallsBackToOneThatFits(t *testing.T) {
	// HostBox rows = BodyRows-1 = (8-2-2)-1 = 3. One feed's MinRows=3 fits;
	// two would need 6, so b and c drop. Focus starting on c must land on a.
	kit := testKit(100, 8, 2)
	sections := []Section{feed("a", 20), feed("b", 20), feed("c", 20)}
	state := NewState().WithFocus("c")
	next, handled := state.HandleKey(kit, "down", sections...)
	if !handled {
		t.Fatal("down was not handled")
	}
	res := Arrange(kit, next, sections...)
	if p, _ := res.Placement("c"); p.Dropped && next.Focus() == "c" {
		t.Fatal("focus stayed on a section the body could not afford")
	}
	if next.Focus() != "a" {
		t.Fatalf("focus = %q, want the surviving leading section", next.Focus())
	}
}

func TestStateReportsDefaultsForASectionItHasNeverSeen(t *testing.T) {
	s := NewState()
	if got := s.Cursor("never"); got != -1 {
		t.Fatalf("Cursor of an unknown section = %d, want -1", got)
	}
	if got := s.Offset("never"); got != 0 {
		t.Fatalf("Offset of an unknown section = %d, want 0", got)
	}
	if got := s.Focus(); got != "" {
		t.Fatalf("Focus of a fresh state = %q, want empty", got)
	}
}

// --- the movement helpers' defaults ----------------------------------------
//
// ActionNone reaches these only through a caller that has not yet been written.
// They are exercised directly so the "leave it where it is" branch is a stated
// behaviour rather than an unreviewed fallthrough.

func TestMovementHelpersLeaveASectionAloneForAnActionTheyDoNotOwn(t *testing.T) {
	r := measured{cursor: 4, offset: 2, itemViewport: 6, items: numbered("row", 10), heights: scrollUnit(10)}
	if got := cursorTarget(r, ActionNone); got != 4 {
		t.Errorf("cursorTarget(ActionNone) = %d, want the cursor left at 4", got)
	}
	if got := offsetTarget(r, ActionNone); got != 2 {
		t.Errorf("offsetTarget(ActionNone) = %d, want the offset left at 2", got)
	}
	if got := cursorTarget(r, ActionNextSection); got != 4 {
		t.Errorf("cursorTarget(ActionNextSection) = %d, want the cursor left at 4", got)
	}
}

// TestPageItemsFallsBackWhenThereIsNothingToMeasure is pageStep's twin on the
// cursorless side. A body-scroll surface with no viewport or no measured items
// has no page to count off, and one item is the smallest step that still moves.
func TestPageItemsFallsBackWhenThereIsNothingToMeasure(t *testing.T) {
	if got := pageItems(measured{itemViewport: 0, heights: scrollUnit(10)}); got != 1 {
		t.Errorf("pageItems with a dead viewport = %d, want the floor of 1", got)
	}
	if got := pageItems(measured{itemViewport: 20}); got != 1 {
		t.Errorf("pageItems with no measured items = %d, want the floor of 1", got)
	}
}

func TestPageStepFallsBackWhenThereIsNothingToMeasure(t *testing.T) {
	if got := pageStep(measured{itemViewport: 0, heights: scrollUnit(10)}); got != 2 {
		t.Errorf("pageStep with a dead viewport = %d, want the floor of 2", got)
	}
	if got := pageStep(measured{itemViewport: 20}); got != 2 {
		t.Errorf("pageStep with no measured items = %d, want the floor of 2", got)
	}
	if got := pageStep(measured{itemViewport: 3, heights: scrollUnit(10)}); got != 2 {
		t.Errorf("pageStep on a tiny viewport = %d, want the floor of 2", got)
	}
}

func scrollUnit(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = 1
	}
	return out
}

func TestASideBySideColumnIsCappedByItsMaximumRows(t *testing.T) {
	kit := testKit(140, 40, 2)
	tallColumn := columnSpec("tall", 40)
	shortColumn := columnSpec("short", 40)
	shortColumn.MaxRows = 5
	res := Arrange(kit, NewState(),
		listSection("tall", tallColumn, numbered("t", 100)),
		listSection("short", shortColumn, numbered("s", 100)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	short, _ := res.Placement("short")
	if short.Rows != 5 {
		t.Fatalf("the capped column got %d rows, want its declared maximum 5", short.Rows)
	}
	tall, _ := res.Placement("tall")
	if want := BodyRows(kit) - 1; tall.Rows != want {
		t.Fatalf("the uncapped column got %d rows, want the whole body height %d", tall.Rows, want)
	}
}
