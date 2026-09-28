package screenlayout

import "testing"

// ---------------------------------------------------------------------------
// Spec.MaxWidth (#2445).
//
// The one gap the pilot found in an otherwise faithful side-by-side
// reproduction of Project: `activityMaxWidth = 96` had no Spec field to live
// in. A comfortable measure is not the terminal's width, and a section that
// cannot DECLARE its cap has to keep the constant — which is the private copy
// this package exists to delete.
// ---------------------------------------------------------------------------

func TestAStackedSectionIgnoresAComfortableMeasureCap(t *testing.T) {
	// MaxWidth is a COLUMN rule: what a section is worth BESIDE a sibling.
	// Stacked there is no sibling to be worth anything beside, so the cap is
	// not applied — Task Detail's activity feed used to paint visibly
	// narrower than the sections it no longer shared a column with.
	kit := testKit(200, 30, 2)
	const measure = 96
	res := Arrange(kit, NewState(),
		listSection("wide", Spec{ID: "wide", MinWidth: 20, MinRows: 4, Scroll: ScrollItems}, numbered("w", 40)),
		listSection("uncapped", Spec{ID: "uncapped", MinWidth: 20, MaxWidth: measure, MinRows: 4, Scroll: ScrollItems}, numbered("c", 40)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked", res.Arrangement)
	}
	wide, _ := res.Placement("wide")
	uncapped, _ := res.Placement("uncapped")
	if wide.Width != kit.AvailableWidth() {
		t.Fatalf("the uncapped section got %d columns, want the whole %d available", wide.Width, kit.AvailableWidth())
	}
	if uncapped.Width != kit.AvailableWidth() {
		t.Fatalf("the capped section got %d columns stacked, want the whole %d available — MaxWidth is a column rule", uncapped.Width, kit.AvailableWidth())
	}
}

func TestAStackedFixedWidthSectionKeepsIt(t *testing.T) {
	// MinWidth == MaxWidth is a FIXED section, the same signal a fixed-height
	// section states with MinRows == MaxRows — the board's uniform lanes are
	// the case this exists for. Unlike a comfortable-measure cap it is not a
	// column rule, so it is honoured whether or not the section is stacked.
	kit := testKit(200, 30, 2)
	const fixed = 40
	res := Arrange(kit, NewState(),
		listSection("wide", Spec{ID: "wide", MinWidth: 20, MinRows: 4, Scroll: ScrollItems}, numbered("w", 40)),
		listSection("fixed", Spec{ID: "fixed", MinWidth: fixed, MaxWidth: fixed, MinRows: 4, Scroll: ScrollItems}, numbered("f", 40)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked", res.Arrangement)
	}
	got, _ := res.Placement("fixed")
	if got.Width != fixed {
		t.Fatalf("the fixed section got %d columns stacked, want its declared %d", got.Width, fixed)
	}
}

func TestAStackedCapWiderThanTheTerminalIsIgnored(t *testing.T) {
	kit := testKit(70, 30, 2)
	res := Arrange(kit, NewState(),
		listSection("capped", Spec{ID: "capped", MinWidth: 20, MaxWidth: 96, MinRows: 4, Scroll: ScrollItems}, numbered("c", 40)),
	)
	p, _ := res.Placement("capped")
	if p.Width != kit.AvailableWidth() {
		t.Fatalf("a 96-column cap on a %d-column terminal produced %d; a maximum is not a minimum",
			kit.AvailableWidth(), p.Width)
	}
}

func TestASideBySideCapLeavesItsSurplusToTheColumnsThatCanTakeIt(t *testing.T) {
	kit := testKit(200, 30, 2)
	capped := columnSpec("capped", 40)
	capped.MaxWidth = 50
	res := Arrange(kit, NewState(),
		Func{Def: capped, Body: constantBody(numbered("c", 40))},
		Func{Def: columnSpec("open", 40), Body: constantBody(numbered("o", 40))},
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	got := map[ID]int{}
	for _, p := range res.Placements {
		got[p.ID] = p.Width
	}
	if got["capped"] != 50 {
		t.Fatalf("the capped column got %d, want its declared maximum 50", got["capped"])
	}
	// Everything the cap refused went to the column that could still take it,
	// so the two still spend the available width exactly.
	if total := got["capped"] + got["open"] + defaultColumnGap; total != kit.AvailableWidth() {
		t.Fatalf("the columns spend %d of the %d available; the surplus the cap refused went nowhere",
			total, kit.AvailableWidth())
	}
}

func TestEveryColumnCappedLeavesTheRemainingWidthUnspent(t *testing.T) {
	// The width twin of "leftover rows simply go unpainted". The contract is
	// never more than available, not exactly available.
	kit := testKit(200, 30, 2)
	left, right := columnSpec("left", 40), columnSpec("right", 40)
	left.MaxWidth, right.MaxWidth = 50, 50
	res := Arrange(kit, NewState(),
		Func{Def: left, Body: constantBody(numbered("l", 40))},
		Func{Def: right, Body: constantBody(numbered("r", 40))},
	)
	total := defaultColumnGap
	for _, p := range res.Placements {
		if p.Width != 50 {
			t.Fatalf("column %s got %d columns past its declared maximum of 50", p.ID, p.Width)
		}
		total += p.Width
	}
	if total >= kit.AvailableWidth() {
		t.Fatalf("the capped columns spend %d of %d; the fixture no longer leaves anything unspent", total, kit.AvailableWidth())
	}
	for i, line := range lines(res.View) {
		if got := len([]rune(line)); got > kit.AvailableWidth() {
			t.Fatalf("body line %d is %d cells wide, past the %d available", i, got, kit.AvailableWidth())
		}
	}
}

func TestAMaximumWidthBelowTheMinimumIsRaisedToIt(t *testing.T) {
	got := Spec{ID: "s", MinWidth: 40, MaxWidth: 10}.normalize()
	if got.MaxWidth != 40 {
		t.Fatalf("MaxWidth normalised to %d, want it raised to the declared minimum 40", got.MaxWidth)
	}
	if unset := (Spec{ID: "s", MinWidth: 40}).normalize(); unset.MaxWidth != 0 {
		t.Fatalf("an undeclared MaxWidth normalised to %d, want it left unbounded", unset.MaxWidth)
	}
}

// ---------------------------------------------------------------------------
// Widths (#2425).
//
// A screen memoizing its own cards needs the column width to key on before it
// renders anything, so Widths reports the geometry without calling a body.
// Its whole justification is that it runs the SAME pass Arrange runs — if the
// two could disagree, a screen would be keying its memo on a width the frame
// does not paint, which is precisely the private copy this package exists to
// delete. That claim is only worth its doc comment if something checks it.
// ---------------------------------------------------------------------------

func TestWidthsReportsWhatTheFrameActuallyPaints(t *testing.T) {
	// Swept across the breakpoint so both arrangements are covered: a stacked
	// body gives every section the full width, a side-by-side one splits it.
	sections := func() []Section {
		activity := columnSpec("activity", 44)
		activity.WidthPercent, activity.MaxWidth = 45, 96
		return []Section{
			Func{Def: columnSpec("meta", 32), Body: constantBody(numbered("m", 30))},
			Func{Def: activity, Body: constantBody(numbered("a", 30))},
		}
	}

	for width := 40; width <= 240; width++ {
		kit := testKit(width, 30, 2)
		secs := sections()

		reported := Widths(kit, secs...)
		if len(reported) != len(secs) {
			t.Fatalf("width %d: Widths reported %d columns for %d sections", width, len(reported), len(secs))
		}

		res := Arrange(kit, NewState(), secs...)
		for i, s := range secs {
			id := s.Spec().ID
			p, ok := res.Placement(id)
			if !ok {
				t.Fatalf("width %d: %q was not placed", width, id)
			}
			if reported[i] != p.Width {
				t.Fatalf("width %d: Widths says %q gets %d columns, the frame paints it %d — a memo keyed on this would miss every time",
					width, id, reported[i], p.Width)
			}
		}
	}
}

func TestWidthsCallsNoSectionBody(t *testing.T) {
	// The point of asking for widths rather than arranging is that it costs
	// nothing: a screen calls this before deciding whether its cached cards are
	// still valid, so rendering them here would defeat the memo it feeds.
	rendered := 0
	counting := Func{
		Def: columnSpec("counted", 40),
		Body: func(c Canvas) Block {
			rendered++
			return constantBody(numbered("c", 20))(c)
		},
	}
	if got := Widths(testKit(200, 30, 2), counting); len(got) != 1 {
		t.Fatalf("Widths reported %d columns for one section", len(got))
	}
	if rendered != 0 {
		t.Fatalf("Widths rendered %d section bodies, want 0 — it is the cheap half by construction", rendered)
	}
}
