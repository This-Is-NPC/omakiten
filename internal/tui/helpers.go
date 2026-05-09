package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func truncateText(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// wrapWords breaks s into lines where the first line is constrained to firstWidth
// and subsequent lines to restWidth. It tries to keep whole words.
func wrapWords(s string, firstWidth, restWidth int) []string {
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		limit := firstWidth
		if len(lines) > 0 {
			limit = restWidth
		}
		if lipgloss.Width(current+" "+word) <= limit {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	lines = append(lines, current)
	return lines
}

func renderFixedBox(lines []string, width int, border lipgloss.Style) string {
	rows := []string{border.Render("┌" + strings.Repeat("─", width) + "┐")}
	for _, line := range lines {
		rows = append(rows, border.Render("│")+padStyledLine(line, width)+border.Render("│"))
	}
	rows = append(rows, border.Render("└"+strings.Repeat("─", width)+"┘"))
	return strings.Join(rows, "\n")
}

// renderGridTable renders rows in a multi-row, multi-column table where every
// cell is delimited by ─ and │ with shared junctions (┌┬┐ ├┼┤ └┴┘). Each row
// must have len(widths) cells; missing trailing cells render as empty. A row
// with a single cell when n>1 is treated as a spanned row that covers the full
// width, and the surrounding horizontal dividers omit the internal junction.
func renderGridTable(rows [][]string, widths []int, border lipgloss.Style) string {
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
			lines := wrapLinesToWidth(strings.Split(row[0], "\n"), totalWidth)
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
			lines := wrapLinesToWidth(strings.Split(text, "\n"), widths[c])
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
				out.WriteString(padStyledLine(rowLines[r][0][line], totalWidth))
			} else {
				for c, w := range widths {
					out.WriteString(padStyledLine(rowLines[r][c][line], w))
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

func wrapLinesToWidth(lines []string, width int) []string {
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

func padStyledLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}

func clampInt(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func indentBlock(block string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}
