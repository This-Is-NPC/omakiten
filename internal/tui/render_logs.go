package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) handleLogsKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
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
// inside the viewport. Each log row is exactly 1 line (no wrapping) so this is
// a simple cursor-following scroll — no height heuristic needed.
func (m *Model) syncLogsScroll() {
	viewport := m.logsViewportRows()
	if viewport <= 0 {
		return
	}
	if m.logsSelected < m.logsScroll {
		m.logsScroll = m.logsSelected
	}
	if m.logsSelected >= m.logsScroll+viewport {
		m.logsScroll = m.logsSelected - viewport + 1
	}
	if m.logsScroll < 0 {
		m.logsScroll = 0
	}
}

// logsViewportRows returns how many data rows fit in the activity log panel
// after accounting for the screen chrome, panel borders, and the panel's
// internal header (kicker + column header + separator) and footer (blank +
// hint) rows. Returns 0 when the height is unknown or too small to scroll.
func (m Model) logsViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 3 panel header rows + 2 panel footer rows = 15.
	chrome := 15
	if m.status != "" {
		chrome++
	}
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
	if m.availableWidth() < 92 {
		return m.renderLogsCompact()
	}

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

	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
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
	maxOffset := len(dataRows) - viewport
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

func (m Model) renderLogsCompact() string {
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
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}
