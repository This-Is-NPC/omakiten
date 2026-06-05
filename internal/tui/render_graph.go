package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

func (m *Model) handleGraphKey(msg tea.KeyMsg) {
	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)
	m.graphCursor = m.graphCursor.
		WithItemCount(len(sel)).
		WithViewport(m.graphViewportRows())

	switch msg.String() {
	case "up", "k":
		m.graphCursor = m.graphCursor.MoveCursor(-1)
	case "down", "j":
		m.graphCursor = m.graphCursor.MoveCursor(1)
	case "pgup", "ctrl+u":
		m.graphCursor = m.graphCursor.MoveCursor(-taskViewPageStep(m.graphViewportRows()))
	case "pgdown", "ctrl+d":
		m.graphCursor = m.graphCursor.MoveCursor(taskViewPageStep(m.graphViewportRows()))
	case "home", "g":
		m.graphCursor = m.graphCursor.JumpFirst()
	case "end", "G":
		m.graphCursor = m.graphCursor.JumpLast()
	case "enter":
		idx := m.graphCursor.Cursor()
		if len(sel) > 0 && idx >= 0 && idx < len(sel) {
			taskID := lines[sel[idx]].taskID
			if task, ok := m.taskByID(taskID); ok {
				m.openTaskView(task)
			}
		}
	}
	m.syncGraphScroll(sel, len(lines))
}

// syncGraphScroll syncs the graphList linelist.Model so the cursor
// node's LINE stays inside the viewport. graphCursor owns the cursor
// as a selectable-index; the linelist needs a line-index so we map
// selectable → line via the sel slice. Routes through WithLines +
// WithViewport + WithCursor; scrollwindow.Resync owns the
// follow-cursor + clamp chain.
func (m *Model) syncGraphScroll(sel []int, totalLines int) {
	viewport := m.graphViewportRows()
	m.graphCursor = m.graphCursor.WithItemCount(len(sel)).WithViewport(viewport)
	if viewport <= 0 || len(sel) == 0 {
		return
	}
	cursorLine := sel[m.graphCursor.Cursor()]
	lines := make([]string, totalLines)
	m.graphList = m.graphList.WithLines(lines).WithViewport(viewport).WithCursor(cursorLine)
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
			m.styles.kickerCount(m.t("tui.kicker.dependency_graph"), 0),
			"",
			m.styles.hint.Render(m.t("tui.empty.graph_no_deps")),
			m.styles.hint.Render(m.t("tui.graph.use_okt_depend")) + m.styles.hintAccent.Render(m.t("tui.graph.depend_cmd")) + m.styles.hint.Render(m.t("tui.graph.depend_suffix")),
		}, "\n"))
		return "\n" + indentBlock(content, 2)
	}

	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)

	// Defensively guard the cursor index against the current selectable
	// count. refresh() clamps graphCursor via clampGraphCursor, but this is
	// the value-receiver render path: a stale cursor reassigned between
	// clamps must never index past sel and panic. Clamp into range here so
	// the renderer is self-contained.
	cursorLineIdx := -1
	if len(sel) > 0 {
		cursor := m.graphCursor.Cursor()
		if cursor < 0 {
			cursor = 0
		}
		if cursor >= len(sel) {
			cursor = len(sel) - 1
		}
		cursorLineIdx = sel[cursor]
	}

	// Bound every DAG row to the panel body width before styling, mirroring
	// render_table.go's contentWidth derivation. Without this the raw DAG
	// text (deep indentation + long titles + diamond back-refs) flows past
	// the panel border, pushing the right edge off-screen. truncateText
	// trims from the right, so the leading "#<id>" prefix stays visible and
	// users can still identify the task even when its title is clipped.
	contentWidth := m.availableWidth() - 4
	dataRows := make([]string, len(lines))
	for i, l := range lines {
		text := truncateText(l.text, contentWidth)
		if i == cursorLineIdx {
			dataRows[i] = m.styles.hintAccent.Render(text)
		} else {
			dataRows[i] = text
		}
	}

	// Bound the kicker/header row to the same contentWidth the DAG rows use:
	// a long translated dependency_graph label could otherwise exceed the
	// panel on a narrow terminal and push the right edge off-screen.
	// Truncate the raw label BEFORE kickerCount styles it (mirrors the DAG
	// rows, which truncate l.text before styling) so the cut never lands
	// inside an ANSI escape sequence. The "// " prefix + " · N" suffix
	// kickerCount adds are short and fixed; reserve room for them so the
	// composed row stays within contentWidth on a narrow terminal.
	kickerLabel := m.t("tui.kicker.dependency_graph")
	if labelBudget := contentWidth - len("//  · ") - len(strconv.Itoa(len(m.dependencies))); labelBudget > 0 {
		kickerLabel = truncateText(kickerLabel, labelBudget)
	}
	rows := []string{
		m.styles.kickerCount(kickerLabel, len(m.dependencies)),
		"",
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.graphList.Scroll(), m.graphViewportRows())...)
	return m.renderPanel(strings.Join(rows, "\n"))
}

// graphRootLess turns the graph view sort config into a comparator the DAG
// builder can use. Returns nil when the config is at its default (id asc),
// so the default ordering path stays untouched for users who never touch
// the views section.
func (m Model) graphRootLess() func(a, b domain.Task) bool {
	// Validator guarantees both fields are set in the loaded bundle;
	// no fallback. The "skip sort builder when at canonical asc-id"
	// shortcut still applies because it's a perf optimisation, not a
	// default — compare against the configured (not hardcoded) values.
	field := m.views.Graph.Sort.Field
	order := m.views.Graph.Sort.Order
	if field == "id" && order == "asc" {
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
