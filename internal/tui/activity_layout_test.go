package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestActivityViewportLinesStackedFillsOuter pins the W13 single-pane
// stacked contract: when the terminal is too narrow for the side-by-
// side layout, the activity panel — when focused — owns the full
// outer viewport. The pre-W13 joint split would shrink activity to
// share with sub-tasks even when sub-tasks was not on screen.
func TestActivityViewportLinesStackedFillsOuter(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60 // narrow → forces stacked layout
	model.height = 40

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	model.tasks = []domain.Task{parent}
	model.taskID = parent.ID
	model.taskScreen = taskScreenView
	model.taskFocus = taskFocusActivity

	rows := model.activityViewportLines()

	// Outer viewport = m.height - 5 (screen chrome) = 35.
	// Activity rows = OuterH - 2 (header) - 2 (borders) = 31.
	// Anything well below outer means the joint split is still alive.
	if rows < 25 {
		t.Fatalf("activityViewportLines stacked = %d, want close to outer viewport (~31)", rows)
	}
}

// TestActivityViewportLinesStackedIgnoresChildCount pins the
// single-pane contract: the activity row budget does not shrink as
// sub-tasks grow. Pre-W12 the helper summed every child card height
// and subtracted it; W12 used a joint split that still moved rows
// with focus. W13 is independent of sub-tasks: tab to activity =
// activity fills the screen, regardless of how many children the
// parent task has.
// TestActivityViewportLinesStackedFormFocusShrinksForSiblings pins
// the W14 form-focus cascade: when the user is on the form panel in
// stacked layout and the terminal has room for all three panels, the
// activity slot only gets half of the leftover (50/50 split with
// sub-tasks), not the full outer viewport. The single-pane fill-
// outer behaviour stays exclusive to subtasks/activity focus.
func TestActivityViewportLinesStackedFormFocusShrinksForSiblings(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60 // narrow → forces stacked layout
	model.height = 50 // tall enough for the 3-pane cascade

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	model.tasks = []domain.Task{parent}
	model.taskID = parent.ID
	model.taskScreen = taskScreenView
	model.taskFocus = taskFocusForm

	formRows := model.activityViewportLines()

	model.taskFocus = taskFocusActivity
	singlePaneRows := model.activityViewportLines()

	// Single-pane budget MUST exceed the form-focus 3-pane slot —
	// otherwise the cascade and the single-pane path are returning
	// the same value and one of the policies is broken.
	if singlePaneRows <= formRows {
		t.Fatalf("activityViewportLines form-focus (%d) ≥ activity-focus (%d) — cascade not shrinking sibling slot", formRows, singlePaneRows)
	}
}

func TestActivityViewportLinesStackedIgnoresChildCount(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 60
	model.height = 40

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	model.taskID = parent.ID
	model.taskScreen = taskScreenView
	model.taskFocus = taskFocusActivity

	model.tasks = []domain.Task{parent}
	noChildren := model.activityViewportLines()

	model.tasks = []domain.Task{parent}
	for i := 0; i < 20; i++ {
		parentID := parent.ID
		model.tasks = append(model.tasks, domain.Task{
			ID:        int64(100 + i),
			Title:     "Child",
			BucketKey: "backlog",
			Priority:  domain.Priority(2),
			ParentID:  &parentID,
		})
	}
	manyChildren := model.activityViewportLines()

	if noChildren != manyChildren {
		t.Fatalf("activityViewportLines depends on child count: 0→%d vs 20→%d (single-pane should be independent)", noChildren, manyChildren)
	}
}
