package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestActivityViewportLinesShrinksInStackedLayout pins the W11-B-5
// bug-2 fix: activityViewportLines must subtract the form + sub-tasks
// budget when the task detail view is in stacked layout, so the
// activity panel does not grow taller than the outer taskViewportHeight
// allows. Prior behaviour read m.height directly and produced a joined
// content string twice the terminal height in fullscreen.
func TestActivityViewportLinesShrinksInStackedLayout(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60 // narrow → forces stacked layout
	model.height = 40

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	children := make([]domain.Task, 5)
	for i := range children {
		parentID := parent.ID
		children[i] = domain.Task{
			ID:        int64(100 + i),
			Title:     "Child",
			BucketKey: "backlog",
			Priority:  domain.Priority(2),
			ParentID:  &parentID,
		}
	}
	model.tasks = append([]domain.Task{parent}, children...)
	model.taskID = parent.ID
	model.taskScreen = taskScreenView

	rows := model.activityViewportLines()

	// In stacked layout with form ≈ 10 rows + sub-tasks panel
	// (header + 5 cards × ~4 rows each ≈ 23 rows) the leftover row
	// budget for activity must be well under the height-based
	// estimate (40 - 9 = 31). Anything ≥ 31 means the layout-aware
	// branch did not fire and bug 2 is alive.
	if rows >= 31 {
		t.Fatalf("activityViewportLines in stacked layout = %d, want < 31 (form + sub-tasks should carve out budget)", rows)
	}
}

// TestActivityViewportLinesUnaffectedOutsideTaskView pins the
// no-regression contract: when the task detail view is not open
// (m.taskScreen != taskScreenView), the layout-aware branch must
// stay off and the prior height-based budget applies. Stats / Logs /
// other surfaces don't touch activity, but the helper would still be
// called by stale callers if the gate were too permissive.
func TestActivityViewportLinesUnaffectedOutsideTaskView(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60
	model.height = 40
	model.taskScreen = taskScreenClosed

	rows := model.activityViewportLines()
	// height-based budget = 40 - activityChromeBase(9) = 31.
	if rows != 31 {
		t.Fatalf("activityViewportLines outside task view = %d, want 31 (height-based fallback)", rows)
	}
}
