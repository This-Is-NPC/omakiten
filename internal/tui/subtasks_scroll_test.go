package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestSubtasksCursorAdvancesOuterViewportScroll pins the navigation
// regression from task #238: pressing j on a focused sub-tasks panel
// must scroll the outer m.taskView.Viewport when the focused card
// falls below the joined-detail-screen slice. Without that, the
// cursor walked past the outer slice silently and the user saw
// `▲ 0 above · ▼ N below` with no movement.
func TestSubtasksCursorAdvancesOuterViewportScroll(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 80
	model.height = 24

	parent := domain.Task{ID: 1000, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	children := make([]domain.Task, 30)
	for i := range children {
		parentID := parent.ID
		children[i] = domain.Task{
			ID:        int64(2000 + i),
			Title:     "Child " + string(rune('A'+i%26)),
			BucketKey: "backlog",
			Priority:  domain.Priority(2),
			ParentID:  &parentID,
		}
	}
	model.tasks = append([]domain.Task{parent}, children...)
	model.taskID = parent.ID
	model.applyTaskFocus(taskFocusSubtasks)

	beforeOuter := model.taskView.Viewport.Scroll
	for i := 0; i < 25; i++ {
		model.moveSubtaskCursor(1)
	}
	afterOuter := model.taskView.Viewport.Scroll
	if afterOuter <= beforeOuter {
		t.Fatalf("outer viewport scroll did not advance after 25× j on sub-tasks; before=%d after=%d cursor=%d", beforeOuter, afterOuter, model.subtaskCursor)
	}
}

// TestSubtasksViewportRowsCarvesOutFormHeightInStackedLayout asserts
// the subtasksViewportRows fix: in stacked layout the budget must
// shrink by the form box height so the inner scroll-window math
// matches the actual on-screen real estate.
func TestSubtasksViewportRowsCarvesOutFormHeightInStackedLayout(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60 // narrow → forces stacked layout
	model.height = 40

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog"}
	model.tasks = []domain.Task{parent}
	model.taskID = parent.ID

	full := 40 - 7 // historic chrome=7 budget
	got := model.subtasksViewportRows()
	if got >= full {
		t.Fatalf("subtasksViewportRows = %d, want < %d (stacked layout should subtract form height)", got, full)
	}
}
