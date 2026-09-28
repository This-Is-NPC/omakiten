package screenlayout

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Spec.WidthPercent (#2446, shape 1 of 3).
//
// The pilot's blocking finding. Project and Task Detail both size their
// activity column as `available*45/100` clamped to [44, 96], arrived at
// independently in two packages. Spec sized a column as a MINIMUM plus a
// weighted share of the SURPLUS, and the pilot brute-forced all 4096 integer
// weight pairs (1..64 x 1..64) against Project's real widths at both
// side-by-side geometries: none reproduces both, and the closest misses by 5
// columns. The two are different functions of `available`, so no amount of
// tuning closes the gap — the model had to gain the shape.
// ---------------------------------------------------------------------------

// screensActivityWidth is the expression BOTH pilot screens already use, copied
// from `project.projectActivityWidth` and `taskdetail.activityPanelWidth`
// verbatim rather than restated in terms of the implementation under test. If
// the arranger and this function ever disagree, the migration moves a column.
func screensActivityWidth(available int) int {
	width := available * 45 / 100
	if width < 44 {
		width = 44
	}
	ceiling := min(96, available)
	return min(width, ceiling)
}

// proportionalColumn is the Spec that says "45% of the available width, never
// below 44 columns, never above a 96-column comfortable measure".
func proportionalColumn(id ID) Spec {
	return Spec{
		ID: id, MinWidth: 44, MaxWidth: 96, WidthPercent: 45, MinRows: 3,
		Column: true, Weight: 1, Scroll: ScrollItems,
	}
}

func TestAColumnCanTakeAShareOfTheAvailableWidth(t *testing.T) {
	// The two geometries the pilot recorded, at the arranger's own one-column
	// gap so this measures the WIDTH RULE and not the gap. These are the exact
	// numbers TestScreenlayoutCannotExpressThisScreensColumnWidths swept 4096
	// weight pairs against and could not reach.
	for _, at := range []struct{ available, rest, activity int }{
		{116, 63, 52},
		{196, 107, 88},
	} {
		kit := testKit(at.available+4, 40, 2)
		if got := kit.AvailableWidth(); got != at.available {
			t.Fatalf("a %d-column terminal reports %d available, want %d", at.available+4, got, at.available)
		}
		res := Arrange(kit, NewState(),
			listSection("meta", columnSpec("meta", 32), numbered("m", 40)),
			listSection("activity", proportionalColumn("activity"), numbered("a", 40)),
		)
		if res.Arrangement != SideBySide {
			t.Fatalf("available %d: arrangement = %v, want SideBySide", at.available, res.Arrangement)
		}
		meta, _ := res.Placement("meta")
		activity, _ := res.Placement("activity")
		if activity.Width != at.activity {
			t.Errorf("available %d: the proportional column got %d, want %d — 45%% of %d clamped to [44,96]",
				at.available, activity.Width, at.activity, at.available)
		}
		if meta.Width != at.rest {
			t.Errorf("available %d: the remaining column got %d, want the rest %d",
				at.available, meta.Width, at.rest)
		}
		if total := meta.Width + activity.Width + defaultColumnGap; total != at.available {
			t.Errorf("available %d: the columns spend %d", at.available, total)
		}
	}
}

func TestAProportionalColumnReproducesTheScreensExpressionAtEveryWidth(t *testing.T) {
	// Two geometries could be a coincidence. This is the whole range the
	// side-by-side branch is reachable at, compared against the screens' own
	// closed form — floor, percentage band and ceiling alike.
	exercised := 0
	for available := 40; available <= 400; available++ {
		kit := testKit(available+4, 40, 2)
		res := Arrange(kit, NewState(),
			listSection("meta", columnSpec("meta", 32), numbered("m", 40)),
			listSection("activity", proportionalColumn("activity"), numbered("a", 40)),
		)
		if res.Arrangement != SideBySide {
			continue
		}
		exercised++
		activity, _ := res.Placement("activity")
		if want := screensActivityWidth(available); activity.Width != want {
			t.Fatalf("available %d: the proportional column got %d, the screens' own expression gives %d",
				available, activity.Width, want)
		}
		meta, _ := res.Placement("meta")
		if total := meta.Width + activity.Width + defaultColumnGap; total != available {
			t.Fatalf("available %d: the columns spend %d", available, total)
		}
	}
	if exercised < 200 {
		t.Fatalf("only %d widths reached the side-by-side branch; the sweep is not measuring the rule", exercised)
	}
	t.Logf("%d widths reproduce the screens' `available*45/100` clamped to [44,96] exactly", exercised)
}

func TestAProportionalColumnIsClampedByItsMinimumAndMaximum(t *testing.T) {
	// The floor and the ceiling are the only places the percentage stops
	// applying, and both are outside the two recorded geometries.
	for _, at := range []struct{ available, activity int }{
		{80, 44},  // 45% is 36, raised to the declared minimum
		{400, 96}, // 45% is 180, cut to the declared comfortable measure
	} {
		kit := testKit(at.available+4, 40, 2)
		res := Arrange(kit, NewState(),
			listSection("meta", columnSpec("meta", 32), numbered("m", 40)),
			listSection("activity", proportionalColumn("activity"), numbered("a", 40)),
		)
		activity, _ := res.Placement("activity")
		if activity.Width != at.activity {
			t.Errorf("available %d: the proportional column got %d, want %d", at.available, activity.Width, at.activity)
		}
	}
}

func TestAProportionalColumnLeavesTheRestToItsSiblings(t *testing.T) {
	// A proportion is a claim on `available`, not on the surplus, so it takes
	// its share FIRST and the weighted split runs on what is left. Two siblings
	// share that remainder by weight, exactly as they would with no proportional
	// column present.
	kit := testKit(204, 40, 2) // available 200, so the proportional column takes 90
	one := columnSpec("one", 20)
	two := columnSpec("two", 20)
	two.Weight = 3
	res := Arrange(kit, NewState(),
		listSection("one", one, numbered("1", 20)),
		listSection("two", two, numbered("2", 20)),
		listSection("activity", proportionalColumn("activity"), numbered("a", 20)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	got := map[ID]int{}
	for _, p := range res.Placements {
		got[p.ID] = p.Width
	}
	if got["activity"] != 90 {
		t.Fatalf("the proportional column got %d, want 45%% of 200", got["activity"])
	}
	// 200 - 90 - 2 gaps = 108 for two columns of minimum 20 and weights 1:3.
	// Surplus 68 splits 17:51, so 37 and 71.
	if got["one"] != 37 || got["two"] != 71 {
		t.Fatalf("the weighted siblings got %d and %d, want 37 and 71 out of the 108 the proportion left",
			got["one"], got["two"])
	}
	if total := got["one"] + got["two"] + got["activity"] + 2*defaultColumnGap; total != 200 {
		t.Fatalf("the columns spend %d of 200", total)
	}
}

func TestEveryColumnProportionalLeavesTheRemainderUnspent(t *testing.T) {
	// The proportional twin of TestEveryColumnCappedLeavesTheRemainingWidthUnspent:
	// the contract is never MORE than available, not exactly available.
	kit := testKit(204, 40, 2)
	left, right := proportionalColumn("left"), proportionalColumn("right")
	left.ID, right.ID = "left", "right"
	left.WidthPercent, right.WidthPercent = 30, 30
	res := Arrange(kit, NewState(),
		listSection("left", left, numbered("l", 20)),
		listSection("right", right, numbered("r", 20)),
	)
	total := defaultColumnGap
	for _, p := range res.Placements {
		if p.Width != 60 {
			t.Fatalf("column %s got %d columns, want 30%% of 200", p.ID, p.Width)
		}
		total += p.Width
	}
	if total >= kit.AvailableWidth() {
		t.Fatalf("two 30%% columns spend %d of %d; the fixture no longer leaves anything unspent", total, kit.AvailableWidth())
	}
	for i, line := range lines(res.View) {
		if got := len([]rune(line)); got > kit.AvailableWidth() {
			t.Fatalf("body line %d is %d cells wide, past the %d available", i, got, kit.AvailableWidth())
		}
	}
}

func TestAProportionThatWouldStarveASiblingStacksInstead(t *testing.T) {
	// A proportional column's share is its EFFECTIVE minimum, so the
	// side-by-side breakpoint is decided against the width the column will
	// actually take. Otherwise a wide proportion would be honoured by pushing a
	// sibling below the minimum it declared, which is the overflow this package
	// exists to make inexpressible.
	greedy := proportionalColumn("greedy")
	greedy.WidthPercent, greedy.MaxWidth = 90, 0
	res := Arrange(testKit(120, 40, 2), NewState(),
		listSection("greedy", greedy, numbered("g", 20)),
		listSection("meta", columnSpec("meta", 32), numbered("m", 20)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked — 90%% of 116 leaves 11 columns for a section that declared 32", res.Arrangement)
	}
}

func TestAStackedSectionIgnoresItsProportion(t *testing.T) {
	// A proportion is a COLUMN rule: it says what share of a row of columns this
	// section takes. Stacked there is one column and the section takes it,
	// capped only by its comfortable measure — which is what both pilot screens
	// already do in their narrow branch.
	kit := testKit(120, 30, 2)
	narrow := proportionalColumn("narrow")
	narrow.Column, narrow.MaxWidth = false, 0
	res := Arrange(kit, NewState(), listSection("narrow", narrow, numbered("n", 20)))
	p, _ := res.Placement("narrow")
	if p.Width != kit.AvailableWidth() {
		t.Fatalf("a stacked section with a 45%% proportion got %d columns, want the whole %d available",
			p.Width, kit.AvailableWidth())
	}
}

func TestAProportionIsNormalisedIntoThePercentageRange(t *testing.T) {
	if got := (Spec{ID: "s", WidthPercent: -5}).normalize(); got.WidthPercent != 0 {
		t.Errorf("a negative proportion normalised to %d, want it dropped to 0", got.WidthPercent)
	}
	if got := (Spec{ID: "s", WidthPercent: 140}).normalize(); got.WidthPercent != 100 {
		t.Errorf("a proportion past 100%% normalised to %d, want 100", got.WidthPercent)
	}
	if got := (Spec{ID: "s", WidthPercent: 45}).normalize(); got.WidthPercent != 45 {
		t.Errorf("a declared proportion normalised to %d, want the declared 45", got.WidthPercent)
	}
}

func TestAColumnWithNoProportionKeepsItsWeightedShareOfTheSurplus(t *testing.T) {
	// Acceptance criterion 6, stated as an equality rather than as a sample:
	// across every width the side-by-side branch is reachable at and every
	// weight pair up to 8, a set of sections declaring no proportion is
	// distributed by minimum-plus-weighted-surplus, byte for byte the function
	// that shipped before this field existed.
	exercised := 0
	for available := 45; available <= 300; available++ {
		for leftWeight := 1; leftWeight <= 8; leftWeight++ {
			for rightWeight := 1; rightWeight <= 8; rightWeight++ {
				exercised += assertWeightedShare(t, available, leftWeight, rightWeight)
			}
		}
	}
	if exercised < 8192 {
		t.Fatalf("only %d width/weight combinations were exercised, want at least 8192", exercised)
	}
	t.Logf("%d width/weight combinations distribute identically to the pre-proportion rule", exercised)
}

func assertWeightedShare(t *testing.T, available, leftWeight, rightWeight int) int {
	left, right := columnSpec("left", 20), columnSpec("right", 24)
	left.Weight, right.Weight = leftWeight, rightWeight
	specs := []Spec{left.normalize(), right.normalize()}
	if !fitsSideBySide(specs, available) {
		return 0
	}
	got := distributeWidths(specs, available)
	want := weightedSurplusWidths(specs, available)
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("available %d weights %d/%d: distributeWidths gave %v, the pre-existing rule gives %v", available, leftWeight, rightWeight, got, want)
	}
	return 1
}

// weightedSurplusWidths is the width rule as it stood before WidthPercent
// existed: every column starts at its minimum and the surplus goes out by
// weight. Kept here, in the test, so criterion 6 is checked against a
// SEPARATELY WRITTEN function rather than against the one under test.
func weightedSurplusWidths(specs []Spec, available int) []int {
	widths := make([]int, len(specs))
	used := defaultColumnGap * (len(specs) - 1)
	shares := make([]share, len(specs))
	for i, s := range specs {
		widths[i] = s.MinWidth
		used += s.MinWidth
		shares[i] = share{floor: s.MinWidth, ceiling: s.MaxWidth, weight: s.Weight, eligible: true}
	}
	spreadSurplus(widths, shares, available-used)
	return widths
}
