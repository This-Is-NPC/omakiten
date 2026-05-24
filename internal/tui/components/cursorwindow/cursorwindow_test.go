package cursorwindow

import (
	"math/rand"
	"testing"
)

// assertInvariants is the contract every mutator must preserve:
// cursor lives inside [0, max(0, itemCount-1)] AND inside the visible
// range. Centralised so the property test does not re-litigate it
// per case — a regression in any mutator surfaces with the same
// error message regardless of which keypress triggered it.
func assertInvariants(t *testing.T, m Model, step int) {
	t.Helper()
	c := m.Cursor()
	if m.ItemCount() == 0 {
		// Empty: cursor and scroll both pinned at 0.
		if c != 0 {
			t.Fatalf("step %d: empty list cursor = %d, want 0", step, c)
		}
		if m.Scroll() != 0 {
			t.Fatalf("step %d: empty list scroll = %d, want 0", step, m.Scroll())
		}
		return
	}
	if c < 0 || c > m.ItemCount()-1 {
		t.Fatalf("step %d: cursor %d outside [0, %d]", step, c, m.ItemCount()-1)
	}
	start, end := m.VisibleRange()
	if start > end {
		t.Fatalf("step %d: VisibleRange start=%d > end=%d", step, start, end)
	}
	if start < 0 {
		t.Fatalf("step %d: VisibleRange start=%d < 0", step, start)
	}
	if end > m.ItemCount() {
		t.Fatalf("step %d: VisibleRange end=%d > itemCount=%d", step, end, m.ItemCount())
	}
	// Cursor must be inside [start, end) — the resync contract.
	if c < start || c >= end {
		t.Fatalf("step %d: cursor %d outside visible [%d, %d) with viewport=%d itemCount=%d scroll=%d",
			step, c, start, end, m.ViewportRows(), m.ItemCount(), m.Scroll())
	}
}

// TestCursorWindowPropertyInvariants is the headline property test:
// generate random sequences of every mutator and assert the cursor
// stays inside both [0, itemCount-1] AND the visible range after each
// step. Closes the bug class that motivated this component (cursor
// stranded out of viewport / out of bounds after delete or resize).
func TestCursorWindowPropertyInvariants(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	const iterations = 2000
	// Start with a viewport in [3, 20] and itemCount in [0, 50] so the
	// random walk hits every boundary case: empty list, single item,
	// viewport larger than list, viewport smaller than list, cursor
	// at index 0, cursor at index itemCount-1.
	m := New(3 + r.Intn(18))
	m = m.WithItemCount(r.Intn(51))
	assertInvariants(t, m, 0)
	for step := 1; step <= iterations; step++ {
		switch r.Intn(8) {
		case 0:
			m = m.MoveCursor(r.Intn(7) - 3)
		case 1:
			m = m.JumpFirst()
		case 2:
			m = m.JumpLast()
		case 3:
			m = m.PageDown()
		case 4:
			m = m.PageUp()
		case 5:
			m = m.WithItemCount(r.Intn(51))
		case 6:
			m = m.WithViewport(3 + r.Intn(18))
		case 7:
			m = m.SetCursor(r.Intn(60) - 5) // sometimes out of range
		}
		assertInvariants(t, m, step)
	}
}

// TestWithItemCountClampsCursorAfterShrink documents the delete-the-
// active-item flow: a 5-item list with cursor at idx 4, after
// WithItemCount(4), lands the cursor on idx 3. The shrink-past-cursor
// case is the most common stranded-cursor regression in the existing
// codebase — pin it.
func TestWithItemCountClampsCursorAfterShrink(t *testing.T) {
	m := New(10).WithItemCount(5).MoveCursor(4)
	if m.Cursor() != 4 {
		t.Fatalf("setup: cursor = %d, want 4", m.Cursor())
	}
	m = m.WithItemCount(4)
	if m.Cursor() != 3 {
		t.Fatalf("after shrink: cursor = %d, want 3 (clamp to new last)", m.Cursor())
	}
}

// TestWithItemCountZeroResetsToZero pins the empty-list path: when
// the data drops to zero, cursor and scroll both pin at 0 (not the
// -1 sentinel cardlist uses) so the parent renderer's empty-state
// branch fires off itemCount == 0 directly.
func TestWithItemCountZeroResetsToZero(t *testing.T) {
	m := New(5).WithItemCount(3).MoveCursor(2)
	m = m.WithItemCount(0)
	if m.Cursor() != 0 || m.Scroll() != 0 {
		t.Fatalf("empty: cursor=%d scroll=%d, want 0/0", m.Cursor(), m.Scroll())
	}
}

// TestWithViewportShrinkAdvancesScroll documents the "terminal got
// smaller, cursor must remain visible" path: a 20-item list with
// cursor at idx 19 and viewport 10, after WithViewport(3), scroll
// advances so cursor 19 sits inside the new tiny window.
func TestWithViewportShrinkAdvancesScroll(t *testing.T) {
	m := New(10).WithItemCount(20).JumpLast()
	scrollBefore := m.Scroll()
	m = m.WithViewport(3)
	start, end := m.VisibleRange()
	if m.Cursor() < start || m.Cursor() >= end {
		t.Fatalf("shrink viewport: cursor %d outside [%d, %d); scroll before=%d after=%d",
			m.Cursor(), start, end, scrollBefore, m.Scroll())
	}
}

// TestMoveCursorClampsAtEnd documents the explicit boundary —
// MoveCursor(+99) with a 5-item list lands the cursor at idx 4, not
// past it. The component never wraps (mirrors cardlist's clamp shape;
// surfaces that want wrap behaviour build it on top).
func TestMoveCursorClampsAtEnd(t *testing.T) {
	m := New(5).WithItemCount(5).MoveCursor(99)
	if m.Cursor() != 4 {
		t.Fatalf("clamp at end: cursor = %d, want 4", m.Cursor())
	}
	m = m.MoveCursor(-99)
	if m.Cursor() != 0 {
		t.Fatalf("clamp at start: cursor = %d, want 0", m.Cursor())
	}
}

// TestPageStepFloor pins the "tiny viewport still navigates" path:
// PageDown with viewport=1 must still move the cursor (floor of 2)
// so the user is not stuck pressing PageDown to no effect on a
// pathologically small terminal.
func TestPageStepFloor(t *testing.T) {
	m := New(1).WithItemCount(10)
	c0 := m.Cursor()
	m = m.PageDown()
	if m.Cursor() == c0 {
		t.Fatalf("PageDown on viewport=1 did not move cursor (still %d)", c0)
	}
}

// TestVisibleRangeRespectsBelowHintReservation pins that the
// HintsSplit reservation is wired through: a 10-item list with
// viewport 5, scroll 0 reserves 1 row for the "▼ N below" indicator,
// so the visible end is 4 (not 5).
func TestVisibleRangeRespectsBelowHintReservation(t *testing.T) {
	m := New(5).WithItemCount(10)
	start, end := m.VisibleRange()
	if start != 0 || end != 4 {
		t.Fatalf("VisibleRange = [%d, %d), want [0, 4) (reserve 1 row for ▼)", start, end)
	}
}

// TestZeroValueModelIsSafe pins the zero-value contract — a fresh
// var m Model declaration is safe to call mutators on (cursor=0,
// itemCount=0, viewport=0). The empty-list branch handles it.
func TestZeroValueModelIsSafe(t *testing.T) {
	var m Model
	m = m.MoveCursor(5)
	m = m.JumpLast()
	m = m.PageDown()
	m = m.WithItemCount(0)
	if m.Cursor() != 0 || m.Scroll() != 0 {
		t.Fatalf("zero value: cursor=%d scroll=%d after empty mutators, want 0/0",
			m.Cursor(), m.Scroll())
	}
}
