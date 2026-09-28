package tui

import (
	"context"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screens/taskdetail"
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

func subtaskMoveSubmitFixture(t *testing.T) (context.Context, *snapstore.Store, domain.Project, domain.Task, domain.Task, Model) {
	t.Helper()
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
	parentID := parent.ID
	child, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", &parentID, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask child: %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store,
		Cache: runtimecache.InstallWithStore(0, store), Events: store,
		ActivityLogs: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return ctx, store, project, parent, child, model
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

func TestMoveInputRouteSanitizesWorkflowBucketPrompt(t *testing.T) {
	m := moveInputFixture(t)
	hostile := "dev 漢字 \x1b[31mred\x1b]0;owned\a \x00\u009b31m\u009d"
	m.repos.Cache = runtimecache.Install(0, config.BuildSnapshot(config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			Key:     "root",
			Buckets: []config.Bucket{{Key: hostile, Position: 1}},
		}},
	}))
	m.beginMoveInputForTask(domain.Task{ID: 100})
	plain := ansi.Strip(m.renderInput())
	if strings.Contains(plain, "owned") {
		t.Fatalf("move prompt retained OSC payload: %q", plain)
	}
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("move prompt retained control U+%04X: %q", r, plain)
		}
	}
	if !strings.Contains(plain, "漢字") {
		t.Fatalf("move prompt lost harmless Unicode: %q", plain)
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
	m.storeScreen(m.taskDetailScreen.WithFocus(taskdetail.FocusSubtasks))
	child, ok := m.taskDetailScreen.FocusedSubtask()
	if !ok {
		t.Fatal("activeSubtask returned ok=false (fixture should have at least one child)")
	}

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	gotModel, ok := got.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", got)
	}
	if gotModel.taskDetailScreen.State().Mode != taskdetail.ModeMove {
		t.Fatalf("mode = %v, want move after pressing m on sub-tasks pane", gotModel.taskDetailScreen.State().Mode)
	}
	if gotModel.taskDetailScreen.MoveTaskID() != child.ID {
		t.Fatalf("move target = %d, want %d (focused child)", gotModel.taskDetailScreen.MoveTaskID(), child.ID)
	}
	parentID := m.taskDetailScreen.Payload().Task.ID
	if gotModel.taskDetailScreen.MoveTaskID() == parentID {
		t.Fatalf("move target matches parent id %d", parentID)
	}
}

// TestTaskViewMOnFormPaneTargetsOpenTask is the regression guard for
// the other branch: when the form pane (default focus) owns focus,
// `m` must still target the open task. The pre-fix behaviour is
// preserved for parent-task moves.
func TestTaskViewMOnFormPaneTargetsOpenTask(t *testing.T) {
	m := moveInputFixture(t)
	m.storeScreen(m.taskDetailScreen.WithFocus(taskdetail.FocusDetails))

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	gotModel, ok := got.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", got)
	}
	if gotModel.taskDetailScreen.State().Mode != taskdetail.ModeMove {
		t.Fatalf("mode = %v, want move", gotModel.taskDetailScreen.State().Mode)
	}
	parentID := m.taskDetailScreen.Payload().Task.ID
	if gotModel.taskDetailScreen.MoveTaskID() != parentID {
		t.Fatalf("move target = %d, want %d (open parent)", gotModel.taskDetailScreen.MoveTaskID(), parentID)
	}
}

// TestModeMoveSubmitMovesSubtaskNotParent is the end-to-end proof: a
// sub-task move via the new routing path mutates only the focused
// child's bucket; the parent stays where it was. Without the
// moveInputTargetID fix this test would surface the regression as the
// parent landing in `dev` while the child stayed put.
func TestModeMoveSubmitMovesSubtaskNotParent(t *testing.T) {
	ctx, store, project, parent, child, model := subtaskMoveSubmitFixture(t)

	// Open the parent task screen via the table lens so taskID is set.
	got := pressStringKey(t, model, "/")
	got = pressKey(t, got, tea.KeyEnter)
	if got.taskDetailScreen.Payload().Task.ID != parent.ID {
		t.Fatalf("taskID after open = %d, want parent %d", got.taskDetailScreen.Payload().Task.ID, parent.ID)
	}

	// Focus the sub-tasks pane, ensure cursor lands on the child, then
	// trigger the move flow.
	got = pressStringKey(t, got, "s")
	if got.taskDetailScreen.State().Focus != taskdetail.FocusSubtasks {
		t.Fatalf("task focus = %v, want subtasks", got.taskDetailScreen.State().Focus)
	}
	focused, ok := got.taskDetailScreen.FocusedSubtask()
	if !ok || focused.ID != child.ID {
		t.Fatalf("activeSubtask = (%+v, %v), want the only child id %d", focused, ok, child.ID)
	}

	got = pressRune(t, got, 'm')
	if got.taskDetailScreen.State().Mode != taskdetail.ModeMove {
		t.Fatalf("mode after m = %v, want move", got.taskDetailScreen.State().Mode)
	}
	if got.taskDetailScreen.MoveTaskID() != child.ID {
		t.Fatalf("move target = %d, want child %d", got.taskDetailScreen.MoveTaskID(), child.ID)
	}
	got = sendText(t, got, "dev")
	got = pressKey(t, got, tea.KeyEnter)
	if got.taskDetailScreen.State().Mode != taskdetail.ModeNormal {
		t.Fatalf("mode after enter = %v, want normal (status=%q)", got.taskDetailScreen.State().Mode, got.status)
	}
	if got.taskDetailScreen.MoveTaskID() != 0 {
		t.Fatalf("move target = %d after submit, want 0", got.taskDetailScreen.MoveTaskID())
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
