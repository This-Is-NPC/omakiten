// Package gridtable renders bordered multi-row, multi-column tables
// with shared junction glyphs (┌┬┐ ├┼┤ └┴┘). Render is the cell
// primitive; Summaries is the responsive key/value policy; Detail is
// the fluent two-column builder. WrapLines and PadLine are the cell
// helpers several TUI surfaces used to copy/paste.
//
// The package is a leaf: it imports presentation primitives only, never
// config, domain, app, sqlite, the parent tui package, or any sibling
// adapter. Screens translate domain into raw rows; framework-painted cells
// use the explicit Cell/Styled path.
package gridtable

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Cell is one gridtable cell. Raw cells are sanitized at the terminal boundary;
// styled cells are reserved for framework output whose source text was already
// sanitized before Lipgloss rendered it.
type Cell struct {
	value  string
	styled bool
}

// Raw creates the default, unstyled cell. Terminal controls, including SGR,
// are removed before this cell is measured, truncated, wrapped, or painted.
func Raw(value string) Cell { return Cell{value: value} }

// Styled creates a trusted cell for framework-rendered output. Callers must
// sanitize any external data before composing and styling the value.
func Styled(value string) Cell { return Cell{value: value, styled: true} }

// CellWidth reports the visible width using the same trusted/raw policy as a
// grid render.
func CellWidth(cell Cell) int { return lipgloss.Width(renderedText(cell)) }

// RawRows converts ordinary rows to raw cells for APIs that need mixed cells.
func RawRows(rows [][]string) [][]Cell {
	converted := make([][]Cell, len(rows))
	for i, row := range rows {
		converted[i] = make([]Cell, len(row))
		for j, value := range row {
			converted[i][j] = Raw(value)
		}
	}
	return converted
}

// Layout reports where each input row landed in the rendered table.
//
// It exists because the mapping from an input row to a rendered line is
// NOT a formula. Borders and dividers contribute fixed chrome lines, but
// WrapLines gives each row a height that depends on the column widths, so
// the same row sits on a different line at a different terminal width. A
// caller that wants to point a cursor or a scroll anchor at a row must be
// told where the row is; every attempt to compute it has been wrong at
// some width.
type Layout struct {
	// RowOffsets[r] is the zero-based index, into the lines of the
	// rendered string, of the FIRST content line of input row r. It never
	// points at a border or a divider.
	RowOffsets []int
	// RowHeights[r] is how many consecutive content lines row r occupies,
	// always at least 1. Row r therefore covers
	// [RowOffsets[r], RowOffsets[r]+RowHeights[r]).
	RowHeights []int
	// Lines is the total number of lines in the rendered string.
	Lines int
}

// RowLine returns the first content line of the given input row. ok is
// false for an out-of-range row (and for the zero Layout an empty render
// produces), so callers never index the slices themselves.
func (l Layout) RowLine(row int) (int, bool) {
	if row < 0 || row >= len(l.RowOffsets) {
		return 0, false
	}
	return l.RowOffsets[row], true
}

// FitWidths shrinks natural column widths so their sum fits available, never
// reducing any column below minWidth. While the total overflows, the currently
// widest column loses one cell — once two columns tie they shrink together.
// The same path FormatRow and bordered Render callers use to stay inside a
// panel, so a table that asked "will I fit?" before painting cannot disagree
// with the paint.
//
// available is the budget for the DATA cells only (no gaps, no borders). Callers
// that join cells with spaces subtract those gaps first; callers that draw a
// bordered gridtable subtract the │ chrome first.
func FitWidths(natural []int, available, minWidth int) []int {
	if minWidth < 1 {
		minWidth = 1
	}
	out := make([]int, len(natural))
	copy(out, natural)
	if len(out) == 0 {
		return out
	}
	sum := func() int {
		total := 0
		for _, w := range out {
			total += w
		}
		return total
	}
	if available < minWidth*len(out) {
		available = minWidth * len(out)
	}
	for sum() > available {
		idx, widest := -1, minWidth-1
		for i, w := range out {
			if w > widest {
				widest, idx = w, i
			}
		}
		if idx < 0 || out[idx] <= minWidth {
			break
		}
		out[idx]--
	}
	return out
}

// FormatRow joins cells into a space-separated row at the given widths. Each
// cell is truncated then PadLine'd so columns stay aligned across rows. Width
// and FormatRow share Truncate+PadLine so a measured row and a painted row
// cannot disagree about cell boundaries.
func FormatRow(cells []string, widths []int) string {
	return FormatRowAligned(cells, widths, nil)
}

// FormatRowAligned is FormatRow with optional per-column right alignment.
// right[i] == true puts the padding on the left (after truncate), which is how
// numeric columns stay flush when FitWidths shrinks them.
func FormatRowAligned(cells []string, widths []int, right []bool) string {
	parts := make([]string, len(widths))
	for i, w := range widths {
		text := ""
		if i < len(cells) {
			text = sanitizeCell(cells[i])
		}
		text = Truncate(text, w)
		if i < len(right) && right[i] {
			parts[i] = padLeft(text, w)
			continue
		}
		parts[i] = PadLine(text, w)
	}
	return strings.Join(parts, " ")
}

func padLeft(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return strings.Repeat(" ", width-visible) + line
}

// Truncate cuts s to at most width display cells, appending an ellipsis when
// it had to. Width is measured in cells (CJK/emoji count as two). Empty when
// width is non-positive.
func Truncate(s string, width int) string {
	s = sanitizeCell(s)
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	budget := width - 1
	if budget < 0 {
		return ""
	}
	used := 0
	var b strings.Builder
	for _, r := range s {
		w := ansi.StringWidth(string(r))
		if used+w > budget {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// Render produces a multi-row, multi-column bordered table. Each row
// must have len(widths) cells; missing trailing cells render as empty.
// A row with a single cell (when n>1) is treated as a spanned row
// covering the full width — surrounding horizontal dividers omit the
// internal junction so the spanned content reads as a contiguous block.
//
// Callers that also need to know which line a row landed on call
// RenderWithLayout instead; this wrapper stays for the majority that
// only render.
func Render(rows [][]string, widths []int, border lipgloss.Style) string {
	out, _ := RenderCellsWithLayout(RawRows(rows), widths, border)
	return out
}

// RenderWithLayout renders exactly what Render renders and additionally
// reports where every input row landed. The two share one implementation
// so the layout can never drift from the string it describes.
func RenderWithLayout(rows [][]string, widths []int, border lipgloss.Style) (string, Layout) {
	return RenderCellsWithLayout(RawRows(rows), widths, border)
}

// RenderCells renders a table containing raw and explicitly trusted cells.
// Prefer Render for ordinary rows; this entry point is only for framework
// output that must retain its intended styling.
func RenderCells(rows [][]Cell, widths []int, border lipgloss.Style) string {
	out, _ := RenderCellsWithLayout(rows, widths, border)
	return out
}

// RenderCellsWithLayout is RenderCells with the row-to-line layout report.
func RenderCellsWithLayout(rows [][]Cell, widths []int, border lipgloss.Style) (string, Layout) {
	n := len(widths)
	if len(rows) == 0 || n == 0 {
		return "", Layout{}
	}

	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += n - 1

	prepared := prepareGridRows(rows, widths, totalWidth)
	return renderGridRows(widths, border, totalWidth, prepared)
}

type preparedGridRows struct {
	spanned    []bool
	rowLines   [][][]renderedCell
	rowHeights []int
}

type renderedCell struct {
	text   string
	styled bool
}

func prepareGridRows(rows [][]Cell, widths []int, totalWidth int) preparedGridRows {
	n := len(widths)
	prepared := preparedGridRows{spanned: make([]bool, len(rows)), rowLines: make([][][]renderedCell, len(rows)), rowHeights: make([]int, len(rows))}
	for r, row := range rows {
		if n > 1 && len(row) == 1 {
			prepared.spanned[r] = true
			lines := wrapCell(row[0], totalWidth)
			prepared.rowLines[r] = [][]renderedCell{lines}
			prepared.rowHeights[r] = len(lines)
			continue
		}
		prepared.rowLines[r], prepared.rowHeights[r] = prepareGridCells(row, widths)
	}
	return prepared
}

func prepareGridCells(row []Cell, widths []int) ([][]renderedCell, int) {
	cells := make([][]renderedCell, len(widths))
	height := 0
	for c, width := range widths {
		cell := Raw("")
		if c < len(row) {
			cell = row[c]
		}
		cells[c] = wrapCell(cell, width)
		if len(cells[c]) > height {
			height = len(cells[c])
		}
	}
	for c := range cells {
		for len(cells[c]) < height {
			cells[c] = append(cells[c], renderedCell{})
		}
	}
	return cells, height
}

func renderGridRows(widths []int, border lipgloss.Style, totalWidth int, prepared preparedGridRows) (string, Layout) {
	bar := border.Render("│")
	var out strings.Builder
	horizontal := func(left, right string, aboveSpanned, belowSpanned bool) string {
		junc := "┼"
		switch {
		case aboveSpanned && belowSpanned:
			junc = "─"
		case aboveSpanned:
			junc = "┬"
		case belowSpanned:
			junc = "┴"
		}
		var line strings.Builder
		line.WriteString(left)
		for i, width := range widths {
			line.WriteString(strings.Repeat("─", width))
			if i < len(widths)-1 {
				line.WriteString(junc)
			}
		}
		line.WriteString(right)
		return border.Render(line.String())
	}
	out.WriteString(horizontal("┌", "┐", true, prepared.spanned[0]))
	emitted := 0
	offsets := make([]int, len(prepared.rowLines))
	for r, height := range prepared.rowHeights {
		offsets[r] = emitted + 1
		emitted += renderGridRow(&out, bar, widths, totalWidth, prepared, r, height)
		if r < len(prepared.rowLines)-1 {
			out.WriteString("\n")
			out.WriteString(horizontal("├", "┤", prepared.spanned[r], prepared.spanned[r+1]))
			emitted++
		}
	}
	out.WriteString("\n")
	out.WriteString(horizontal("└", "┘", prepared.spanned[len(prepared.rowLines)-1], true))
	emitted++
	return out.String(), Layout{RowOffsets: offsets, RowHeights: prepared.rowHeights, Lines: emitted + 1}
}

func renderGridRow(out *strings.Builder, bar string, widths []int, totalWidth int, prepared preparedGridRows, row, height int) int {
	for line := 0; line < height; line++ {
		out.WriteString("\n")
		out.WriteString(bar)
		if prepared.spanned[row] {
			out.WriteString(padRendered(prepared.rowLines[row][0][line], totalWidth))
		} else {
			for c, width := range widths {
				out.WriteString(padRendered(prepared.rowLines[row][c][line], width))
				if c < len(widths)-1 {
					out.WriteString(bar)
				}
			}
		}
		out.WriteString(bar)
	}
	return height
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
		line = sanitizeCell(line)
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
	line = sanitizeCell(line)
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}

func renderedText(cell Cell) string {
	if cell.styled {
		return cell.value
	}
	return sanitizeCell(cell.value)
}

func wrapCell(cell Cell, width int) []renderedCell {
	if width <= 0 {
		width = 1
	}
	text := renderedText(cell)
	var wrapped []renderedCell
	for _, line := range strings.Split(text, "\n") {
		parts := []string{line}
		if lipgloss.Width(line) > width {
			parts = strings.Split(ansi.Wrap(line, width, " "), "\n")
		}
		for _, part := range parts {
			wrapped = append(wrapped, renderedCell{text: part, styled: cell.styled})
		}
	}
	return wrapped
}

func padRendered(cell renderedCell, width int) string {
	visible := lipgloss.Width(cell.text)
	if visible >= width {
		return cell.text
	}
	return cell.text + strings.Repeat(" ", width-visible)
}

// sanitizeCell removes terminal controls from raw content while retaining LF
// separators for the multiline cell API. Unlike the old only-SGR exception,
// raw cells never retain caller-supplied styling.
func sanitizeCell(value string) string {
	if !hasControl(value) {
		return value
	}
	var out strings.Builder
	for i := 0; i < len(value); {
		if next, handled := skipRawC1(value, i, &out); handled {
			i = next
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if next, handled := skipRuneControl(value, i, r, size, &out); handled {
			i = next
			continue
		}
		if r == '\n' {
			out.WriteByte('\n')
		} else if size == 1 && r == utf8.RuneError {
			// Drop malformed bytes rather than making them visible terminal data.
		} else if !unicode.IsControl(r) && r != '\u007f' {
			out.WriteString(value[i : i+size])
		}
		i += size
	}
	return out.String()
}

func hasControl(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f || value[i] >= 0x80 && value[i] <= 0x9f {
			return true
		}
	}
	return false
}

func skipRawC1(value string, i int, out *strings.Builder) (int, bool) {
	if value[i] < 0x80 || value[i] > 0x9f {
		return i, false
	}
	switch value[i] {
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return skipStringControl(value, i+1, out), true
	case 0x9b:
		return skipCSI(value, i+1, out), true
	default:
		return i + 1, true
	}
}

func skipRuneControl(value string, i int, r rune, size int, out *strings.Builder) (int, bool) {
	switch r {
	case '\x1b':
		return skipEscape(value, i+size, out), true
	case '\u0090', '\u0098', '\u009d', '\u009e', '\u009f':
		return skipStringControl(value, i+size, out), true
	case '\u009b':
		return skipCSI(value, i+size, out), true
	case '\u009c':
		return i + size, true
	default:
		return i, false
	}
}

func skipEscape(value string, i int, out *strings.Builder) int {
	if i >= len(value) {
		return i
	}
	switch value[i] {
	case '[':
		return skipCSI(value, i+1, out)
	case ']', 'P', '^', '_':
		return skipStringControl(value, i+1, out)
	}
	j := i
	for j < len(value) && value[j] >= 0x20 && value[j] <= 0x2f {
		j++
	}
	if j > i {
		if j < len(value) && value[j] >= 0x30 && value[j] <= 0x7e {
			return j + 1
		}
		return j
	}
	if value[i] >= 0x30 && value[i] <= 0x7e {
		return i + 1
	}
	r, size := utf8.DecodeRuneInString(value[i:])
	if r == '\n' {
		out.WriteByte('\n')
		return i + size
	}
	return i
}

func skipCSI(value string, i int, out *strings.Builder) int {
	for i < len(value) {
		r, size := utf8.DecodeRuneInString(value[i:])
		if r >= 0x40 && r <= 0x7e {
			return i + size
		}
		if r == '\n' {
			out.WriteByte('\n')
		}
		i += size
	}
	return i
}

func skipStringControl(value string, i int, out *strings.Builder) int {
	for i < len(value) {
		if next, terminated := stringControlTerminator(value, i); terminated {
			return next
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == '\n' {
			out.WriteByte('\n')
		}
		i += size
	}
	return i
}

func stringControlTerminator(value string, i int) (int, bool) {
	if value[i] == 0x9c {
		return i + 1, true
	}
	r, size := utf8.DecodeRuneInString(value[i:])
	switch r {
	case '\a', '\u009c':
		return i + size, true
	case '\x1b':
		next := i + size
		if next < len(value) && value[next] == '\\' {
			return next + 1, true
		}
	}
	return i, false
}
