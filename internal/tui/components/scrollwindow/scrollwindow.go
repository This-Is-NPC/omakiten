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

import "strings"

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
		// No selection — preserve the caller's scroll within bounds
		// so prior body-scroll work (linelist.ScrollBy) survives
		// the cursor sentinel. Clamp against the largest offset that
		// still renders the last line inside the viewport.
		if scroll < 0 {
			scroll = 0
		}
		if bound := MaxOffset(len(heights), viewport, HintsSplit); scroll > bound {
			scroll = bound
		}
		return -1, scroll
	}
	scroll = Follow(scroll, cursor, heights, viewport, HintsSplit)
	// Follow only ever advances. When a widen (or a shrink of the item
	// list) makes every item fit from offset 0, a stale offset leaves
	// blank rows under a cursor that is already on-screen — collapse it.
	// MaxOffset(len, viewport) is unit-height only, so the fit check is
	// the height-sum form that variable-height surfaces need too.
	if end := Slice(0, heights, viewport, HintsSplit); end >= len(heights) {
		scroll = 0
	}
	return cursor, scroll
}

// PartialRows is the partial-item budget at each edge of the window
// [offset, end) that Slice just chose: how many terminal rows of the
// item immediately BEFORE offset, and of the item AT end, the caller
// should preview so the window spends `viewport` exactly.
//
// Slice closes its window on an ITEM boundary, so whatever rows are
// left between the last whole item and the viewport go unpainted
// unless the caller fills them. For a list of multi-line items that
// residue is not a rounding error — it is up to (tallest item - 1)
// blank terminal rows sitting under a list that is simultaneously
// hiding content, which is the same harm as overdrawing arriving from
// the other direction.
//
// The rows are split between the two edges, the trailing edge winning
// the odd row so the surface emphasises the direction the user is
// scrolling toward. An edge with nothing outside it (offset == 0, or
// end == len) contributes no cap and yields its share to the other.
//
// `end` must be the value Slice returned for the same
// (offset, heights, viewport, mode); the reservation arithmetic here
// mirrors the reservation Slice made.
//
// Partial rows are a VISUAL preview only. Cursor and offset still move
// in whole items, and the "▲ N above" / "▼ N below" counts still count
// a partially previewed item on its own side — the caller has not seen
// all of it.
//
// Extracted from cardlist.View, which has rendered partial cards at its
// edges since the component landed; screenlayout needs the identical
// arithmetic, and two derivations of it is the defect class both
// packages exist to remove.
func PartialRows(offset, end int, heights []int, viewport int, mode HintMode) (leading, trailing int) {
	total := len(heights)
	if total == 0 || viewport <= 0 || offset < 0 || end <= offset || end > total {
		return 0, 0
	}
	// Everything the whole-item window already spends: the indicator rows
	// Slice reserved for this (offset, end), then every visible item.
	remaining := viewport - hintReserve(offset, end-1, total, mode)
	for _, h := range heights[offset:end] {
		remaining -= h
	}
	if remaining <= 0 {
		return 0, 0
	}
	leadCap, trailCap := 0, 0
	if offset > 0 {
		leadCap = heights[offset-1]
	}
	if end < total {
		trailCap = heights[end]
	}
	if leadCap > 0 && trailCap > 0 {
		trailing = (remaining + 1) / 2
	} else {
		trailing = remaining
	}
	// An edge can never preview more rows than the item outside it has, so
	// the split above is a preference and these clamps are the partition.
	trailing = min(trailing, trailCap)
	leading = min(remaining-trailing, leadCap)
	return leading, trailing
}

// HeadRows returns the first `rows` terminal rows of pre-rendered
// content, or all of it when it is shorter. Companion to TailRows for
// the trailing partial preview: the surface shows the TOP of the
// next-below item so the user sees what is coming without scrolling.
func HeadRows(content string, rows int) string {
	if rows <= 0 {
		return ""
	}
	lines := splitRows(content)
	if len(lines) <= rows {
		return content
	}
	return strings.Join(lines[:rows], "\n")
}

// TailRows returns the last `rows` terminal rows of pre-rendered
// content, or all of it when it is shorter. Used for the leading
// partial preview: the surface shows the BOTTOM of the item just above
// the offset so scrolling keeps its continuity.
func TailRows(content string, rows int) string {
	if rows <= 0 {
		return ""
	}
	lines := splitRows(content)
	if len(lines) <= rows {
		return content
	}
	return strings.Join(lines[len(lines)-rows:], "\n")
}

func splitRows(content string) []string { return strings.Split(content, "\n") }

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

// UnitHeights is the heights slice for a fixed-height (single-line) list
// of n items. Slice, Follow and Resync are variable-height by contract;
// fixed-height surfaces are the special case where every item is one
// terminal row, and they all need the same throwaway []int{1, 1, ...}.
// Kept here so screenkit, linelist and picker share one spelling instead
// of each carrying a private `ones` helper.
func UnitHeights(n int) []int {
	if n <= 0 {
		return nil
	}
	heights := make([]int, n)
	for i := range heights {
		heights[i] = 1
	}
	return heights
}

// MaxOffset is the largest scroll offset that still renders the final
// item of a `total`-item unit-height list inside `viewport` terminal rows
// under mode's hint reservation.
//
// Every scrolling surface needs this bound and the naive form —
// `total - viewport` — is wrong for every mode that reserves rows: it
// assumes the renderer paints a full viewport of items, when Slice
// spends AboveHintRows(mode) of them on indicator chrome. The last items
// then sit permanently behind the "▼ N below" hint. Centralised here so
// linelist, Resync and picker share one ceiling instead of each
// re-deriving a subtly different one — three independent derivations is
// exactly how the picker ended up two rows short.
//
// Returns 0 for a non-positive viewport or a list that already fits, so
// callers can use it as an unconditional upper clamp.
func MaxOffset(total, viewport int, mode HintMode) int {
	if viewport <= 0 || total <= 0 {
		return 0
	}
	bound := total - viewport + AboveHintRows(mode)
	if bound < 0 {
		return 0
	}
	return bound
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
