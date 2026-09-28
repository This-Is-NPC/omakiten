package list

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// Viewport owns the scroll offset for a single scrollable content surface.
// Total content and viewport height are passed at View() time so the
// parent can recompute layout on each frame without re-creating the
// component (a common need when terminal width changes mid-session).
//
// Scroll is exported so tests that assert on cursor/scroll state can read
// it directly without going through getters; LastEvent reports whether
// the most recent Update consumed a key (so the parent dispatcher knows
// whether to fall through to its own handlers).
type Viewport struct {
	Scroll int

	lastEvent ViewportEvent
	text      func(string) string
	// width is the column budget the hint is held to. Zero means "not told",
	// which keeps the pre-WithWidth behaviour.
	width int
}

// WithText wires the catalog resolver used for the overflow footer hint.
func (m Viewport) WithText(text func(string) string) Viewport {
	m.text = text
	return m
}

// ViewportEvent reports the outcome of the most recent Update. ViewportCancel fires
// on esc; parents typically use it to close the surrounding screen.
type ViewportEvent int

const (
	ViewportNone ViewportEvent = iota
	ViewportCancel
)

// NewViewport returns a zero-value model — Scroll defaults to 0. Construction is
// trivial so this exists mostly for symmetry with NewPicker and to
// give callers an obvious entry point.
func NewViewport() Viewport { return Viewport{} }

// Init satisfies the Bubble Tea Model interface; no startup commands.
func (m Viewport) Init() tea.Cmd { return nil }

// Update consumes a key message and returns the new model plus a nil cmd
// (no async work). viewport is needed for the page-step calculation;
// totalLines isn't — clamping happens in View/Slice where the lines are
// already in hand. Keeping Update geometry-agnostic on totals means the
// detail-screen builder doesn't have to render its grid twice (once for
// Update bounds, once for View output).
//
// The "end" sentinel (1<<20) intentionally bypasses any range check here
// because Slice clamps it down before rendering.
func (m Viewport) Update(msg tea.Msg, viewport int) (Viewport, tea.Cmd) {
	m.lastEvent = ViewportNone
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "j", "down":
		m.Scroll++
	case "k", "up":
		if m.Scroll > 0 {
			m.Scroll--
		}
	case "pgdown", "ctrl+d":
		m.Scroll += pageStep(viewport)
	case "pgup", "ctrl+u":
		m.Scroll -= pageStep(viewport)
		if m.Scroll < 0 {
			m.Scroll = 0
		}
	case "home", "g":
		m.Scroll = 0
	case "end", "G":
		m.Scroll = 1 << 20 // sentinel — Slice clamps to a valid offset
	case "esc":
		m.lastEvent = ViewportCancel
	}
	return m, nil
}

// LastEvent returns the high-level outcome of the most recent Update.
// Parents check this to decide whether to close the surrounding screen
// or fall through to their own handlers.
func (m Viewport) LastEvent() ViewportEvent { return m.lastEvent }

// WithScroll returns a copy of m with the scroll offset set to the
// given value, clamped at 0. Used by parents that reset scroll on
// open/close transitions (the help overlay does this every time the
// user re-opens it so the previous session's scroll doesn't bleed
// into the next view). Same shape as picker.WithScroll — exported
// field stays for read access, writers route through a typed
// mutator so the scroll-boundary arch test recognises the call.
func (m Viewport) WithScroll(scroll int) Viewport {
	if scroll < 0 {
		scroll = 0
	}
	m.Scroll = scroll
	return m
}

// View renders the slice of lines visible at the current scroll offset
// plus the footer hint when content overflows. When everything fits, the
// footer is omitted so the caller can drop straight into compact layout
// without a trailing blank line.
//
// hintStyle paints the footer text; pass the Model's hint lipgloss style.
// The style is taken as a parameter (rather than read from a styles
// struct on the Model) so the component stays decoupled from omakiten's
// theme types.
func (m Viewport) View(lines []string, viewport int, hintStyle lipgloss.Style) string {
	visible, above, below := SliceLines(lines, m.Scroll, viewport)
	if above == 0 && below == 0 {
		return strings.Join(visible, "\n")
	}
	hint := tr(m.text, "tui.scroll.combined_fmt", "▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below)
	if m.width > 0 {
		hint = screenkit.Truncate(hint, m.width)
	}
	return strings.Join(visible, "\n") + "\n" + hintStyle.Render(hint)
}

// Fit windows an already-rendered block into height. Content that fits (or a
// non-positive height) is returned unchanged; overflow reserves one row for
// the scroll hint. This is the policy detail screens used to own inside the
// grid builder's View — the builder now paints the grid, the screen holds
// the viewport, and Fit is the join.
func (m Viewport) Fit(content string, height int, hintStyle lipgloss.Style, text func(string) string) string {
	if height <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}
	return m.WithText(text).View(lines, height-1, hintStyle)
}

// WithWidth tells the viewport how many columns it may spend, so the scroll
// hint is held to the same budget as the content above it.
//
// The hint is the longest fixed string this component paints — counts, a
// separator and five key spellings — and nothing measured it, so a surface
// narrower than about 44 columns had the footer run past its right edge while
// every row above it fitted. It is the defect the layout plan recorded as
// "hint de scroll estoura seção < 24 col — pinned by a test, not fixed".
//
// Additive on purpose: zero keeps the previous behaviour exactly, so a caller
// that has not been told its width yet renders as it always did rather than
// truncating against a budget it never declared.
func (m Viewport) WithWidth(width int) Viewport {
	if width < 0 {
		width = 0
	}
	m.width = width
	return m
}

// SliceLines clamps scroll to a valid offset for `lines` at the given viewport
// height and returns the visible window plus counts hidden above/below.
// Routes through scrollwindow.Slice with HintsNone — the detail-screen
// View renders the combined-footer hint OUTSIDE the slice budget by
// asking callers to pass viewport-1 when they need a footer row, so
// the slicer itself reserves nothing. The pre-clamp preserves the
// "jump to end" behavior callers depend on (sentinel scroll = 1<<20
// resolves to len-viewport, not len-1).
func SliceLines(lines []string, scroll, viewport int) (visible []string, above, below int) {
	if viewport <= 0 || len(lines) <= viewport {
		return lines, 0, 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if maxOffset := len(lines) - viewport; scroll > maxOffset {
		scroll = maxOffset
	}
	heights := make([]int, len(lines))
	for i := range heights {
		heights[i] = 1
	}
	end := scrollwindow.Slice(scroll, heights, viewport, scrollwindow.HintsNone)
	return lines[scroll:end], scrollwindow.Above(scroll), scrollwindow.Below(end, len(lines))
}
