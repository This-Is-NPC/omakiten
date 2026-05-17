package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) handleLogsKey(msg tea.KeyMsg) {
	switch msg.String() {
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
		return m.renderPanel(m.t("tui.empty.activity_logging_unavailable"))
	}
	if len(m.logs) == 0 {
		return m.renderPanel(m.t("tui.empty.logs"))
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
// layout grammar as Stats › General. Aggregates over the **full
// project log** via `m.logsStats` (populated alongside `m.logs` on
// every refresh), so the headline numbers reflect everything the
// project has recorded — independent of how many rows the panel
// beneath happens to render under its `views.logs.limit`.
func (m Model) renderLogsSummaryTables() string {
	stats := m.logsStats

	statusRows := m.summaryRows(m.t("tui.kicker.status"),
		[2]string{m.t("tui.log.total"), fmt.Sprintf("%d", stats.Total)},
		[2]string{m.t("tui.log.ok"), fmt.Sprintf("%d", stats.Ok)},
		[2]string{m.t("tui.log.error"), fmt.Sprintf("%d", stats.Error)},
		[2]string{m.t("tui.log.running"), fmt.Sprintf("%d", stats.Running)},
	)
	sourceRows := m.summaryRows(m.t("tui.kicker.sources"),
		[2]string{m.t("tui.log.cli"), fmt.Sprintf("%d", stats.CLI)},
		[2]string{m.t("tui.log.mcp"), fmt.Sprintf("%d", stats.MCP)},
		[2]string{m.t("tui.log.tui"), fmt.Sprintf("%d", stats.TUI)},
	)

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth:  13,
		ValueWidth:  27,
		SideBySide:  true,
		MergeNarrow: true,
	}, statusRows, sourceRows)
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
		marker := m.cursorMarker(m.logsSelected == i)

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
		m.styles.kickerCount(m.t("tui.kicker.activity"), limit),
		m.styles.info.Render(fmt.Sprintf(m.t("tui.log.column_header_fmt"), logOperationWidth, m.t("tui.log.operation_col"), logProjectWidth, m.t("tui.log.project_col"))),
		m.hRule(contentWidth),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render(m.t("tui.log.tui_refresh_note")))

	return m.styles.panel.Render(strings.Join(rows, "\n"))
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
		marker := m.cursorMarker(m.logsSelected == i)
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
		m.styles.kickerCount(m.t("tui.kicker.activity"), limit),
		m.hRule(width),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render(m.t("tui.log.refresh_hint")))
	return m.styles.panel.Render(strings.Join(rows, "\n"))
}
