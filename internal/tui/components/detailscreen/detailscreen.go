// Package detailscreen wraps the "kicker + label rows + body + scroll"
// surface used by the task view, comment screen, and entity view. It
// composes the grid builder (kicker / two-column label-rows / spanned
// body) with an embedded viewport.Model so the screen state is one
// struct rather than four parallel fields on the parent Model.
//
// The package keeps the grid layout policy (label width = 13, value
// column from caller) and the viewport policy (footer hint when
// content overflows, line-based scroll) co-located: both belong to the
// same conceptual surface, and changing one usually means changing the
// other.
//
// Rendering primitives (Render, WrapLines, PadLine) live in the leaf
// gridtable package so the package-level summary tables and the detail
// screens share one implementation. Callers build a screen via the
// chainable Builder methods, then call View() to produce the
// scrollable content string ready to embed in the panel.
package detailscreen

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/viewport"
)

// LabelWidth is the standard width of the `// LABEL` column. Exposed so
// callers computing valueWidth from the available content area use the
// same constant the package uses internally — keeping the two in sync
// avoids mysterious off-by-one alignment bugs.
const LabelWidth = 13

// Model is a builder + scroll state combo. Build the rows fluently with
// Custom/Row/Kicker/KickerCount/Span; pass viewport height + border/hint
// styles to View() to render. Update delegates to the embedded viewport
// for scroll keys and esc.
//
// Viewport is exported so tests can read m.Viewport.Scroll directly when
// asserting on cursor/scroll state — same pattern as picker.Model.
type Model struct {
	Viewport viewport.Model

	rows   [][]string
	labelW int
	valueW int
}

// New starts a fresh detail screen scoped to the given value-column
// width. labelW is fixed at the package constant so screens stay
// aligned with each other; valueW is per-screen because the activity
// column on the task view eats into the available width differently
// from the full-width comment screen.
func New(valueW int) Model {
	return Model{
		Viewport: viewport.New(),
		labelW:   LabelWidth,
		valueW:   valueW,
	}
}

// Reset clears the builder rows and applies a new value-column width while
// preserving the embedded viewport state. Parents use it at render time: the
// layout is rebuilt every frame, but scroll position belongs to the model.
func (m Model) Reset(valueW int) Model {
	m.rows = nil
	m.labelW = LabelWidth
	m.valueW = valueW
	return m
}

// Custom appends a pre-rendered row that spans both columns. Used when
// the caller has already chosen between kicker and kickerFocused based
// on focus state — the package doesn't take a "focused?" bool because
// only one screen needs it and exposing it would force the same flag
// on every Custom call.
func (m Model) Custom(content string) Model {
	m.rows = append(m.rows, []string{content})
	return m
}

// Kicker appends a section-header row.
func (m Model) Kicker(label string) Model {
	m.rows = append(m.rows, []string{kicker(label)})
	return m
}

// KickerCount appends a kicker with a trailing count, e.g.
// `// BLOCKERS · 3`.
func (m Model) KickerCount(label string, count int) Model {
	m.rows = append(m.rows, []string{kickerCount(label, count)})
	return m
}

// Row appends a `// LABEL` + value pair.
func (m Model) Row(label, value string) Model {
	m.rows = append(m.rows, []string{labelCell(label), value})
	return m
}

// Span appends a single-cell row covering the full grid width — used
// for body text, hints, and any content that doesn't fit the two-column
// label/value layout.
func (m Model) Span(content string) Model {
	m.rows = append(m.rows, []string{content})
	return m
}

// Update consumes a key message, delegating to the embedded viewport
// for scroll keys (j/k/pgup/pgdn/g/G) and esc. Returns the new model;
// parents that need to react to esc check m.LastEvent() == EventCancel.
func (m Model) Update(msg tea.Msg, viewportHeight int) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg, viewportHeight)
	return m, cmd
}

// LastEvent reports the high-level outcome of the most recent Update,
// surfacing the embedded viewport's event so parents only check one
// place. EventCancel fires on esc.
func (m Model) LastEvent() viewport.Event { return m.Viewport.LastEvent() }

// View renders the accumulated rows through the grid layout, applies
// scroll via the embedded viewport, and returns the final string. When
// content fits in the viewport, the footer hint is omitted — the caller
// can wrap the result in any panel/box without worrying about a
// stray indicator line.
//
// border paints the grid borders and the panel-internal vertical bars;
// hint paints the "▲ N above · ▼ N below · j/k pgup/pgdn g/G" footer.
func (m Model) View(viewportHeight int, border, hint lipgloss.Style) string {
	rendered := gridtable.Render(m.rows, []int{m.labelW, m.valueW}, border)
	if viewportHeight <= 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) <= viewportHeight {
		return rendered
	}
	// Overflow: reserve one line for the footer indicator.
	return m.Viewport.View(lines, viewportHeight-1, hint)
}

// kicker / kickerCount / labelCell — small style-rendering helpers kept
// in this package so the package owns the entire visual policy of a
// detail screen. They use the secondary-info colour (taken from a
// caller-passed style would force a 4-arg dance on every builder call).
// The styles are package-scoped via a single lipgloss.Style each, set
// at first use to lipgloss defaults; the caller can theme them via
// SetStyles below if it wants different colours.
var (
	infoStyle = lipgloss.NewStyle()
)

// SetStyles lets the host application paint kicker/label cells with its
// own theme. omakiten calls this once at startup with the secondary
// info colour; component tests use the default (no styling) so output
// stays stable regardless of theme. Only kicker/label colour is themed
// — body cells render whatever the caller passes verbatim.
func SetStyles(info lipgloss.Style) { infoStyle = info }

func kicker(label string) string { return infoStyle.Render("// " + strings.ToUpper(label)) }
func kickerCount(l string, n int) string {
	return infoStyle.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(l), n))
}
func labelCell(label string) string { return infoStyle.Render("// " + strings.ToUpper(label)) }
