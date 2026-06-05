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
	// Navigation bounds track the visible (filtered/sorted) table
	// projection, not raw m.tasks — m.selected indexes the row the user
	// actually sees, so a non-default table sort/filter can never carry
	// the cursor onto a hidden task. See task #594.
	rowCount := len(m.tableRows())
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.syncTableScroll()
		}
	case "down", "j":
		if m.selected < rowCount-1 {
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
		if m.selected > rowCount-1 {
			m.selected = rowCount - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		m.syncTableScroll()
	case "home", "g":
		m.selected = 0
		m.syncTableScroll()
	case "end", "G":
		if rowCount > 0 {
			m.selected = rowCount - 1
			m.syncTableScroll()
		}
	case "enter":
		if task, ok := m.selectedTask(); ok {
			m.openTaskView(task)
		}
	case "m":
		if task, ok := m.selectedTask(); ok {
			m.beginMoveInputForTask(task)
		}
	}
}

// syncTableScroll syncs the tableList linelist.Model so the selected
// task row stays in view. Routes through WithLines + WithViewport +
// WithCursor so scrollwindow.Resync owns the follow-cursor + clamp
// chain.
func (m *Model) syncTableScroll() {
	tasks := m.applyTableView()
	lines := make([]string, len(tasks))
	m.tableList = m.tableList.WithLines(lines).WithViewport(m.tableViewportRows()).WithCursor(m.selected)
}

// tableViewportRows returns how many task rows fit in the table panel.
// Sources its chrome from the shared `panelViewportRows` helper so the
// budget tracks live header / status / nav strip changes — the prior
// hard-coded `chrome := 13` undercounted the screen header on the
// standard 4-line nav layout, so the table over-rendered and the bottom
// rows fell off the terminal. Panel chrome here = 2 borders + 3 header
// rows (kicker / info / separator) = 5.
func (m Model) tableViewportRows() int {
	return m.panelViewportRows(5)
}

func (m Model) renderTable() string {
	tasks := m.applyTableView()
	if len(tasks) == 0 {
		if len(m.tasks) == 0 {
			return m.renderPanel(m.t("tui.empty.table_no_tasks"))
		}
		return m.renderPanel(m.t("tui.empty.table_filtered"))
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
		marker := m.cursorMarker(selectedID == task.ID)
		// Cap the variable-width columns to their field budgets so an
		// overlong bucket key or priority label can't push the fixed-width
		// `%-11s`/`%-8s` cells past their slots and bleed into the title /
		// past the panel edge. The title uses the visible-width truncateText
		// so a wide-glyph or unbroken title stays inside its column too.
		bucket := truncateText(task.BucketKey, 11)
		prio := truncateText(m.priorityLabel(task.Priority), 8)
		dataRows = append(dataRows, fmt.Sprintf("%s %-4d %-11s %-8s %-5d %-9d %s", marker, task.ID, bucket, prio, m.dependencyCount(task.ID), m.commentCount(task.ID), truncateText(task.Title, titleWidth)))
	}

	rows := []string{
		m.styles.kickerCount(m.t("tui.kicker.tasks"), len(tasks)),
		m.styles.info.Render(m.t("tui.table.header")),
		m.hRule(contentWidth),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableList.Scroll(), m.tableViewportRows())...)
	return m.renderPanel(strings.Join(rows, "\n"))
}

func (m Model) renderTableCompactWith(tasks []domain.Task) string {
	width := clampInt(m.availableWidth()-4, 32, 68)
	selectedID := m.selectedTaskID()
	dataRows := make([]string, 0, len(tasks))
	for _, task := range tasks {
		marker := m.cursorMarker(selectedID == task.ID)
		// Cap the bucket key + priority label so a long key/label can't blow
		// the prefix past the compact width — once the prefix overflows the
		// title budget clamps to its floor but the prefix itself still
		// overflows. Bounding both keeps the whole row inside `width`.
		bucket := truncateText(task.BucketKey, 12)
		prio := truncateText(m.priorityLabel(task.Priority), 10)
		prefix := fmt.Sprintf("%s #%d %s %s ", marker, task.ID, bucket, prio)
		// Remaining cells after the prefix decide the title budget. When the
		// prefix already consumes (nearly) the whole row — a tiny terminal or a
		// long bounded prefix — there is no room for a title, so emit the
		// prefix trimmed to the row width and skip the title rather than letting
		// a floored budget push the row past `width`.
		remaining := width - lipgloss.Width(prefix)
		if remaining < 8 {
			dataRows = append(dataRows, truncateText(strings.TrimRight(prefix, " "), width))
			continue
		}
		dataRows = append(dataRows, prefix+truncateText(task.Title, remaining))
	}
	rows := []string{
		m.styles.kickerCount(m.t("tui.kicker.tasks"), len(tasks)),
		m.hRule(width),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableList.Scroll(), m.tableViewportRows())...)
	return m.renderPanel(strings.Join(rows, "\n"))
}

// tableRows returns the visible (filtered/sorted) table projection that
// m.selected indexes into. Centralised so navigation bounds, the
// highlight marker, Enter-open, and move all resolve against the exact
// rows the user sees — never raw m.tasks. See task #594.
func (m Model) tableRows() []domain.Task {
	return m.applyTableView()
}

// selectedTaskID resolves the currently-selected task id from the visible
// table projection; used by the table view to flag the highlighted row.
// Indexing the projection (not raw m.tasks) keeps the marker on the row
// the cursor actually sits on under a non-default table sort/filter.
func (m Model) selectedTaskID() int64 {
	rows := m.tableRows()
	if m.selected < 0 || m.selected >= len(rows) {
		return 0
	}
	return rows[m.selected].ID
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
	if m.cachedTableView != nil {
		return m.cachedTableView
	}
	return buildTableView(m.tasks, m.views.Table, m.priorities)
}

// buildTableView is the stateless filter+sort the refresh() cache
// populator and the value-receiver fallback share.
func buildTableView(tasks []domain.Task, view config.TableViewSettings, priorities []config.PriorityDefinition) []domain.Task {
	prioAllowed := priorityAllowSet(view.Filter.Priority)
	bucketAllowedSet := bucketAllowSet(view.Filter.Bucket)
	out := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if !priorityAllowedFromTable(prioAllowed, task.Priority, priorities) {
			continue
		}
		if !bucketAllowed(bucketAllowedSet, task.BucketKey) {
			continue
		}
		out = append(out, task)
	}
	sortTasks(out, view.Sort)
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

// priorityRank returns the configured id of the priority — which is also
// the sort weight (config authors order the priorities table low→high
// by id). Treating the id as the rank means renaming a label or
// inserting a new mid-priority is a YAML edit, not a code change.
func priorityRank(p domain.Priority) int {
	return int(p)
}

func stableSort(tasks []domain.Task, less func(i, j int) bool, asc bool) {
	if asc {
		sort.SliceStable(tasks, less)
		return
	}
	sort.SliceStable(tasks, func(i, j int) bool { return less(j, i) })
}
