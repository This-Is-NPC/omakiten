package screenkit

import (
	"github.com/charmbracelet/lipgloss"
)

// Chrome is the running tally of terminal rows a screen body spends on its own
// chrome — everything drawn between the host chrome (which Kit already
// subtracts) and the scrollable data window: the panel border, the kicker, the
// column header, the separator rule, the trailing hint.
//
// # Why this exists
//
// Before this type every screen passed a hand-counted integer to
// PanelViewportRows — 2 for insights, 4 for graph, 5 for plans — and that
// integer was a private copy of a number the rendering components already knew.
// Kit.Panel knows how many rows its border costs. HRule knows a rule is one
// row. The header slice a screen builds knows how long it is. A copy has to be
// maintained by hand, and #2413 showed what happens when it is not: `kit.Height
// - 10` charged nothing for a panel's own border and kicker, so the panel drew
// three rows more than the terminal had at every geometry and clipped its own
// tail for a release.
//
// Chrome removes the copy. Every method charges rows by MEASURING the thing
// that renders them, so the number has exactly one author — the component that
// draws it. A screen that grows a hint line pays for it the moment the line is
// appended to the header it already builds, with no second edit.
//
// # Usage
//
// Build the chrome blocks once, use them for both the budget and the body:
//
//	func (s Screen) header(kit screenkit.Kit) []string {
//		return []string{kit.Styles.KickerCount("tasks", n), kit.HRule(width)}
//	}
//	func (s Screen) viewportRows(kit screenkit.Kit) int {
//		return kit.PanelChrome().Lines(s.header(kit)...).ViewportRows()
//	}
//
// # Value semantics
//
// Chrome is a value and every method returns a new one, so a screen may tally a
// shared prefix and then branch per layout without the branches contaminating
// each other.
//
// Chrome deliberately knows nothing about section declaration or layout
// selection. Measuring chrome and choosing a layout are separate concerns and
// stay in separate types.
type Chrome struct {
	kit  Kit
	rows int
	// wrapWidth is the width the chrome blocks will be wrapped to by whatever
	// frames this body, or 0 when nothing wraps them. PanelChrome sets it to
	// the panel's content width; the bare Chrome leaves it zero because a
	// screen that frames its own body decides its own wrapping.
	wrapWidth int
}

// Chrome starts an empty chrome tally for a body the screen frames itself
// (home paints a Column, settings paints no box at all).
func (k Kit) Chrome() Chrome { return Chrome{kit: k} }

// PanelChrome starts a chrome tally already charged for the rows Kit.Panel
// draws around a body.
//
// The charge is measured, not assumed: Panel is rendered around a one-row body
// and the difference is the border. The leading blank Panel prepends is NOT
// charged here, because ViewportRows already subtracts it for every screen —
// charging it twice is the exact double-count this type exists to prevent.
func (k Kit) PanelChrome() Chrome {
	const probeRows, leadingBlank = 1, 1
	return Chrome{
		kit:       k,
		rows:      max(0, lipgloss.Height(k.Panel("probe"))-probeRows-leadingBlank),
		wrapWidth: k.PanelContentWidth(),
	}
}

// Lines charges the rows a set of already-rendered chrome blocks occupies.
//
// The blocks are the same strings the screen joins into its body, so the tally
// and the render can never disagree. An empty string costs one row, because
// that is what an empty element costs inside a strings.Join — the blank spacer
// row between a kicker and a rule is real chrome.
//
// A block is charged the rows it occupies AFTER the frame has wrapped it, not
// the rows it occupies as a string. A panel clamps a body wider than its
// content width, so an over-long kicker or hint arrives on screen as two rows;
// charging it one would hand the data window a row the chrome has already
// spent, and the body would paint past the bottom of the terminal — the exact
// overdraw this type exists to prevent, arriving through the horizontal axis
// instead of the vertical one.
func (c Chrome) Lines(blocks ...string) Chrome {
	for _, block := range blocks {
		c.rows += lipgloss.Height(c.wrapped(block))
	}
	return c
}

// wrapped returns the block as the frame will paint it. The wrap is performed
// by lipgloss itself rather than re-derived, so the tally cannot drift from the
// rendering: this is the same engine, and the same width, that PanelBox hands
// to Style.Width.
func (c Chrome) wrapped(block string) string {
	if c.wrapWidth <= 0 || lipgloss.Width(block) <= c.wrapWidth {
		return block
	}
	return lipgloss.NewStyle().Width(c.wrapWidth).Render(block)
}

// Box charges the vertical chrome a lipgloss style adds around its content —
// border edges plus vertical padding — for a screen that frames its body in a
// style other than Kit.Panel.
//
// Measured the same way PanelChrome measures Panel: render a one-row body and
// take the difference.
func (c Chrome) Box(style lipgloss.Style) Chrome { return c.Around(style.Render("probe")) }

// Around charges the chrome a renderer draws around a body, given what that
// renderer produced for a ONE-ROW body. For a component whose frame is not a
// lipgloss style — a multiline form that draws its own border, say — this is
// the same probe technique Box uses, with the probe render supplied.
func (c Chrome) Around(oneRowRender string) Chrome {
	const probeRows = 1
	c.rows += max(0, lipgloss.Height(oneRowRender)-probeRows)
	return c
}

// Rows is the tally: the terminal rows this body spends on its own chrome.
func (c Chrome) Rows() int { return c.rows }

// ViewportRows is the row budget the scrollable data window gets — the host
// budget minus the chrome measured here. Same contract as Kit.ViewportRows:
// zero on a terminal too small to be worth scrolling, never negative.
func (c Chrome) ViewportRows() int { return c.kit.ViewportRows(c.rows) }
