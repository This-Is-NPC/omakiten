// Package cursorwindow is the canonical cursor + scroll state holder
// for fixed-row TUI surfaces whose cursor lives on a single terminal
// row (one item = one row) but whose surface chrome (lane borders,
// frame headers, footer hints) is owned by the parent renderer rather
// than delegated to cardlist / linelist's View.
//
// The bug class this component closes is the same one cardlist and
// linelist already closed for their respective surfaces: surfaces
// declared two raw `int` fields on the parent Model (`fooCursor`,
// `fooScroll`) and re-litigated cursor clamping + scroll resync per
// surface. A fraction of them got it wrong (forgot the clamp after a
// delete, mixed cursor units between renderColumnFrame consumers).
//
// cursorwindow makes both fields unexported: surface code cannot
// write a wrong value because the fields are not part of the public
// API. Every state change routes through a typed mutator
// (MoveCursor, JumpFirst, JumpLast, PageUp, PageDown, WithItemCount,
// WithViewport), each of which re-runs the resync invariant
// internally — the unit mismatch and stale-clamp bugs become
// impossible to write at the API surface.
//
// Pair with cardlist / linelist:
//   - cardlist: variable-height cards (board lanes, subtasks)
//   - linelist: fixed-height lines with body rendered by the component
//   - cursorwindow: fixed-height rows with body rendered by the PARENT
//
// The cursorwindow does not own a View — surfaces using it still own
// their own row rendering (the home grid, the plans list, the graph
// ascii, the activity feed). The component only owns the
// (cursor, scroll, itemCount, viewportRows) tuple and the resync
// math that keeps the cursor visible.
package cursorwindow

import "omakiten/internal/tui/components/scrollwindow"

// Model owns the cursor + scroll pair for a fixed-row viewport whose
// content the parent renders. The cursor and scroll fields are
// unexported so surface code has no way to write a wrong value;
// every mutation routes through a typed method that re-runs the
// resync invariant.
//
// Zero-value Model is empty + safe; callers seed via WithItemCount /
// WithViewport (typically in a refresh path) before navigation lands.
type Model struct {
	cursor       int
	scroll       int
	itemCount    int
	viewportRows int
}

// New returns a cursorwindow Model sized for the given viewport row
// budget. itemCount defaults to 0; callers grow it via WithItemCount
// once their data is loaded. Cursor and scroll start at 0 — fixed-row
// surfaces overwhelmingly want index 0 highlighted on first paint
// (the home grid, the plans list, the graph), rather than the -1
// "no selection" sentinel cardlist / linelist use for card surfaces.
func New(viewportRows int) Model {
	if viewportRows < 0 {
		viewportRows = 0
	}
	return Model{viewportRows: viewportRows}
}

// Cursor returns the current item index.
func (m Model) Cursor() int { return m.cursor }

// Scroll returns the current scroll offset (item index of the first
// visible row). Exposed read-only so callers that compute "first
// visible item" for their own viewport assembly can do so without the
// component owning that responsibility.
func (m Model) Scroll() int { return m.scroll }

// ItemCount returns the current item count.
func (m Model) ItemCount() int { return m.itemCount }

// ViewportRows returns the current viewport budget.
func (m Model) ViewportRows() int { return m.viewportRows }

// VisibleRange returns [start, end) — the half-open item index range
// the parent renderer should iterate. Returns (0, 0) when the list is
// empty. start is the first visible item; end-start is the number of
// rows the renderer will actually draw given the current viewport.
//
// The range respects HintsSplit indicator reservation so the parent
// renderer can prepend an "▲ N above" line and append a "▼ N below"
// line outside the [start, end) slice without overflowing the
// viewport.
func (m Model) VisibleRange() (start, end int) {
	if m.itemCount <= 0 {
		return 0, 0
	}
	if m.viewportRows <= 0 {
		return 0, m.itemCount
	}
	heights := ones(m.itemCount)
	stop := scrollwindow.Slice(m.scroll, heights, m.viewportRows, scrollwindow.HintsSplit)
	if stop <= m.scroll {
		stop = m.scroll + 1
	}
	if stop > m.itemCount {
		stop = m.itemCount
	}
	return m.scroll, stop
}

// MoveCursor advances the cursor by delta and re-runs the resync
// invariant so scroll follows. Clamps to [0, itemCount-1]. No-op when
// itemCount is 0 (cursor stays at 0; scroll stays at 0).
func (m Model) MoveCursor(delta int) Model {
	if m.itemCount <= 0 {
		m.cursor = 0
		m.scroll = 0
		return m
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next > m.itemCount-1 {
		next = m.itemCount - 1
	}
	m.cursor = next
	m.resync()
	return m
}

// JumpFirst lands the cursor on the first item and the scroll at the
// top. Used by g / Home.
func (m Model) JumpFirst() Model {
	if m.itemCount <= 0 {
		m.cursor = 0
		m.scroll = 0
		return m
	}
	m.cursor = 0
	m.resync()
	return m
}

// JumpLast lands the cursor on the last item; scroll follows. Used by
// G / End. The follow respects HintsSplit reservation so the last row
// is fully visible (not hidden behind a "▼ N below" hint).
func (m Model) JumpLast() Model {
	if m.itemCount <= 0 {
		m.cursor = 0
		m.scroll = 0
		return m
	}
	m.cursor = m.itemCount - 1
	m.resync()
	return m
}

// PageDown advances the cursor by roughly half a viewport. Floor of 2
// keeps the cursor moving on tiny viewports.
func (m Model) PageDown() Model { return m.MoveCursor(m.pageStep()) }

// PageUp reverses PageDown.
func (m Model) PageUp() Model { return m.MoveCursor(-m.pageStep()) }

// SetCursor jumps the cursor to a specific index and re-runs resync.
// Clamps idx to [0, itemCount-1]. Used by surfaces that drive the
// cursor through an external authoritative field (e.g. the plan
// network selects the wave whose row matches the focused entity).
func (m Model) SetCursor(idx int) Model {
	if m.itemCount <= 0 {
		m.cursor = 0
		m.scroll = 0
		return m
	}
	if idx < 0 {
		idx = 0
	}
	if idx > m.itemCount-1 {
		idx = m.itemCount - 1
	}
	m.cursor = idx
	m.resync()
	return m
}

// WithItemCount replaces the item count and re-runs resync. Cursor is
// preserved when the new count still has an index at the prior cursor;
// otherwise it clamps to the last item (or 0 when the list goes empty).
//
// This is the post-delete safe-mutate path: a surface that deletes the
// active item just decrements its count, calls WithItemCount(n-1), and
// the component lands the cursor on the previous-last item
// automatically. Closes the bug class "cursor stranded past the new
// end after delete".
func (m Model) WithItemCount(n int) Model {
	if n < 0 {
		n = 0
	}
	m.itemCount = n
	if n == 0 {
		m.cursor = 0
		m.scroll = 0
		return m
	}
	if m.cursor > n-1 {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.resync()
	return m
}

// WithViewport changes the row budget. Re-runs resync so scroll may
// advance to keep cursor visible (or retreat when the viewport grows).
func (m Model) WithViewport(rows int) Model {
	if rows < 0 {
		rows = 0
	}
	m.viewportRows = rows
	m.resync()
	return m
}

// resync drives every state change through scrollwindow.Resync so the
// cursor + scroll pair stay coherent regardless of which mutator was
// the entry point. Kept private so callers cannot accidentally skip
// it or supply a stale heights slice.
func (m *Model) resync() {
	if m.itemCount <= 0 {
		m.cursor = 0
		m.scroll = 0
		return
	}
	heights := ones(m.itemCount)
	// scrollwindow.Resync returns -1 for cursor when len(heights) == 0
	// and clamps to [0, len-1] otherwise. Since we already early-
	// returned the empty case, cursor will land in [0, itemCount-1].
	cursor, scroll := scrollwindow.Resync(m.cursor, m.scroll, heights, m.viewportRows)
	if cursor < 0 {
		cursor = 0
	}
	m.cursor = cursor
	m.scroll = scroll
}

// pageStep approximates "half a viewport" for PageUp / PageDown.
// Floor of 2 keeps the cursor moving on tiny viewports.
func (m Model) pageStep() int {
	if m.viewportRows <= 0 {
		return 2
	}
	step := m.viewportRows / 2
	if step < 2 {
		step = 2
	}
	return step
}

func ones(n int) []int {
	if n <= 0 {
		return nil
	}
	out := make([]int, n)
	for i := range out {
		out[i] = 1
	}
	return out
}
