package tui

// scrollDataRows is the canonical adapter between a panel's full row
// budget (which `sliceScrollRows` expects) and the data-row window any
// cursor-tracking helper should target. The renderer reserves up to 2
// of those rows for "▲ above" / "▼ below" hints, so the window the
// cursor can actually live in is `viewport - 2`.
//
// The contract for every panel that wraps `sliceScrollRows`:
//   - pass the raw `*ViewportRows()` budget to `sliceScrollRows` (so
//     the renderer reserves the right amount of chrome);
//   - pass `scrollDataRows(*ViewportRows())` to anything that decides
//     scroll from cursor — `followCursor`, `picker.Model.Update`, or a
//     bespoke sync routine like `syncGraphScroll`.
//
// Returning at least 1 keeps followCursor responsive on tiny terminals
// instead of locking up at zero.
func scrollDataRows(viewport int) int {
	const reservedHints = 2
	if viewport <= reservedHints {
		return 1
	}
	return viewport - reservedHints
}

// followCursor returns the new scroll offset that keeps `cursor` inside the
// visible window of length `viewport`, given the current `scroll`. The
// rule is symmetric: if the cursor moves above the window, scroll equals
// the cursor; if it moves past the bottom, scroll trails by viewport-1.
//
// Callers (table, logs, blocker picker, board column) used to inline this
// 12-line block — small enough to be tempting to copy, large enough to
// drift across copies. The total bound caps scroll so an over-eager
// "jump to end" sentinel cannot land past the last row.
//
// Returns 0 when viewport is non-positive (caller should skip syncing) or
// when the content fits entirely (no scroll needed).
func followCursor(scroll, cursor, viewport, total int) int {
	if viewport <= 0 || total <= viewport {
		return 0
	}
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+viewport {
		scroll = cursor - viewport + 1
	}
	if scroll < 0 {
		scroll = 0
	}
	if max := total - viewport; scroll > max {
		scroll = max
	}
	return scroll
}
