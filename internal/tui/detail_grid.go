package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// detailGridLabelWidth is the column width used by every detail screen so
// the `// LABEL` cells line up across views — keep this in sync with any
// custom value passed to renderGridTable for narrow-mode fallbacks.
const detailGridLabelWidth = 13

// detailGrid is a small builder over renderGridTable for the kicker + label
// rows + body pattern used by every detail screen (task view, comment
// screen, and any future "show one entity in two columns" layout). Methods
// chain so callers read top-to-bottom in the order the user sees them on
// screen — the spanned/two-column distinction is implicit in which method
// you call instead of being a bool flag at every site.
type detailGrid struct {
	rows   [][]string
	labelW int
	valueW int
	styles styles
}

// newDetailGrid starts a fresh grid scoped to the given value-column width.
// labelW is the standard 13-cell `// LABEL` column; pass a custom valueW
// based on the available content area minus borders and the label gutter.
func (m Model) newDetailGrid(valueW int) *detailGrid {
	return &detailGrid{
		labelW: detailGridLabelWidth,
		valueW: valueW,
		styles: m.styles,
	}
}

// Kicker appends a section-header row (`// LABEL` styled with the kicker
// secondary). The row spans both columns because the kicker doubles as a
// visual divider between groups of label rows.
func (g *detailGrid) Kicker(label string) *detailGrid {
	g.rows = append(g.rows, []string{g.styles.kicker(label)})
	return g
}

// KickerCount appends a kicker with a trailing count, e.g. `// BLOCKERS · 3`.
// Useful for sections with a variable number of follow-up rows.
func (g *detailGrid) KickerCount(label string, count int) *detailGrid {
	g.rows = append(g.rows, []string{g.styles.kickerCount(label, count)})
	return g
}

// Row appends a `// LABEL` + value pair, the canonical detail row.
func (g *detailGrid) Row(label, value string) *detailGrid {
	g.rows = append(g.rows, []string{g.styles.info.Render("// " + strings.ToUpper(label)), value})
	return g
}

// Span appends a single-cell row that covers the full grid width — used
// for body text, hints, and any content that doesn't fit the two-column
// label/value layout. renderGridTable detects single-cell rows and skips
// the internal vertical divider so the spanned content reads as a block.
func (g *detailGrid) Span(content string) *detailGrid {
	g.rows = append(g.rows, []string{content})
	return g
}

// Custom appends a pre-rendered kicker (the caller already chose between
// kicker and kickerFocused based on focus state). Avoids leaking the
// "focused?" flag into the builder API for the one screen that needs it.
func (g *detailGrid) Custom(kicker string) *detailGrid {
	g.rows = append(g.rows, []string{kicker})
	return g
}

// Render lays out the accumulated rows through renderGridTable using the
// builder's column widths and the caller-supplied border style.
func (g *detailGrid) Render(border lipgloss.Style) string {
	return renderGridTable(g.rows, []int{g.labelW, g.valueW}, border)
}

// detailKickerWithID is shorthand for the very common `Kicker · #ID` form.
// Used by the comment screen for both the "found" and "not found" paths so
// the header text is identical regardless of whether the comment loaded.
func (m Model) detailKickerWithID(label string, id int64) string {
	return m.styles.kicker(fmt.Sprintf("%s · #%d", label, id))
}
