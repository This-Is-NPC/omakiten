package tui

import "strings"

// panelViewportRows is the canonical "rows the data area gets" budget
// for any view that draws a single panel under the screen chrome (table,
// board lane, entity grid, settings entity). It subtracts the live
// chrome — measured rather than hard-coded so changes in the header /
// nav / sub strip update the budget automatically — from the terminal
// height. The Stats / Logs panel keeps its own variant because its
// summary tables sit between the screen header and the panel body; the
// callers here all start the panel right after the screen chrome.
//
// `panelChrome` is the rows the panel itself owns (border + kicker +
// separator + any trailing hint), so the returned number is exactly
// what `sliceScrollRows` (or a per-card scroller) should treat as its
// data window. Returns 0 on tiny terminals so callers fall back to
// "render everything and let clampViewToHeight chop" — never returning
// a negative budget.
func (m Model) panelViewportRows(panelChrome int) int {
	if m.height <= 0 {
		return 0
	}
	screenHeader := strings.Count(m.renderHeader(), "\n") + 1
	statusLine := 0
	if m.status != "" && !m.isEmbeddedCommentInput() {
		statusLine = 2 // separator newline + the status badge
	}
	const (
		leadingBlank = 1 // "\n" prepended by every renderXxx before the body
		footerLines  = 2 // newline + indented keybinding row
	)
	chrome := screenHeader + statusLine + leadingBlank + panelChrome + footerLines
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

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
