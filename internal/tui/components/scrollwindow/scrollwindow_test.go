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
	// Cursor=-1 means "no selection" — callers (list.Cards post
	// focus transitions and linelist.Model after ScrollBy rely on Resync
	// NOT promoting -1 to 0 silently. Scroll is preserved within
	// bounds so prior body-scroll work survives the sentinel.
	cursor, scroll := Resync(-1, 7, ones(10), 5)
	if cursor != -1 {
		t.Fatalf("Resync no-selection cursor = %d, want -1", cursor)
	}
	// 10 items - 5 viewport + 1 above-hint = 6 max scroll. 7 clamps to 6.
	if scroll != 6 {
		t.Fatalf("Resync no-selection scroll = %d, want 6 (preserved + clamped)", scroll)
	}
}

func TestResyncNoSelectionClampsNegativeScrollToZero(t *testing.T) {
	cursor, scroll := Resync(-1, -3, ones(10), 5)
	if cursor != -1 || scroll != 0 {
		t.Fatalf("Resync neg scroll = (%d, %d), want (-1, 0)", cursor, scroll)
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

func TestUnitHeightsIsOnesOrNil(t *testing.T) {
	if got := UnitHeights(0); got != nil {
		t.Fatalf("UnitHeights(0) = %v, want nil", got)
	}
	if got := UnitHeights(-2); got != nil {
		t.Fatalf("UnitHeights(-2) = %v, want nil", got)
	}
	got := UnitHeights(3)
	if len(got) != 3 {
		t.Fatalf("UnitHeights(3) length = %d, want 3", len(got))
	}
	for i, h := range got {
		if h != 1 {
			t.Fatalf("UnitHeights(3)[%d] = %d, want 1", i, h)
		}
	}
}

// TestMaxOffsetRendersTheFinalItem is the contract MaxOffset exists for: at
// the returned offset the renderer must still paint the LAST item, and one
// row further must be wasted space rather than a requirement. The naive
// `total - viewport` fails the first half — that is the picker defect.
func TestMaxOffsetRendersTheFinalItem(t *testing.T) {
	for _, tc := range []struct{ total, viewport int }{
		{10, 3}, {10, 4}, {36, 7}, {36, 12}, {24, 5}, {9, 8},
	} {
		bound := MaxOffset(tc.total, tc.viewport, HintsSplit)
		heights := UnitHeights(tc.total)
		if end := Slice(bound, heights, tc.viewport, HintsSplit); end != tc.total {
			t.Errorf("MaxOffset(%d, %d) = %d, but Slice stops at %d — the last item is stranded",
				tc.total, tc.viewport, bound, end)
		}
		if naive := tc.total - tc.viewport; naive > 0 {
			if end := Slice(naive, heights, tc.viewport, HintsSplit); end == tc.total && naive != bound {
				t.Errorf("total-viewport (%d) already reaches the end for total=%d viewport=%d; the case proves nothing",
					naive, tc.total, tc.viewport)
			}
		}
	}
}

func TestMaxOffsetIsZeroWhenNothingScrolls(t *testing.T) {
	for _, tc := range []struct {
		total, viewport int
		mode            HintMode
	}{
		{10, 0, HintsSplit}, {10, -1, HintsSplit}, {0, 5, HintsSplit}, {3, 10, HintsSplit}, {5, 5, HintsNone},
	} {
		if got := MaxOffset(tc.total, tc.viewport, tc.mode); got != 0 {
			t.Errorf("MaxOffset(%d, %d, %v) = %d, want 0", tc.total, tc.viewport, tc.mode, got)
		}
	}
}

// --- partial-item edges ----------------------------------------------------
//
// The rows Slice cannot spend. Slice closes its window on an ITEM boundary, so
// for a list of multi-line items the residue between the last whole item and
// the viewport is up to (tallest item - 1) blank rows — under a list that is
// simultaneously hiding content. PartialRows is what the caller fills them
// with, and the property every one of these tests is really about is that
// whole items + hints + previews == viewport, exactly.

// spend is the rows a window of whole items plus its reserved hints occupies,
// derived here INDEPENDENTLY of PartialRows so the equality below is a check
// rather than a restatement.
func spend(offset, end int, heights []int, mode HintMode) int {
	used := 0
	if offset > 0 && AboveHintRows(mode) > 0 {
		used += AboveHintRows(mode)
	}
	if end < len(heights) && mode == HintsSplit {
		used++
	}
	for _, h := range heights[offset:end] {
		used += h
	}
	return used
}

func TestPartialRowsSpendsWhateverTheWholeItemWindowLeftOver(t *testing.T) {
	// The sweep the pilot ran, at the level the arithmetic lives: for every
	// item height and every viewport, a window that is hiding content must
	// leave nothing blank.
	for height := 2; height <= 9; height++ {
		assertPartialHeight(t, height)
	}
}

func assertPartialHeight(t *testing.T, height int) {
	heights := make([]int, 40)
	for i := range heights {
		heights[i] = height
	}
	for viewport := height + 2; viewport <= 50; viewport++ {
		for _, offset := range []int{0, 1, 7} {
			assertPartialWindow(t, height, viewport, offset, heights)
		}
	}
}

func assertPartialWindow(t *testing.T, height, viewport, offset int, heights []int) {
	end := Slice(offset, heights, viewport, HintsSplit)
	if offset == 0 && end == len(heights) {
		return
	}
	leading, trailing := PartialRows(offset, end, heights, viewport, HintsSplit)
	got := spend(offset, end, heights, HintsSplit) + leading + trailing
	if got != viewport {
		t.Fatalf("height=%d viewport=%d offset=%d: window spends %d rows (whole %d + lead %d + trail %d), want the viewport exactly", height, viewport, offset, got, spend(offset, end, heights, HintsSplit), leading, trailing)
	}
}

func TestPartialRowsFavoursTheTrailingEdgeOnAnOddSplit(t *testing.T) {
	// Both edges have an item outside them, and the surface is being scrolled
	// downward — so the odd row goes to the direction the user is heading.
	heights := []int{4, 4, 4, 4, 4, 4}
	// offset 1, viewport 12: ▲ + ▼ = 2 rows, two whole items = 8, 2 left over.
	leading, trailing := PartialRows(1, Slice(1, heights, 12, HintsSplit), heights, 12, HintsSplit)
	if leading != 1 || trailing != 1 {
		t.Fatalf("an even leftover split %d/%d, want 1/1", leading, trailing)
	}
	// viewport 13 leaves 3, and the extra row goes trailing.
	leading, trailing = PartialRows(1, Slice(1, heights, 13, HintsSplit), heights, 13, HintsSplit)
	if leading != 1 || trailing != 2 {
		t.Fatalf("an odd leftover split %d/%d, want 1 leading and 2 trailing", leading, trailing)
	}
}

func TestPartialRowsGivesEverythingToTheOnlyEdgeThatHasAnItem(t *testing.T) {
	heights := []int{5, 5, 5, 5}
	// At the top: nothing above, so the whole leftover previews the next item.
	end := Slice(0, heights, 9, HintsSplit)
	leading, trailing := PartialRows(0, end, heights, 9, HintsSplit)
	if leading != 0 || trailing != 3 {
		t.Fatalf("at the top the split was %d/%d, want 0 leading and 3 trailing", leading, trailing)
	}
	// At the bottom: nothing below, so the leftover previews the item above —
	// capped at that item's own height, which is all it has to show.
	leading, trailing = PartialRows(3, 4, heights, 9, HintsSplit)
	if leading != 3 || trailing != 0 {
		t.Fatalf("at the bottom the split was %d/%d, want 3 leading and 0 trailing", leading, trailing)
	}
	leading, _ = PartialRows(3, 4, heights, 20, HintsSplit)
	if leading != 5 {
		t.Fatalf("leading preview = %d rows, want the whole %d-row item above and no more", leading, heights[2])
	}
}

func TestPartialRowsIsZeroWhenTheWindowAlreadySpentTheViewport(t *testing.T) {
	heights := []int{3, 3, 3, 3}
	if l, tr := PartialRows(0, 4, heights, 12, HintsNone); l != 0 || tr != 0 {
		t.Fatalf("a flush window asked for %d/%d preview rows, want none", l, tr)
	}
	// Slice's "render at least one item" escape: the item does not fit, so
	// there is nothing left over to preview with.
	if l, tr := PartialRows(0, 1, []int{9}, 3, HintsNone); l != 0 || tr != 0 {
		t.Fatalf("an over-tall item asked for %d/%d preview rows, want none", l, tr)
	}
}

func TestPartialRowsRefusesAWindowItDidNotSlice(t *testing.T) {
	heights := ones(4)
	for _, tc := range []struct {
		name                          string
		offset, end, viewport, length int
	}{
		{"empty list", 0, 1, 5, 0},
		{"no viewport", 0, 1, 0, 4},
		{"negative offset", -1, 1, 5, 4},
		{"empty window", 2, 2, 5, 4},
		{"end past the list", 0, 9, 5, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if l, tr := PartialRows(tc.offset, tc.end, heights[:tc.length], tc.viewport, HintsSplit); l != 0 || tr != 0 {
				t.Fatalf("PartialRows returned %d/%d for %s, want none", l, tr, tc.name)
			}
		})
	}
}

func TestPartialRowsAccountsForTheCombinedFooterReservation(t *testing.T) {
	heights := []int{4, 4, 4, 4}
	// HintsCombined spends one row for both directions, not two, so it has one
	// more row to preview with than HintsSplit at the same geometry.
	splitLead, splitTrail := PartialRows(1, Slice(1, heights, 11, HintsSplit), heights, 11, HintsSplit)
	combLead, combTrail := PartialRows(1, Slice(1, heights, 11, HintsCombined), heights, 11, HintsCombined)
	split, combined := splitLead+splitTrail, combLead+combTrail
	if combined != split+1 {
		t.Fatalf("combined-footer previews %d rows and split-hint %d; the combined footer costs one row less, so it has exactly one more to preview with", combined, split)
	}
}

func TestHeadAndTailRowsTakeTheEdgeTheirNameSays(t *testing.T) {
	content := "r0\nr1\nr2\nr3"
	if got := HeadRows(content, 2); got != "r0\nr1" {
		t.Fatalf("HeadRows = %q, want the first two rows", got)
	}
	if got := TailRows(content, 2); got != "r2\nr3" {
		t.Fatalf("TailRows = %q, want the last two rows", got)
	}
	if got := HeadRows(content, 9); got != content {
		t.Fatalf("HeadRows past the end = %q, want the whole content", got)
	}
	if got := TailRows(content, 9); got != content {
		t.Fatalf("TailRows past the end = %q, want the whole content", got)
	}
	if got := HeadRows(content, 0); got != "" {
		t.Fatalf("HeadRows(0) = %q, want nothing", got)
	}
	if got := TailRows(content, -1); got != "" {
		t.Fatalf("TailRows(-1) = %q, want nothing", got)
	}
}
