package screenlayout

import (
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// ---------------------------------------------------------------------------
// The cross-call measurement (#2448).
//
// A keystroke needs the MEASUREMENT of the body it is moving within, not its
// lines, so [State] carries the measurement the last resolve took and the only
// resolve left per keystroke is the one that paints. These are the properties
// that make carrying it safe, stated one at a time.
// ---------------------------------------------------------------------------

// TestACarriedMeasurementCannotChangeWhatIsPainted is the whole safety case in
// one assertion.
//
// [Arrange] never consults a carried measurement for anything but the rows an
// item it has already been handed occupies, and wrapping is a pure function of
// an item and a width — so a frame painted by a state that carries a
// measurement is BYTE-IDENTICAL to the same frame painted by a state that
// carries none. Nothing a measurement remembers can reach the screen.
func TestACarriedMeasurementCannotChangeWhatIsPainted(t *testing.T) {
	for _, geometry := range []struct{ width, height int }{{80, 24}, {120, 40}, {200, 50}} {
		kit := testKit(geometry.width, geometry.height, 2)
		sections := []Section{
			listSection("top", Spec{ID: "top", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, cards("t", 40, 3)),
			listSection("bottom", Spec{ID: "bottom", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, cards("b", 40, 5)),
		}
		carried := NewState()
		bare := NewState()
		for press := 0; press < 6; press++ {
			for _, key := range []string{"j", "j", "pgdown", "tab", "k", "end", "home"} {
				carried, _ = carried.HandleKey(kit, key, sections...)
				// The bare state is driven through the same keys but is stripped
				// of its measurement before every paint, so it resolves cold.
				bare, _ = bare.HandleKey(kit, key, sections...)
				bare = bare.remember(nil)

				withMemo := Arrange(kit, carried, sections...)
				without := Arrange(kit, bare, sections...)
				if withMemo.View != without.View {
					t.Fatalf("%dx%d after %q: a carried measurement changed what was painted",
						geometry.width, geometry.height, key)
				}
			}
		}
	}
}

// TestADiscardedScreenValueCannotCorruptALiveOne is criterion 6.
//
// #2424 rejected a `*store` inside [State] because a discarded screen value
// could mutate a live one, and in an Elm architecture holding two screen values
// at once is normal rather than exotic. The measurement is carried behind a
// pointer, so the rejection has to be answered rather than assumed away: a
// frame is FROZEN, every update returns a new one, and nothing is ever written
// through the pointer two states share.
//
// So a state may be forked, the fork driven as hard as you like, and the
// original still paints exactly what it painted before the fork existed.
func TestADiscardedScreenValueCannotCorruptALiveOne(t *testing.T) {
	kit := testKit(120, 40, 2)
	sections := []Section{feed("left", 80), feed("right", 80)}

	live, _ := NewState().HandleKey(kit, "j", sections...)
	before := Arrange(kit, live, sections...).View
	liveCursor, liveOffset := live.Cursor("left"), live.Offset("left")

	// A discarded value: forked from the live one, driven through everything the
	// arranger owns, and thrown away.
	discarded := live
	for press := 0; press < 20; press++ {
		for _, key := range []string{"j", "pgdown", "end", "tab", "k", "home", "G"} {
			discarded, _ = discarded.HandleKey(kit, key, sections...)
			discarded = discarded.Resync(kit, sections...)
			Arrange(kit, discarded, sections...)
		}
	}

	if got := Arrange(kit, live, sections...).View; got != before {
		t.Error("a discarded state's keystrokes changed what the live state paints")
	}
	if got, want := live.Cursor("left"), liveCursor; got != want {
		t.Errorf("the live state's cursor moved to %d, want %d", got, want)
	}
	if got, want := live.Offset("left"), liveOffset; got != want {
		t.Errorf("the live state's offset moved to %d, want %d", got, want)
	}
	// And the live state still carries the measurement it had, not the one the
	// discarded value ended on.
	if live.frame == nil {
		t.Fatal("the live state lost its measurement to a value that was thrown away")
	}
	if got, want := live.frame.sections[0].cursor, liveCursor; got != want {
		t.Errorf("the live state's carried measurement says cursor %d, want %d", got, want)
	}
}

// TestEveryWriteToAStateDropsTheMeasurementItDescribes is invalidation by
// construction rather than by discipline.
//
// A measurement describes the pairs it was taken with. The moment one of those
// pairs is written it no longer does — so [State.mutate] drops it outright, and
// there is no path through the public API that writes a cursor and keeps a
// measurement of the body it was in.
func TestEveryWriteToAStateDropsTheMeasurementItDescribes(t *testing.T) {
	kit := testKit(120, 40, 2)
	renders := 0
	one := countingSection("one", cards("1", 40, 3), &renders)
	two := countingSection("two", cards("2", 40, 5), &renders)

	warm, _ := NewState().HandleKey(kit, "j", one, two)
	if warm.frame == nil {
		t.Fatal("a keystroke left no measurement behind; the rest of this test is vacuous")
	}

	moved := warm.WithCursor("one", 4)
	if moved.frame != nil {
		t.Error("a screen moved a cursor and the state kept a measurement of the body before the move")
	}
	renders = 0
	if _, handled := moved.HandleKey(kit, "j", one, two); !handled {
		t.Fatal("the arranger declined \"j\"")
	}
	if renders != 2 {
		t.Errorf("the keystroke after a screen moved a cursor rendered %d section bodies, want 2 — it has to measure the body again", renders)
	}

	// The state the write came from is untouched: it still carries its own.
	if warm.frame == nil {
		t.Error("writing to a derived state dropped the measurement of the state it was derived from")
	}
}

// TestACarriedMeasurementIsDroppedWhenTheInputsItWitnessesMove covers every way
// [frame.fits] can say no. Each one re-resolves, which is the behaviour that
// shipped before the measurement was carried at all — a miss costs exactly what
// every keystroke used to cost.
func TestACarriedMeasurementIsDroppedWhenTheInputsItWitnessesMove(t *testing.T) {
	renders := 0
	one := countingSection("one", cards("1", 40, 3), &renders)
	two := countingSection("two", cards("2", 40, 5), &renders)
	// A third section, and the same two sections with one spec changed.
	three := countingSection("three", cards("3", 40, 2), &renders)
	widened := Func{
		Def:  Spec{ID: "one", MinWidth: 44, MinRows: 4, Weight: 1, Scroll: ScrollItems},
		Body: one.Body,
	}

	cases := []struct {
		name     string
		width    int
		height   int
		sections []Section
	}{
		{name: "a wider terminal", width: 160, height: 40, sections: []Section{one, two}},
		{name: "a shorter terminal", width: 120, height: 30, sections: []Section{one, two}},
		{name: "a section that was not there", width: 120, height: 40, sections: []Section{one, two, three}},
		{name: "a section that declares something else", width: 120, height: 40, sections: []Section{widened, two}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			warm := testKit(120, 40, 2)
			state, handled := NewState().HandleKey(warm, "j", one, two)
			if !handled {
				t.Fatal("the arranger declined \"j\"; the measurement is vacuous")
			}
			renders = 0
			if _, handled = state.HandleKey(testKit(testCase.width, testCase.height, 2), "j", testCase.sections...); !handled {
				t.Fatal("the arranger declined the second \"j\"")
			}
			if renders != len(testCase.sections) {
				t.Errorf("%s: the keystroke rendered %d section bodies for %d sections, want one apiece — the carried measurement does not describe this body",
					testCase.name, renders, len(testCase.sections))
			}
		})
	}
}

// TestMeasureItemsWillNotCarryAMeasurementTakenAtAnotherWidth exercises the
// width guard directly.
//
// No caller can reach it today: a measurement is only offered back to the
// resolver once [frame.fits] has witnessed the same available width and the
// same specs, which is what fixes every section's width. It is exercised here
// for the same reason the movement helpers' ActionNone branch is — so the
// behaviour is stated rather than left as an unreviewed fallthrough, and so the
// next caller cannot introduce the bug by supplying a prev of its own.
func TestMeasureItemsWillNotCarryAMeasurementTakenAtAnotherWidth(t *testing.T) {
	item := "a line long enough that it wraps once at forty columns and twice at twenty"
	block := Block{Items: []string{item}}

	narrow := measured{width: 20}
	narrow.raw, narrow.items, narrow.heights, _ = measureItems(block, 20, nil)

	_, wide, heights, _ := measureItems(block, 40, &narrow)
	_, alone, standalone, _ := measureItems(block, 40, nil)

	if heights[0] != standalone[0] || wide[0] != alone[0] {
		t.Errorf("a measurement taken at width 20 was carried into a width-40 measurement: got %d rows, want %d",
			heights[0], standalone[0])
	}
	if narrow.heights[0] == standalone[0] {
		t.Skip("this item wraps to the same height at both widths; the guard is untested rather than passing")
	}
}

// TestACarriedMeasurementIsNotOfferedForItemsTheSectionNoLongerHas is the other
// edge of the carry: a section that shed items is offered a measurement longer
// than its new item list, and every item past the end is measured afresh.
func TestACarriedMeasurementIsNotOfferedForItemsTheSectionNoLongerHas(t *testing.T) {
	long := Block{Items: numbered("row", 6)}
	short := Block{Items: numbered("row", 2)}

	prev := measured{width: 40}
	prev.raw, prev.items, prev.heights, _ = measureItems(long, 40, nil)

	_, items, heights, _ := measureItems(short, 40, &prev)
	if len(items) != 2 || len(heights) != 2 {
		t.Fatalf("measuring 2 items against a 6-item measurement produced %d items and %d heights, want 2 and 2",
			len(items), len(heights))
	}

	// And the other way round: more items than the measurement covers.
	shortPrev := measured{width: 40}
	shortPrev.raw, shortPrev.items, shortPrev.heights, _ = measureItems(short, 40, nil)
	_, grown, grownHeights, _ := measureItems(long, 40, &shortPrev)
	_, alone, aloneHeights, _ := measureItems(long, 40, nil)
	for i := range grown {
		if grown[i] != alone[i] || grownHeights[i] != aloneHeights[i] {
			t.Fatalf("item %d measured differently when carried from a shorter measurement", i)
		}
	}
}

// TestACarriedMeasurementIsKeyedOnTheItemAndNotOnItsPosition is the false
// positive the snapshot exists to make impossible.
//
// A section that rebuilds its items in place would, without the copy, compare
// its NEW string against itself and be handed the OLD height. Against a
// snapshot this package owns, a changed item is a changed string.
func TestACarriedMeasurementIsKeyedOnTheItemAndNotOnItsPosition(t *testing.T) {
	items := []string{"one line", "another line"}
	block := Block{Items: items}

	prev := measured{width: 40}
	prev.raw, prev.items, prev.heights, _ = measureItems(block, 40, nil)

	// The section mutates the slice it already handed over, in place.
	items[0] = "one line\nthat now paints\non three rows"
	_, _, heights, _ := measureItems(Block{Items: items}, 40, &prev)
	if heights[0] != 3 {
		t.Errorf("an item rebuilt in place measured %d rows, want 3 — the carry compared it against itself", heights[0])
	}
}

// TestTheCarriedMeasurementAlwaysDescribesTheStateItIsAttachedTo is the
// invariant every other property rests on: a measurement is attached only
// alongside the very pairs it measured, so a keystroke reading it is reading
// what the state already holds rather than a second opinion about it.
func TestTheCarriedMeasurementAlwaysDescribesTheStateItIsAttachedTo(t *testing.T) {
	kit := testKit(120, 40, 2)
	sections := []Section{
		listSection("form", Spec{ID: "form", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems}, cards("f", 40, 2)),
		feed("list", 90),
	}

	state := NewState().Resync(kit, sections...)
	for press := 0; press < 8; press++ {
		for _, key := range []string{"j", "pgdown", "tab", "k", "end", "home", "G", "g"} {
			state = assertMeasurementKey(t, kit, state, sections, key)
		}
	}
}

func assertMeasurementKey(t *testing.T, kit screenkit.Kit, state State, sections []Section, key string) State {
	next, handled := state.HandleKey(kit, key, sections...)
	if !handled {
		return state
	}
	if next.frame == nil {
		t.Fatalf("%q left the state without a measurement", key)
	}
	for _, m := range next.frame.sections {
		if m.dropped {
			continue
		}
		if got, want := m.cursor, next.Cursor(m.spec.ID); got != want {
			t.Fatalf("%q: the measurement says %s is at cursor %d, the state says %d", key, m.spec.ID, got, want)
		}
		if got, want := m.offset, next.Offset(m.spec.ID); got != want {
			t.Fatalf("%q: the measurement says %s is at offset %d, the state says %d", key, m.spec.ID, got, want)
		}
	}
	return next
}
