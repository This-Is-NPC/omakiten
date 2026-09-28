package studio

import (
	"strings"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// The `lista | inspector` constructors live in screenlayout and screengrid.
// This file is the shared painting: the field table that trims by field, the
// title that says a view-only zone holds the focus, and the budget those
// helpers share.

// studioZoneIsInspector reports whether the focus is on one of the inspector's
// zones rather than on the list.
//
// It is written as "not the list" rather than as a set of inspector ids on
// purpose: the inspector's zones vary by row — a Workflow bucket has no detail
// box, an open transition has no field table — so a screen that enumerated them
// would fall behind the tree the moment a row kind changed shape. The list is
// the one zone every sub-screen always has, and it is what the up/down keys are
// claimed for.
func studioZoneIsInspector(focus, list screenlayout.ID) bool {
	return focus != "" && focus != list
}

// studioFieldsTitle is the field table's title row, and it is the only thing a
// read-only zone has with which to say it holds the focus.
//
// Focused it takes the same ▸ HintAccent treatment every other Studio zone
// header takes, so the four sub-screens say "you are here" one way rather than
// two. Unfocused it is the plain Kicker the tables have always painted.
func (m Screen) studioFieldsTitle(title string, focused bool) string {
	if focused {
		return m.styles.HintAccent.Render("▸ " + strings.ToUpper(screenkit.Sanitize(title)))
	}
	return m.styles.Kicker(screenkit.Sanitize(title))
}

// studioFieldRows is [gridtable.Rows] with the title painted by
// [Screen.studioFieldsTitle] instead of by the plain kicker style.
func (m Screen) studioFieldRows(title string, focused bool, fields ...[2]string) [][]gridtable.Cell {
	rows := m.summaryRows(title, fields...)
	if len(rows) > 0 {
		rows[0] = []gridtable.Cell{gridtable.Styled(m.studioFieldsTitle(title, focused))}
	}
	return rows
}

// studioFieldTable renders a field table into `rows` terminal rows, trimming it
// by FIELD — never by line.
//
// A bordered table's last line is the line that CLOSES it, so there is no line
// index a cut can land on and still leave a table behind. Cutting by lines is
// the defect reported against Studio › Hooks, where `// 01 // TASK.CREATED` was
// painted as a titled box with no bottom and a field row sliced through the
// middle. Dropping a field and re-rendering is the only trim that produces a
// table, because re-rendering is what moves the closing border up with it.
//
// `rows` below one is an unmeasured canvas, not a canvas of no rows: the table
// is kept whole and the arranger is left to say what fits.
func (m Screen) studioFieldTable(width, rows int, opts summaryTablesOpts, table [][]gridtable.Cell) []string {
	for n := len(table); n > 0; n-- {
		painted := splitNonEmpty(m.renderSummaryTables(width, opts, table[:n]))
		if rows < 1 || len(painted) <= rows {
			return painted
		}
	}
	return nil
}

// studioFieldsBlock is what a fields zone hands back: the table and the footer
// chrome under it, with no selection.
func studioFieldsBlock(table, footer []string) screenlayout.Block {
	return screenlayout.Block{Items: table, Footer: footer, Cursor: screenlayout.NoSelection()}
}

// studioFieldsBudget is the rows the TABLE may take inside a fields zone: the
// zone's rows less the footer chrome under it, measured at the zone's width the
// way the arranger measures it rather than counted as one row per string.
func studioFieldsBudget(canvas screenlayout.Canvas, footer []string) int {
	if canvas.Rows() < 1 {
		return 0
	}
	return max(0, canvas.Rows()-len(gridtable.WrapLines(footer, canvas.Width())))
}
