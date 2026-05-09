package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// summaryRows builds a [][]string suitable for renderGridTable from a
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
type summaryTablesOpts struct {
	LabelWidth  int
	ValueWidth  int
	SideBySide  bool
	MergeNarrow bool
}

// renderSummaryTables draws multiple key-value summary tables behind a
// shared responsive policy. Each table is a [][]string (typically built
// via summaryRows) ready for renderGridTable.
func (m Model) renderSummaryTables(opts summaryTablesOpts, tables ...[][]string) string {
	const gap = 2
	tableWidth := 1 + opts.LabelWidth + 1 + opts.ValueWidth + 1
	widths := []int{opts.LabelWidth, opts.ValueWidth}

	n := len(tables)
	if opts.SideBySide && n > 1 && m.availableWidth() >= tableWidth*n+gap*(n-1) {
		parts := make([]string, 0, 2*n-1)
		for i, rows := range tables {
			if i > 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, renderGridTable(rows, widths, m.styles.border))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}

	if m.availableWidth() >= tableWidth {
		parts := make([]string, len(tables))
		for i, rows := range tables {
			parts[i] = renderGridTable(rows, widths, m.styles.border)
		}
		return strings.Join(parts, "\n\n")
	}

	valueW := clampInt(m.availableWidth()-opts.LabelWidth-3, 8, opts.ValueWidth)
	narrowWidths := []int{opts.LabelWidth, valueW}
	if opts.MergeNarrow {
		var all [][]string
		for _, rows := range tables {
			all = append(all, rows...)
		}
		return renderGridTable(all, narrowWidths, m.styles.border)
	}
	parts := make([]string, len(tables))
	for i, rows := range tables {
		parts[i] = renderGridTable(rows, narrowWidths, m.styles.border)
	}
	return strings.Join(parts, "\n\n")
}
