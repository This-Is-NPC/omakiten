package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// moveInputFixture returns a Model parked on a parent task that owns
// two sub-tasks under a sub-kit. Used by every test in this file so
// the routing of `m` between form/sub-task focus is exercised against
// a snapshot with distinct root vs sub-kit bucket sets.
func moveInputFixture(t *testing.T) Model {
	t.Helper()
	rootBundle := config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "shipped", Name: "Shipped", Position: 2},
			},
		}},
		SubtaskBundle: &config.Bundle{
			Kit:    config.Kit{Key: "sub"},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "sub",
				Name: "Sub",
				Buckets: []config.Bucket{
					{ID: 10, Key: "todo", Name: "Todo", Position: 1},
					{ID: 11, Key: "doing", Name: "Doing", Position: 2},
					{ID: 12, Key: "closed", Name: "Closed", Position: 3},
				},
			}},
		},
	}
	snap := config.BuildSnapshot(rootBundle)

	parent := domain.Task{ID: 100, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)}
	c1 := domain.Task{ID: 101, Title: "Child 1", BucketKey: "todo", Priority: domain.Priority(2), ParentID: ptrInt64(100)}
	c2 := domain.Task{ID: 102, Title: "Child 2", BucketKey: "doing", Priority: domain.Priority(2), ParentID: ptrInt64(100)}

	m := Model{
		styles:   newStyles(config.Theme{}),
		width:    200,
		height:   50,
		tasks:    []domain.Task{parent, c1, c2},
		workflow: snap.Workflow(),
		repos:    Repositories{Cache: runtimecache.Install(0, snap)},
	}
	m.openTaskView(parent)
	return m
}

// TestBeginMoveInputForTask_CapturesTargetID locks the routing fix:
// once `m` is pressed on a focused sub-task, the next modeMove
// submission must rewrite THAT child, not whatever `selectedTask`
// returns. Pre-fix the input had no per-task binding so the bucket
// key always landed on the open task screen's parent.
func TestBeginMoveInputForTask_CapturesTargetID(t *testing.T) {
	m := moveInputFixture(t)
	child := domain.Task{ID: 101, Title: "Child 1"}
	m.beginMoveInputForTask(child)
	if m.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", m.mode)
	}
	if m.moveInputTargetID != child.ID {
		t.Fatalf("moveInputTargetID = %d, want %d (focused child)", m.moveInputTargetID, child.ID)
	}
}

// TestCancelInputResetsMoveTargetID guards against the routing fix
// leaking across moves: a cancelled sub-task move must NOT carry the
// child id into the next move (which could be a parent move from the
// board lens).
func TestCancelInputResetsMoveTargetID(t *testing.T) {
	m := moveInputFixture(t)
	m.beginMoveInputForTask(domain.Task{ID: 101})
	if m.moveInputTargetID != 101 {
		t.Fatalf("setup precondition: moveInputTargetID = %d, want 101", m.moveInputTargetID)
	}
	m.cancelInput()
	if m.moveInputTargetID != 0 {
		t.Fatalf("moveInputTargetID = %d, want 0 after cancel", m.moveInputTargetID)
	}
}

// TestMoveInputPromptListsResolvedKitBuckets pins the bucket-key hint
// fix: the prompt label appends the resolved kit's bucket keys so the
// user sees the valid targets instead of guessing them. Sub-tasks see
// the sub-kit's keys; root tasks see the root-kit's keys.
func TestMoveInputPromptListsResolvedKitBuckets(t *testing.T) {
	m := moveInputFixture(t)

	parent := domain.Task{ID: 100}
	gotParent := m.moveInputPromptForTask(parent)
	for _, want := range []string{"backlog", "shipped"} {
		if !strings.Contains(gotParent, want) {
			t.Fatalf("parent prompt missing root-kit bucket %q; got %q", want, gotParent)
		}
	}
	for _, never := range []string{"todo", "doing", "closed"} {
		if strings.Contains(gotParent, never) {
			t.Fatalf("parent prompt leaked sub-kit bucket %q; got %q", never, gotParent)
		}
	}

	childParentID := int64(100)
	child := domain.Task{ID: 101, ParentID: &childParentID}
	gotChild := m.moveInputPromptForTask(child)
	for _, want := range []string{"todo", "doing", "closed"} {
		if !strings.Contains(gotChild, want) {
			t.Fatalf("child prompt missing sub-kit bucket %q; got %q", want, gotChild)
		}
	}
	for _, never := range []string{"backlog", "shipped"} {
		if strings.Contains(gotChild, never) {
			t.Fatalf("child prompt leaked root-kit bucket %q; got %q", never, gotChild)
		}
	}
}

// TestTaskViewMOnSubtasksPaneTargetsFocusedChild pins the focus-based
// routing: pressing `m` while the sub-tasks pane owns focus must set
// moveInputTargetID to the focused child id, NOT the open task id.
// Without this branch every sub-task move silently hit the parent
// because submitInput's `selectedTask()` always returns the open task
// while the task screen is up.
func TestTaskViewMOnSubtasksPaneTargetsFocusedChild(t *testing.T) {
	m := moveInputFixture(t)
	m.applyTaskFocus(taskFocusSubtasks)
	m.refreshSubtaskList()
	if m.subtasks.Cursor() < 0 {
		m.subtasks = m.subtasks.JumpFirst()
	}
	child, ok := m.activeSubtask()
	if !ok {
		t.Fatal("activeSubtask returned ok=false (fixture should have at least one child)")
	}

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	gotModel, ok := got.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", got)
	}
	if gotModel.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove after pressing m on sub-tasks pane", gotModel.mode)
	}
	if gotModel.moveInputTargetID != child.ID {
		t.Fatalf("moveInputTargetID = %d, want %d (focused child) — m routed to parent instead of child", gotModel.moveInputTargetID, child.ID)
	}
	if gotModel.moveInputTargetID == m.taskID {
		t.Fatalf("moveInputTargetID matches parent id %d — sub-task routing regressed", m.taskID)
	}
}

// TestTaskViewMOnFormPaneTargetsOpenTask is the regression guard for
// the other branch: when the form pane (default focus) owns focus,
// `m` must still target the open task. The pre-fix behaviour is
// preserved for parent-task moves.
func TestTaskViewMOnFormPaneTargetsOpenTask(t *testing.T) {
	m := moveInputFixture(t)
	m.applyTaskFocus(taskFocusForm)

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	gotModel, ok := got.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", got)
	}
	if gotModel.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", gotModel.mode)
	}
	if gotModel.moveInputTargetID != m.taskID {
		t.Fatalf("moveInputTargetID = %d, want %d (open parent) — form-pane m no longer routes to parent", gotModel.moveInputTargetID, m.taskID)
	}
}

// TestModeMoveSubmitMovesSubtaskNotParent is the end-to-end proof: a
// sub-task move via the new routing path mutates only the focused
// child's bucket; the parent stays where it was. Without the
// moveInputTargetID fix this test would surface the regression as the
// parent landing in `dev` while the child stayed put.
func TestModeMoveSubmitMovesSubtaskNotParent(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	parent, err := store.CreateTask(ctx, project.ID, "Parent", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	pid := parent.ID
	child, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", &pid, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask child: %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Events:       store,
		ActivityLogs: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// Open the parent task screen via the table lens so taskID is set.
	got := pressStringKey(t, model, "/")
	got = pressKey(t, got, tea.KeyEnter)
	if got.taskID != parent.ID {
		t.Fatalf("taskID after open = %d, want parent %d", got.taskID, parent.ID)
	}

	// Focus the sub-tasks pane, ensure cursor lands on the child, then
	// trigger the move flow.
	got = pressStringKey(t, got, "s")
	if got.taskFocus != taskFocusSubtasks {
		t.Fatalf("taskFocus = %v, want subtasks", got.taskFocus)
	}
	focused, ok := got.activeSubtask()
	if !ok || focused.ID != child.ID {
		t.Fatalf("activeSubtask = (%+v, %v), want the only child id %d", focused, ok, child.ID)
	}

	got = pressRune(t, got, 'm')
	if got.mode != modeMove {
		t.Fatalf("mode after m = %v, want modeMove", got.mode)
	}
	if got.moveInputTargetID != child.ID {
		t.Fatalf("moveInputTargetID = %d, want child %d (routing regression)", got.moveInputTargetID, child.ID)
	}
	got = sendText(t, got, "dev")
	got = pressKey(t, got, tea.KeyEnter)
	if got.mode != modeNormal {
		t.Fatalf("mode after enter = %v, want modeNormal (submit failed: status=%q)", got.mode, got.status)
	}
	if got.moveInputTargetID != 0 {
		t.Fatalf("moveInputTargetID = %d after submit, want 0 (not reset)", got.moveInputTargetID)
	}

	rows, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var parentAfter, childAfter domain.Task
	for _, r := range rows {
		switch r.ID {
		case parent.ID:
			parentAfter = r
		case child.ID:
			childAfter = r
		}
	}
	if childAfter.BucketKey != "dev" {
		t.Fatalf("child bucket = %q, want dev (sub-task move dropped — routing regression)", childAfter.BucketKey)
	}
	if parentAfter.BucketKey != "backlog" {
		t.Fatalf("parent bucket = %q, want backlog (parent was incorrectly moved — pre-fix bug)", parentAfter.BucketKey)
	}
}
