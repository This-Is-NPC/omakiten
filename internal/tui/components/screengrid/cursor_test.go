package screengrid

import (
	"testing"

	"omakiten/internal/tui/components/screenlayout"
)

// countingBody is the witness the frame memo is measured with, as in
// [TestHandleKeyReusesEquivalentGridFrame]: a leaf that records every time it
// was composed, so "did this keystroke re-render the body" is a number rather
// than an impression.
func countingBody(calls *int) func(screenlayout.Canvas) screenlayout.Block {
	items := []string{"one", "two", "three", "four", "five"}
	return func(canvas screenlayout.Canvas) screenlayout.Block {
		*calls++
		// It READS the cursor and inherits it rather than restating it, which is
		// the conservative shape: screenlayout marks such a body's measurement
		// stale when the selection moves, so nothing here is memoised on an
		// assumption a real screen's body would not also make.
		at := canvas.Cursor()
		out := make([]string, len(items))
		for i, item := range items {
			marker := "  "
			if i == at {
				marker = "> "
			}
			out[i] = marker + item
		}
		return screenlayout.Block{Items: out}
	}
}

func cursorSpec() screenlayout.Spec {
	return screenlayout.Spec{
		ID:          "body",
		MinRows:     4,
		Scroll:      screenlayout.ScrollItems,
		SelectFirst: true,
	}
}

// The cursor a screen seeds is the cursor the grid carries, and the state it
// was seeded from does not observe the write.
func TestWithCursorWritesTheCursorThrough(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	root := Cell(cursorSpec(), countingBody(&calls))
	base := NewState().Resync(kit, b, root).WithFocus("body")

	cases := map[string]struct {
		id    screenlayout.ID
		index int
	}{
		"an item of the composed body": {"body", 3},
		"the last item":                {"body", 4},
		// A seed may name a leaf this box did not place — a zone dropped at this
		// geometry, or one that appears in another archetype of the same screen.
		"a leaf the body never named": {"unmounted", 2},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assertCursorWrite(t, base, tc.id, tc.index)
		})
	}
}

func assertCursorWrite(t *testing.T, base State, id screenlayout.ID, index int) {
	before := base.Layout().Cursor(id)
	next := base.WithCursor(id, index)
	if got := next.Layout().Cursor(id); got != index {
		t.Fatalf("cursor of %q is %d; want the seeded %d", id, got, index)
	}
	if got := base.Layout().Cursor(id); got != before {
		t.Fatalf("the seeded-from state observed the write: cursor of %q moved %d -> %d", id, before, got)
	}
	if next.Focus() != base.Focus() || next.Fullscreen() != base.Fullscreen() || len(next.Path()) != len(base.Path()) {
		t.Fatalf("cursor write changed unrelated state for %q", id)
	}
}

// The point of the primitive: seeding a cursor keeps the arrangement metadata
// the last composition recorded, so the next keystroke does not re-render the
// body merely to rediscover a frame that never changed.
func TestWithCursorPreservesTheGridFrame(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	root := Cell(cursorSpec(), countingBody(&calls))
	seeded := NewState().Resync(kit, b, root).WithFocus("body").WithCursor("body", 2)

	if !seeded.grid.fits(seeded, b, root) {
		t.Fatal("the frame Resync recorded did not survive WithCursor")
	}

	calls = 0
	next, handled := seeded.HandleKey(kit, b, "j", root)
	if !handled {
		t.Fatal("j was not handled")
	}
	// Exactly one composition, and the one is not the frame's: screenlayout
	// drops its own measurement on a cursor write, so the keystroke takes one
	// fresh resolve at the recorded box. What the grid frame spares is the
	// SECOND composition — the render that would rediscover the arrangement.
	if calls != 1 {
		t.Fatalf("the keystroke after WithCursor composed the body %d time(s); want 1", calls)
	}
	// And the keystroke moved the cursor the seed had just written, rather than
	// the one the frame was recorded with.
	if got := next.Layout().Cursor("body"); got != 3 {
		t.Fatalf("j settled the cursor at %d; want 3, one past the seeded 2", got)
	}
}

func TestWithCursorPreservesFrameForASecondaryCursor(t *testing.T) {
	kit := testKit()
	b := box(120, 40)
	var calls int
	root := Rows(screenlayout.Spec{ID: "root"},
		Cell(screenlayout.Spec{ID: "primary", MinRows: 3, Scroll: screenlayout.ScrollItems, SelectFirst: true}, countingBody(&calls)),
		Cell(screenlayout.Spec{ID: "secondary", MinRows: 3, Scroll: screenlayout.ScrollItems, SelectFirst: true}, countingBody(&calls)),
	)
	base := NewState().Resync(kit, b, root).WithFocus("primary")
	seeded := base.WithCursor("secondary", 2)

	if !seeded.grid.fits(seeded, b, root) {
		t.Fatal("seeding a secondary cursor discarded the reusable grid frame")
	}
	if got := seeded.Layout().Cursor("secondary"); got != 2 {
		t.Fatalf("secondary cursor = %d, want seeded 2", got)
	}
	calls = 0
	if _, handled := seeded.HandleKey(kit, b, "j", root); !handled {
		t.Fatal("j was not handled by the focused primary cursor")
	}
	if calls == 0 {
		t.Fatal("focused section did not resolve after the secondary cursor seed")
	}
}

// The conservative path stays conservative. WithLayout takes a whole arranger
// state a screen drove itself, and has no way to know what in it moved.
func TestWithLayoutStillResetsTheGridFrame(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	root := Cell(cursorSpec(), countingBody(&calls))
	base := NewState().Resync(kit, b, root).WithFocus("body")

	replaced := base.WithLayout(base.Layout().WithCursor("body", 2))
	if replaced.grid.fits(replaced, b, root) {
		t.Fatal("WithLayout kept the frame memo; it must reset it")
	}

	calls = 0
	if _, handled := replaced.HandleKey(kit, b, "j", root); !handled {
		t.Fatal("j was not handled")
	}
	// Two: the render that rebuilds the discarded frame, then the resolve the
	// keystroke settles in. That second composition is what WithCursor saves.
	if calls != 2 {
		t.Fatalf("the keystroke after WithLayout composed the body %d time(s); want 2", calls)
	}
}

// Preserving the frame across a cursor write does not preserve it across the
// things that genuinely invalidate it. Every witness fits reads still misses.
func TestWithCursorStillMissesWhenFrameInputsChange(t *testing.T) {
	kit := testKit()
	b := box(80, 20)
	calls := 0
	spec := cursorSpec()
	root := Cell(spec, countingBody(&calls))
	seeded := func() State {
		return NewState().Resync(kit, b, root).WithFocus("body").WithCursor("body", 2)
	}

	cases := map[string]struct {
		state State
		box   screenlayout.Box
		root  Node
	}{
		"box":        {state: seeded(), box: box(64, 20), root: root},
		"focus":      {state: seeded().WithFocus("other"), box: b, root: root},
		"tree":       {state: seeded(), box: b, root: Cell(specWithMinRows(spec, 5), countingBody(&calls))},
		"fullscreen": {state: seeded().withFullscreen(true), box: b, root: root},
		"path":       {state: seeded().enter("entered", "body"), box: b, root: root},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.state.grid.fits(tc.state, tc.box, tc.root) {
				t.Fatal("a changed frame input still fit the recorded frame")
			}
			calls = 0
			if _, handled := tc.state.HandleKey(kit, tc.box, "j", tc.root); !handled {
				t.Fatal("j was not handled")
			}
			if calls != 2 {
				t.Fatalf("the keystroke composed the body %d time(s); want 2 — the rebuild and the resolve", calls)
			}
		})
	}
}
