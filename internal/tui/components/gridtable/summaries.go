package gridtable

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Options configures Summaries. SideBySide opts into the "wide enough → horizontal"
// layout used by Stats and Logs; without it, tables stack vertically.
// MergeNarrow folds all rows into a single table when the panel is too narrow
// for even one stacked table at the requested width — Stats/Logs use this to
// avoid clipping. Settings keeps tables separate and only narrows columns.
//
// Auto switches the label/value sizing from the option constants to a scan of
// the supplied rows: the label column expands to the widest rendered kicker,
// and the value column expands to fill the remaining panel width. LabelWidth /
// ValueWidth are treated as minimums in Auto mode.
type Options struct {
	LabelWidth  int
	ValueWidth  int
	SideBySide  bool
	MergeNarrow bool
	Auto        bool
}

// Rows builds typed cells suitable for Summaries from a titled list of
// label/value pairs. kicker styles each label (typically Styles.Kicker). The
// first row is a single-cell title that RenderCells spans across the full grid
// width — the same chrome Detail.Kicker paints. Each subsequent row is
// label + value. Callers may append extra pre-rendered rows to the result.
func Rows(kicker func(string) string, title string, fields ...[2]string) [][]Cell {
	if kicker == nil {
		kicker = func(s string) string { return s }
	}
	rows := make([][]Cell, 0, len(fields)+1)
	rows = append(rows, []Cell{Styled(kicker(sanitizeCell(title)))})
	for _, f := range fields {
		rows = append(rows, []Cell{Styled(kicker(sanitizeCell(f[0]))), Raw(f[1])})
	}
	return rows
}

// MaxLabelWidth scans the first cell of every two-column row across all
// supplied tables and returns the widest rendered label. Single-cell rows are
// spanned headers — they do not bound the label column.
func MaxLabelWidth(tables [][][]Cell) int {
	widest := 0
	for _, rows := range tables {
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			if w := lipgloss.Width(renderedText(row[0])); w > widest {
				widest = w
			}
		}
	}
	return widest
}

// columnSizes is the shared sizing path Summaries and Width both run, so a budget
// asked before a paint cannot disagree with the paint.
func columnSizes(available int, opts Options, tables [][][]Cell) (labelW, valueW int) {
	// Guard at the one place both the paint and the measurement pass through,
	// so a non-positive budget cannot produce two different answers. Below this
	// floor every branch downstream degenerates the same way — the auto-grow
	// arithmetic reads `available - labelW - 3` as a large negative and keeps
	// the declared floor, which is the behaviour we want stated rather than
	// arrived at.
	available = max(1, available)
	labelW, valueW = opts.LabelWidth, opts.ValueWidth
	if opts.Auto {
		if scanned := MaxLabelWidth(tables); scanned > labelW {
			labelW = scanned
		}
		if room := available - labelW - 3; room > valueW {
			valueW = room
		}
	}
	return labelW, valueW
}

// Width is the terminal columns one Summaries call would occupy at available —
// the widest of the side-by-side / stacked / narrowed layouts that Summaries
// would pick. It runs columnSizes then FitWidths, never a second arithmetic.
func Width(available int, opts Options, tables ...[][]Cell) int {
	labelW, valueW := columnSizes(available, opts, tables)
	natural := 1 + labelW + 1 + valueW + 1
	n := len(tables)
	const gap = 2
	if opts.SideBySide && n > 1 && available >= natural*n+gap*(n-1) {
		return natural*n + gap*(n-1)
	}
	if available >= natural {
		return natural
	}
	fitted := ColumnWidths(available, opts, tables...)
	return 1 + fitted[0] + 1 + fitted[1] + 1
}

// ColumnWidths returns the [label, value] widths a bordered summary table
// should paint at available — natural Auto sizes when they fit, otherwise
// FitWidths so both columns shrink together. Callers that compose multiple
// Render calls themselves still share the sizing path Summaries uses.
func ColumnWidths(available int, opts Options, tables ...[][]Cell) []int {
	labelW, valueW := columnSizes(available, opts, tables)
	if available >= 1+labelW+1+valueW+1 {
		return []int{labelW, valueW}
	}
	return FitWidths([]int{labelW, valueW}, max(2, available-3), 1)
}

// Summaries draws the tables inside available columns with border styling.
func Summaries(available int, border lipgloss.Style, opts Options, tables ...[][]Cell) string {
	const gap = 2
	labelW, valueW := columnSizes(available, opts, tables)
	tableWidth := 1 + labelW + 1 + valueW + 1
	widths := []int{labelW, valueW}
	n := len(tables)

	if opts.SideBySide && n > 1 && available >= tableWidth*n+gap*(n-1) {
		parts := make([]string, 0, 2*n-1)
		for i, rows := range tables {
			if i > 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, RenderCells(rows, widths, border))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}

	if available >= tableWidth {
		parts := make([]string, len(tables))
		for i, rows := range tables {
			parts[i] = RenderCells(rows, widths, border)
		}
		return strings.Join(parts, "\n\n")
	}

	// Narrow: shrink BOTH columns so the bordered table fits available.
	// The previous floor of 8 on the value column alone could emit a table
	// wider than the terminal (#2442's sibling failure mode for summaries).
	fitted := ColumnWidths(available, opts, tables...)
	if opts.MergeNarrow {
		var all [][]Cell
		for _, rows := range tables {
			all = append(all, rows...)
		}
		return RenderCells(all, fitted, border)
	}
	parts := make([]string, len(tables))
	for i, rows := range tables {
		parts[i] = RenderCells(rows, fitted, border)
	}
	return strings.Join(parts, "\n\n")
}
