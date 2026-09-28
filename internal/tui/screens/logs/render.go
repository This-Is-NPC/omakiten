package logs

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

// wideLayoutWidth is the available-width threshold at which the inspector
// switches from the compact two-column row to the full 5-column grid.
const wideLayoutWidth = 92

// View renders the Stats › Logs body.
//
// Filter chips stay OUTER chrome. The body itself is a screengrid Rows tree:
// the event feed is the primary Weight:1 cell and the summary cell is declared
// last so a short panel drops the supplemental aggregate first.
func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if !s.available() {
		return kit.Panel(kit.T("tui.empty.activity_logging_unavailable"))
	}
	chips := kit.WrapBody(s.renderFilterChips(kit))
	if len(s.rows) == 0 {
		// Keep the chip strip visible on the empty state so the user can see
		// they have a non-`all` filter active and cycle back without leaving
		// the surface. The empty-state panel still owns the rest of the body.
		return "\n" + screenkit.Indent(chips, 2) + kit.Panel(kit.T("tui.empty.logs"))
	}

	body := screengrid.Render(kit, s.grid, s.panelBox(kit), s.bodyRoot(kit)).View
	return "\n" + screenkit.Indent(chips+"\n\n"+body, 2)
}

// renderFilterChips emits the single-line chip strip above the panel. Delegates
// to components/chipstrip so the overflow cut (hint → distant inactive chips →
// truncate active) is the same decision Width would measure. The active chip is
// bracketed and painted with the hint-accent style; inactive chips render muted.
// The trailing `(F cycle)` hint surfaces the keybinding.
func (s Screen) renderFilterChips(kit screenkit.Kit) string {
	chips := make([]tokenstrip.Chip, 0, len(FilterModes))
	for _, mode := range FilterModes {
		chips = append(chips, tokenstrip.Chip{
			Label:  kit.T(FilterChipKey(mode)),
			Active: mode == s.filter,
		})
	}
	return tokenstrip.Chips(chips, tokenstrip.ChipStyles{
		Kicker:   kit.Styles.Info,
		Active:   kit.Styles.HintAccent,
		Inactive: kit.Styles.Hint,
		Hint:     kit.Styles.Hint,
	}, tokenstrip.ChipOptions{
		Kicker:        "// " + strings.ToUpper(kit.T("tui.log.filter.kicker")) + ":",
		Hint:          kit.T("tui.log.filter.hint"),
		BracketActive: true,
		// BoxWidth is the bare body column WrapBody / Indent share — the strip
		// must cut inside that budget so it stays one row after indent.
		Width: kit.BoxWidth(),
	})
}

// EventStats is the unbounded aggregate the inspector's summary tables render.
// It splits cleanly into two tables: per-category totals (every known category
// present, count 0 acceptable) and a tool-call health subset (ok / error /
// running computed over `*.tool_call` + `hook.executed` rows only).

// renderSummaryTables renders two bordered grid tables stacked side-by-side on
// wide terminals (or vertically when the panel is too narrow): Categories
// (every known event category with its window total) and Tool-call health
// (ok / error / running across the `*.tool_call` + `hook.executed` subset
// only). The Tool-call header is scoped so headline numbers cannot be confused
func (s Screen) renderSummaryTables(kit screenkit.Kit, width int) string {
	stats := s.stats

	categoryFields := make([][2]string, 0, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		categoryFields = append(categoryFields, [2]string{string(c), fmt.Sprintf("%d", stats.Categories[c])})
	}
	categoryTable := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.log.categories"), categoryFields...)

	healthTable := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.log.health_tool_calls"),
		[2]string{kit.T("tui.log.ok"), fmt.Sprintf("%d", stats.ToolCallOK)},
		[2]string{kit.T("tui.log.error"), fmt.Sprintf("%d", stats.ToolCallError)},
		[2]string{kit.T("tui.log.running"), fmt.Sprintf("%d", stats.ToolCallRunning)},
	)

	return gridtable.Summaries(width, kit.Styles.Border, gridtable.Options{
		LabelWidth:  13,
		ValueWidth:  27,
		SideBySide:  true,
		MergeNarrow: true,
	}, categoryTable, healthTable)
}

// wideLayout reports whether the arranged cell affords the full five-column grid.
func (s Screen) wideLayout(width int) bool { return width >= wideLayoutWidth }

// wideColumns is the fixed-width column budget of the five-column grid, plus
// the DETAIL width that consumes whatever the cell has left.
func wideColumns(width int) (timeW, typeW, entityW, whoW, detailW, contentW int) {
	const (
		timeWidth   = 12
		typeWidth   = 20
		entityWidth = 16
		whoWidth    = 8
	)
	contentWidth := width
	detailWidth := contentWidth - timeWidth - typeWidth - entityWidth - whoWidth - 2 - 4
	if detailWidth < 10 {
		detailWidth = 10
	}
	return timeWidth, typeWidth, entityWidth, whoWidth, detailWidth, contentWidth
}

func compactWidth(width int) int { return screenkit.Clamp(width, 32, 72) }

// panelKicker is the chrome the panel draws above its data window.
func (s Screen) panelKicker(kit screenkit.Kit, width int) (kicker string, extra []string) {
	kicker = kit.Styles.FocusKickerCount(kit.T("tui.kicker.activity"), len(s.rows))
	if !s.wideLayout(width) {
		return kicker, nil
	}
	timeW, typeW, entityW, whoW, detailW, _ := wideColumns(width)
	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		timeW, kit.T("tui.log.col.time"),
		typeW, kit.T("tui.log.col.type"),
		entityW, kit.T("tui.log.col.entity"),
		whoW, kit.T("tui.log.col.who"),
		detailW, kit.T("tui.log.col.detail"),
	)
	return kicker, []string{kit.Styles.Info.Render(header)}
}

// panelFooter is the chrome the panel draws below its data window.
func (s Screen) panelFooter(kit screenkit.Kit) []string {
	return []string{"", kit.Styles.Hint.Render(s.renderFooterNotes(kit))}
}

// rowLines renders one line per loaded event in whichever grid the arranged
// cell affords, marking the row at `cursor`. A cursor outside the buffer marks
// nothing, which is how [Screen.feedItems] composes the set it memoises.
func (s Screen) rowLines(kit screenkit.Kit, width, cursor int) []string {
	lines := make([]string, len(s.rows))
	for i := range s.rows {
		lines[i] = s.dataRow(kit, width, i, i == cursor)
	}
	return lines
}

// dataRow renders ONE loaded event. It is the single row shape both the whole
// feed and a cursor repaint go through, so a memoised line and a freshly marked
// one can never be composed differently.
func (s Screen) dataRow(kit screenkit.Kit, width, index int, selected bool) string {
	marker := kit.CursorMarker(selected)
	row := s.rows[index]
	if !s.wideLayout(width) {
		return compactRow(kit, marker, row, compactWidth(width))
	}
	timeW, typeW, entityW, whoW, detailW, _ := wideColumns(width)
	return formatWideRow(kit, marker, row, timeW, typeW, entityW, whoW, detailW)
}

// formatWideRow renders one event row into the 5-column grid. Pulled out so the
// wide variant body stays terse and the derivation rules (TIME slice, ENTITY
// composition, WHO fallback) are reachable by tests. Columns go through
// gridtable.FormatRow so a FitWidths pass on the widths stays aligned.
func formatWideRow(kit screenkit.Kit, marker string, row domain.EventRow, timeW, typeW, entityW, whoW, detailW int) string {
	timeStr := ShortTime(screenkit.Sanitize(row.CreatedAt), timeW)
	typeStr := gridtable.Truncate(screenkit.Sanitize(row.Display), typeW)
	typeStyled := kit.Styles.Accent(CategoryAccent(row.Category)).Render(gridtable.PadLine(typeStr, typeW))
	entityStr := gridtable.Truncate(screenkit.Sanitize(FormatEntity(row)), entityW)
	whoStr := gridtable.Truncate(screenkit.Sanitize(FormatWho(row)), whoW)
	detailStr := gridtable.Truncate(screenkit.Sanitize(row.Summary), detailW)
	// TYPE carries its own colour and is already padded; FormatRow would
	// re-PadLine and break the ANSI width, so the row is assembled by hand
	// around the shared Truncate/PadLine primitives.
	return fmt.Sprintf("%s %s %s %s %s %s",
		marker,
		gridtable.PadLine(timeStr, timeW),
		typeStyled,
		gridtable.PadLine(entityStr, entityW),
		gridtable.PadLine(whoStr, whoW),
		gridtable.PadLine(detailStr, detailW),
	)
}

// compactRow is the narrow-terminal flavor of the event row. The grid drops the
// explicit TYPE / ENTITY / WHO columns — terminal width does not afford them —
// and collapses to `marker time type detail`. The enclosing panel owns paint;
// keeping the row plain avoids a Style.Render call for every line.
func compactRow(kit screenkit.Kit, marker string, row domain.EventRow, width int) string {
	timeStr := ShortTime(screenkit.Sanitize(row.CreatedAt), 8)
	typeStr := screenkit.Sanitize(row.Display)
	prefix := fmt.Sprintf("%s %s %s ", marker, timeStr, typeStr)
	budget := screenkit.Clamp(width-screenkit.VisibleWidth(prefix), 8, width)
	return prefix + screenkit.Truncate(screenkit.Sanitize(row.Summary), budget)
}

// renderFooterNotes composes the panel footer: retention vs display-window
// context plus the existing refresh / tracking hints.
func (s Screen) renderFooterNotes(kit screenkit.Kit) string {
	parts := []string{s.renderRetentionNote(kit)}
	if note := strings.TrimSpace(kit.T("tui.log.tui_refresh_note")); note != "" && note != "tui.log.tui_refresh_note" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " · ")
}

// renderRetentionNote surfaces the resolved tool_call storage retention beside
// the configured display window so operators understand why older tool calls
// may be absent from the panel.
func (s Screen) renderRetentionNote(kit screenkit.Kit) string {
	retention := s.deps.Settings.Retention
	if !retention.Known {
		return kit.T("tui.log.retention_note_unavailable")
	}
	if retention.MaxRows == 0 {
		return fmt.Sprintf(kit.T("tui.log.retention_note_window_only_fmt"), retention.WindowDays)
	}
	if retention.MaxAgeDays == 0 {
		return fmt.Sprintf(kit.T("tui.log.retention_note_rows_only_fmt"), retention.MaxRows, retention.WindowDays)
	}
	return fmt.Sprintf(kit.T("tui.log.retention_note_fmt"), retention.MaxAgeDays, retention.MaxRows, retention.WindowDays)
}

// ShortTime trims the SQLite "YYYY-MM-DD HH:MM:SS" timestamp down to the
// rightmost `width` characters (the HH:MM:SS portion when width=8, MM:SS when
// narrower). Returns the value untouched when it already fits — useful for
// fixtures that pass non-SQL timestamps.
func ShortTime(ts string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(ts) <= width {
		return ts
	}
	return ts[len(ts)-width:]
}

// FormatEntity composes the ENTITY column value. Pure event-row projection:
// `<entity_type>#<entity_id>` for entity-scoped rows, `system` for
// project-wide rows whose entity_id is 0.
func FormatEntity(row domain.EventRow) string {
	entityType := strings.TrimSpace(row.EntityType)
	if entityType == "" {
		entityType = "event"
	}
	if row.EntityID == 0 {
		if entityType == "system" {
			return "system"
		}
		return entityType
	}
	return fmt.Sprintf("%s#%d", entityType, row.EntityID)
}

// FormatWho composes the WHO column value: `source` for tool-call rows
// (cli / mcp / tui), `author_type` for comments (human / agent), "—" for system
// events. Falls back to the empty string when none of those signals are
// present — the caller pads to the column width.
func FormatWho(row domain.EventRow) string {
	switch row.Category {
	case domain.EventCategoryToolCall, domain.EventCategoryHook:
		if s := strings.TrimSpace(row.Source); s != "" {
			return s
		}
	case domain.EventCategoryComment:
		if a := strings.TrimSpace(row.AuthorType); a != "" {
			return a
		}
	}
	if strings.EqualFold(row.EntityType, "system") {
		return "—"
	}
	if s := strings.TrimSpace(row.Source); s != "" {
		return s
	}
	if a := strings.TrimSpace(row.AuthorType); a != "" {
		return a
	}
	return "—"
}

// CategoryAccent maps an EventCategory to the accent the theme paints it with.
//
// It answers the half of the question this package knows — that a tag/dep edit
// reads as a task event and a hook reads as a tool call — and stops there.
// Which colour the accent resolves to is [screenkit.Styles.Accent]'s, which is
// why nothing here names a style type. Unknown / catch-all categories return
// the zero accent, so themes that pre-date the event inspector keep rendering
// through the neutral hint tone rather than a default-black glyph.
func CategoryAccent(cat domain.EventCategory) screenkit.Accent {
	switch cat {
	case domain.EventCategoryTask, domain.EventCategoryTagDep:
		return screenkit.AccentTask
	case domain.EventCategoryComment:
		return screenkit.AccentComment
	case domain.EventCategoryPlan:
		return screenkit.AccentPlan
	case domain.EventCategoryAudit, domain.EventCategoryDomain:
		return screenkit.AccentAudit
	case domain.EventCategoryGuard:
		return screenkit.AccentGuard
	case domain.EventCategoryTrick:
		return screenkit.AccentTrick
	case domain.EventCategoryToolCall, domain.EventCategoryHook:
		return screenkit.AccentToolCall
	}
	return screenkit.AccentNone
}
