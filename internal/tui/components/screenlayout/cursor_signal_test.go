package screenlayout

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Placement.CursorUnread (#2445).
//
// Block.Heights is advisory and a section that lies about it is CAUGHT.
// The cursor had no equivalent: a section that never reads Canvas.Cursor()
// paints no highlight while Placement.Cursor and CursorLine both report a
// selection that moved. Every number agrees with every other number and the
// screen simply looks like it has nothing selected — which is exactly the class
// DeclaredHeightsDisagree exists to make loud.
// ---------------------------------------------------------------------------

func forgetfulSection(id ID, selectFirst bool) Func {
	return Func{
		Def: Spec{ID: id, MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: selectFirst},
		Body: func(Canvas) Block { // never calls Canvas.Cursor
			return Block{Items: numbered("item", 20)}
		},
	}
}

func TestASectionThatNeverReadsItsCursorIsReported(t *testing.T) {
	// Block.Heights is advisory and a section that lies about it is CAUGHT.
	// There was no equivalent for the cursor: a section that never reads it
	// paints no highlight, while Cursor and CursorLine both report a selection
	// that moved. Every number agrees with every other number and the screen
	// looks like it has nothing selected — which is exactly the class
	// DeclaredHeightsDisagree exists to make loud.
	kit := testKit(120, 40, 2)
	res := Arrange(kit, NewState().WithCursor("list", 5), forgetfulSection("list", true))
	p, ok := res.Placement("list")
	if !ok {
		t.Fatal("the list section was not placed")
	}
	if p.Cursor <= 0 || p.CursorLine < 0 {
		t.Fatalf("the arranger reports cursor=%d line=%d; the finding is not being exercised", p.Cursor, p.CursorLine)
	}
	if !p.CursorUnread {
		t.Fatal("a section held a moved cursor, never asked for it, and painted no highlight — and nothing said so")
	}
	if p.DeclaredHeightsDisagree {
		t.Error("the height check fired on a section that declared no heights")
	}
}

func TestASectionThatReadsItsCursorIsNotReported(t *testing.T) {
	kit := testKit(120, 40, 2)
	attentive := Func{
		Def: Spec{ID: "list", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(c Canvas) Block {
			items := numbered("item", 20)
			if c.Cursor() >= 0 {
				items[c.Cursor()] = "› " + items[c.Cursor()]
			}
			return Block{Items: items}
		},
	}
	res := Arrange(kit, NewState().WithCursor("list", 5), attentive)
	p, _ := res.Placement("list")
	if p.CursorUnread {
		t.Fatal("a section that read its cursor and painted the highlight was reported as ignoring it")
	}
}

func TestNoCursorIsNoDisagreement(t *testing.T) {
	// Two ways a section legitimately has nothing to paint: it was never given
	// a selection, or it said so itself. Neither is a disagreement, and flagging
	// them would make the signal noise.
	kit := testKit(120, 40, 2)
	res := Arrange(kit, NewState(), forgetfulSection("fresh", false))
	if p, _ := res.Placement("fresh"); p.CursorUnread {
		t.Fatalf("a section with no selection (cursor %d) was reported as ignoring one", p.Cursor)
	}
	declining := Func{
		Def:  Spec{ID: "declining", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block { return Block{Items: numbered("i", 20), Cursor: NoSelection()} },
	}
	res = Arrange(kit, NewState().WithCursor("declining", 5), declining)
	if p, _ := res.Placement("declining"); p.CursorUnread {
		t.Fatal("a section that declared NoSelection was reported as ignoring a cursor it does not have")
	}
}

func TestASectionThatNamesItsOwnSelectionIsNotReported(t *testing.T) {
	// A section whose selection is authoritative elsewhere states it with
	// Block.Cursor.At and paints the highlight from the same knowledge. It never
	// needs to READ the arranger's cursor, and flagging it would be a false
	// positive — the flag is for a section that INHERITED a cursor and ignored it.
	kit := testKit(120, 40, 2)
	authoritative := Func{
		Def:  Spec{ID: "board", MinWidth: 30, MinRows: 4, Weight: 1, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: numbered("i", 20), Cursor: At(3)} },
	}
	res := Arrange(kit, NewState(), authoritative)
	p, _ := res.Placement("board")
	if p.Cursor != 3 {
		t.Fatalf("cursor = %d, want the 3 the block named", p.Cursor)
	}
	if p.CursorUnread {
		t.Fatal("a section that named its own selected item was reported as ignoring the cursor")
	}
}

func TestACanvasBuiltForATestTracksNoRead(t *testing.T) {
	// NewCanvas exists for testing a section in isolation, where there is no
	// arranger listening. Reading its cursor must not panic on the absent flag.
	c := NewCanvas(40, 10, 2)
	if got := c.Cursor(); got != 2 {
		t.Fatalf("Cursor() = %d, want the 2 it was built with", got)
	}
}
