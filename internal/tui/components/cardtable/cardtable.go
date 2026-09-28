// Package cardtable joins already-painted cards into a grid: rows with a
// one-column gutter, a column count from cell width, and the 1D-to-row map.
//
// It is layout math, not a tea.Model. The screen still paints each card (via
// card.Painter.Render) and owns 2D keyboard motion; this package only joins
// and measures what it is handed. It does not import card.
package cardtable

import "github.com/charmbracelet/lipgloss"

// Row joins already-painted cards left-to-right, top-aligned, one-space gutter.
func Row(cards []string) string {
	if len(cards) == 0 {
		return ""
	}
	pieces := make([]string, 0, len(cards)*2-1)
	for i, painted := range cards {
		if i > 0 {
			pieces = append(pieces, " ")
		}
		pieces = append(pieces, painted)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, pieces...)
}

// RowHeight is the tallest card in the row (at least 1).
func RowHeight(cards []string) int {
	height := 1
	for _, painted := range cards {
		if h := lipgloss.Height(painted); h > height {
			height = h
		}
	}
	return height
}

// Cols is how many cells of cellWidth fit in width with a 1-col gutter between
// them. Always at least 1, matching the entity-list grid that used to own this
// arithmetic: a terminal narrower than one cell still shows one column.
func Cols(width, cellWidth int) int {
	cols := (width + 1) / (cellWidth + 1)
	if cols < 1 {
		return 1
	}
	return cols
}

// Layout splits cards into rows of cols and returns joined rows plus per-row
// heights. cols < 1 is treated as 1.
func Layout(cards []string, cols int) (rows []string, heights []int) {
	if cols < 1 {
		cols = 1
	}
	if len(cards) == 0 {
		return nil, nil
	}
	count := (len(cards) + cols - 1) / cols
	rows, heights = make([]string, count), make([]int, count)
	for row := 0; row < count; row++ {
		start := row * cols
		end := start + cols
		if end > len(cards) {
			end = len(cards)
		}
		chunk := cards[start:end]
		rows[row] = Row(chunk)
		heights[row] = RowHeight(chunk)
	}
	return rows, heights
}
