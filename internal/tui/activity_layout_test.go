package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestActivityViewportLinesShrinksInStackedLayout pins the W12 joint-
// split contract: in stacked layout the activity panel must not claim
// the full m.height-based budget — it shares the row leftover with the
// sub-tasks panel via layout.TaskViewBudget. The pre-W12 surface
// summed every child card's height to estimate sub-tasks and then
// asked for the remainder; with many children the activity slot
// floored to activityViewportMinLines (~6) regardless of focus. The
// new split decides both shares from the leftover, independent of
// the measured sub-tasks height.
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

	// Joint split caps activity at roughly half the leftover under the
	// form. On a 40-row terminal the prior height-based bound was 31
	// rows; the new joint-aware budget must come in well under that.
	if rows >= 31 {
		t.Fatalf("activityViewportLines in stacked layout = %d, want < 31 (joint split should shrink activity)", rows)
	}
	// Floor still applies — never below the minimum-readable budget.
	if rows < activityViewportMinLines {
		t.Fatalf("activityViewportLines in stacked layout = %d, want ≥ activityViewportMinLines (%d)", rows, activityViewportMinLines)
	}
}

// TestActivityViewportLinesFocusShiftsBudget pins the focus-aware
// behaviour: when the user tabs from sub-tasks-focused to activity-
// focused, the activity row budget must grow (and sub-tasks must
// shrink). The split lives inside layout.TaskViewBudget; this test
// reads it through the panel surface to make sure the Focus field
// flows from m.taskFocus correctly.
func TestActivityViewportLinesFocusShiftsBudget(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	// Need a terminal tall enough that the joint-split mins do not
	// dominate both shares — when the leftover is small both panels
	// floor to their mins and the focus signal vanishes. 60 rows
	// gives enough headroom for the 65/35 split to produce different
	// budgets per focus.
	model.width = 60
	model.height = 60

	parent := domain.Task{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	children := make([]domain.Task, 3)
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

	model.taskFocus = taskFocusSubtasks
	subFocusRows := model.activityViewportLines()

	model.taskFocus = taskFocusActivity
	actFocusRows := model.activityViewportLines()

	if actFocusRows <= subFocusRows {
		t.Fatalf("activity-focused budget (%d) did not grow over sub-tasks-focused (%d) — focus signal not flowing", actFocusRows, subFocusRows)
	}
}
