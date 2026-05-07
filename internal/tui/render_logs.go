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
	case "left", "h":
		m.cycleLegacyView(-1)
	case "right", "l":
		m.cycleLegacyView(1)
	case "up", "k":
		if m.logsSelected > 0 {
			m.logsSelected--
			m.syncLogsScroll()
		}
	case "down", "j":
		if m.logsSelected < len(m.logs)-1 {
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
		if m.logsSelected > len(m.logs)-1 {
			m.logsSelected = len(m.logs) - 1
		}
		if m.logsSelected < 0 {
			m.logsSelected = 0
		}
		m.syncLogsScroll()
	case "home", "g":
		m.logsSelected = 0
		m.syncLogsScroll()
	case "end", "G":
		if len(m.logs) > 0 {
			m.logsSelected = len(m.logs) - 1
			m.syncLogsScroll()
		}
	}
}

// syncLogsScroll keeps m.logsScroll aligned so the selected log row stays
// inside the viewport. `sliceScrollRows` reserves up to 2 of the panel
// rows for "▲ above" / "▼ below" hints, so the data window the cursor
// can actually live in is `logsViewportRows() - 2`. Pass that effective
// size to followCursor — otherwise the cursor lands in the reserved
// zone and the render clips it without anyone scrolling.
func (m *Model) syncLogsScroll() {
	m.logsScroll = followCursor(m.logsScroll, m.logsSelected, scrollDataRows(m.logsViewportRows()), len(m.logs))
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
		gapToPanel   = 1 // blank line between summary and panel
		panelChrome  = 7 // 2 borders + 3 header rows + 2 footer rows
		footerLines  = 2 // newline + indented keybinding hint
	)
	chrome := screenHeader + statusLine + leadingBlank + summary + gapToPanel + panelChrome + footerLines
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m Model) renderLogs() string {
	if m.repos.ActivityLogs == nil {
		return "\n" + indentBlock(m.styles.panel.Render("Activity logging is not available for this project."), 2)
	}
	if len(m.logs) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No activity yet. Use the CLI, TUI, or MCP to interact with Omakiten."), 2)
	}

	summary := m.renderLogsSummaryTables()
	var panel string
	if m.availableWidth() < 92 {
		panel = m.renderLogsCompactPanel()
	} else {
		panel = m.renderLogsWidePanel()
	}
	return "\n" + indentBlock(summary+"\n\n"+panel, 2)
}

// renderLogsSummaryTables renders Status (total / ok / error / running)
// and Sources (cli / mcp / tui) as two bordered grid tables — same
// layout grammar as Stats › General. Aggregates over the loaded
// `m.logs` window (capped by the view's `limit` setting), so the
// numbers reflect what is actually shown below.
func (m Model) renderLogsSummaryTables() string {
	okCount := 0
	errorCount := 0
	runningCount := 0
	cliCount := 0
	mcpCount := 0
	tuiCount := 0
	for _, log := range m.logs {
		switch log.Status {
		case "ok":
			okCount++
		case "error":
			errorCount++
		case "running":
			runningCount++
		}
		switch log.Source {
		case domain.ActivitySourceCLI:
			cliCount++
		case domain.ActivitySourceMCP:
			mcpCount++
		case domain.ActivitySourceTUI:
			tuiCount++
		}
	}

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}
	statusRows := [][]string{
		{labelCell("Status"), ""},
		{labelCell("total"), fmt.Sprintf("%d", len(m.logs))},
		{labelCell("ok"), fmt.Sprintf("%d", okCount)},
		{labelCell("error"), fmt.Sprintf("%d", errorCount)},
		{labelCell("running"), fmt.Sprintf("%d", runningCount)},
	}
	sourceRows := [][]string{
		{labelCell("Sources"), ""},
		{labelCell("cli"), fmt.Sprintf("%d", cliCount)},
		{labelCell("mcp"), fmt.Sprintf("%d", mcpCount)},
		{labelCell("tui"), fmt.Sprintf("%d", tuiCount)},
	}

	const (
		labelWidth = 13
		valueWidth = 27
		tableWidth = 1 + labelWidth + 1 + valueWidth + 1
		gap        = 2
	)
	widths := []int{labelWidth, valueWidth}

	switch {
	case m.availableWidth() >= tableWidth*2+gap:
		left := renderGridTable(statusRows, widths, m.styles.border)
		right := renderGridTable(sourceRows, widths, m.styles.border)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	case m.availableWidth() >= tableWidth:
		left := renderGridTable(statusRows, widths, m.styles.border)
		right := renderGridTable(sourceRows, widths, m.styles.border)
		return left + "\n\n" + right
	default:
		valueW := clampInt(m.availableWidth()-labelWidth-3, 8, valueWidth)
		narrowWidths := []int{labelWidth, valueW}
		all := append(append([][]string{}, statusRows...), sourceRows...)
		return renderGridTable(all, narrowWidths, m.styles.border)
	}
}

// renderLogsWidePanel renders the multi-column logs panel used on
// terminals wider than 92 cells. Returned without the leading "\n" or
// indentBlock so `renderLogs` can stack the summary block above it
// inside a single indent block.
func (m Model) renderLogsWidePanel() string {
	const (
		logOperationWidth = 35
		logProjectWidth   = 11
		logFixedWidth     = 34
	)
	contentWidth := m.availableWidth() - 4
	argsWidth := contentWidth - logFixedWidth - logOperationWidth - logProjectWidth

	limit := minInt(len(m.logs), 50)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		log := m.logs[i]
		marker := normalMarker
		if i == m.logsSelected {
			marker = m.styles.marker.Render(selectionMarker)
		}

		timeStr := log.StartedAt
		if len(timeStr) > 12 {
			timeStr = timeStr[len(timeStr)-12:]
		}

		statusStyle := m.styles.success
		if log.Status == "error" {
			statusStyle = m.styles.error
		}
		status := statusStyle.Render(fmt.Sprintf("%-5s", log.Status))

		row := fmt.Sprintf("%s %-12s %-4s %-*s %-*s %s %-4d %s",
			marker, timeStr, log.Source, logOperationWidth, truncateText(log.Operation, logOperationWidth), logProjectWidth, truncateText(log.ProjectSlug, logProjectWidth),
			status, log.DurationMs, truncateText(log.ArgumentsJSON, argsWidth))
		dataRows = append(dataRows, row)
	}

	rows := []string{
		m.styles.kickerCount("Activity", limit),
		m.styles.info.Render(fmt.Sprintf("// TIME        SRC  %-*s %-*s STATUS  MS   ARGS", logOperationWidth, "OPERATION", logProjectWidth, "PROJECT")),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render("Only app service calls are logged. TUI refreshes and direct reads are not shown."))

	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

// sliceScrollRows clamps `scroll` into a valid range and returns the visible
// slice of single-line data rows plus up-to-2 indicator rows ("▲ N above" /
// "▼ N below") inserted only when content is hidden in that direction. Each
// data row is assumed to be exactly one physical line, so no height heuristic
// is needed. Used by table, logs, and any future list-style view.
func (m Model) sliceScrollRows(dataRows []string, scroll, viewport int) []string {
	if viewport <= 0 || len(dataRows) <= viewport {
		return dataRows
	}
	offset := scroll
	if offset < 0 {
		offset = 0
	}
	// When the scroll has any items above (offset > 0), the visibleHeight
	// loses 1 row to the "▲ above" hint, so the renderer can still paint
	// the last data row at offset = len(dataRows) - viewport + 1. Cap the
	// clamp accordingly — otherwise the cursor at the last row gets
	// scrolled off the panel because the old `total - viewport` cap was
	// one row too tight to expose the trailing items.
	maxOffset := len(dataRows) - viewport + 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}

	above := offset
	belowAvailable := len(dataRows) - offset
	visibleHeight := viewport
	if above > 0 {
		visibleHeight--
	}
	if belowAvailable-visibleHeight > 0 {
		visibleHeight--
	}
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	end := offset + visibleHeight
	if end > len(dataRows) {
		end = len(dataRows)
	}
	below := len(dataRows) - end

	out := make([]string, 0, visibleHeight+2)
	if above > 0 {
		out = append(out, m.styles.hint.Render(fmt.Sprintf("▲ %d above", above)))
	}
	out = append(out, dataRows[offset:end]...)
	if below > 0 {
		out = append(out, m.styles.hint.Render(fmt.Sprintf("▼ %d below", below)))
	}
	return out
}

// renderLogsCompactPanel is the narrow-terminal flavor of the activity
// logs panel. Same body as the wide variant minus the project / args
// columns. Returned without the leading "\n" or indentBlock so
// `renderLogs` can stack the summary block above it.
func (m Model) renderLogsCompactPanel() string {
	width := clampInt(m.availableWidth()-4, 32, 72)
	limit := minInt(len(m.logs), 50)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		log := m.logs[i]
		marker := normalMarker
		if i == m.logsSelected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		timeStr := log.StartedAt
		if len(timeStr) > 8 {
			timeStr = timeStr[len(timeStr)-8:]
		}
		statusStyle := m.styles.success
		if log.Status == "error" {
			statusStyle = m.styles.error
		}
		prefix := fmt.Sprintf("%s %s %s ", marker, timeStr, statusStyle.Render(log.Status))
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		dataRows = append(dataRows, prefix+truncateText(log.Operation, budget))
	}
	rows := []string{
		m.styles.kickerCount("Activity", limit),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render("r refresh · full arguments appear on wider terminals"))
	return m.styles.panel.Render(strings.Join(rows, "\n"))
}
