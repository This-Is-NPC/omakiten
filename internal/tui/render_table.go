package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func (m *Model) handleListKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.syncTableScroll()
		}
	case "down", "j":
		if m.selected < len(m.tasks)-1 {
			m.selected++
			m.syncTableScroll()
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.tableViewportRows())
		m.selected -= step
		if m.selected < 0 {
			m.selected = 0
		}
		m.syncTableScroll()
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.tableViewportRows())
		m.selected += step
		if m.selected > len(m.tasks)-1 {
			m.selected = len(m.tasks) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		m.syncTableScroll()
	case "home", "g":
		m.selected = 0
		m.syncTableScroll()
	case "end", "G":
		if len(m.tasks) > 0 {
			m.selected = len(m.tasks) - 1
			m.syncTableScroll()
		}
	case "enter":
		if task, ok := m.selectedTask(); ok {
			m.openTaskView(task)
		}
	case "m":
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeMove, "Target bucket key", "")
		}
	}
}

// syncTableScroll keeps m.tableScroll aligned so the selected task row stays
// in view. Each row is exactly 1 line — no height heuristic, same pattern as
// syncLogsScroll.
func (m *Model) syncTableScroll() {
	viewport := m.tableViewportRows()
	if viewport <= 0 {
		return
	}
	if m.selected < m.tableScroll {
		m.tableScroll = m.selected
	}
	if m.selected >= m.tableScroll+viewport {
		m.tableScroll = m.selected - viewport + 1
	}
	if m.tableScroll < 0 {
		m.tableScroll = 0
	}
}

// tableViewportRows returns how many task rows fit in the table panel after
// the screen chrome and the panel's internal header rows. Returns 0 when the
// height is unknown or too small.
func (m Model) tableViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 3 panel header rows (kicker/info/separator) = 13.
	chrome := 13
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m Model) renderTable() string {
	tasks := m.applyTableView()
	if len(tasks) == 0 {
		if len(m.tasks) == 0 {
			return "\n" + indentBlock(m.styles.panel.Render("No tasks yet. Press n to create one."), 2)
		}
		return "\n" + indentBlock(m.styles.panel.Render("No tasks match the configured table filter."), 2)
	}
	if m.availableWidth() < 74 {
		return m.renderTableCompactWith(tasks)
	}
	const tableFixedWidth = 44
	contentWidth := m.availableWidth() - 4
	titleWidth := contentWidth - tableFixedWidth

	selectedID := m.selectedTaskID()
	dataRows := make([]string, 0, len(tasks))
	for _, task := range tasks {
		marker := normalMarker
		if task.ID == selectedID {
			marker = m.styles.marker.Render(selectionMarker)
		}
		dataRows = append(dataRows, fmt.Sprintf("%s %-4d %-11s %-8s %-5d %-9d %s", marker, task.ID, task.BucketKey, task.Priority, m.dependencyCount(task.ID), m.commentCount(task.ID), truncateText(task.Title, titleWidth)))
	}

	rows := []string{
		m.styles.kickerCount("Tasks", len(tasks)),
		m.styles.info.Render("// ID   BUCKET      PRI      DEPS  COMMENTS  TITLE"),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableScroll, m.tableViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderTableCompactWith(tasks []domain.Task) string {
	width := clampInt(m.availableWidth()-4, 32, 68)
	selectedID := m.selectedTaskID()
	dataRows := make([]string, 0, len(tasks))
	for _, task := range tasks {
		marker := normalMarker
		if task.ID == selectedID {
			marker = m.styles.marker.Render(selectionMarker)
		}
		prefix := fmt.Sprintf("%s #%d %s %s ", marker, task.ID, task.BucketKey, task.Priority)
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		dataRows = append(dataRows, prefix+truncateText(task.Title, budget))
	}
	rows := []string{
		m.styles.kickerCount("Tasks", len(tasks)),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableScroll, m.tableViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

// selectedTaskID resolves the currently-selected task id via m.tasks; used
// by the table view to flag the matching row even when the visible list
// has been re-sorted/filtered by view config.
func (m Model) selectedTaskID() int64 {
	if m.selected < 0 || m.selected >= len(m.tasks) {
		return 0
	}
	return m.tasks[m.selected].ID
}

// priorityAllowSet returns nil when the configured slice is empty (meaning
// "allow everything"), otherwise a lookup set. Centralised so board and
// table views agree on the filter semantics.
func priorityAllowSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

func priorityAllowed(allowed map[string]struct{}, priority domain.Priority) bool {
	if allowed == nil {
		return true
	}
	_, ok := allowed[string(priority)]
	return ok
}

func bucketAllowSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

func bucketAllowed(allowed map[string]struct{}, bucketKey string) bool {
	if allowed == nil {
		return true
	}
	_, ok := allowed[bucketKey]
	return ok
}

// applyTableView returns m.tasks filtered and sorted according to the
// `table` view config. The returned slice is a copy — callers free to
// re-order without mutating m.tasks (which is the board's source of truth).
func (m Model) applyTableView() []domain.Task {
	prioAllowed := priorityAllowSet(m.views.Table.Filter.Priority)
	bucketAllowedSet := bucketAllowSet(m.views.Table.Filter.Bucket)
	out := make([]domain.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if !priorityAllowed(prioAllowed, task.Priority) {
			continue
		}
		if !bucketAllowed(bucketAllowedSet, task.BucketKey) {
			continue
		}
		out = append(out, task)
	}
	sortTasks(out, m.views.Table.Sort)
	return out
}

func sortTasks(tasks []domain.Task, sort config.SortSettings) {
	if sort.Field == "" {
		return
	}
	asc := sort.Order != "desc"
	sortableLess := func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		switch sort.Field {
		case "title":
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case "priority":
			return priorityRank(a.Priority) < priorityRank(b.Priority)
		case "created_at":
			return a.CreatedAt < b.CreatedAt
		default:
			return a.ID < b.ID
		}
	}
	stableSort(tasks, sortableLess, asc)
}

func priorityRank(p domain.Priority) int {
	switch p {
	case domain.PriorityLow:
		return 1
	case domain.PriorityNormal:
		return 2
	case domain.PriorityHigh:
		return 3
	}
	return 0
}

func stableSort(tasks []domain.Task, less func(i, j int) bool, asc bool) {
	if asc {
		sort.SliceStable(tasks, less)
		return
	}
	sort.SliceStable(tasks, func(i, j int) bool { return less(j, i) })
}
