package screenlayout

import (
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// feed is a scrollable section of n single-line items.
func feed(id ID, n int) Func {
	return listSection(id, Spec{ID: id, MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems, SelectFirst: true}, numbered("row", n))
}

// scrollTo drives the focused section down by pressing "down" n times, which is
// the only way a screen is allowed to move an offset.
func scrollTo(kit screenkit.Kit, state State, n int, sections ...Section) State {
	for i := 0; i < n; i++ {
		next, handled := state.HandleKey(kit, "down", sections...)
		if !handled {
			panic("down was not handled")
		}
		state = next
	}
	return state
}

func TestOffsetsSurviveAcrossFrames(t *testing.T) {
	kit := testKit(100, 20, 2)
	sec := feed("feed", 200)
	state := scrollTo(kit, NewState(), 40, sec)

	first := Arrange(kit, state, sec)
	p1, _ := first.Placement("feed")
	if p1.Offset == 0 {
		t.Fatal("forty presses of down left the offset at zero")
	}
	// A second frame with the same carried state renders identically — the
	// offset lived in the value the screen holds, not in the View.
	second := Arrange(kit, state, sec)
	p2, _ := second.Placement("feed")
	if p1.Offset != p2.Offset || p1.Cursor != p2.Cursor || first.View != second.View {
		t.Fatalf("frame two differs: offset %d->%d cursor %d->%d", p1.Offset, p2.Offset, p1.Cursor, p2.Cursor)
	}
}

func TestOffsetsSurviveAResizeAndStayInsideTheNewViewport(t *testing.T) {
	tall := testKit(100, 50, 2)
	sec := feed("feed", 200)
	state := scrollTo(tall, NewState(), 120, sec)
	before, _ := Arrange(tall, state, sec).Placement("feed")

	// Shrink the terminal hard, then wire the resize the way a screen does.
	short := testKit(100, 12, 2)
	state = state.Resync(short, sec)
	after, _ := Arrange(short, state, sec).Placement("feed")

	if after.Cursor != before.Cursor {
		t.Fatalf("the resize moved the selection from item %d to item %d", before.Cursor, after.Cursor)
	}
	if after.Cursor < after.First || after.Cursor > after.Last {
		t.Fatalf("after the resize the cursor %d is outside the visible range [%d,%d]", after.Cursor, after.First, after.Last)
	}
	if state.Offset("feed") != after.Offset {
		t.Fatalf("Resync persisted offset %d but the frame paints from %d", state.Offset("feed"), after.Offset)
	}
	// Grow it back: the selection is still the item the user chose.
	state = state.Resync(tall, sec)
	if got := state.Cursor("feed"); got != before.Cursor {
		t.Fatalf("growing the terminal back left the cursor on item %d, want %d", got, before.Cursor)
	}
}

func TestAFrameRenderedBeforeUpdateSawTheResizeStillDoesNotOverdraw(t *testing.T) {
	// The failure mode State cannot prevent on its own: a View at the new
	// geometry with an offset stored at the old one. Arrange clamps for the
	// frame it is painting without persisting, so the body still fits.
	tall := testKit(100, 50, 2)
	sec := feed("feed", 200)
	state := scrollTo(tall, NewState(), 150, sec)

	short := testKit(100, 10, 2) // no Resync — this is the unwired path
	res := Arrange(short, state, sec)
	if got, budget := res.Rows(), BodyRows(short); got > budget {
		t.Fatalf("painted %d rows into a %d-row body", got, budget)
	}
	p, _ := res.Placement("feed")
	if p.Cursor < p.First || p.Cursor > p.Last {
		t.Fatalf("cursor %d outside the visible range [%d,%d]", p.Cursor, p.First, p.Last)
	}
}

func TestStateIsAValueAndAnOlderCopyNeverSeesALaterOffset(t *testing.T) {
	kit := testKit(100, 20, 2)
	sec := feed("feed", 200)
	older := scrollTo(kit, NewState(), 10, sec)
	snapshot := older.Offset("feed")
	newer := scrollTo(kit, older, 40, sec)

	if older.Offset("feed") != snapshot {
		t.Fatalf("mutating the newer state moved the older one from %d to %d", snapshot, older.Offset("feed"))
	}
	if newer.Offset("feed") == snapshot {
		t.Fatal("the newer state did not move at all")
	}
}

func TestASectionWithoutASelectionScrollsItsOffsetInstead(t *testing.T) {
	// A body-scroll section — a rendered description, an ascii graph — has no
	// selectable item. The same keys move its window rather than a cursor, and
	// the screen does not have to say which kind it is.
	kit := testKit(100, 20, 2)
	sec := Func{
		Def:  Spec{ID: "body", MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: numbered("line", 200), Cursor: NoSelection()} },
	}
	state, handled := NewState().HandleKey(kit, "pgdown", sec)
	if !handled {
		t.Fatal("pgdown was not handled by a scrollable section")
	}
	p, _ := Arrange(kit, state, sec).Placement("body")
	if p.Cursor != -1 {
		t.Fatalf("cursor = %d, want the no-selection sentinel", p.Cursor)
	}
	if p.Offset == 0 {
		t.Fatal("pgdown on a cursorless section left the offset at zero")
	}
}

func TestResyncClampsACursorPastTheEndOfAShrunkList(t *testing.T) {
	kit := testKit(100, 30, 2)
	state := NewState().WithCursor("feed", 500)
	sec := feed("feed", 12)
	state = state.Resync(kit, sec)
	if got := state.Cursor("feed"); got != 11 {
		t.Fatalf("cursor = %d, want it clamped to the last of twelve items", got)
	}
}
