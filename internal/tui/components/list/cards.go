package list

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/scrollwindow"
)

// Item is one entry in a Cards list. Content is the pre-rendered card
// string (Cards does not own card chrome); Height is the
// terminal-row count of Content, used by scrollwindow.Resync to keep
// the cursor visible across variable-height layouts.
//
// Callers responsible for keeping Height consistent with Content —
// the canonical recipe is `strings.Count(Content, "\n") + 1`. NewItem
// applies that recipe for the common case; advanced callers (those
// that compute height ahead of time via cardHeightFromSpec to avoid
// re-rendering) build Item literals directly.
type Item struct {
	Content string
	Height  int
}

// NewItem builds an Item from a pre-rendered card string, deriving
// Height from the line count. Use this helper for callers that
// already have the rendered string and want the natural row count;
// pre-computed-height callers build the struct literal themselves.
func NewItem(content string) Item {
	return Item{
		Content: content,
		Height:  strings.Count(content, "\n") + 1,
	}
}

// Cards owns the cursor + scroll pair for a card-based viewport. The
// scroll field is unexported so surface code has no way to write a
// wrong card-index/line-offset value: every mutation routes through
// a typed method that re-runs the resync invariant internally.
//
// Zero-value Cards is empty + safe to use; callers seed items and
// viewport via WithItems + WithViewport (typically in a refresh path)
// before navigation lands.
type Cards struct {
	cursor       int
	scroll       int
	items        []Item
	viewportRows int
	// text resolves catalog keys for scroll hints. Nil uses the English
	// fallback at each tr callsite — the same shape studio/screens use.
	text func(string) string
}

// NewCards returns an empty Cards value. Use WithItems / WithViewport
// to populate it; the zero-value Cards is intentionally minimal so
// callers can compose it inside surface state structs without an
// init step.
func NewCards() Cards {
	return Cards{cursor: -1}
}

// WithText wires the catalog resolver used for ▲/▼ scroll hints.
func (m Cards) WithText(text func(string) string) Cards {
	m.text = text
	return m
}

// Cursor returns the current item index, or -1 when no item is
// selected (empty list or freshly opened panel pre-first-keystroke).
func (m Cards) Cursor() int {
	return m.cursor
}

// Scroll returns the current scroll offset as a card index. Exposed
// read-only so callers that need to compute "first visible card"
// (e.g. for a parent viewport's auto-scroll hack) can do so without
// the cardlist owning that secondary responsibility.
func (m Cards) Scroll() int {
	return m.scroll
}

// Len returns the number of items currently held.
func (m Cards) Len() int {
	return len(m.items)
}

// ActiveItem returns the item currently under the cursor, plus ok=false
// when no item is selected (cursor=-1) or the cursor has drifted past
// the end (e.g. items shrank after a refresh that has not yet been
// followed by a WithItems call).
func (m Cards) ActiveItem() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return Item{}, false
	}
	return m.items[m.cursor], true
}

// MoveCursor advances the cursor by delta and re-runs the resync
// invariant. Wraps from the no-selection sentinel (-1) to the first
// or last card depending on direction so a single keypress always
// lands on a real row; mirrors the prior moveActivityCursor /
// moveSubtaskCursor shape callers already expect.
//
// No-op when the list is empty — cursor stays at -1 so the caller's
// "no selection" rendering branch keeps firing.
func (m Cards) MoveCursor(delta int) Cards {
	if len(m.items) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	if m.cursor < 0 {
		if delta > 0 {
			m.cursor = 0
		} else {
			m.cursor = len(m.items) - 1
		}
	} else {
		next := m.cursor + delta
		if next < 0 {
			next = 0
		}
		if next >= len(m.items) {
			next = len(m.items) - 1
		}
		m.cursor = next
	}
	m.resync()
	return m
}

// JumpFirst lands the cursor on the first item and the scroll at the
// top. Used by the `g` / Home keybinding family every list surface
// already exposes.
func (m Cards) JumpFirst() Cards {
	if len(m.items) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	m.cursor = 0
	m.resync()
	return m
}

// JumpLast lands the cursor on the last item; scroll follows so the
// item stays visible. Used by `G` / End. The follow respects
// HintsSplit reservation so the last card is fully visible (not
// hidden behind a "▼ N below" hint).
func (m Cards) JumpLast() Cards {
	if len(m.items) == 0 {
		m.cursor = -1
		m.scroll = 0
		return m
	}
	m.cursor = len(m.items) - 1
	m.resync()
	return m
}

// PageDown advances the cursor by roughly half a viewport. Used by
// pgdown / ctrl+d. The step is computed from the viewport rows
// divided by an approximation of an average card height (4 rows) so
// the cursor stays comfortably inside the next visible window
// without overshooting on tiny terminals.
func (m Cards) PageDown() Cards {
	return m.MoveCursor(m.pageStep())
}

// PageUp reverses PageDown.
func (m Cards) PageUp() Cards {
	return m.MoveCursor(-m.pageStep())
}

// WithCursor jumps the cursor to a specific item index and re-runs
// the resync invariant. Clamps idx to [-1, len(items)-1] so callers
// can pass an out-of-range value (e.g. after items shrank) and get
// safe behaviour. -1 is the no-selection sentinel — pass it to
// clear the cursor without nuking the items slice (compare with
// WithItems(nil) which drains everything).
//
// Used by surfaces that drive the cursor through a parallel field
// on the parent Model (e.g. the board's m.cardIdx as the focused-
// column cursor sentinel) instead of routing every keystroke
// through MoveCursor. Those surfaces call WithCursor at sync time
// so the cardlist's internal cursor stays aligned with the
// authoritative one.
func (m Cards) WithCursor(idx int) Cards {
	m.cursor = idx
	m.resync()
	return m
}

// WithItems replaces the item list and re-runs the resync invariant.
// Cursor is preserved when the new list still has an index at the
// prior cursor; otherwise it clamps to the last item (e.g. the
// active card was removed mid-session).
//
// The cardlist does not memo-compare items — callers that want to
// avoid the resync hit on a no-op refresh should fingerprint
// upstream. The resync itself is O(viewport / min-card-height) so
// the cost is bounded and small.
func (m Cards) WithItems(items []Item) Cards {
	m.items = items
	m.resync()
	return m
}

// WithViewport changes the row budget and re-runs the resync
// invariant. Called on every resize so the cursor stays visible as
// the user grows or shrinks the terminal.
func (m Cards) WithViewport(rows int) Cards {
	if rows < 0 {
		rows = 0
	}
	m.viewportRows = rows
	m.resync()
	return m
}

// View renders the visible window of items separated by newlines,
// prepending an "▲ N above" hint when above-items are hidden and
// appending a "▼ N below" hint when below-items are hidden. Mirrors
// the prior renderScrollWindowSplit assembly so existing surface
// chrome (column borders, kicker rows) wraps the cardlist's output
// unchanged.
//
// When the viewport has rows left over after the whole-card slice,
// View renders partial cards on the leading / trailing edges so the
// column fills its allotted height instead of leaving a blank gap.
// The leftover rows are split between the two edges (trailing
// favoured by 1 row on an odd split). Partial cards are visual
// previews only — cursor + scroll still advance in whole-card
// units, and the above / below hints still count partial cards in
// their respective sides. Matches the linelist's line-based slice
// behaviour so the two surfaces feel symmetric.
//
// hintStyle is applied to the indicator rows. The cardlist does not
// own the style choice because different surfaces (board vs activity
// vs subtasks) use different theme tokens for the hint colour.
func (m Cards) View(hintStyle lipgloss.Style) string {
	if len(m.items) == 0 {
		return ""
	}
	if m.viewportRows <= 0 {
		return joinItems(m.items)
	}
	heights := m.heights()
	end := scrollwindow.Slice(m.scroll, heights, m.viewportRows, scrollwindow.HintsSplit)
	if m.scroll == 0 && end == len(m.items) {
		return joinItems(m.items)
	}

	// The partial-card budget at each edge. The arithmetic lives in
	// scrollwindow so this component and screenlayout's arranger share
	// one derivation of it rather than two.
	above := scrollwindow.Above(m.scroll)
	below := scrollwindow.Below(end, len(m.items))
	leadingRows, trailingRows := scrollwindow.PartialRows(m.scroll, end, heights, m.viewportRows, scrollwindow.HintsSplit)

	parts := make([]string, 0, end-m.scroll+4)
	if above > 0 {
		parts = append(parts, hintStyle.Render(formatAboveHint(above, m.text)))
	}
	if leadingRows > 0 {
		parts = append(parts, scrollwindow.TailRows(m.items[m.scroll-1].Content, leadingRows))
	}
	for _, item := range m.items[m.scroll:end] {
		parts = append(parts, item.Content)
	}
	if trailingRows > 0 {
		parts = append(parts, scrollwindow.HeadRows(m.items[end].Content, trailingRows))
	}
	if below > 0 {
		parts = append(parts, hintStyle.Render(formatBelowHint(below, m.text)))
	}
	return strings.Join(parts, "\n")
}

// VisibleRange returns the [first, last] inclusive item indices the
// next View call will render, plus ok=false when the list is empty.
// Exposed so callers that decouple cursor movement from scroll (the
// "pgup snaps cursor to viewport edge" case in activity) can ask the
// cardlist where the visible band sits.
func (m Cards) VisibleRange() (first, last int, ok bool) {
	if len(m.items) == 0 {
		return 0, 0, false
	}
	if m.viewportRows <= 0 {
		return 0, len(m.items) - 1, true
	}
	heights := m.heights()
	end := scrollwindow.Slice(m.scroll, heights, m.viewportRows, scrollwindow.HintsSplit)
	if end <= m.scroll {
		return m.scroll, m.scroll, true
	}
	return m.scroll, end - 1, true
}

// resync drives every state change through scrollwindow.Resync so the
// cursor + scroll pair stay coherent regardless of which mutator was
// the entry point. The cardlist's whole correctness story sits in
// this one method — kept private so callers cannot accidentally skip
// it or supply a stale heights slice.
func (m *Cards) resync() {
	heights := m.heights()
	cursor, scroll := scrollwindow.Resync(m.cursor, m.scroll, heights, m.viewportRows)
	m.cursor = cursor
	m.scroll = scroll
}

// heights extracts the per-item terminal-row counts into a fresh
// slice scrollwindow consumes. Allocates per call — caller cost is
// O(len(items)) which the existing surfaces already pay; not worth
// caching until profiling proves it.
func (m Cards) heights() []int {
	if len(m.items) == 0 {
		return nil
	}
	out := make([]int, len(m.items))
	for i, item := range m.items {
		h := item.Height
		if h < 1 {
			h = 1
		}
		out[i] = h
	}
	return out
}

// pageStep approximates "half a viewport in cards" for PageUp /
// PageDown. The avg-card-height constant matches the prior board
// boardScrollPageStep value (4 rows ≈ border + content + badges).
// Floor of 2 keeps the cursor moving on tiny viewports.
func (m Cards) pageStep() int {
	const avgCardRows = 4
	if m.viewportRows <= 0 {
		return 2
	}
	step := m.viewportRows / (2 * avgCardRows)
	if step < 2 {
		step = 2
	}
	return step
}

func joinItems(items []Item) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.Content
	}
	return strings.Join(parts, "\n")
}

// Indicator strings route through the catalog when text is wired. Surfaces
// that hold a Kit pass kit.Text via WithText; headless tests leave it nil and
// assert the English fallback.
func formatAboveHint(n int, text func(string) string) string {
	return tr(text, "tui.scroll.above_fmt", "▲ %d above", n)
}

func formatBelowHint(n int, text func(string) string) string {
	return tr(text, "tui.scroll.below_fmt", "▼ %d below", n)
}
