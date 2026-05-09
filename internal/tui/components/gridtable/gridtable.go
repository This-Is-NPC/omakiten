// Package gridtable renders bordered multi-row, multi-column tables
// with shared junction glyphs (┌┬┐ ├┼┤ └┴┘). It owns three primitives
// — Render, WrapLines, PadLine — that several TUI surfaces (panel
// summary tables, detail screens) used to copy/paste.
//
// The package is a leaf: it imports lipgloss and x/ansi only, never
// the parent tui package or any sibling adapter. Callers from
// internal/tui and internal/tui/components/detailscreen share the same
// rendering policy through this single implementation.
package gridtable

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Render produces a multi-row, multi-column bordered table. Each row
// must have len(widths) cells; missing trailing cells render as empty.
// A row with a single cell (when n>1) is treated as a spanned row
// covering the full width — surrounding horizontal dividers omit the
// internal junction so the spanned content reads as a contiguous block.
func Render(rows [][]string, widths []int, border lipgloss.Style) string {
	n := len(widths)
	if len(rows) == 0 || n == 0 {
		return ""
	}

	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += n - 1

	spanned := make([]bool, len(rows))
	rowLines := make([][][]string, len(rows))
	rowHeights := make([]int, len(rows))
	for r, row := range rows {
		if n > 1 && len(row) == 1 {
			spanned[r] = true
			lines := WrapLines(strings.Split(row[0], "\n"), totalWidth)
			rowLines[r] = [][]string{lines}
			rowHeights[r] = len(lines)
			continue
		}
		cells := make([][]string, n)
		h := 0
		for c := 0; c < n; c++ {
			text := ""
			if c < len(row) {
				text = row[c]
			}
			lines := WrapLines(strings.Split(text, "\n"), widths[c])
			cells[c] = lines
			if len(lines) > h {
				h = len(lines)
			}
		}
		for c := 0; c < n; c++ {
			for len(cells[c]) < h {
				cells[c] = append(cells[c], "")
			}
		}
		rowLines[r] = cells
		rowHeights[r] = h
	}

	horizontal := func(left, right string, aboveSpanned, belowSpanned bool) string {
		var b strings.Builder
		b.WriteString(border.Render(left))
		for i, w := range widths {
			b.WriteString(border.Render(strings.Repeat("─", w)))
			if i < n-1 {
				var junc string
				switch {
				case aboveSpanned && belowSpanned:
					junc = "─"
				case aboveSpanned && !belowSpanned:
					junc = "┬"
				case !aboveSpanned && belowSpanned:
					junc = "┴"
				default:
					junc = "┼"
				}
				b.WriteString(border.Render(junc))
			}
		}
		b.WriteString(border.Render(right))
		return b.String()
	}

	bar := border.Render("│")
	var out strings.Builder
	out.WriteString(horizontal("┌", "┐", true, spanned[0]))
	for r, h := range rowHeights {
		for line := 0; line < h; line++ {
			out.WriteString("\n")
			out.WriteString(bar)
			if spanned[r] {
				out.WriteString(PadLine(rowLines[r][0][line], totalWidth))
			} else {
				for c, w := range widths {
					out.WriteString(PadLine(rowLines[r][c][line], w))
					if c < n-1 {
						out.WriteString(bar)
					}
				}
			}
			out.WriteString(bar)
		}
		if r < len(rows)-1 {
			out.WriteString("\n")
			out.WriteString(horizontal("├", "┤", spanned[r], spanned[r+1]))
		}
	}
	out.WriteString("\n")
	out.WriteString(horizontal("└", "┘", spanned[len(rows)-1], true))
	return out.String()
}

// WrapLines splits each input line at width using ANSI-aware soft
// wrapping. Empty input returns a single empty line so callers can
// rely on at least one row to render.
func WrapLines(lines []string, width int) []string {
	if width <= 0 {
		width = 1
	}

	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if lipgloss.Width(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}

		parts := strings.Split(ansi.Wrap(line, width, " "), "\n")
		wrapped = append(wrapped, parts...)
	}

	if len(wrapped) == 0 {
		return []string{""}
	}
	return wrapped
}

// PadLine right-pads a styled (potentially ANSI-coloured) line so its
// visible width equals width. No-op when the line is already wider.
func PadLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}
