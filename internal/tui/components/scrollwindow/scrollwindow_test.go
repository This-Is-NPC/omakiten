package scrollwindow

import "testing"

// ones returns a heights slice of `n` items each of height 1 — used to
// drive the fixed-height code path of Slice/Follow without copy-pasting
// the boilerplate across every test case.
func ones(n int) []int {
	h := make([]int, n)
	for i := range h {
		h[i] = 1
	}
	return h
}

func TestSliceFixedHeightFitsExactly(t *testing.T) {
	// 5 items of height 1, viewport=5, no scroll → the whole list fits
	// without reserving any indicator row.
	end := Slice(0, ones(5), 5, HintsSplit)
	if end != 5 {
		t.Fatalf("Slice(0, ones(5), 5, Split) = %d, want 5 (everything fits flush)", end)
	}
}

func TestSliceReservesOneRowForBelowHintWhenScrollingFromTop(t *testing.T) {
	// 10 items, viewport=5, scroll=0. Top of list, no above-hint,
	// items remain below so 1 row is reserved → 4 items visible.
	end := Slice(0, ones(10), 5, HintsSplit)
	if end != 4 {
		t.Fatalf("Slice(0, ones(10), 5, Split) = %d, want 4 (reserve 1 for ▼ below)", end)
	}
}

func TestSliceReservesTwoRowsForBothHintsMidScroll(t *testing.T) {
	// 10 items, viewport=5, scroll=3. Items hidden in BOTH directions
	// → 2 rows reserved → 3 items visible.
	end := Slice(3, ones(10), 5, HintsSplit)
	if end != 6 {
		t.Fatalf("Slice(3, ones(10), 5, Split) = %d, want 6 (reserve 1 ▲ + 1 ▼)", end)
	}
}

func TestSliceReservesOneRowForAboveHintAtBottom(t *testing.T) {
	// 10 items, viewport=5, scroll=6. Above hint costs 1, no below
	// (last item visible) → 4 items visible (6,7,8,9).
	end := Slice(6, ones(10), 5, HintsSplit)
	if end != 10 {
		t.Fatalf("Slice(6, ones(10), 5, Split) = %d, want 10 (▲ above only, last item included)", end)
	}
}

func TestSliceVariableHeightsRespectBudget(t *testing.T) {
	// Cards of heights 4, 4, 4, 4, 4 → 20 rows total. Viewport=12,
	// scroll=0. Below-hint fires (items remain), reservation=1 →
	// usable budget for cards = 11. Two cards (4+4=8) fit; third
	// would push to 12 + 1 reserve = 13 > 12.
	heights := []int{4, 4, 4, 4, 4}
	end := Slice(0, heights, 12, HintsSplit)
	if end != 2 {
		t.Fatalf("Slice variable heights = %d, want 2", end)
	}
}

func TestSliceVariableHeightsAtLastItemNoBelowReserve(t *testing.T) {
	// 5 cards of height 4. Viewport=12, scroll=2. Cursor position not
	// the focus here — what matters is the slice math: above-reserve=1,
	// 12-1 = 11 budget for cards. Items 2, 3, 4 are 4+4+4=12; only
	// 2,3 fit. Card 4 has no below-reserve (last item), so the loop
	// considers used+4+1+0 = ?+5; depends on `used` going in.
	heights := []int{4, 4, 4, 4, 4}
	end := Slice(2, heights, 12, HintsSplit)
	// Walk: offset=2, above=1.
	//  end=2: belowReserve=1 (2<4), used+4+1+1=6 ≤ 12, used=4.
	//  end=3: belowReserve=1, used+4+1+1=10 ≤ 12, used=8.
	//  end=4: belowReserve=0, used+4+1+0=13 > 12 → break.
	// end=4. items[2:4] = 2 items.
	if end != 4 {
		t.Fatalf("Slice variable at near-last = %d, want 4", end)
	}
}

func TestSliceCombinedModeReservesOneFooterWhenScrolling(t *testing.T) {
	// 10 items, viewport=5, scroll=3. Combined mode: any scroll = 1
	// row reserved (vs split which would reserve 2). So 4 items fit.
	end := Slice(3, ones(10), 5, HintsCombined)
	if end != 7 {
		t.Fatalf("Slice combined mid-scroll = %d, want 7 (4 items + 1 footer)", end)
	}
}

func TestSliceCombinedModeNoReservationWhenContentFits(t *testing.T) {
	// 5 items, viewport=5, scroll=0 → content fits, 0 reservation.
	end := Slice(0, ones(5), 5, HintsCombined)
	if end != 5 {
		t.Fatalf("Slice combined fits flush = %d, want 5", end)
	}
}

func TestSliceNoneModeNeverReserves(t *testing.T) {
	// HintsNone: caller handles indicator chrome outside the slice.
	// 10 items, viewport=5, scroll=3 → 5 items visible regardless of
	// scroll position.
	end := Slice(3, ones(10), 5, HintsNone)
	if end != 8 {
		t.Fatalf("Slice none = %d, want 8 (5 items, no reservation)", end)
	}
}

func TestSliceEmptyInputs(t *testing.T) {
	if got := Slice(0, []int{}, 10, HintsSplit); got != 0 {
		t.Fatalf("Slice empty heights = %d, want 0", got)
	}
	if got := Slice(0, ones(5), 0, HintsSplit); got != 5 {
		t.Fatalf("Slice viewport=0 = %d, want full len 5 (caller handles overflow)", got)
	}
	if got := Slice(0, ones(5), -3, HintsSplit); got != 5 {
		t.Fatalf("Slice negative viewport = %d, want full len 5", got)
	}
}

func TestSliceNeverReturnsEmptyWindowOnTinyViewport(t *testing.T) {
	// Even when viewport is too small to fit a single item with its
	// reservations, the helper returns at least offset+1 so the caller
	// always has something to render. Better one item bleeding off-
	// screen than zero items inside an empty box.
	end := Slice(0, []int{8}, 3, HintsSplit)
	if end != 1 {
		t.Fatalf("Slice tiny viewport = %d, want 1 (force at least one item)", end)
	}
}

func TestSliceClampsOffsetIntoRange(t *testing.T) {
	// Out-of-range offset is silently clamped — callers don't have to
	// bounds-check before calling. Negative → 0; past-end → last item.
	if got := Slice(-2, ones(5), 3, HintsSplit); got <= 0 {
		t.Fatalf("Slice negative offset returned %d, want >0", got)
	}
	if got := Slice(99, ones(5), 3, HintsSplit); got != 5 {
		t.Fatalf("Slice over-end offset returned %d, want 5", got)
	}
}

func TestFollowKeepsCursorOnScreen(t *testing.T) {
	// 10 items height 1, viewport=5, cursor=8, scroll starts at 0.
	// Helper advances scroll until cursor 8 fits with reserved hints.
	off := Follow(0, 8, ones(10), 5, HintsSplit)
	if off > 8 {
		t.Fatalf("Follow drove offset past cursor: %d", off)
	}
	// And the resulting slice must contain the cursor.
	end := Slice(off, ones(10), 5, HintsSplit)
	if 8 < off || 8 >= end {
		t.Fatalf("Follow+Slice did not keep cursor on screen: offset=%d end=%d", off, end)
	}
}

func TestFollowVariableHeightCardsAtEnd(t *testing.T) {
	// 5 cards of height 4 (total 20 rows). Viewport=12. Cursor=4.
	// Above-reserve=1. To fit cursor=4 (last) with no below-reserve,
	// need sum(heights[off..4]) + 1 ≤ 12 → sum ≤ 11 → at most 2
	// cards of height 4 (8 ≤ 11). offset must be 3.
	heights := []int{4, 4, 4, 4, 4}
	off := Follow(0, 4, heights, 12, HintsSplit)
	if off != 3 {
		t.Fatalf("Follow on var-height at end = %d, want 3", off)
	}
}

func TestFollowClampsOutOfRange(t *testing.T) {
	// Cursor past end is clamped to last index; offset past end too.
	if got := Follow(0, 99, ones(5), 5, HintsSplit); got < 0 || got >= 5 {
		t.Fatalf("Follow out-of-range cursor = %d, want valid index", got)
	}
}

func TestAboveHintRowsByMode(t *testing.T) {
	if got := AboveHintRows(HintsSplit); got != 1 {
		t.Fatalf("AboveHintRows(HintsSplit) = %d, want 1", got)
	}
	if got := AboveHintRows(HintsCombined); got != 1 {
		t.Fatalf("AboveHintRows(HintsCombined) = %d, want 1", got)
	}
	if got := AboveHintRows(HintsNone); got != 0 {
		t.Fatalf("AboveHintRows(HintsNone) = %d, want 0", got)
	}
}

func TestResyncEmptyHeightsResetsCursorAndScroll(t *testing.T) {
	// Empty list: Resync must report no-selection (-1) and zero scroll
	// regardless of the caller's prior state. Callers depend on this so
	// a list that drains (last child removed) does not keep a ghost
	// cursor / non-zero scroll that the next render would mis-clamp.
	cursor, scroll := Resync(3, 5, nil, 10)
	if cursor != -1 || scroll != 0 {
		t.Fatalf("Resync empty = (%d, %d), want (-1, 0)", cursor, scroll)
	}
}

func TestResyncClampsCursorPastEnd(t *testing.T) {
	// Out-of-range cursor (e.g. items shrank from 10 to 5) clamps to
	// last item; scroll follows so the clamped cursor stays visible.
	cursor, scroll := Resync(99, 0, ones(5), 5)
	if cursor != 4 {
		t.Fatalf("Resync past-end cursor = %d, want 4", cursor)
	}
	if scroll < 0 || scroll > cursor {
		t.Fatalf("Resync past-end scroll = %d, want in [0,4]", scroll)
	}
}

func TestResyncPreservesNoSelectionSentinel(t *testing.T) {
	// Cursor=-1 means "no selection" — callers (cardlist.Model post
	// applyTaskFocus) rely on Resync NOT promoting -1 to 0 silently.
	// Scroll is reset to 0 so the first j/k lands on the first card
	// with the panel at the top.
	cursor, scroll := Resync(-1, 7, ones(10), 5)
	if cursor != -1 || scroll != 0 {
		t.Fatalf("Resync no-selection = (%d, %d), want (-1, 0)", cursor, scroll)
	}
}

func TestResyncCursorAtEndFollowsScroll(t *testing.T) {
	// Cursor at the last item of a list bigger than viewport must
	// produce a scroll that places the cursor inside the visible slice
	// (with HintsSplit reservation accounted for). End=Slice(off,...)
	// must be > cursor.
	heights := []int{4, 4, 4, 4, 4}
	cursor, scroll := Resync(4, 0, heights, 12)
	if cursor != 4 {
		t.Fatalf("Resync cursor = %d, want 4", cursor)
	}
	end := Slice(scroll, heights, 12, HintsSplit)
	if cursor < scroll || cursor >= end {
		t.Fatalf("Resync left cursor invisible: cursor=%d scroll=%d end=%d", cursor, scroll, end)
	}
}

func TestAboveBelowHelpers(t *testing.T) {
	if got := Above(7); got != 7 {
		t.Fatalf("Above(7) = %d, want 7", got)
	}
	if got := Above(-3); got != 0 {
		t.Fatalf("Above(-3) = %d, want 0", got)
	}
	if got := Below(8, 10); got != 2 {
		t.Fatalf("Below(8, 10) = %d, want 2", got)
	}
	if got := Below(10, 10); got != 0 {
		t.Fatalf("Below(10, 10) = %d, want 0", got)
	}
}
