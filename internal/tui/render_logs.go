package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
)

func (m *Model) handleLogsKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "f":
		m.cycleLogsFilter(1)
	case "F":
		// shift+F surfaces as "F" in bubbletea's KeyMsg.String() when
		// the key is uppercased; we route it through the same cycle
		// helper with step=-1 so the reverse direction stays in one
		// place and the tests can pin both arms against logsFilterCycle.
		m.cycleLogsFilter(-1)
	case "up", "k":
		if m.logsSelected > 0 {
			m.logsSelected--
			m.syncLogsScroll()
		}
	case "down", "j":
		if m.logsSelected < len(m.events)-1 {
			m.logsSelected++
			m.syncLogsScroll()
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.logsViewportRows())
		m.logsSelected -= step
		if m.logsSelected < 0 {
			m.logsSelected = 0
		}
		m.syncLogsScroll()
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.logsViewportRows())
		m.logsSelected += step
		if m.logsSelected > len(m.events)-1 {
			m.logsSelected = len(m.events) - 1
		}
		if m.logsSelected < 0 {
			m.logsSelected = 0
		}
		m.syncLogsScroll()
	case "home", "g":
		m.logsSelected = 0
		m.syncLogsScroll()
	case "end", "G":
		if len(m.events) > 0 {
			m.logsSelected = len(m.events) - 1
			m.syncLogsScroll()
		}
	}
}

// cycleLogsFilter rotates the active filter preset and re-fetches
// the panel rows through refreshActivityLogs so the chip selection
// and the visible rows stay aligned in one tick. Cursor / scroll
// are reset (the filtered row buffer can be shorter than the
// current selection) so the user lands on the first matching row
// instead of an empty selection. The repo lookup failure surfaces
// via m.status the same way the `r` refresh path does.
func (m *Model) cycleLogsFilter(step int) {
	m.logsFilterMode = logsFilterCycle(m.logsFilterMode, step)
	m.logsSelected = 0
	if m.repos.Events == nil {
		// Tests that exercise the cycle without a live Events port
		// still expect the mode field to roll over; the refresh
		// short-circuits to nil there so we leave the events buffer
		// untouched.
		m.syncLogsScroll()
		return
	}
	if err := m.refreshActivityLogs(); err != nil {
		m.status = err.Error()
	}
	m.syncLogsScroll()
}

// syncLogsScroll syncs the logsList linelist.Model so the selected
// log row stays inside the viewport. Routes through WithLines +
// WithViewport + WithCursor so scrollwindow.Resync owns the
// follow-cursor + clamp chain in one place.
//
// Items carry only Height (Content empty) because the renderLogs
// path builds its own dataRows + uses sliceScrollRows for the
// visible slice — the linelist's role here is scroll state, not
// rendering.
func (m *Model) syncLogsScroll() {
	lines := make([]string, len(m.events))
	m.logsList = m.logsList.WithLines(lines).WithViewport(m.logsViewportRows()).WithCursor(m.logsSelected)
}

// logsViewportRows returns how many data rows fit in the activity log
// panel. The screen header (1- or 2-row nav kicker depending on the
// active top) and the summary block above the panel both grow / shrink
// with state, so they are measured live instead of folded into a static
// constant — this keeps the cursor on-screen when the user adds a sub
// strip or summary tables that change the chrome height.
func (m Model) logsViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	screenHeader := strings.Count(m.renderHeader(), "\n") + 1
	summary := strings.Count(m.renderLogsSummaryTables(), "\n") + 1
	statusLine := 0
	if m.status != "" && !m.isEmbeddedCommentInput() {
		statusLine = 2 // separator newline + the status badge
	}
	const (
		leadingBlank = 1 // "\n" prepended by renderLogs before the body
		chipStrip    = 1 // single-line F-cycle filter chip row above the summary
		gapAfterChip = 1 // blank line between chip strip and summary tables
		gapToPanel   = 1 // blank line between summary and panel
		panelChrome  = 7 // 2 borders + 3 header rows + 2 footer rows
		footerLines  = 2 // newline + indented keybinding hint
	)
	chrome := screenHeader + statusLine + leadingBlank + chipStrip + gapAfterChip + summary + gapToPanel + panelChrome + footerLines
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

// renderLogs is the Stats › Logs surface. The renderer is the unified
// event inspector (umbrella #320, sub-task #325): every event_type the
// project has recorded inside the snapshot's `views.logs.window_days`
// window is rendered through a single 5-column row shape (time · type
// · entity · who · detail) with a categories + tool-call-health
// summary stacked above and an F-cycle filter chip strip above that
// (sub-task #326).
func (m Model) renderLogs() string {
	if m.repos.Events == nil {
		return m.renderPanel(m.t("tui.empty.activity_logging_unavailable"))
	}
	chips := m.renderLogsFilterChips()
	if len(m.events) == 0 {
		// Keep the chip strip visible on the empty state so the user
		// can see they have a non-`all` filter active and cycle back
		// without leaving the surface. The empty-state panel still
		// owns the rest of the body.
		return "\n" + indentBlock(chips, 2) + m.renderPanel(m.t("tui.empty.logs"))
	}

	summary := m.renderLogsSummaryTables()
	var panel string
	if m.availableWidth() < 92 {
		panel = m.renderLogsCompactPanel()
	} else {
		panel = m.renderLogsWidePanel()
	}
	return "\n" + indentBlock(chips+"\n\n"+summary+"\n\n"+panel, 2)
}

// renderLogsFilterChips emits the single-line chip strip above the
// summary tables. The active chip is bracketed and painted with the
// hint-accent style (the same primary accent the surrounding focus
// contract uses); inactive chips render in the muted hint color so
// the active one carries the eye. Trailing `(F cycle)` hint
// surfaces the keybinding without forcing the user into the help
// overlay.
//
// The strip is rendered through m.t so locale packs can translate
// each chip label independently; the cycle order itself stays a
// code constant (logsFilterModes) because it is also the order the
// chips render in.
func (m Model) renderLogsFilterChips() string {
	parts := []string{
		m.styles.info.Render("// " + strings.ToUpper(m.t("tui.log.filter.kicker")) + ":"),
	}
	for _, mode := range logsFilterModes {
		label := m.t(logsFilterChipKey(mode))
		if mode == m.logsFilterMode {
			parts = append(parts, m.styles.hintAccent.Render("[ "+label+" ]"))
			continue
		}
		parts = append(parts, m.styles.hint.Render(label))
	}
	parts = append(parts, "  "+m.styles.hint.Render(m.t("tui.log.filter.hint")))
	return strings.Join(parts, " ")
}

// eventStats is the unbounded aggregate the Logs inspector summary
// tables render. It splits cleanly into two tables: per-category
// totals (every known category present, count 0 acceptable) and a
// tool-call health subset (ok / error / running computed over
// `*.tool_call` + `hook.executed` rows only). Kept TUI-local so the
// renderer can ship without growing the domain package.
type eventStats struct {
	// Categories carries the count for every domain.KnownEventCategory.
	// EventCategoryCounts seeds the map with zero entries so the
	// renderer can walk KnownEventCategories deterministically.
	Categories map[domain.EventCategory]int
	// ToolCallOK / ToolCallError / ToolCallRunning aggregate the
	// `*.tool_call` and `hook.executed` subset by status — the
	// summary table header is explicit about the scope so the
	// numbers cannot be confused with "everything in the window".
	ToolCallOK      int
	ToolCallError   int
	ToolCallRunning int
}

// computeEventStats folds the loaded row buffer into the tool-call
// health counts and merges them with the per-category counts the
// repository returns. The category map is taken verbatim — the
// repository already normalises every known category to at least a
// zero so the renderer never has to fill gaps.
func computeEventStats(rows []domain.EventRow, counts map[domain.EventCategory]int) eventStats {
	stats := eventStats{Categories: counts}
	if stats.Categories == nil {
		stats.Categories = map[domain.EventCategory]int{}
		for _, c := range domain.KnownEventCategories {
			stats.Categories[c] = 0
		}
	}
	for _, r := range rows {
		if !isToolCallHealthRow(r.EventType) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(r.Status)) {
		case "ok":
			stats.ToolCallOK++
		case "error":
			stats.ToolCallError++
		case "running":
			stats.ToolCallRunning++
		}
	}
	return stats
}

// isToolCallHealthRow scopes the health table to the rows the user
// expects: every `*.tool_call` event_type plus the matching hook
// dispatch event. Anything else (task lifecycle, comments, plan
// updates, …) is excluded — the table header in renderLogsSummaryTables
// says so explicitly.
func isToolCallHealthRow(eventType string) bool {
	switch eventType {
	case domain.EventTypeCLIToolCall,
		domain.EventTypeMCPToolCall,
		domain.EventTypeTUIToolCall,
		domain.EventTypeHookExecuted:
		return true
	}
	return false
}

// renderLogsSummaryTables renders two bordered grid tables stacked
// side-by-side on wide terminals (or stacked vertically when the
// panel is too narrow): Categories (every known event category with
// its window total) and Tool-call health (ok / error / running across
// the `*.tool_call` + `hook.executed` subset only). The Tool-call
// header is scoped so headline numbers cannot be confused with the
// project-wide window total.
func (m Model) renderLogsSummaryTables() string {
	stats := m.eventStats

	categoryFields := make([][2]string, 0, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		categoryFields = append(categoryFields, [2]string{string(c), fmt.Sprintf("%d", stats.Categories[c])})
	}
	categoryTable := m.summaryRows("Categories", categoryFields...)

	// AC#5: the Tool-call health subset is explicit so the headline
	// counts cannot be confused with the project-wide window total.
	// The kicker stays one row (fits the 13-cell label column) and
	// a second header row carries the scope hint in the value cell.
	healthTable := m.summaryRows("Health · tool_calls",
		[2]string{m.t("tui.log.ok"), fmt.Sprintf("%d", stats.ToolCallOK)},
		[2]string{m.t("tui.log.error"), fmt.Sprintf("%d", stats.ToolCallError)},
		[2]string{m.t("tui.log.running"), fmt.Sprintf("%d", stats.ToolCallRunning)},
	)

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth:  13,
		ValueWidth:  27,
		SideBySide:  true,
		MergeNarrow: true,
	}, categoryTable, healthTable)
}

// renderLogsWidePanel renders the multi-column logs panel used on
// terminals wider than 92 cells. Five columns: TIME · TYPE · ENTITY ·
// WHO · DETAIL. The DETAIL column is `SummarizeEvent(row)` verbatim
// and consumes the remaining width budget; TIME / TYPE / ENTITY / WHO
// are fixed-width so adjacent rows align vertically regardless of
// per-event payload variance.
func (m Model) renderLogsWidePanel() string {
	const (
		timeWidth   = 12
		typeWidth   = 20
		entityWidth = 16
		whoWidth    = 8
	)
	contentWidth := m.availableWidth() - 4
	// 5 single-space gaps separate marker + 5 data columns.
	detailWidth := contentWidth - timeWidth - typeWidth - entityWidth - whoWidth - 2 - 4
	if detailWidth < 10 {
		detailWidth = 10
	}

	limit := len(m.events)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		row := m.events[i]
		marker := m.cursorMarker(m.logsSelected == i)
		dataRows = append(dataRows, formatLogsWideRow(m, marker, row, timeWidth, typeWidth, entityWidth, whoWidth, detailWidth))
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		timeWidth, "TIME",
		typeWidth, "TYPE",
		entityWidth, "ENTITY",
		whoWidth, "WHO",
		detailWidth, "DETAIL",
	)

	rows := []string{
		m.styles.kickerCount(m.t("tui.kicker.activity"), limit),
		m.styles.info.Render(header),
		m.hRule(contentWidth),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsList.Scroll(), m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render(m.t("tui.log.tui_refresh_note")))

	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

// formatLogsWideRow renders one event_row into the 5-column grid.
// Pulled out so the wide variant body stays terse and the
// derivation rules (TIME slice, ENTITY composition, WHO fallback)
// are reachable by tests.
func formatLogsWideRow(m Model, marker string, row domain.EventRow, timeW, typeW, entityW, whoW, detailW int) string {
	timeStr := shortTimeForLogs(row.CreatedAt, timeW)
	typeStr := truncateText(eventDisplayLabel(row.EventType), typeW)
	typeStyled := categoryStyle(m, domain.EventCategoryOf(row.EventType)).Render(padRight(typeStr, typeW))
	entityStr := truncateText(formatLogsEntity(row), entityW)
	whoStr := truncateText(formatLogsWho(row), whoW)
	detailStr := truncateText(domain.SummarizeEvent(row), detailW)
	return fmt.Sprintf("%s %-*s %s %-*s %-*s %-*s",
		marker,
		timeW, timeStr,
		typeStyled,
		entityW, entityStr,
		whoW, whoStr,
		detailW, detailStr,
	)
}

// renderLogsCompactPanel is the narrow-terminal flavor of the Logs
// event inspector. The grid drops the explicit TYPE / ENTITY / WHO
// columns — terminal width does not afford them — and collapses to
// `marker time type detail`, with TYPE coloured per category so the
// per-row signal carries even without the dedicated column.
func (m Model) renderLogsCompactPanel() string {
	width := clampInt(m.availableWidth()-4, 32, 72)
	limit := len(m.events)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		row := m.events[i]
		marker := m.cursorMarker(m.logsSelected == i)
		timeStr := shortTimeForLogs(row.CreatedAt, 8)
		typeStr := categoryStyle(m, domain.EventCategoryOf(row.EventType)).Render(eventDisplayLabel(row.EventType))
		prefix := fmt.Sprintf("%s %s %s ", marker, timeStr, typeStr)
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		dataRows = append(dataRows, prefix+truncateText(domain.SummarizeEvent(row), budget))
	}
	rows := []string{
		m.styles.kickerCount(m.t("tui.kicker.activity"), limit),
		m.hRule(width),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsList.Scroll(), m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render(m.t("tui.log.refresh_hint")))
	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

// shortTimeForLogs trims the SQLite "YYYY-MM-DD HH:MM:SS" timestamp
// down to the rightmost `width` characters (the HH:MM:SS portion when
// width=8, MM:SS when narrower). Returns the value untouched when it
// already fits — useful for fixtures that pass non-SQL timestamps.
func shortTimeForLogs(ts string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(ts) <= width {
		return ts
	}
	return ts[len(ts)-width:]
}

// formatLogsEntity composes the ENTITY column value. Pure event_row
// projection: `<entity_type>#<entity_id>` for entity-scoped rows,
// `system` for project-wide rows whose entity_id is 0.
func formatLogsEntity(row domain.EventRow) string {
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

// formatLogsWho composes the WHO column value: `source` for tool-call
// rows (cli / mcp / tui), `author_type` for comments (human / agent),
// "—" for system events. Falls back to the empty string when none of
// those signals are present — caller pads to the column width.
func formatLogsWho(row domain.EventRow) string {
	switch domain.EventCategoryOf(row.EventType) {
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

// categoryStyle maps an EventCategory to the matching theme-token
// style declared in newStyles. Unknown / catch-all categories fall
// back to the neutral `hint` style so themes that pre-date the Logs
// event inspector keep rendering without panic or a default-black
// glyph.
func categoryStyle(m Model, cat domain.EventCategory) lipgloss.Style {
	switch cat {
	case domain.EventCategoryTask, domain.EventCategoryTagDep:
		return m.styles.hintTasks
	case domain.EventCategoryComment:
		return m.styles.hintComment
	case domain.EventCategoryPlan:
		return m.styles.hintPlan
	case domain.EventCategoryAudit, domain.EventCategoryDomain:
		return m.styles.hintAudit
	case domain.EventCategoryGuard:
		return m.styles.hintGuard
	case domain.EventCategoryTrick:
		return m.styles.hintTrick
	case domain.EventCategoryToolCall, domain.EventCategoryHook:
		return m.styles.hintToolCall
	}
	return m.styles.hint
}

// padRight right-pads s with spaces so its visible width is at least
// width. Unlike `fmt.Sprintf("%-*s", w, s)`, ANSI-styled inputs would
// double-count the escape bytes — padRight measures visible width
// with lipgloss so styled values still align across rows.
func padRight(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// eventDisplayLabel returns the human-friendly label for an event_type
// from the YAML-loaded registry (EventDefByKey). When the registry has
// no entry for the key — unmapped event_types, headless tests that
// never loaded YAML — the raw key is returned so the row still surfaces.
func eventDisplayLabel(eventType string) string {
	if def, ok := domain.EventDefByKey[eventType]; ok && def.Display != "" {
		return def.Display
	}
	return eventType
}

// filterLogVisibleRows drops EventRows whose event_type maps to an
// EventDef with LogVisible == false. Rows whose event_type is missing
// from the registry, or whose def has LogVisible == true, pass through
// unchanged. Phase 3 wires this hook so Phase 4+ can flip individual
// events without touching TUI code; shipped configs currently mark
// every event LogVisible == true, so this is a no-op in production.
func filterLogVisibleRows(rows []domain.EventRow) []domain.EventRow {
	if len(rows) == 0 {
		return rows
	}
	filtered := make([]domain.EventRow, 0, len(rows))
	for _, r := range rows {
		def, ok := domain.EventDefByKey[r.EventType]
		if ok && !def.LogVisible {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// sliceScrollRows is the public assembly helper for fixed-height
// (single-line) list panels — table, logs, graph, blocker picker,
// persona picker. Implementation flows through scrollwindow.Slice with
// heights of 1s so single-line and multi-line surfaces share one
// algorithm. Inserts up to two indicator rows ("▲ N above" /
// "▼ N below") only when content is hidden in that direction.
func (m Model) sliceScrollRows(dataRows []string, scroll, viewport int) []string {
	heights := make([]int, len(dataRows))
	for i := range heights {
		heights[i] = 1
	}
	return m.renderScrollWindowSplit(dataRows, heights, scroll, viewport)
}
