// Package viewport encapsulates the "scrollable rendered content" surface
// shared by every detail screen and overlay in omakiten's TUI. The Model
// owns the scroll offset so parents stop tracking it as a flat field on
// the root struct (was: m.taskViewScroll, m.commentScreenScroll, m.helpScroll).
//
// The component handles only the navigation half of Bubble Tea — j/k,
// pgup/pgdn, ctrl+u/d, home/g, end/G — and renders the
// "▲ N above · ▼ N below · j/k pgup/pgdn g/G" footer when content
// overflows. Selection or "open detail" semantics belong to the parent.
package viewport

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model owns the scroll offset for a single scrollable content surface.
// Total content and viewport height are passed at View() time so the
// parent can recompute layout on each frame without re-creating the
// component (a common need when terminal width changes mid-session).
//
// Scroll is exported so tests that assert on cursor/scroll state can read
// it directly without going through getters; LastEvent reports whether
// the most recent Update consumed a key (so the parent dispatcher knows
// whether to fall through to its own handlers).
type Model struct {
	Scroll int

	lastEvent Event
}

// Event reports the outcome of the most recent Update. EventCancel fires
// on esc; parents typically use it to close the surrounding screen.
type Event int

const (
	EventNone Event = iota
	EventCancel
)

// New returns a zero-value model — Scroll defaults to 0. Construction is
// trivial so this exists mostly for symmetry with picker.New() and to
// give callers an obvious entry point.
func New() Model { return Model{} }

// Init satisfies the Bubble Tea Model interface; no startup commands.
func (m Model) Init() tea.Cmd { return nil }

// Update consumes a key message and returns the new model plus a nil cmd
// (no async work). viewport is needed for the page-step calculation;
// totalLines isn't — clamping happens in View/Slice where the lines are
// already in hand. Keeping Update geometry-agnostic on totals means the
// detail-screen builder doesn't have to render its grid twice (once for
// Update bounds, once for View output).
//
// The "end" sentinel (1<<20) intentionally bypasses any range check here
// because Slice clamps it down before rendering.
func (m Model) Update(msg tea.Msg, viewport int) (Model, tea.Cmd) {
	m.lastEvent = EventNone
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
		m.lastEvent = EventCancel
	}
	return m, nil
}

// LastEvent returns the high-level outcome of the most recent Update.
// Parents check this to decide whether to close the surrounding screen
// or fall through to their own handlers.
func (m Model) LastEvent() Event { return m.lastEvent }

// View renders the slice of lines visible at the current scroll offset
// plus the footer hint when content overflows. When everything fits, the
// footer is omitted so the caller can drop straight into compact layout
// without a trailing blank line.
//
// hintStyle paints the footer text; pass the Model's hint lipgloss style.
// The style is taken as a parameter (rather than read from a styles
// struct on the Model) so the component stays decoupled from omakiten's
// theme types.
func (m Model) View(lines []string, viewport int, hintStyle lipgloss.Style) string {
	visible, above, below := Slice(lines, m.Scroll, viewport)
	if above == 0 && below == 0 {
		return strings.Join(visible, "\n")
	}
	hint := hintStyle.Render(fmt.Sprintf("▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below))
	return strings.Join(visible, "\n") + "\n" + hint
}

// Slice clamps scroll to a valid offset for `lines` at the given viewport
// height and returns the visible window plus counts hidden above/below.
// Exported so callers that don't want the auto-rendered footer (e.g. the
// activity feed which prefers split top/bottom hints) can reuse the math
// directly.
func Slice(lines []string, scroll, viewport int) (visible []string, above, below int) {
	if viewport <= 0 || len(lines) <= viewport {
		return lines, 0, 0
	}
	offset := scroll
	if offset < 0 {
		offset = 0
	}
	maxOffset := len(lines) - viewport
	if offset > maxOffset {
		offset = maxOffset
	}
	return lines[offset : offset+viewport], offset, len(lines) - (offset + viewport)
}

// pageStep returns the half-page step used by pgup/pgdn. Floored at 4 so
// tiny viewports still feel responsive — without the floor a 6-row panel
// would page by 3 rows, which feels indistinguishable from j/k.
func pageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}

