package screenlayout

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// ---------------------------------------------------------------------------
// Partial items at the window edge (#2445).
//
// The pilot (#2425) returned a STOP on this: two card sections left 5-7 rows
// blank — 6 of 15 on an 80x24 body — WHILE BOTH SECTIONS WERE HIDING CONTENT.
// Every one of those rows is a row of content the user was not shown, which is
// the same harm as overdrawing arriving from the other direction, and both
// pilot screens spend their budget exactly today, so migrating onto the engine
// as it stood would have been a regression rather than a limitation.
//
// The cause was structural. scrollwindow.Slice closes a window on an ITEM
// boundary; the residue between the last whole item and the section's
// allocation is up to (tallest item - 1) rows and no screen can pad it, because
// a screen is never told how many items will fit — computing that is precisely
// what this package exists to make impossible.
// ---------------------------------------------------------------------------

// cards builds n items of h rows each, every row naming its item and its
// position inside it. The naming is what makes a CLIPPED item legible to a
// test: a preview of the item above must show that item's LAST rows, and a
// preview of the item below its FIRST.
func cards(tag string, n, h int) []string {
	out := make([]string, n)
	for i := range out {
		rows := make([]string, h)
		for j := range rows {
			rows[j] = fmt.Sprintf("%s%02d-row%d", tag, i, j)
		}
		out[i] = strings.Join(rows, "\n")
	}
	return out
}

func cardSection(id ID, items []string, weight int) Func {
	return listSection(id, Spec{ID: id, MinWidth: 30, MinRows: 4, Weight: weight, Scroll: ScrollItems}, items)
}

func TestASectionSpendsEveryRowItWasGivenWhenTheWindowClosesMidItem(t *testing.T) {
	// The pilot's sweep, restated as the requirement rather than the defect:
	// whether a window happens to close on its allocation is a function of the
	// height of content the USER supplied, so the only honest question is
	// whether ANY item heights at this geometry strand rows.
	for _, geometry := range []struct {
		name          string
		width, height int
	}{{"80x24", 80, 24}, {"120x40", 120, 40}, {"200x50", 200, 50}} {
		kit := testKit(geometry.width, geometry.height, 2)
		budget := BodyRows(kit)
		exercised := partialWindowPairs(t, kit, geometry.name, budget)
		if exercised == 0 {
			t.Fatalf("%s: no card-height pair overflowed its sections, so nothing was proved", geometry.name)
		}
		t.Logf("%s (budget %d): %d windowed card-height pairs, every one spending the budget exactly", geometry.name, budget, exercised)
	}
}

func partialWindowPairs(t *testing.T, kit screenkit.Kit, geometry string, budget int) int {
	exercised := 0
	for top := 2; top <= 8; top++ {
		for bottom := 2; bottom <= 8; bottom++ {
			exercised += checkPartialPair(t, kit, geometry, budget, top, bottom)
		}
	}
	return exercised
}

func checkPartialPair(t *testing.T, kit screenkit.Kit, geometry string, budget, top, bottom int) int {
	res := Arrange(kit, NewState(),
		cardSection("subtasks", cards("sub", 40, top), 1),
		cardSection("activity", cards("act", 40, bottom), 1),
	)
	if !hasHiddenPlacement(res) {
		return 0
	}
	if got := res.Rows(); got != budget {
		t.Fatalf("%s cards %d+%d: the body paints %d rows against a %d-row budget while both sections hide content", geometry, top, bottom, got, budget)
	}
	return 1
}

func hasHiddenPlacement(res Result) bool {
	for _, placement := range res.Placements {
		if placement.Above > 0 || placement.Below > 0 {
			return true
		}
	}
	return false
}

func TestBothPartialEdgesAreLegible(t *testing.T) {
	// Acceptance criterion 2. A clipped card is only worth painting if the user
	// can read it, and which END is clipped is the whole information: the item
	// above must show its bottom (the rows just scrolled past) and the item
	// below its top (the rows coming next). A preview clipped from the wrong end
	// is worse than a blank row, because it looks like content that is there.
	kit := testKit(100, 24, 2)
	items := cards("card", 20, 6)
	sec := cardSection("feed", items, 1)
	// Scroll into the middle so BOTH edges have an item outside them.
	state := NewState().WithCursor("feed", 6)
	res := Arrange(kit, state, sec)
	p, ok := res.Placement("feed")
	if !ok {
		t.Fatal("the feed was not placed")
	}
	if p.LeadingPartialRows == 0 || p.TrailingPartialRows == 0 {
		t.Fatalf("previews are %d leading / %d trailing rows; the fixture is not exercising both edges",
			p.LeadingPartialRows, p.TrailingPartialRows)
	}
	if p.First == 0 || p.Last == len(items)-1 {
		t.Fatalf("visible range [%d,%d] reaches an end of the list; there is no item outside one of the edges", p.First, p.Last)
	}
	body := lines(res.View)

	// The item above the window, clipped at the TOP: its last rows show and its
	// first do not.
	above := items[p.First-1]
	aboveRows := strings.Split(above, "\n")
	for _, row := range aboveRows[len(aboveRows)-p.LeadingPartialRows:] {
		if !containsLine(body, row) {
			t.Errorf("the top-clipped preview is missing %q, one of the last %d rows of the item above the window", row, p.LeadingPartialRows)
		}
	}
	if containsLine(body, aboveRows[0]) {
		t.Errorf("the top-clipped preview painted %q, the FIRST row of the item above — it is clipped at the wrong end", aboveRows[0])
	}

	// The item below the window, clipped at the BOTTOM: its first rows show and
	// its last do not.
	below := items[p.Last+1]
	belowRows := strings.Split(below, "\n")
	for _, row := range belowRows[:p.TrailingPartialRows] {
		if !containsLine(body, row) {
			t.Errorf("the bottom-clipped preview is missing %q, one of the first %d rows of the item below the window", row, p.TrailingPartialRows)
		}
	}
	if containsLine(body, belowRows[len(belowRows)-1]) {
		t.Errorf("the bottom-clipped preview painted %q, the LAST row of the item below — it is clipped at the wrong end", belowRows[len(belowRows)-1])
	}

	// And the previews sit INSIDE the hint rows, so the section still reads top
	// to bottom: "▲ N above", the tail of what is above, the whole items, the
	// head of what is below, "▼ N below".
	if got := body[p.TopLine]; !strings.HasPrefix(got, "▲") {
		t.Fatalf("the section's first line is %q, want the above hint — the preview was spliced outside the chrome", got)
	}
	if got := body[p.TopLine+1]; got != aboveRows[len(aboveRows)-p.LeadingPartialRows] {
		t.Fatalf("the line under the above hint is %q, want the first row of the top-clipped preview", got)
	}
}

func containsLine(body []string, want string) bool {
	for _, line := range body {
		if line == want {
			return true
		}
	}
	return false
}

func TestPreviewsAreVisualAndTheHiddenCountsStayWholeItem(t *testing.T) {
	// A previewed item has NOT been seen — only part of it has — so it still
	// counts as hidden on its own side, and the cursor still lands on whole
	// items. Reporting a previewed item as visible would be the private-copy
	// defect in a new place: two answers to "what has the user seen".
	kit := testKit(100, 24, 2)
	items := cards("card", 20, 6)
	res := Arrange(kit, NewState().WithCursor("feed", 6), cardSection("feed", items, 1))
	p, _ := res.Placement("feed")

	if p.Above != p.First {
		t.Fatalf("Above = %d for a window starting at item %d; a partially previewed item is still hidden", p.Above, p.First)
	}
	if want := len(items) - (p.Last + 1); p.Below != want {
		t.Fatalf("Below = %d, want %d — the previewed item below is still counted as hidden", p.Below, want)
	}
	if p.Cursor < p.First || p.Cursor > p.Last {
		t.Fatalf("cursor %d is outside the whole-item range [%d,%d]; a preview is not a cursor position", p.Cursor, p.First, p.Last)
	}
}

func TestTheCursorLineStillPointsAtTheCursorItemThroughAPreview(t *testing.T) {
	// The leading preview pushes every visible item down by its own row count.
	// A CursorLine that forgot it would be the Flow defect again: a reported
	// line that is not where the renderer put the row.
	kit := testKit(100, 24, 2)
	items := cards("card", 20, 6)
	res := Arrange(kit, NewState().WithCursor("feed", 6), cardSection("feed", items, 1))
	p, _ := res.Placement("feed")
	if p.LeadingPartialRows == 0 {
		t.Fatal("no leading preview; the fixture does not exercise the offset this test is about")
	}
	body := lines(res.View)
	if p.CursorLine < 0 || p.CursorLine >= len(body) {
		t.Fatalf("CursorLine %d outside the %d-line body", p.CursorLine, len(body))
	}
	if want := strings.Split(items[p.Cursor], "\n")[0]; body[p.CursorLine] != want {
		t.Fatalf("line %d is %q, want the cursor item's first row %q", p.CursorLine, body[p.CursorLine], want)
	}
}

func TestANonScrollableSectionAlsoFillsItsTrailingEdge(t *testing.T) {
	// A section that does not scroll is still CLIPPED to its rows, and clipping
	// mid-item leaves the same residue. It has no hint rows and no item above
	// its window, so everything left over goes to the one edge it has.
	kit := testKit(100, 20, 2)
	items := cards("row", 20, 4)
	spec := Spec{ID: "form", MinWidth: 30, MinRows: 4, Weight: 1} // ScrollNone
	res := Arrange(kit, NewState(), listSection("form", spec, items))
	p, _ := res.Placement("form")

	if p.LeadingPartialRows != 0 {
		t.Fatalf("a section pinned to offset 0 previewed %d rows above itself; there is nothing there", p.LeadingPartialRows)
	}
	if p.TrailingPartialRows == 0 {
		t.Fatal("the non-scrollable section left its residue blank")
	}
	if p.Below == 0 {
		t.Fatal("nothing was hidden, so the fixture proves nothing")
	}
	if got := res.Rows(); got != BodyRows(kit) {
		t.Fatalf("the body paints %d rows against a %d-row budget", got, BodyRows(kit))
	}
	body := lines(res.View)
	if got := strings.Split(items[p.Last+1], "\n")[0]; !containsLine(body, got) {
		t.Errorf("the trailing preview is missing %q, the first row of the clipped item", got)
	}
}

func TestASectionWithTooLittleContentToFillItsRowsIsNotPadded(t *testing.T) {
	// Filling the allocation is "spend the rows you have CONTENT for", not
	// "emit blank lines to the budget". A section with two items and eight rows
	// paints two — and the six it did not use are then reclaimed by a section
	// that is hiding content, which is the pass that already existed.
	kit := testKit(100, 30, 2)
	form := listSection("form", Spec{ID: "form", MinWidth: 20, MinRows: 8, MaxRows: 8}, numbered("f", 2))
	res := Arrange(kit, NewState(), form, cardSection("feed", cards("c", 60, 3), 1))
	p, _ := res.Placement("form")
	if p.TrailingPartialRows != 0 || p.LeadingPartialRows != 0 {
		t.Fatalf("a section with nothing hidden previewed %d/%d rows", p.LeadingPartialRows, p.TrailingPartialRows)
	}
	if got := res.Rows(); got != BodyRows(kit) {
		t.Fatalf("the body paints %d rows against a %d-row budget", got, BodyRows(kit))
	}
}

func TestSideBySideColumnsEachSpendTheirWholeHeight(t *testing.T) {
	// Side by side is where the shortfall was worst, because reclaimSlack does
	// not run there at all: whatever a column strands, stays stranded. Every
	// column is assigned the whole body height, so every column must fill it.
	kit := testKit(140, 30, 2)
	for top := 2; top <= 8; top++ {
		for bottom := 2; bottom <= 8; bottom++ {
			res := Arrange(kit, NewState(),
				Func{Def: columnSpec("left", 40), Body: constantBody(cards("l", 40, top))},
				Func{Def: columnSpec("right", 40), Body: constantBody(cards("r", 40, bottom))},
			)
			if res.Arrangement != SideBySide {
				t.Fatalf("cards %d+%d: arrangement = %v, want SideBySide", top, bottom, res.Arrangement)
			}
			if got := res.Rows(); got != BodyRows(kit) {
				t.Fatalf("cards %d+%d: side-by-side body paints %d rows against a %d-row budget", top, bottom, got, BodyRows(kit))
			}
		}
	}
}

func TestSideBySideHasNoSlackToReclaimBecauseColumnsAreNotFungible(t *testing.T) {
	// The other half of acceptance criterion 5. reclaimSlack does not run
	// BETWEEN columns, and the question is whether that is a gap. It is not: a
	// row idle at the bottom of a short column is BESIDE the tall column, not
	// above it, so there is no transfer to make. Each of these columns holds one
	// section and therefore the whole body height — the quantity reclaimSlack
	// would move is identically zero here.
	//
	// #2446 narrowed the claim without weakening it. [Spec.Group] lets two
	// sections SHARE a column and split its height, and there the transfer is
	// real: see TestAStackedColumnReclaimsRowsAMemberDidNotUse. What stays true
	// is the sentence this test is named for — no row ever crosses between
	// columns. The assignment below is what makes it true for a body of
	// ungrouped sections, so if columns ever stop being assigned the full height
	// this test says so.
	kit := testKit(140, 30, 2)
	res := Arrange(kit, NewState(),
		Func{Def: columnSpec("stub", 40), Body: constantBody(numbered("s", 2))},
		Func{Def: columnSpec("feed", 40), Body: constantBody(cards("f", 60, 4))},
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	full := BodyRows(kit) - leadingBlankRows
	for _, p := range res.Placements {
		if p.Rows != full {
			t.Fatalf("column %s was assigned %d rows, not the whole body height %d — if columns ever stop being assigned the full height, reclaim has something to move and this test is wrong",
				p.ID, p.Rows, full)
		}
	}
	// The stub column paints two rows and the feed paints the whole height. The
	// body is as tall as the taller column, which is the budget.
	if got := res.Rows(); got != BodyRows(kit) {
		t.Fatalf("side-by-side body paints %d rows against a %d-row budget", got, BodyRows(kit))
	}
	feed, _ := res.Placement("feed")
	if feed.Below == 0 {
		t.Fatal("the feed is not hiding anything, so there is no would-be recipient and the test proves nothing")
	}
}
