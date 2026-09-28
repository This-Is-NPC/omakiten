package screenlayout

import (
	"fmt"
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// ---------------------------------------------------------------------------
// The render multiplier (#2446, the fourth question).
//
// #2424 recorded "two full renders per keystroke, unmeasured" and the pilot
// measured three: HandleKey resolved the frame once to find the focused section,
// Resync resolved it again to persist a paintable pair, and the View that
// followed resolved it a third time. Every resolve renders every section.
//
// #2447 removed Task Detail's own undeclared render multiplier, so the engine's
// count stopped being something a migration would REPLACE and became something
// it would ADD to a clean 1x. At 1.33 ms/keystroke measured there, three renders
// would put that screen back where it started.
//
// Two of the three looked structural: one keystroke has to know the geometry it
// is moving within, and the frame after it has to be painted. The third was not,
// and #2446 removed it.
//
// #2448 removed the second. A keystroke needs the MEASUREMENT of the body it is
// moving within, not its lines — so [State] carries the measurement the last
// resolve took, and the only resolve left per keystroke is the one that paints.
// The pilot measured a +15.2% regression against the unmigrated screen with two;
// with one, the engine costs a migrated screen less than its own hand-rolled
// layout did.
// ---------------------------------------------------------------------------

// countingSection reports every call to its body, so the multiplier is measured
// rather than reasoned about.
func countingSection(id ID, items []string, renders *int) Func {
	return Func{
		Def: Spec{ID: id, MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block {
			*renders++
			return Block{Items: items}
		},
	}
}

// cursorReadingSection is a body that PAINTS its selection, so its output
// depends on the cursor and its measurement cannot outlive a move of it.
func cursorReadingSection(id ID, items []string, renders *int) Func {
	return Func{
		Def: Spec{ID: id, MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(c Canvas) Block {
			*renders++
			out := append([]string(nil), items...)
			if at := c.Cursor(); at >= 0 && at < len(out) {
				out[at] = "▌" + out[at]
			}
			return Block{Items: out}
		},
	}
}

// TestAKeystrokeAndTheFrameAfterItRenderEachSectionOnce is criterion 1, counted.
//
// The FIRST keystroke off a fresh state still resolves twice, and that is not a
// miss to be apologised for: nothing has measured this body yet, so the frame it
// moves within has to be taken. From the second keystroke on — which is every
// keystroke a user actually types — the measurement is carried and the only
// render left is the paint.
func TestAKeystrokeAndTheFrameAfterItRenderEachSectionOnce(t *testing.T) {
	kit := testKit(120, 40, 2)
	renders := 0
	subtasks := countingSection("subtasks", cards("sub", 40, 3), &renders)
	activity := countingSection("activity", cards("act", 40, 5), &renders)
	const sections = 2

	state, handled := NewState().HandleKey(kit, "j", subtasks, activity)
	if !handled {
		t.Fatal("the arranger declined \"j\"; the measurement is vacuous")
	}
	Arrange(kit, state, subtasks, activity)

	renders = 0
	state, _ = state.HandleKey(kit, "j", subtasks, activity)
	onKey := renders

	renders = 0
	Arrange(kit, state, subtasks, activity)
	onView := renders

	if onKey != 0 {
		t.Errorf("one keystroke renders %d section bodies, want none — the body it moves within was measured by the frame on screen, and a key moves a number",
			onKey)
	}
	if onView != sections {
		t.Errorf("the frame after a keystroke renders %d section bodies for %d sections, want one apiece", onView, sections)
	}
	t.Logf("keystroke costs %d section renders and the frame after it %d — %d per section per keystroke",
		onKey, onView, (onKey+onView)/sections)
}

// TestAFirstKeystrokeOffAFreshStateStillMeasuresTheBodyItMovesWithin states the
// miss, so the carried measurement is never mistaken for a guarantee that a
// keystroke NEVER resolves.
func TestAFirstKeystrokeOffAFreshStateStillMeasuresTheBodyItMovesWithin(t *testing.T) {
	kit := testKit(120, 40, 2)
	renders := 0
	one := countingSection("one", cards("1", 40, 3), &renders)
	two := countingSection("two", cards("2", 40, 5), &renders)

	if _, handled := NewState().HandleKey(kit, "j", one, two); !handled {
		t.Fatal("the arranger declined \"j\"; the measurement is vacuous")
	}
	if renders != 2 {
		t.Errorf("the first keystroke rendered %d section bodies, want 2 — nothing had measured this body yet", renders)
	}
}

// TestASectionThatReadsItsCursorIsRemeasuredWhenThatCursorMoves is the other
// half of the rule, and the half that keeps the carried measurement honest: a
// body that paints its selection may paint different ROWS for a different
// selection, so its measurement does not survive the move. Exactly that one
// section is retaken — a keystroke moves one cursor, so its neighbours are not
// implicated.
func TestASectionThatReadsItsCursorIsRemeasuredWhenThatCursorMoves(t *testing.T) {
	kit := testKit(120, 40, 2)
	quiet, painting := 0, 0
	still := countingSection("still", cards("s", 40, 3), &quiet)
	moving := cursorReadingSection("moving", cards("m", 40, 5), &painting)

	state, handled := NewState().WithFocus("moving").HandleKey(kit, "j", still, moving)
	if !handled {
		t.Fatal("the arranger declined \"j\"; the measurement is vacuous")
	}
	quiet, painting = 0, 0
	if _, handled = state.HandleKey(kit, "j", still, moving); !handled {
		t.Fatal("the arranger declined the second \"j\"")
	}
	if painting != 1 {
		t.Errorf("the section whose body reads the cursor rendered %d times for a move of that cursor, want 1", painting)
	}
	if quiet != 0 {
		t.Errorf("the section that never reads the cursor rendered %d times, want 0 — its neighbour's cursor is not its business", quiet)
	}
}

func TestSectionCyclingRendersNothing(t *testing.T) {
	kit := testKit(120, 40, 2)
	renders := 0
	one := countingSection("one", cards("1", 20, 3), &renders)
	two := countingSection("two", cards("2", 20, 3), &renders)

	state, handled := NewState().HandleKey(kit, "tab", one, two)
	if !handled {
		t.Fatal("tab was not handled; the measurement is vacuous")
	}
	renders = 0
	if _, handled = state.HandleKey(kit, "tab", one, two); !handled {
		t.Fatal("the second tab was not handled")
	}
	if renders != 0 {
		t.Errorf("cycling focus rendered %d section bodies, want 0 — moving focus moves no cursor and measures nothing", renders)
	}
}

// TestWhatAKeystrokePersistsIsAlreadyResynced is the licence for the render
// above being one and not two.
//
// The dropped resolve existed to clamp what the key had just moved. If what
// HandleKey persists is already a FIXED POINT of the full resolve — running
// Resync over it changes nothing, for any key, at any geometry, over any of
// these content shapes — then the dropped work was redundant rather than
// skipped, and the equality says so directly instead of by argument.
func TestWhatAKeystrokePersistsIsAlreadyResynced(t *testing.T) {
	keys := []string{"down", "up", "pgdown", "pgup", "home", "end", "j", "k", "G", "g", "ctrl+d", "ctrl+u"}
	shapes := []struct {
		name  string
		build func() []Section
	}{
		{"two card feeds", func() []Section {
			return []Section{
				listSection("top", Spec{ID: "top", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, cards("t", 40, 3)),
				listSection("bottom", Spec{ID: "bottom", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, cards("b", 40, 5)),
			}
		}},
		{"a cursorless body scroll beside a list", func() []Section {
			return []Section{bodyScroll("body", 200), feed("list", 200)}
		}},
		{"an authoritative selection", func() []Section {
			return []Section{
				Func{
					Def:  Spec{ID: "pinned", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems},
					Body: func(Canvas) Block { return Block{Items: numbered("p", 60), Cursor: At(17)} },
				},
				feed("list", 60),
			}
		}},
		{"two stacked members and a column beside them", func() []Section {
			return []Section{
				Func{Def: groupedColumn("form", "left", 40, 4, 1), Body: constantBody(cards("f", 40, 2))},
				Func{Def: groupedColumn("subtasks", "left", 40, 4, 1), Body: constantBody(cards("s", 40, 4))},
				Func{Def: groupedColumn("activity", "", 40, 4, 1), Body: constantBody(cards("a", 40, 3))},
			}
		}},
		{"a section short of content beside one hiding it", func() []Section {
			return []Section{
				listSection("stub", Spec{ID: "stub", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, numbered("s", 2)),
				listSection("feed", Spec{ID: "feed", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, cards("f", 80, 4)),
			}
		}},
	}

	for _, geometry := range []struct{ width, height int }{{80, 24}, {120, 40}, {200, 50}, {60, 12}} {
		kit := testKit(geometry.width, geometry.height, 2)
		assertResyncGeometry(t, kit, geometry.width, geometry.height, keys, shapes)
	}
}

func assertResyncGeometry(t *testing.T, kit screenkit.Kit, width, height int, keys []string, shapes []struct {
	name  string
	build func() []Section
}) {
	for _, shape := range shapes {
		sections := shape.build()
		for _, focus := range idsOf(sections) {
			state := NewState().WithFocus(focus)
			for press := 0; press < 4; press++ {
				for _, key := range keys {
					next, handled := state.HandleKey(kit, key, sections...)
					if !handled {
						continue
					}
					assertSameState(t, kit, next, sections, fmt.Sprintf("%dx%d %s focus=%s key=%q press=%d", width, height, shape.name, focus, key, press))
					state = next
				}
			}
		}
	}
}

func assertSameState(tb testing.TB, kit screenkit.Kit, state State, sections []Section, label string) {
	tb.Helper()
	resynced := state.Resync(kit, sections...)
	for _, id := range idsOf(sections) {
		if got, want := state.Cursor(id), resynced.Cursor(id); got != want {
			tb.Fatalf("%s: %s persisted cursor %d, a full resolve makes it %d", label, id, got, want)
		}
		if got, want := state.Offset(id), resynced.Offset(id); got != want {
			tb.Fatalf("%s: %s persisted offset %d, a full resolve makes it %d", label, id, got, want)
		}
	}
}

func idsOf(sections []Section) []ID {
	out := make([]ID, len(sections))
	for i, s := range sections {
		out[i] = s.Spec().ID
	}
	return out
}

// TestASectionWhoseHeightsFollowItsCursorSelfCorrectsOnTheNextFrame is the
// exception the equality above cannot cover, stated rather than hidden.
//
// A section may render the selected item at a different height from the rest. A
// keystroke then clamps against the heights measured BEFORE the move, and the
// stored offset can be one the new heights would not have produced. Nothing
// paints wrong: Arrange re-resolves and re-clamps everything it is handed, and
// the next keystroke or Resync writes the corrected pair back — which is the
// same self-correction the package already relies on for the frame after a
// resize the Update path has not seen.
func TestASectionWhoseHeightsFollowItsCursorSelfCorrectsOnTheNextFrame(t *testing.T) {
	kit := testKit(100, 20, 2)
	items := numbered("row", 60)
	elastic := Func{
		Def: Spec{ID: "elastic", MinWidth: 20, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(c Canvas) Block {
			out := append([]string(nil), items...)
			if at := c.Cursor(); at >= 0 && at < len(out) {
				out[at] += "\nexpanded\nexpanded\nexpanded"
			}
			return Block{Items: out}
		},
	}

	state := NewState()
	for press := 0; press < 40; press++ {
		next, handled := state.HandleKey(kit, "down", elastic)
		if !handled {
			t.Fatal("down was not handled")
		}
		state = next
		res := Arrange(kit, state, elastic)
		if got, budget := res.Rows(), BodyRows(kit); got > budget {
			t.Fatalf("press %d: painted %d rows into a %d-row body", press, got, budget)
		}
		p, _ := res.Placement("elastic")
		if p.Cursor < p.First || p.Cursor > p.Last {
			t.Fatalf("press %d: the cursor landed on item %d, outside the visible range [%d,%d]",
				press, p.Cursor, p.First, p.Last)
		}
	}
	// One Resync settles whatever the moving heights left behind, and a second
	// changes nothing — the state converges rather than drifting.
	once := state.Resync(kit, elastic)
	assertSameState(t, kit, once, []Section{elastic}, "after one resync")
}
