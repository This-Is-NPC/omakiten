package screenlayout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// occupyLine makes one terminal row occupy exactly `width` cells, or at most
// `width` when pad is false.
//
// Truncate is the occupancy of a section that painted past its canvas: the
// bytes may say whatever they like, the cells they get are the arranger's.
// Pad is the occupancy of a side-by-side join: a short hint in the left
// column must still own its full column, or JoinHorizontal places the next
// column's pipes on the same row as the hint.
func occupyLine(line string, width int, pad bool) string {
	if width <= 0 {
		return ""
	}
	visible := lipgloss.Width(line)
	if visible > width {
		return screenkit.TruncateStyled(line, width)
	}
	if pad && visible < width {
		return screenkit.PadRight(line, width)
	}
	return line
}

func occupyLines(lines []string, width int, pad bool) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = occupyLine(line, width, pad)
	}
	return out
}

// joinPaintedColumns occupies each column and joins them. Non-last columns are
// padded to width+gap so the next column starts at a stable x, even when a
// row is a short scroll hint. The last column is truncated to its width and
// left unpadded: trailing spaces on every row of a side-by-side body paint as
// a block of theme background.
func joinPaintedColumns(painted []paintedColumn, gap int) []string {
	if len(painted) == 0 {
		return nil
	}
	columns := make([]string, 0, len(painted))
	last := len(painted) - 1
	for i, col := range painted {
		width := col.width
		pad := false
		if i < last {
			width += gap
			pad = true
		}
		columns = append(columns, strings.Join(occupyLines(col.lines, width, pad), "\n"))
	}
	joined := strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, columns...), "\n")
	for i, line := range joined {
		// JoinHorizontal squares every column off against the tallest, so a
		// short column contributes a run of spaces to the end of each line
		// past its content. Trimming the tail is safe — a styled run ends in
		// its reset sequence, not in a space — and leaving it in would paint
		// a block of theme background out to the right edge on every row a
		// column does not reach.
		joined[i] = strings.TrimRight(line, " ")
	}
	return joined
}

// sideBySideOverflows reports whether the UNFITTED painted columns would
// occupy more cells than the box, gaps included. "Fits" is measured from the
// strings the sections produced, not from the declared MinWidth that chose
// the breakpoint. A SideBySide frame that fails this is restacked.
func sideBySideOverflows(laid []resolved, columns [][]int, gap, boxWidth int) bool {
	if boxWidth <= 0 {
		return true
	}
	total := 0
	painted := 0
	for _, members := range columns {
		colW, any := occupiedColumnWidth(laid, members)
		if !any {
			continue
		}
		if painted > 0 {
			total += gap
		}
		total += colW
		painted++
	}
	return total > boxWidth
}

func occupiedColumnWidth(laid []resolved, members []int) (int, bool) {
	colW := 0
	any := false
	for _, i := range members {
		if i < 0 || i >= len(laid) || laid[i].dropped {
			continue
		}
		any = true
		for _, line := range laid[i].lines {
			if w := lipgloss.Width(line); w > colW {
				colW = w
			}
		}
	}
	return colW, any
}

func linesOverflow(lines []string, width int) bool {
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			return true
		}
	}
	return false
}

// clampView is the last occupancy gate: no line wider than the box, no more
// rows than the box. Truncation here is fail-closed for a join that still
// overdrew after occupy; it is not the layout.
func clampView(lines []string, width, rows int) []string {
	if rows <= 0 || width < 0 {
		return nil
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = occupyLine(line, width, false)
	}
	return out
}
