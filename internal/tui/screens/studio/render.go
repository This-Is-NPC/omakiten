package studio

import (
	bundledraft "omakiten/internal/config/bundledraft"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
)

type summaryTablesOpts struct {
	LabelWidth int
	ValueWidth int
	Auto       bool
}

func (m Screen) summaryRows(title string, pairs ...[2]string) [][]gridtable.Cell {
	fields := make([][2]string, len(pairs))
	for i, pair := range pairs {
		fields[i] = [2]string{screenkit.Sanitize(pair[0]), screenkit.Sanitize(pair[1])}
	}
	return gridtable.Rows(m.styles.Kicker, screenkit.Sanitize(title), fields...)
}

// renderSummaryTables lays the label/value grids out inside `width`, which is
// the width the ARRANGER gave the section. Delegates to gridtable.Summaries
// so Studio shares the same responsive policy as Stats and Logs.
func (m Screen) renderSummaryTables(width int, opts summaryTablesOpts, tables ...[][]gridtable.Cell) string {
	return gridtable.Summaries(width, m.styles.Border, gridtable.Options{
		LabelWidth: opts.LabelWidth,
		ValueWidth: opts.ValueWidth,
		Auto:       opts.Auto,
	}, tables...)
}

// studioDirtyStateLabel is the copy Workflow and the apply overlay print for
// the candidate's dirty/clean state. It used to live on the dump Overview
// table; the overlay and remaining editors still need one shared word.
func (m Screen) studioDirtyStateLabel() string {
	if m.studioCandidateDirty() {
		return m.t("tui.studio.dirty.dirty")
	}
	return m.t("tui.studio.dirty.clean")
}

func (m Screen) activeTaskCountsByBucket() map[string]int {
	return m.projection.TaskCounts
}

func permissionLabel(text bundledraft.Text, allowed bool, explicit bool) string {
	if allowed {
		if explicit {
			return tr(text, "tui.studio.permission.allow_explicit", "allow (explicit)")
		}
		return tr(text, "tui.studio.permission.allow_inherited", "allow (inherited)")
	}
	if explicit {
		return tr(text, "tui.studio.permission.deny_explicit", "deny (explicit)")
	}
	return tr(text, "tui.studio.permission.deny_inherited", "deny (inherited)")
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
