// Package linelist is the canonical cursor + scroll state holder for
// every LINE-based viewport surface in the TUI (logs panel, table
// rows, plan list, graph ascii, settings grid).
//
// Distinct from cardlist on purpose: surfaces whose cursor lives on
// a single terminal row (one line = one selectable entry) import
// linelist; surfaces whose cursor lives on a multi-line card import
// cardlist. The two packages are intentionally separate so the
// compiler enforces the unit-of-cursor at every callsite — a
// linelist user cannot accidentally pass a card index where a line
// number is expected and vice versa.
//
// The bug class this prevents is the same one cardlist closes: prior
// surfaces stored `fooScroll int` as a line offset OR a row index
// depending on the surface, and `renderScrollWindowSplit` accepted
// either. With linelist owning the scroll field internally, the
// unit ambiguity disappears at the API surface.
package linelist

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/scrollwindow"
)

// Model owns the cursor + scroll pair for a line-based viewport.
// Cursor and scroll are both line indices into the lines slice; the
// scroll field is unexported so surface code cannot accidentally
// write the wrong unit.
type Model struct {
	cursor       int
	scroll       int
	lines        []string
	viewportRows int
}

// New returns an empty linelist Model with cursor=-1 (no-selection
// sentinel). Use WithLines + WithViewport to populate.
func New() Model {
	return Model{cursor: -1}
}

// Cursor returns the current line index, or -1 when no line is
// selected.
func (m Model) Cursor() int {
	return m.cursor
}

// Scroll returns the current scroll offset (line index of the first
// visible line). Read-only by design.
func (m Model) Scroll() int {
	return m.scroll
}

// Len returns the number of lines held.
func (m Model) Len() int {
	return len(m.lines)
}

// ActiveLine returns the line under the cursor, plus ok=false when
// the list is empty or cursor=-1.
func (m Model) ActiveLine() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.lines) {
		return "", false
	}
	return m.lines[m.cursor], true
}

// MoveCursor advances the cursor by delta and re-runs the resync
// invariant. Wraps from -1 to first/last depending on direction.
func (m Model) MoveCursor(delta int) Model {
	if len(m.lines) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	if m.cursor < 0 {
		if delta > 0 {
			m.cursor = 0
		} else {
			m.cursor = len(m.lines) - 1
		}
	} else {
		next := m.cursor + delta
		if next < 0 {
			next = 0
		}
		if next >= len(m.lines) {
			next = len(m.lines) - 1
		}
		m.cursor = next
	}
	m.resync()
	return m
}

// JumpFirst lands the cursor on the first line. Used by g / Home.
func (m Model) JumpFirst() Model {
	if len(m.lines) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	m.cursor = 0
	m.resync()
	return m
}

// JumpLast lands the cursor on the last line. Used by G / End.
func (m Model) JumpLast() Model {
	if len(m.lines) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	m.cursor = len(m.lines) - 1
	m.resync()
	return m
}

// PageDown advances the cursor by half a viewport. Used by pgdown /
// ctrl+d. Floor of 2 keeps the cursor moving on tiny viewports.
func (m Model) PageDown() Model {
	return m.MoveCursor(m.pageStep())
}

// PageUp reverses PageDown.
func (m Model) PageUp() Model {
	return m.MoveCursor(-m.pageStep())
}

// ScrollBy nudges the scroll offset by delta lines WITHOUT moving
// the cursor. Used by surfaces that decouple cursor and viewport
// (e.g. activity feed body-scroll via pgup/pgdn while the cursor
// stays anchored). Re-clamps to keep scroll inside [0, last
// renderable offset].
func (m Model) ScrollBy(delta int) Model {
	if len(m.lines) == 0 || m.viewportRows <= 0 {
		return m
	}
	m.scroll += delta
	m.clampScroll()
	return m
}

// WithLines replaces the line list and re-runs resync. Cursor is
// preserved when the new list still has a line at that index; else
// clamps to last.
func (m Model) WithLines(lines []string) Model {
	m.lines = lines
	m.resync()
	return m
}

// WithViewport changes the row budget. Re-runs resync so cursor
// stays visible.
func (m Model) WithViewport(rows int) Model {
	if rows < 0 {
		rows = 0
	}
	m.viewportRows = rows
	m.resync()
	return m
}

// View renders the visible slice of lines with HintsSplit
// reservation. hintStyle is applied to the ▲/▼ indicator rows.
//
// When no line is selected, the renderer still produces the
// scrolled body — surfaces that want a selection-only highlight
// must inject their own cursor styling into the lines they pass
// via WithLines.
func (m Model) View(hintStyle lipgloss.Style) string {
	if len(m.lines) == 0 {
		return ""
	}
	if m.viewportRows <= 0 {
		return strings.Join(m.lines, "\n")
	}
	heights := ones(len(m.lines))
	end := scrollwindow.Slice(m.scroll, heights, m.viewportRows, scrollwindow.HintsSplit)
	if m.scroll == 0 && end == len(m.lines) {
		return strings.Join(m.lines, "\n")
	}
	parts := make([]string, 0, end-m.scroll+2)
	if above := scrollwindow.Above(m.scroll); above > 0 {
		parts = append(parts, hintStyle.Render("▲ "+strconv.Itoa(above)+" above"))
	}
	parts = append(parts, m.lines[m.scroll:end]...)
	if below := scrollwindow.Below(end, len(m.lines)); below > 0 {
		parts = append(parts, hintStyle.Render("▼ "+strconv.Itoa(below)+" below"))
	}
	return strings.Join(parts, "\n")
}

// VisibleRange returns [first, last] line indices the next View
// will render, plus ok=false when empty.
func (m Model) VisibleRange() (first, last int, ok bool) {
	if len(m.lines) == 0 {
		return 0, 0, false
	}
	if m.viewportRows <= 0 {
		return 0, len(m.lines) - 1, true
	}
	heights := ones(len(m.lines))
	end := scrollwindow.Slice(m.scroll, heights, m.viewportRows, scrollwindow.HintsSplit)
	if end <= m.scroll {
		return m.scroll, m.scroll, true
	}
	return m.scroll, end - 1, true
}

func (m *Model) resync() {
	heights := ones(len(m.lines))
	cursor, scroll := scrollwindow.Resync(m.cursor, m.scroll, heights, m.viewportRows)
	m.cursor = cursor
	m.scroll = scroll
}

// clampScroll keeps scroll inside [0, last valid offset for the
// current viewport]. Used by ScrollBy where the cursor is not
// moving but the scroll must still respect the same upper bound
// scrollwindow.Slice would impose.
func (m *Model) clampScroll() {
	if m.scroll < 0 {
		m.scroll = 0
		return
	}
	if m.viewportRows <= 0 {
		m.scroll = 0
		return
	}
	// Equivalent to the activityMaxScroll helper the activity feed
	// used inline; centralised here so linelist callers do not
	// re-invent it. bound = len-viewport + above-hint-reservation
	// keeps the last line reachable when scrolling all the way down.
	bound := len(m.lines) - m.viewportRows + scrollwindow.AboveHintRows(scrollwindow.HintsSplit)
	if bound < 0 {
		bound = 0
	}
	if m.scroll > bound {
		m.scroll = bound
	}
}

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
