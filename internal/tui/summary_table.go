package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/gridtable"
)

// summaryRows builds a [][]string suitable for gridtable.Render from a
// titled list of label/value pairs. The first row is a kicker (`// TITLE`)
// spanning into the value column; each subsequent row is `// LABEL` +
// value. Callers may append extra pre-rendered rows (error badges,
// hint-styled placeholders) to the returned slice.
func (m Model) summaryRows(title string, fields ...[2]string) [][]string {
	rows := make([][]string, 0, len(fields)+1)
	rows = append(rows, []string{m.styles.kicker(title), ""})
	for _, f := range fields {
		rows = append(rows, []string{m.styles.kicker(f[0]), f[1]})
	}
	return rows
}

// summaryTablesOpts configures renderSummaryTables. SideBySide opts into
// the "wide enough → horizontal" layout used by Stats and Logs; without
// it, tables stack vertically. MergeNarrow folds all rows into a single
// table when the panel is too narrow for even one stacked table at the
// requested width — Stats/Logs use this to avoid clipping. Settings
// keeps tables separate and only narrows the value column.
//
// Auto switches the label/value sizing from the opts constants to a
// scan of the supplied rows: label column expands to the widest
// rendered kicker (so multi-word kickers like `// CLI LANGUAGE` never
// wrap and lose their ANSI styling on the dropped continuation line),
// and the value column expands to fill the remaining panel width (so a
// 47-char config path no longer wraps its last character to a new
// line on a 70-column panel). opts.LabelWidth / opts.ValueWidth are
// treated as minimums in Auto mode so the layout never gets narrower
// than the previously-hardcoded constants.
type summaryTablesOpts struct {
	LabelWidth  int
	ValueWidth  int
	SideBySide  bool
	MergeNarrow bool
	Auto        bool
}

// renderSummaryTables draws multiple key-value summary tables behind a
// shared responsive policy. Each table is a [][]string (typically built
// via summaryRows) ready for gridtable.Render.
func (m Model) renderSummaryTables(opts summaryTablesOpts, tables ...[][]string) string {
	const gap = 2

	labelW := opts.LabelWidth
	valueW := opts.ValueWidth
	if opts.Auto {
		scanned := maxLabelVisibleWidth(tables)
		if scanned > labelW {
			labelW = scanned
		}
		// 3 border columns ("│" + "│" + "│") sit between/around the two
		// data cells inside a single table; subtract them so the value
		// cell consumes the rest of the panel width.
		if room := m.availableWidth() - labelW - 3; room > valueW {
			valueW = room
		}
	}

	tableWidth := 1 + labelW + 1 + valueW + 1
	widths := []int{labelW, valueW}

	n := len(tables)
	if opts.SideBySide && n > 1 && m.availableWidth() >= tableWidth*n+gap*(n-1) {
		parts := make([]string, 0, 2*n-1)
		for i, rows := range tables {
			if i > 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, gridtable.Render(rows, widths, m.styles.border))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}

	if m.availableWidth() >= tableWidth {
		parts := make([]string, len(tables))
		for i, rows := range tables {
			parts[i] = gridtable.Render(rows, widths, m.styles.border)
		}
		return strings.Join(parts, "\n\n")
	}

	narrowValueW := clampInt(m.availableWidth()-labelW-3, 8, valueW)
	narrowWidths := []int{labelW, narrowValueW}
	if opts.MergeNarrow {
		var all [][]string
		for _, rows := range tables {
			all = append(all, rows...)
		}
		return gridtable.Render(all, narrowWidths, m.styles.border)
	}
	parts := make([]string, len(tables))
	for i, rows := range tables {
		parts[i] = gridtable.Render(rows, narrowWidths, m.styles.border)
	}
	return strings.Join(parts, "\n\n")
}

// maxLabelVisibleWidth scans the first cell of every two-column row
// across all supplied tables and returns the widest rendered label.
// Single-cell rows are spanned headers/footers — they do not bound the
// label column. Returns 0 when no qualifying rows exist.
func maxLabelVisibleWidth(tables [][][]string) int {
	max := 0
	for _, rows := range tables {
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			if w := lipgloss.Width(row[0]); w > max {
				max = w
			}
		}
	}
	return max
}
