package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// tableSelectionModel builds a Model parked on the Tasks > table view
// with a deliberately disordered m.tasks slice so a non-default table
// sort/filter produces a visible projection that differs from raw task
// order. Used by the #594 regression tests to prove navigation, Enter,
// and move all target the visible (filtered/sorted) row.
func tableSelectionModel(tasks []domain.Task, view config.TableViewSettings) Model {
	m := Model{
		styles:     newStyles(config.Theme{}),
		width:      200,
		height:     50,
		top:        topTasks,
		sub:        subTable,
		tasks:      tasks,
		priorities: tableSelectionPriorities(),
	}
	m.views.Table = view
	return m
}

func tableSelectionPriorities() []config.PriorityDefinition {
	return []config.PriorityDefinition{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		{ID: 3, Value: "high"},
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestTableNavBoundsTrackFilteredRowCount pins acceptance criteria #1
// and #4: when the table is filtered by bucket, the cursor's lower
// bound is the visible filtered row count, not len(m.tasks). Pre-fix
// `end`/`down` clamped to len(m.tasks)-1 and could land the cursor on a
// row hidden by the filter.
func TestTableNavBoundsTrackFilteredRowCount(t *testing.T) {
	// Three tasks, two in "dev" and one in "backlog". The table filters
	// to bucket=dev, so only two rows are visible.
	tasks := []domain.Task{
		{ID: 1, Title: "Backlog one", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "Dev one", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 3, Title: "Dev two", BucketKey: "dev", Priority: domain.Priority(2)},
	}
	view := config.TableViewSettings{
		Filter: config.TableFilterSettings{Bucket: []string{"dev"}},
	}
	m := tableSelectionModel(tasks, view)

	if got := len(m.tableRows()); got != 2 {
		t.Fatalf("visible rows = %d, want 2", got)
	}

	// Jump to the end: cursor must clamp to the last VISIBLE row (index
	// 1), not len(m.tasks)-1 (index 2).
	m.handleListKey(keyMsg("end"))
	if m.selected != 1 {
		t.Fatalf("after end, selected = %d, want 1 (last visible row)", m.selected)
	}

	// One more `down` must not advance past the visible bound.
	m.handleListKey(keyMsg("down"))
	if m.selected != 1 {
		t.Fatalf("down past last visible row moved cursor to %d, want 1", m.selected)
	}
}

// TestTableEnterOpensVisibleSortedRow pins acceptance criteria #2: with
// a non-default sort, pressing Enter resolves to the task at the visible
// row, not the raw m.tasks[selected]. The slice is intentionally in a
// different order than the sort.
func TestTableEnterOpensVisibleSortedRow(t *testing.T) {
	// Raw order is C, A, B but the table sorts by title ascending, so
	// the visible order is A (id 1), B (id 2), C (id 3).
	tasks := []domain.Task{
		{ID: 3, Title: "C", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 1, Title: "A", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 2, Title: "B", BucketKey: "dev", Priority: domain.Priority(2)},
	}
	view := config.TableViewSettings{
		Sort: config.SortSettings{Field: "title", Order: "asc"},
	}
	m := tableSelectionModel(tasks, view)

	// Cursor on the second visible row (B, id 2).
	m.handleListKey(keyMsg("down"))
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1", m.selected)
	}

	task, ok := m.selectedTask()
	if !ok {
		t.Fatalf("selectedTask returned ok=false on visible row")
	}
	if task.ID != 2 {
		t.Fatalf("selectedTask id = %d, want 2 (visible sorted row B)", task.ID)
	}
	if id := m.selectedTaskID(); id != 2 {
		t.Fatalf("selectedTaskID = %d, want 2 (highlight marker row)", id)
	}
}

// TestTableMoveTargetsVisibleSortedRow pins acceptance criteria #3: a
// move started from the visible row binds to the visible selected task.
// Move and Enter share the selectedTask() seam, so this proves the move
// target tracks the visible projection too.
func TestTableMoveTargetsVisibleSortedRow(t *testing.T) {
	tasks := []domain.Task{
		{ID: 3, Title: "C", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 1, Title: "A", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 2, Title: "B", BucketKey: "dev", Priority: domain.Priority(2)},
	}
	view := config.TableViewSettings{
		Sort: config.SortSettings{Field: "title", Order: "asc"},
	}
	m := tableSelectionModel(tasks, view)

	// Cursor on the third visible row (C, id 3).
	m.handleListKey(keyMsg("end"))
	if m.selected != 2 {
		t.Fatalf("selected = %d, want 2", m.selected)
	}

	// `m` begins a move; the captured target must be the visible row.
	m.handleListKey(keyMsg("m"))
	if m.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", m.mode)
	}
	if m.moveInputTargetID != 3 {
		t.Fatalf("moveInputTargetID = %d, want 3 (visible sorted row C)", m.moveInputTargetID)
	}
}

// TestTableEnterNoVisibleRowsIsSafe pins acceptance criteria #4: when a
// filter hides every row, navigation and Enter must not panic or target
// a hidden raw task.
func TestTableEnterNoVisibleRowsIsSafe(t *testing.T) {
	tasks := []domain.Task{
		{ID: 1, Title: "Backlog one", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "Backlog two", BucketKey: "backlog", Priority: domain.Priority(2)},
	}
	// Filter to a bucket no task lives in -> zero visible rows.
	view := config.TableViewSettings{
		Filter: config.TableFilterSettings{Bucket: []string{"dev"}},
	}
	m := tableSelectionModel(tasks, view)

	if got := len(m.tableRows()); got != 0 {
		t.Fatalf("visible rows = %d, want 0", got)
	}

	// Navigation must not panic and must not leave the cursor pointing
	// at a hidden raw task.
	m.handleListKey(keyMsg("down"))
	m.handleListKey(keyMsg("end"))

	if _, ok := m.selectedTask(); ok {
		t.Fatalf("selectedTask returned ok=true with no visible rows")
	}
	if id := m.selectedTaskID(); id != 0 {
		t.Fatalf("selectedTaskID = %d, want 0 with no visible rows", id)
	}
}

// TestClampSelectionTracksVisibleRowCount pins that the refresh-time
// clamp bounds m.selected against the visible projection, not raw
// m.tasks — so a filter shrinking the visible set cannot leave the
// cursor on a hidden index.
func TestClampSelectionTracksVisibleRowCount(t *testing.T) {
	tasks := []domain.Task{
		{ID: 1, Title: "A", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 2, Title: "B", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 3, Title: "C", BucketKey: "backlog", Priority: domain.Priority(2)},
	}
	// Only one task is visible after the dev filter.
	view := config.TableViewSettings{
		Filter: config.TableFilterSettings{Bucket: []string{"dev"}},
	}
	m := tableSelectionModel(tasks, view)
	m.selected = 2 // stale index pointing past the visible row count

	m.clampSelection()

	if m.selected != 0 {
		t.Fatalf("clampSelection left selected = %d, want 0 (single visible row)", m.selected)
	}
}

// TestSelectTaskByIDUsesVisibleProjection pins that selectTaskByID lands
// the table cursor on the task's index WITHIN the visible projection,
// not its raw m.tasks position — so the highlighted row matches the
// requested task under a non-default sort.
func TestSelectTaskByIDUsesVisibleProjection(t *testing.T) {
	// Raw order C, A, B; sorted by title -> A, B, C. Task id 3 (C) is at
	// raw index 0 but visible index 2.
	tasks := []domain.Task{
		{ID: 3, Title: "C", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 1, Title: "A", BucketKey: "dev", Priority: domain.Priority(2)},
		{ID: 2, Title: "B", BucketKey: "dev", Priority: domain.Priority(2)},
	}
	view := config.TableViewSettings{
		Sort: config.SortSettings{Field: "title", Order: "asc"},
	}
	m := tableSelectionModel(tasks, view)

	if !m.selectTaskByID(3) {
		t.Fatalf("selectTaskByID(3) returned false")
	}
	if m.selected != 2 {
		t.Fatalf("selected = %d, want 2 (visible index of task C)", m.selected)
	}
	if id := m.selectedTaskID(); id != 3 {
		t.Fatalf("selectedTaskID = %d, want 3 after selectTaskByID", id)
	}
}
