// Package panel owns the framed body every screen sits inside, and the three
// glyphs that frame is made of: the bordered box, the horizontal rule, and the
// selection chevron.
//
// Four surfaces had each written some of this — the root's renderPanel / hRule /
// renderFixedBox, stats' and logs' panel assemblers, and the plan network's
// cursorChevron — and the copies agreed on the shape while disagreeing on the
// details. Collapsing them here keeps the leading-blank / indent / border
// contract in one place so a future tweak (changing the indent, swapping the
// rule glyph) lands everywhere at once.
//
// The package is a leaf. It takes lipgloss styles and widths; it imports nothing
// from tui or any screen. screenkit.Kit.Panel / HRule copy this package's
// indent and HRule algorithms so the Kit does not import this package.
package panel

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FixedBox draws a manually-bordered box of exactly `width` content columns.
// One top border row, one row per content line, one bottom border row — total
// height is always len(lines)+2. Used by the activity rail, where lipgloss
// Height would pad rather than size to the content.
//
// The vertical edge is painted once for the whole box. Every content row gets
// the same two glyphs, and a Render call resolves the style into terminal codes
// before it paints, so asking for `│` inside the loop bought 2n resolutions of
// one unchanging string. The glyph is hoisted; the bytes are the bytes the
// per-row paint produced.
func FixedBox(lines []string, width int, border lipgloss.Style) string {
	edge := border.Render("│")
	rows := make([]string, 0, len(lines)+Borders)
	rows = append(rows, border.Render("┌"+strings.Repeat("─", width)+"┐"))
	for _, line := range lines {
		rows = append(rows, edge+padInner(line, width)+edge)
	}
	rows = append(rows, border.Render("└"+strings.Repeat("─", width)+"┘"))
	return strings.Join(rows, "\n")
}

// Borders is the row cost of the top and bottom border a framed panel wraps
// around its body.
//
// It matters because lipgloss `Style.Height(n)` treats n as the INNER content
// rows and stacks the border outside it, while every budget in this codebase is
// expressed in TOTAL rows. A caller that hands a total straight to `.Height`
// renders two rows taller than it asked for, which is the defect
// TestKanbanColumnSizedTotalRowsMatchBudget pins.
//
// It lives here, with the package that owns the frame, rather than in a layout
// package a screen has to know about: the number is a property of the chrome
// this package draws.
const Borders = 2

// FixedBoxHeight is the terminal rows FixedBox would emit for n content lines.
// It is the formula FixedBox itself implements (n+Borders), not a second
// spelling of the border arithmetic — callers that budget a rail before
// painting it ask here so the number and the paint cannot drift.
func FixedBoxHeight(contentLines int) int {
	if contentLines < 0 {
		contentLines = 0
	}
	return contentLines + Borders
}

// HRule renders a horizontal rule of `width` columns in the separator style —
// the kicker/separator/body sandwich every panel uses.
//
// A non-positive width renders nothing. Surfaces derive their rule width by
// subtracting panel chrome from AvailableWidth, and on a terminal too narrow to
// hold that chrome the subtraction goes negative — which strings.Repeat panics
// on. The guard lives here, in the one place that owns the glyph.
func HRule(separator lipgloss.Style, width int) string {
	if width <= 0 {
		return ""
	}
	return separator.Render(strings.Repeat("─", width))
}

// Top paints the top edge of a framed section (`┌─┐`) in the box border.
func Top(border lipgloss.Style, inner int) string {
	return border.Render("┌" + strings.Repeat("─", innerWidth(inner)) + "┐")
}

// Join paints the kicker rule so it meets the box sides (`├─┤`). Screens that
// wrap HRule through a box instead get `│────│` and lose focused-kicker SGR.
func Join(border lipgloss.Style, inner int) string {
	return border.Render("├" + strings.Repeat("─", innerWidth(inner)) + "┤")
}

// Bottom paints the bottom edge of a framed section (`└─┘`) in the box border.
func Bottom(border lipgloss.Style, inner int) string {
	return border.Render("└" + strings.Repeat("─", innerWidth(inner)) + "┘")
}

// WrapLine paints one inner row: │ + PadRight (SGR kept) + │. Arrangers hand
// this to Block.Chrome so ▲/▼ hints sit inside the column.
func WrapLine(border lipgloss.Style, inner int) func(string) string {
	inner = innerWidth(inner)
	edge := border.Render("│")
	return func(line string) string {
		return edge + padInner(line, inner) + edge
	}
}

// WrapBlock wraps every line of a (possibly multi-line) item through WrapLine
// without flattening item boundaries.
func WrapBlock(border lipgloss.Style, inner int) func(string) string {
	wrap := WrapLine(border, inner)
	return func(item string) string {
		lines := strings.Split(item, "\n")
		for i, line := range lines {
			lines[i] = wrap(line)
		}
		return strings.Join(lines, "\n")
	}
}

// Frame is the Task Detail / Studio section box: top, wrapped kicker, joining
// rule, wrap for items, bottom. Header and footer are already full-width rows.
func Frame(border lipgloss.Style, inner int, kicker string) (header, footer []string, wrap func(string) string) {
	inner = innerWidth(inner)
	wrap = WrapBlock(border, inner)
	header = []string{Top(border, inner)}
	if kicker != "" {
		header = append(header, wrap(kicker))
	}
	header = append(header, Join(border, inner))
	return header, []string{Bottom(border, inner)}, wrap
}

func innerWidth(inner int) int {
	if inner < 1 {
		return 1
	}
	return inner
}

func padInner(line string, width int) string {
	if width < 1 {
		width = 1
	}
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	visible := lipgloss.Width(line)
	if visible < width {
		line += strings.Repeat(" ", width-visible)
	}
	return line
}

// Chevron returns the accent-styled `› ` selection glyph when selected,
// otherwise "". Distinct from screenkit.CursorMarker (`▌` / spacer): the plan
// network and the card cursor want the chevron with its trailing space, and
// conflating the two would silently swap glyphs on one of them.
func Chevron(accent lipgloss.Style, selected bool) string {
	if !selected {
		return ""
	}
	return accent.Render("›") + " "
}
