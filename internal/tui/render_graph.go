package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func (m *Model) handleGraphKey(msg tea.KeyMsg) {
	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)
	maxCursor := len(sel) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}

	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.graphCursor > 0 {
			m.graphCursor--
		}
	case "down", "j":
		if m.graphCursor < maxCursor {
			m.graphCursor++
		}
	case "pgup", "ctrl+u":
		m.graphCursor -= taskViewPageStep(m.graphViewportRows())
		if m.graphCursor < 0 {
			m.graphCursor = 0
		}
	case "pgdown", "ctrl+d":
		m.graphCursor += taskViewPageStep(m.graphViewportRows())
		if m.graphCursor > maxCursor {
			m.graphCursor = maxCursor
		}
	case "home", "g":
		m.graphCursor = 0
	case "end", "G":
		m.graphCursor = maxCursor
	case "enter":
		if m.graphCursor >= 0 && m.graphCursor < len(sel) {
			taskID := lines[sel[m.graphCursor]].taskID
			if task, ok := m.taskByID(taskID); ok {
				m.openTaskView(task)
			}
		}
	}
	m.syncGraphScroll(sel, len(lines))
}

// syncGraphScroll keeps m.graphScroll aligned so the cursor node stays in the viewport.
func (m *Model) syncGraphScroll(sel []int, totalLines int) {
	viewport := m.graphViewportRows()
	if viewport <= 0 || len(sel) == 0 {
		return
	}
	cursorLine := sel[clampInt(m.graphCursor, 0, len(sel)-1)]
	if cursorLine < m.graphScroll {
		m.graphScroll = cursorLine
	}
	if cursorLine >= m.graphScroll+viewport {
		m.graphScroll = cursorLine - viewport + 1
	}
	if m.graphScroll < 0 {
		m.graphScroll = 0
	}
}

// graphViewportRows returns how many DAG lines fit in the graph panel viewport.
// Returns 0 when the terminal is too small to scroll.
func (m Model) graphViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 2 panel header rows (kicker + blank) = 12.
	chrome := 12
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		content := m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join([]string{
			m.styles.kickerCount("Dependency graph", 0),
			"",
			m.styles.hint.Render("No task dependencies yet."),
			m.styles.hint.Render("Use ") + m.styles.hintAccent.Render("okt depend add TASK -i BLOCKER") + m.styles.hint.Render(" to define blocked_by edges."),
		}, "\n"))
		return "\n" + indentBlock(content, 2)
	}

	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)

	cursorLineIdx := -1
	if len(sel) > 0 {
		cursor := clampInt(m.graphCursor, 0, len(sel)-1)
		cursorLineIdx = sel[cursor]
	}

	dataRows := make([]string, len(lines))
	for i, l := range lines {
		if i == cursorLineIdx {
			dataRows[i] = m.styles.hintAccent.Render(l.text)
		} else {
			dataRows[i] = l.text
		}
	}

	rows := []string{
		m.styles.kickerCount("Dependency graph", len(m.dependencies)),
		"",
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.graphScroll, m.graphViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

// graphRootLess turns the graph view sort config into a comparator the DAG
// builder can use. Returns nil when the config is at its default (id asc),
// so the legacy ordering path stays untouched for users who never touch
// the views section.
func (m Model) graphRootLess() func(a, b domain.Task) bool {
	field := m.views.Graph.Sort.Field
	order := m.views.Graph.Sort.Order
	if field == "" {
		field = config.DefaultGraphSortField
	}
	if order == "" {
		order = config.DefaultGraphSortOrder
	}
	if field == config.DefaultGraphSortField && order == config.DefaultGraphSortOrder {
		return nil
	}
	asc := order != "desc"
	switch field {
	case "title":
		return func(a, b domain.Task) bool {
			ai, bi := strings.ToLower(a.Title), strings.ToLower(b.Title)
			if asc {
				return ai < bi
			}
			return ai > bi
		}
	default:
		return func(a, b domain.Task) bool {
			if asc {
				return a.ID < b.ID
			}
			return a.ID > b.ID
		}
	}
}
