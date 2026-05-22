package tui

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func ptrInt64(v int64) *int64 { return &v }

// taskDrillFixture builds a lightweight Model with a parent + child
// + grandchild already loaded. The repos field stays empty so
// refreshTaskActivity short-circuits — we are exercising in-memory
// drill state, not the activity reload path.
func taskDrillFixture(t *testing.T) Model {
	t.Helper()
	parent := domain.Task{ID: 100, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	child := domain.Task{ID: 101, Title: "Child", BucketKey: "backlog", Priority: domain.Priority(2), ParentID: ptrInt64(100)}
	grand := domain.Task{ID: 102, Title: "Grandchild", BucketKey: "backlog", Priority: domain.Priority(2), ParentID: ptrInt64(101)}
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  160,
		height: 40,
		tasks:  []domain.Task{parent, child, grand},
	}
	m.openTaskView(parent)
	return m
}

func TestToggleTaskFocusRotatesThroughSubtasks(t *testing.T) {
	m := taskDrillFixture(t)

	if m.taskFocus != taskFocusForm {
		t.Fatalf("openTaskView default focus = %v, want taskFocusForm", m.taskFocus)
	}
	m.toggleTaskFocus()
	if m.taskFocus != taskFocusSubtasks {
		t.Fatalf("first tab focus = %v, want taskFocusSubtasks (has children)", m.taskFocus)
	}
	if m.subtaskCursor != 0 {
		t.Errorf("subtaskCursor on entering subtasks zone = %d, want 0 (auto-land on first card)", m.subtaskCursor)
	}
	m.toggleTaskFocus()
	if m.taskFocus != taskFocusActivity {
		t.Fatalf("second tab focus = %v, want taskFocusActivity", m.taskFocus)
	}
	if m.subtaskCursor != -1 {
		t.Errorf("subtaskCursor after leaving subtasks = %d, want -1 (cleared)", m.subtaskCursor)
	}
	m.toggleTaskFocus()
	if m.taskFocus != taskFocusForm {
		t.Fatalf("third tab focus = %v, want taskFocusForm (rotation wraps)", m.taskFocus)
	}
}

func TestToggleTaskFocusSkipsSubtasksWhenNoChildren(t *testing.T) {
	leaf := domain.Task{ID: 200, Title: "Leaf", BucketKey: "backlog", Priority: domain.Priority(2)}
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  160,
		height: 40,
		tasks:  []domain.Task{leaf},
	}
	m.openTaskView(leaf)

	m.toggleTaskFocus()
	if m.taskFocus != taskFocusActivity {
		t.Fatalf("tab focus on childless task = %v, want taskFocusActivity (skip empty subtasks)", m.taskFocus)
	}
}

func TestDrillIntoSubtaskPushesParentOntoStack(t *testing.T) {
	m := taskDrillFixture(t)
	m.toggleTaskFocus() // form → subtasks
	if m.subtaskCursor != 0 {
		t.Fatalf("subtaskCursor = %d, want 0", m.subtaskCursor)
	}
	m.drillIntoSubtask()

	if m.taskID != 101 {
		t.Fatalf("taskID after drill = %d, want 101 (child)", m.taskID)
	}
	if len(m.taskViewStack) != 1 || m.taskViewStack[0] != 100 {
		t.Fatalf("taskViewStack after drill = %v, want [100]", m.taskViewStack)
	}
	if m.subtaskCursor != -1 {
		t.Errorf("subtaskCursor after drill = %d, want -1 (reset on open)", m.subtaskCursor)
	}
}

func TestPopTaskViewStackReturnsToParent(t *testing.T) {
	m := taskDrillFixture(t)
	m.toggleTaskFocus()
	m.drillIntoSubtask() // 100 → 101
	m.toggleTaskFocus()
	m.drillIntoSubtask() // 101 → 102

	if m.taskID != 102 {
		t.Fatalf("taskID after two drills = %d, want 102", m.taskID)
	}
	if len(m.taskViewStack) != 2 {
		t.Fatalf("stack depth after two drills = %d, want 2", len(m.taskViewStack))
	}

	if !m.popTaskViewStack() {
		t.Fatal("popTaskViewStack returned false on non-empty stack")
	}
	if m.taskID != 101 {
		t.Fatalf("taskID after one pop = %d, want 101", m.taskID)
	}
	if len(m.taskViewStack) != 1 || m.taskViewStack[0] != 100 {
		t.Fatalf("stack after one pop = %v, want [100]", m.taskViewStack)
	}

	if !m.popTaskViewStack() {
		t.Fatal("popTaskViewStack returned false on second pop")
	}
	if m.taskID != 100 {
		t.Fatalf("taskID after two pops = %d, want 100 (root parent)", m.taskID)
	}
	if len(m.taskViewStack) != 0 {
		t.Fatalf("stack after two pops = %v, want []", m.taskViewStack)
	}

	if m.popTaskViewStack() {
		t.Fatal("popTaskViewStack returned true on empty stack")
	}
}

func TestPopTaskViewStackHandlesDeletedAncestor(t *testing.T) {
	m := taskDrillFixture(t)
	m.toggleTaskFocus()
	m.drillIntoSubtask() // → 101 with stack [100]
	// Simulate parent being deleted while we were drilled in.
	m.tasks = []domain.Task{
		{ID: 101, Title: "Orphan now", BucketKey: "backlog", Priority: domain.Priority(2)},
	}
	if m.popTaskViewStack() {
		t.Fatal("popTaskViewStack returned true when ancestor was deleted; want false (fall through to default close)")
	}
	if len(m.taskViewStack) != 0 {
		t.Fatalf("stack after failed pop = %v, want [] (cleared)", m.taskViewStack)
	}
}

func TestTaskBreadcrumbTrailFormatsAncestors(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	if got := m.taskBreadcrumbTrail(); got != "" {
		t.Fatalf("empty stack breadcrumb = %q, want \"\"", got)
	}
	m.taskViewStack = []int64{1, 2, 3}
	got := stripANSI(m.taskBreadcrumbTrail())
	if !strings.Contains(got, "#3") || !strings.Contains(got, "#2") || !strings.Contains(got, "#1") {
		t.Fatalf("breadcrumb %q must mention all 3 ancestors", got)
	}
	if !strings.Contains(got, "←") {
		t.Fatalf("breadcrumb %q must use ← separator", got)
	}
	m.taskViewStack = []int64{1, 2, 3, 4, 5, 6}
	got = stripANSI(m.taskBreadcrumbTrail())
	if !strings.Contains(got, "…") {
		t.Fatalf("breadcrumb %q must elide with … when depth > 3", got)
	}
	// Trimming keeps only the last 3 ancestors visible (4,5,6).
	if !strings.Contains(got, "#6") || !strings.Contains(got, "#5") || !strings.Contains(got, "#4") {
		t.Fatalf("breadcrumb %q must show last 3 ancestors (4,5,6)", got)
	}
}

func TestOpenDescriptionScreenFlipsFlag(t *testing.T) {
	m := taskDrillFixture(t)
	parent, ok := m.activeTask()
	if !ok {
		t.Fatal("activeTask returned !ok on fixture")
	}
	m.openDescriptionScreen(parent)
	if !m.descriptionScreenOpen {
		t.Fatal("descriptionScreenOpen = false after openDescriptionScreen")
	}
	m.closeDescriptionScreen()
	if m.descriptionScreenOpen {
		t.Fatal("descriptionScreenOpen = true after closeDescriptionScreen")
	}
}

func TestRenderTaskViewIncludesSubtaskCards(t *testing.T) {
	m := taskDrillFixture(t)
	m.width = 200
	out := stripANSI(m.renderTaskView())
	// Sub-tasks column should now carry the kicker + child id chip.
	if !strings.Contains(out, "SUB-TASKS") {
		t.Fatalf("expected SUB-TASKS kicker in wide layout, got:\n%s", out)
	}
	if !strings.Contains(out, "#101") {
		t.Fatalf("expected child #101 card in sub-tasks column, got:\n%s", out)
	}
	// The breadcrumb only appears on drilled views, not on the root.
	if strings.Contains(out, "← #") {
		t.Fatalf("unexpected breadcrumb on root task view:\n%s", out)
	}
}

func TestRenderTaskViewBreadcrumbAfterDrill(t *testing.T) {
	m := taskDrillFixture(t)
	m.width = 200
	m.toggleTaskFocus()
	m.drillIntoSubtask()
	out := stripANSI(m.renderTaskView())
	if !strings.Contains(out, "← #100") {
		t.Fatalf("expected breadcrumb '← #100' on drilled view, got:\n%s", out)
	}
}

func TestRenderTaskViewShowsSubtasksEmptyState(t *testing.T) {
	leaf := domain.Task{ID: 200, Title: "Leaf", BucketKey: "backlog", Priority: domain.Priority(2)}
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  200,
		height: 40,
		tasks:  []domain.Task{leaf},
	}
	m.openTaskView(leaf)
	out := stripANSI(m.renderTaskView())
	if !strings.Contains(out, "SUB-TASKS") {
		t.Fatalf("childless task view must still render SUB-TASKS pane (empty state), got:\n%s", out)
	}
	if !strings.Contains(out, "No sub-tasks") {
		t.Fatalf("empty sub-tasks pane must surface the empty-state hint, got:\n%s", out)
	}
}

func TestMoveSubtaskCursorClampsToBounds(t *testing.T) {
	m := taskDrillFixture(t)
	// One child of task #100 (#101). Cursor begins at -1.
	m.moveSubtaskCursor(1)
	if m.subtaskCursor != 0 {
		t.Fatalf("moveSubtaskCursor(+1) from -1 = %d, want 0", m.subtaskCursor)
	}
	m.moveSubtaskCursor(1)
	if m.subtaskCursor != 0 {
		t.Fatalf("moveSubtaskCursor past last child = %d, want 0 (clamped)", m.subtaskCursor)
	}
	m.moveSubtaskCursor(-5)
	if m.subtaskCursor != 0 {
		t.Fatalf("moveSubtaskCursor below 0 = %d, want 0 (clamped)", m.subtaskCursor)
	}
}
