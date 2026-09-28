package screenlayout

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Spec.ColumnGap (#2446, shape 3 of 3).
//
// The smallest of the pilot's three findings and the one with the fewest
// choices in it: both pilot screens join their columns with two spaces
// (`JoinHorizontal(left, "  ", right)`) and the arranger's gap was a package
// constant of one, with no field to override it. A screen that cannot declare
// its gap keeps the join, and the join is where the width arithmetic used to
// go wrong.
// ---------------------------------------------------------------------------

// gapBetween is the run of blank columns separating the two painted columns on
// the widest body line: the measurement a screen actually cares about, taken
// from the rendered string rather than from the arranger's own arithmetic.
func gapBetween(t *testing.T, view string, leftWidth int) int {
	t.Helper()
	widest := ""
	for _, line := range lines(view) {
		if len([]rune(line)) > len([]rune(widest)) {
			widest = line
		}
	}
	runes := []rune(widest)
	if len(runes) <= leftWidth {
		t.Fatalf("the widest body line is %d cells, not wide enough to hold a %d-column left column and a gap: %q",
			len(runes), leftWidth, widest)
	}
	trimmed := strings.TrimLeft(string(runes[leftWidth:]), " ")
	return len(runes) - leftWidth - len([]rune(trimmed))
}

func TestAnUndeclaredColumnGapIsTheSingleColumnThatShipped(t *testing.T) {
	kit := testKit(120, 30, 2)
	res := Arrange(kit, NewState(),
		listSection("left", columnSpec("left", 40), numbered("l", 40)),
		listSection("right", columnSpec("right", 40), numbered("r", 40)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	left, _ := res.Placement("left")
	if got := gapBetween(t, res.View, left.Width); got != defaultColumnGap {
		t.Fatalf("two columns declaring no gap are separated by %d blank columns, want the %d that shipped",
			got, defaultColumnGap)
	}
}

func TestAScreenCanDeclareTheColumnsGap(t *testing.T) {
	// Two spaces is what both pilot screens spend on their join today.
	kit := testKit(120, 30, 2)
	left, right := columnSpec("left", 40), columnSpec("right", 40)
	left.ColumnGap, right.ColumnGap = 2, 2
	res := Arrange(kit, NewState(),
		listSection("left", left, numbered("l", 40)),
		listSection("right", right, numbered("r", 40)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	placed, _ := res.Placement("left")
	if got := gapBetween(t, res.View, placed.Width); got != 2 {
		t.Fatalf("a declared gap of 2 painted %d blank columns", got)
	}
	total := 2
	for _, p := range res.Placements {
		total += p.Width
	}
	if total != kit.AvailableWidth() {
		t.Fatalf("the columns and their declared gap spend %d of the %d available", total, kit.AvailableWidth())
	}
	for i, line := range lines(res.View) {
		if got := len([]rune(line)); got > kit.AvailableWidth() {
			t.Fatalf("body line %d is %d cells wide, past the %d available", i, got, kit.AvailableWidth())
		}
	}
}

func TestTheWidestDeclaredGapWins(t *testing.T) {
	// A gap is BETWEEN two columns, so it cannot be two numbers. One has to
	// win, and it is the widest: a section that asked for breathing room gets
	// it, and no section is ever pushed closer to its neighbour than it asked.
	kit := testKit(200, 30, 2)
	left, middle, right := columnSpec("left", 40), columnSpec("middle", 40), columnSpec("right", 40)
	middle.ColumnGap = 3
	res := Arrange(kit, NewState(),
		listSection("left", left, numbered("l", 40)),
		listSection("middle", middle, numbered("m", 40)),
		listSection("right", right, numbered("r", 40)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	placed, _ := res.Placement("left")
	if got := gapBetween(t, res.View, placed.Width); got != 3 {
		t.Fatalf("one section declaring a gap of 3 produced %d blank columns", got)
	}
	total := 2 * 3
	for _, p := range res.Placements {
		total += p.Width
	}
	if total != kit.AvailableWidth() {
		t.Fatalf("three columns and two 3-column gaps spend %d of the %d available", total, kit.AvailableWidth())
	}
}

func TestADeclaredGapIsChargedByTheBreakpointToo(t *testing.T) {
	// The gap is width the columns do not get, so it belongs in the fit test.
	// 40 + 40 + one gap fits in 66; the same pair with a six-column gap does
	// not, and must stack rather than overflow.
	tight, wide := columnSpec("tight", 32), columnSpec("wide", 32)
	specs := []Spec{tight.normalize(), wide.normalize()}
	if !fitsSideBySide(specs, 65) {
		t.Fatal("two 32-column minimums and a 1-column gap do not fit in 65; the fixture is wrong")
	}
	tight.ColumnGap, wide.ColumnGap = 6, 6
	gapped := []Spec{tight.normalize(), wide.normalize()}
	if fitsSideBySide(gapped, 65) {
		t.Fatal("two 32-column minimums and a 6-column gap were said to fit in 65")
	}
	if !fitsSideBySide(gapped, 70) {
		t.Fatal("two 32-column minimums and a 6-column gap do not fit in 70")
	}
}

func TestAGapBelowTheDefaultIsRaisedToIt(t *testing.T) {
	// Zero means "undeclared", not "columns touching". Sections that abut with
	// no separator are a different request from the one this field takes, and
	// admitting it here would let a screen silently lose the separator it never
	// asked to remove.
	if got := (Spec{ID: "s", ColumnGap: -4}).normalize(); got.ColumnGap != 0 {
		t.Errorf("a negative gap normalised to %d, want it dropped to 0", got.ColumnGap)
	}
	if got := columnGapOf([]Spec{{ID: "a"}, {ID: "b"}}); got != defaultColumnGap {
		t.Errorf("two sections declaring no gap resolved to %d, want the default %d", got, defaultColumnGap)
	}
}
