package screenlayout

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func columnSpec(id ID, minWidth int) Spec {
	return Spec{ID: id, MinWidth: minWidth, MinRows: 3, Weight: 1, Column: true, Scroll: ScrollItems}
}

func TestSectionsGoSideBySideOnlyWhenEveryMinimumWidthFits(t *testing.T) {
	left := columnSpec("left", 40)
	right := columnSpec("right", 40)
	sections := []Section{
		listSection("left", left, numbered("l", 40)),
		listSection("right", right, numbered("r", 40)),
	}

	wide := testKit(120, 30, 2) // AvailableWidth 116 >= 40 + 40 + 1
	if got := Arrange(wide, NewState(), sections...).Arrangement; got != SideBySide {
		t.Fatalf("at 120 columns the arrangement is %v, want SideBySide", got)
	}
	narrow := testKit(70, 30, 2) // AvailableWidth 66 < 81
	if got := Arrange(narrow, NewState(), sections...).Arrangement; got != Stacked {
		t.Fatalf("at 70 columns the arrangement is %v, want Stacked", got)
	}
}

func TestTwoSpecsFitSideBySideMatchesTheGeneralBreakpoint(t *testing.T) {
	t.Parallel()

	pairs := map[string][2]Spec{
		"plain columns":         {columnSpec("left", 40), columnSpec("right", 40)},
		"declared gap":          {{ID: "left", MinWidth: 20, Column: true, ColumnGap: 4}, {ID: "right", MinWidth: 30, Column: true, ColumnGap: 2}},
		"proportional column":   {{ID: "left", MinWidth: 20, Column: true, WidthPercent: 45}, {ID: "right", MinWidth: 30, Column: true}},
		"normalization":         {{ID: "left", MinWidth: -3, Column: true}, {ID: "right", MinWidth: 30, Column: true, WidthPercent: 130}},
		"missing column opt-in": {{ID: "left", MinWidth: 20}, {ID: "right", MinWidth: 30, Column: true}},
		"one shared group":      {{ID: "left", Group: "lane", MinWidth: 20, Column: true}, {ID: "right", Group: "lane", MinWidth: 30, Column: true}},
		"distinct groups":       {{ID: "left", Group: "a", MinWidth: 20, Column: true}, {ID: "right", Group: "b", MinWidth: 30, Column: true}},
	}
	for name, pair := range pairs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for width := -2; width <= 200; width++ {
				box := Box{Width: width, Rows: 20}
				got := TwoSpecsFitSideBySide(box, pair[0], pair[1])
				want := FitsSideBySide(box, pair[0], pair[1])
				if got != want {
					t.Fatalf("width %d: two-spec fit = %t, general fit = %t", width, got, want)
				}
			}
		})
	}
}

func TestTwoSpecsFitSideBySideDoesNotAllocate(t *testing.T) {
	left := columnSpec("left", 40)
	right := columnSpec("right", 40)
	box := Box{Width: 120, Rows: 30}
	var fit bool
	if got := testing.AllocsPerRun(1000, func() {
		fit = TwoSpecsFitSideBySide(box, left, right)
	}); got != 0 {
		t.Fatalf("two-spec breakpoint allocated %.0f objects per call, want 0", got)
	}
	if !fit {
		t.Fatal("allocation probe did not exercise the fitted branch")
	}
}

func TestASectionThatDidNotOptIntoAColumnIsNeverPlacedBesideOne(t *testing.T) {
	// The council's objection made concrete: a screen with no breakpoint must
	// not acquire one just because its sections happen to fit side by side.
	left := columnSpec("left", 40)
	right := columnSpec("right", 40)
	right.Column = false
	res := Arrange(testKit(200, 30, 2), NewState(),
		listSection("left", left, numbered("l", 40)),
		listSection("right", right, numbered("r", 40)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked — one section did not opt in", res.Arrangement)
	}
}

func TestSideBySideColumnsSpendTheWidthExactlyAndNeverOverflow(t *testing.T) {
	sections := []Section{
		listSection("left", columnSpec("left", 30), numbered("l", 40)),
		listSection("right", columnSpec("right", 20), numbered("r", 40)),
	}
	for width := 50; width <= 200; width++ {
		kit := testKit(width, 30, 2)
		res := Arrange(kit, NewState(), sections...)
		if res.Arrangement != SideBySide {
			continue
		}
		total := defaultColumnGap
		for _, p := range res.Placements {
			if p.Width < p.Spec.MinWidth {
				t.Fatalf("width %d: column %s got %d columns, below its declared minimum %d",
					width, p.ID, p.Width, p.Spec.MinWidth)
			}
			total += p.Width
		}
		if total != kit.AvailableWidth() {
			t.Fatalf("width %d: columns spend %d of the %d available", width, total, kit.AvailableWidth())
		}
		for i, line := range lines(res.View) {
			if got := lipgloss.Width(ansi.Strip(line)); got > kit.AvailableWidth() {
				t.Fatalf("width %d: body line %d is %d cells wide, past the %d available",
					width, i, got, kit.AvailableWidth())
			}
		}
	}
}

func TestSideBySideColumnsBothGetTheFullBodyHeight(t *testing.T) {
	kit := testKit(120, 30, 2)
	res := Arrange(kit, NewState(),
		listSection("left", columnSpec("left", 40), numbered("l", 200)),
		listSection("right", columnSpec("right", 40), numbered("r", 200)),
	)
	for _, p := range res.Placements {
		if want := BodyRows(kit) - 1; p.Rows != want {
			t.Fatalf("column %s got %d rows, want the whole body height %d", p.ID, p.Rows, want)
		}
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestSideBySideNeverPaintsMoreRowsThanTheBodyHas(t *testing.T) {
	sections := []Section{
		listSection("left", columnSpec("left", 30), numbered("l", 200)),
		listSection("right", columnSpec("right", 30), numbered("r", 3)),
	}
	for height := 0; height <= 40; height++ {
		kit := testKit(120, height, 2)
		res := Arrange(kit, NewState(), sections...)
		if got, budget := res.Rows(), BodyRows(kit); got > budget {
			t.Fatalf("height=%d painted %d rows into a %d-row body", height, got, budget)
		}
	}
}

func TestEachColumnIsWindowedAgainstItsOwnWidth(t *testing.T) {
	// Two columns of different widths wrap the same item to different heights.
	// A single shared width — the copy this arranger removes — would give one
	// of them the wrong row count.
	long := "an item long enough that the narrow column has to wrap it but the wide one does not"
	body := func(Canvas) Block { return Block{Items: []string{long}} }
	res := Arrange(testKit(140, 30, 2), NewState(),
		Func{Def: columnSpec("wide", 90), Body: body},
		Func{Def: columnSpec("narrow", 20), Body: body},
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	wide, _ := res.Placement("wide")
	narrow, _ := res.Placement("narrow")
	if wide.Heights[0] >= narrow.Heights[0] {
		t.Fatalf("the same item measured %d rows at width %d and %d rows at width %d; the narrow column should be taller",
			wide.Heights[0], wide.Width, narrow.Heights[0], narrow.Width)
	}
}

func TestSideBySideLeavesNoTrailingWhitespace(t *testing.T) {
	// The last column is not padded. Padding it would put trailing spaces on
	// every row of every side-by-side body, which a themed terminal paints as a
	// block of background colour running to the right edge.
	res := Arrange(testKit(120, 20, 2), NewState(),
		listSection("left", columnSpec("left", 40), numbered("l", 40)),
		listSection("right", columnSpec("right", 40), []string{"short"}),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	for i, line := range lines(res.View) {
		if line != strings.TrimRight(line, " ") {
			t.Fatalf("body line %d ends in whitespace: %q", i, line)
		}
	}
}

func TestRaggedShortHintDoesNotBleedIntoTheNextColumn(t *testing.T) {
	// Enough items that the left column paints "▼ N below", which is shorter
	// than the column. The right column is a full-width pipe row on every line.
	// Occupancy of the join has to keep those pipes at a stable x.
	left := columnSpec("left", 30)
	right := columnSpec("right", 30)
	sections := []Section{
		listSection("left", left, numbered("l", 80)),
		Func{Def: right, Body: func(c Canvas) Block {
			w := max(c.Width(), 1)
			inner := max(w-2, 0)
			bar := "├" + strings.Repeat("─", inner) + "┤"
			items := make([]string, 20)
			for i := range items {
				items[i] = bar
			}
			return Block{Items: items}
		}},
	}
	kit := testKit(100, 24, 2)
	res := Arrange(kit, NewState(), sections...)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	x := -1
	for i, line := range lines(res.View) {
		if got := lipgloss.Width(line); got > kit.AvailableWidth() {
			t.Fatalf("line %d is %d cells, past the %d available", i, got, kit.AvailableWidth())
		}
		plain := ansi.Strip(line)
		at := strings.IndexRune(plain, '├')
		if at < 0 {
			continue
		}
		col := lipgloss.Width(plain[:at])
		if x < 0 {
			x = col
			continue
		}
		if col != x {
			t.Fatalf("inspector pipe drifted: first at %d, line %d at %d\n%s", x, i, col, plain)
		}
	}
	if x < 0 {
		t.Fatal("right column pipes missing")
	}
}
