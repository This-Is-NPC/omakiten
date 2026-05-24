package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestSubtasksCursorAdvancesInnerScrollInStacked pins the W13
// single-pane navigation contract: pressing j on the focused
// sub-tasks panel in stacked layout must advance the cardlist's
// internal scroll when the cursor walks past the visible band. The
// outer m.taskView.Viewport stays at 0 in single-pane mode — the
// cardlist owns the visible window because the sub-tasks panel
// IS the outer view. Pre-W13 this would have been an outer-scroll
// chase; W13 is inner-scroll only.
func TestSubtasksCursorAdvancesInnerScrollInStacked(t *testing.T) {
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

	beforeInner := model.subtasks.Scroll()
	for i := 0; i < 25; i++ {
		model.moveSubtaskCursor(1)
	}
	afterInner := model.subtasks.Scroll()
	if afterInner <= beforeInner {
		t.Fatalf("cardlist inner scroll did not advance after 25× j on sub-tasks; before=%d after=%d cursor=%d", beforeInner, afterInner, model.subtasks.Cursor())
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

// TestSubtaskScrollIsCardIndexNotLineOffset pins the bug 1 fix:
// before W11-B-1 the m.subtaskScroll field carried a LINE offset
// (cursor*4) that renderColumnFrame interpreted as a CARD INDEX,
// so after a handful of j keystrokes the inner slice clamped to
// the last card and the cursor sat on a middle card the user
// could no longer see. With the cardlist component owning the
// scroll field internally, the contract is one type deep — the
// scroll field stays in [0, len(items)-1] regardless of how many
// keys the user pressed.
func TestSubtaskScrollIsCardIndexNotLineOffset(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 120 // wide → side-by-side layout (no outer-scroll compensation hides the bug)
	model.height = 30

	parent := domain.Task{ID: 9000, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	const childCount = 12
	children := make([]domain.Task, childCount)
	for i := range children {
		parentID := parent.ID
		children[i] = domain.Task{
			ID:        int64(9100 + i),
			Title:     "Child",
			BucketKey: "backlog",
			Priority:  domain.Priority(2),
			ParentID:  &parentID,
		}
	}
	model.tasks = append([]domain.Task{parent}, children...)
	model.taskID = parent.ID
	model.applyTaskFocus(taskFocusSubtasks)

	// Walk the cursor through every child. After every j, the
	// cardlist's Scroll() must stay inside [0, childCount-1] and
	// the cursor itself must never sit above the scroll offset —
	// both contracts the pre-W11-B-1 line-offset math violated.
	for step := 0; step < childCount; step++ {
		if step > 0 {
			model.moveSubtaskCursor(1)
		}
		scroll := model.subtasks.Scroll()
		cursor := model.subtasks.Cursor()
		if scroll < 0 || scroll >= childCount {
			t.Fatalf("step %d: subtasks.Scroll=%d out of range [0,%d)", step, scroll, childCount)
		}
		if cursor < scroll {
			t.Fatalf("step %d: cursor=%d sat above scroll=%d (cursor scrolled off the top — bug 1 regression)", step, cursor, scroll)
		}
	}
}
