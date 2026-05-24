// Package scrollwindow is the canonical scroll math for every list /
// grid / line viewport in the TUI. The shape is the same everywhere:
// given a list of items with terminal-row heights, an offset, and a
// viewport row budget, decide which items fit inside the viewport
// while reserving space for "▲ above" / "▼ below" indicators.
//
// Lives in its own package (not the parent `tui` package) so the
// detail-screen viewport sub-component can import it without creating
// an import cycle. Pure math — no styling, no rendering, no tea.Cmd.
//
// Callers compose it with their own assembly:
//   - Split-hint surfaces (board lanes, entity grids, home projects,
//     activity feed, tables/logs/graph, pickers) call Slice with
//     HintsSplit and prepend "▲ N above" + append "▼ N below" rows.
//   - Combined-footer surfaces (detail screens — task view, comment
//     view, help, entity view) call Slice with HintsCombined and emit
//     a single "▲ X above · ▼ Y below · j/k pgup/pgdn g/G" footer.
//   - Surfaces that own their own indicator chrome outside the slice
//     budget pass HintsNone.
//
// The same helper services fixed-height items (single-line table rows,
// log entries, picker rows) by passing heights = []int{1, 1, ...} —
// fixed-height is just a special case of variable-height where every
// item happens to take one terminal row. Resisting that abstraction
// was the cause of the prior copy-paste regressions.
package scrollwindow

// HintMode controls how Slice and Follow reserve viewport rows for
// the scroll indicator(s) the caller plans to render inside the
// viewport budget.
type HintMode int

const (
	// HintsSplit reserves up to two rows: one for "▲ N above" when
	// offset > 0 and one for "▼ N below" when items remain past end.
	// The reservation is dynamic — when scrolled to the very top no
	// above-row is reserved; when scrolled to the very bottom no
	// below-row is reserved; when both directions have hidden items
	// both rows cost.
	HintsSplit HintMode = iota
	// HintsCombined reserves at most one row for a combined footer
	// such as "▲ X above · ▼ Y below". The single row is reserved
	// whenever EITHER above or below would fire; otherwise nothing.
	HintsCombined
	// HintsNone reserves no rows inside the viewport — the caller
	// owns indicator chrome outside the slice budget.
	HintsNone
)

// Slice returns the end index of the visible window [offset, end) such
// that the rendered slice plus dynamically-reserved indicator rows
// never exceeds `viewport` terminal rows. heights[i] is item i's
// terminal-row count; offset is the first visible item.
//
// Returns at least offset+1 when len(heights) > 0 so callers always
// render something rather than collapsing to an empty viewport — this
// matches the existing "render at least one card" rule the board and
// entity grid already enforced.
//
// When viewport is non-positive or the entire content fits without
// reservation, returns len(heights) and the caller can render the
// whole list flush.
func Slice(offset int, heights []int, viewport int, mode HintMode) int {
	if len(heights) == 0 {
		return 0
	}
	if viewport <= 0 {
		return len(heights)
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(heights) {
		offset = len(heights) - 1
	}
	used := 0
	end := offset
	for end < len(heights) {
		reserve := hintReserve(offset, end, len(heights), mode)
		if used+heights[end]+reserve > viewport {
			break
		}
		used += heights[end]
		end++
	}
	if end == offset {
		end = offset + 1
	}
	return end
}

// Follow advances offset until the cursor item fits inside the
// viewport with the same indicator-reservation contract as Slice.
// Used by per-frame sync routines that want to keep the cursor
// on-screen as the user navigates.
//
// Clamped so jump-to-end sentinels and out-of-range cursors are
// silently corrected — callers don't need to bounds-check first.
func Follow(offset, cursor int, heights []int, viewport int, mode HintMode) int {
	if viewport <= 0 || len(heights) == 0 {
		return offset
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(heights) {
		cursor = len(heights) - 1
	}
	if offset > cursor {
		offset = cursor
	}
	if offset < 0 {
		offset = 0
	}
	for offset < cursor {
		used := 0
		fits := true
		for i := offset; i <= cursor; i++ {
			reserve := hintReserve(offset, i, len(heights), mode)
			if used+heights[i]+reserve > viewport {
				fits = false
				break
			}
			used += heights[i]
		}
		if fits {
			break
		}
		offset++
	}
	if offset > len(heights)-1 {
		offset = len(heights) - 1
	}
	return offset
}

// Resync is the cardlist / linelist component glue: given the current
// (cursor, scroll, heights, viewport), return a new (cursor, scroll)
// pair guaranteed to keep cursor visible inside the viewport with the
// HintsSplit reservation contract every component-based surface uses.
//
// Cursor is clamped to [-1, len(heights)-1]; -1 propagates the "no
// selection" sentinel callers rely on (empty list, freshly opened
// view). Scroll is clamped via Follow so the cursor item fits.
//
// Centralising the (clamp cursor, then follow scroll) sequence in one
// helper closes the bug class that motivated the cardlist/linelist
// refactor: every component mutator (MoveCursor, WithItems,
// WithViewport) routes through Resync, so the resync contract is one
// implementation deep instead of being re-invented per surface.
func Resync(cursor, scroll int, heights []int, viewport int) (newCursor, newScroll int) {
	if len(heights) == 0 {
		return -1, 0
	}
	if cursor < -1 {
		cursor = -1
	}
	if cursor >= len(heights) {
		cursor = len(heights) - 1
	}
	if cursor == -1 {
		// No selection — keep scroll at top so the first card stays in
		// view when the cursor lands on it via the first j/k.
		return -1, 0
	}
	return cursor, Follow(scroll, cursor, heights, viewport, HintsSplit)
}

// AboveHintRows is the upper bound of terminal rows the mode can spend
// on the "▲ N above" indicator inside the viewport once offset > 0.
// Callers computing max-scroll bounds use this to add back the row the
// renderer will steal at the bottom of scroll — without it the last
// content line lands behind the "▼ N below" hint and stays unreachable.
//
// Returned as a static upper bound (not the dynamic per-(offset,end,total)
// reservation hintReserve computes) so callers can pre-size without
// knowing the live scroll state.
func AboveHintRows(mode HintMode) int {
	switch mode {
	case HintsSplit, HintsCombined:
		return 1
	}
	return 0
}

// Above reports the item count hidden above offset. Convenience for
// callers building "▲ N above" hint strings without re-deriving the
// number from the slice they already have.
func Above(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// Below reports the item count hidden past end given the total length.
// Companion to Above; same purpose, same trivial math kept in one
// place so future callers don't accidentally invert the subtraction.
func Below(end, total int) int {
	if end >= total {
		return 0
	}
	return total - end
}

// hintReserve is the rows the caller will spend on indicator chrome
// inside the viewport for the given (offset, current-end, total)
// triplet and HintMode. Encapsulated so Slice and Follow agree on the
// reservation contract — diverging the two would silently break the
// invariant "rendered ≤ viewport" again.
func hintReserve(offset, end, total int, mode HintMode) int {
	switch mode {
	case HintsSplit:
		r := 0
		if offset > 0 {
			r++
		}
		if end < total-1 {
			r++
		}
		return r
	case HintsCombined:
		if offset > 0 || end < total-1 {
			return 1
		}
		return 0
	}
	return 0
}
