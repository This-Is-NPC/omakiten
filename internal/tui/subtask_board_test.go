package tui

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
)

// subtaskBoardFixture builds a Model whose snapshot has a 3-bucket
// sub-task kit (izakaya-style: backlog/dev/done). Parent lives in the
// root kit; three children sit one per sub-kit bucket so the panel has
// content in every column.
func subtaskBoardFixture(t *testing.T) Model {
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
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
		}},
		SubtaskBundle: &config.Bundle{
			Kit:    config.Kit{Key: "izakaya"},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "sub",
				Name: "Sub",
				Buckets: []config.Bucket{
					{ID: 10, Key: "backlog", Name: "Backlog", Position: 1},
					{ID: 11, Key: "dev", Name: "Dev", Position: 2},
					{ID: 12, Key: "done", Name: "Done", Position: 3},
				},
			}},
		},
	}
	snap := config.BuildSnapshot(rootBundle)

	parent := domain.Task{ID: 100, Title: "Parent", BucketKey: "dev", Priority: domain.Priority(2)}
	c1 := domain.Task{ID: 101, Title: "Child Backlog", BucketKey: "backlog", Priority: domain.Priority(2), ParentID: ptrInt64(100)}
	c2 := domain.Task{ID: 102, Title: "Child Dev", BucketKey: "dev", Priority: domain.Priority(2), ParentID: ptrInt64(100)}
	c3 := domain.Task{ID: 103, Title: "Child Done", BucketKey: "done", Priority: domain.Priority(2), ParentID: ptrInt64(100)}

	m := Model{
		styles:   newStyles(config.Theme{}),
		width:    200,
		height:   50,
		tasks:    []domain.Task{parent, c1, c2, c3},
		workflow: snap.Workflow(),
		repos:    Repositories{Cache: runtimecache.Install(0, snap)},
	}
	m.openTaskView(parent)
	return m
}

// TestRenderSubtasksPanelBucketGroupedWithSubKit pins AC §1+§2: with a
// sub-task kit configured, the detail-view sub-tasks panel renders one
// column per sub-kit bucket — not the flat checklist.
func TestRenderSubtasksPanelBucketGroupedWithSubKit(t *testing.T) {
	m := subtaskBoardFixture(t)
	m.applyTaskFocus(taskFocusSubtasks)
	out := stripANSI(m.renderTaskView())

	for _, bucket := range []string{"BACKLOG", "DEV", "DONE"} {
		if !strings.Contains(out, "// "+bucket) {
			t.Fatalf("sub-tasks panel missing bucket header %q; got:\n%s", bucket, out)
		}
	}
	// Each card must surface in its own column — assert all three child
	// ids appear.
	for _, id := range []string{"#101", "#102", "#103"} {
		if !strings.Contains(out, id) {
			t.Fatalf("sub-tasks panel missing child %s; got:\n%s", id, out)
		}
	}
}

// TestRenderSubtasksPanelFallsBackToRootKitWhenNoSubKit pins AC §2: a
// project without subtask_kit renders the panel against the root
// workflow's buckets so pre-cascade behaviour stays observable. The
// assertion runs through subtaskPanelWorkflow so it stays decoupled
// from the horizontal-carousel capacity (narrow terminals may scroll
// off-screen columns — what matters is that the panel-resolved
// workflow lists every root bucket).
func TestRenderSubtasksPanelFallsBackToRootKitWhenNoSubKit(t *testing.T) {
	rootBundle := config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
				{ID: 3, Key: "review", Name: "Review", Position: 3},
				{ID: 4, Key: "done", Name: "Done", Position: 4},
			},
		}},
	}
	snap := config.BuildSnapshot(rootBundle)
	parent := domain.Task{ID: 200, Title: "Parent", BucketKey: "dev", Priority: domain.Priority(2)}
	c1 := domain.Task{ID: 201, Title: "Child", BucketKey: "backlog", Priority: domain.Priority(2), ParentID: ptrInt64(200)}

	m := Model{
		styles:   newStyles(config.Theme{}),
		width:    200,
		height:   50,
		tasks:    []domain.Task{parent, c1},
		workflow: snap.Workflow(),
		repos:    Repositories{Cache: runtimecache.Install(0, snap)},
	}
	m.openTaskView(parent)
	m.applyTaskFocus(taskFocusSubtasks)

	resolved := m.subtaskPanelWorkflow()
	if got := len(resolved.Buckets); got != 4 {
		t.Fatalf("subtaskPanelWorkflow bucket count = %d, want 4 (root kit fallback)", got)
	}
	wantKeys := []string{"backlog", "dev", "review", "done"}
	for i, want := range wantKeys {
		if resolved.Buckets[i].Key != want {
			t.Fatalf("bucket[%d].Key = %q, want %q", i, resolved.Buckets[i].Key, want)
		}
	}

	out := stripANSI(m.renderTaskView())
	if !strings.Contains(out, "SUB-TASKS") {
		t.Fatalf("panel kicker missing; got:\n%s", out)
	}
	if !strings.Contains(out, "// BACKLOG") {
		t.Fatalf("at least the first root-kit bucket column should be visible; got:\n%s", out)
	}
}

// TestSubtaskBoardHKeyMovesColumnLeftLKeyMovesRight pins AC §4: the
// detail-view sub-tasks panel honours the root board's h/l semantics
// for column navigation. l advances the focused bucket; the focused
// column's cursor lands on the first card so j/k keep working.
func TestSubtaskBoardHLNavigation(t *testing.T) {
	m := subtaskBoardFixture(t)
	m.applyTaskFocus(taskFocusSubtasks)
	if m.subtaskColIdx != 0 {
		t.Fatalf("subtaskColIdx initial = %d, want 0", m.subtaskColIdx)
	}
	m.moveSubtaskColumn(1)
	if m.subtaskColIdx != 1 {
		t.Fatalf("after moveSubtaskColumn(+1) = %d, want 1 (dev column)", m.subtaskColIdx)
	}
	if got, ok := m.activeSubtask(); !ok || got.BucketKey != "dev" {
		t.Fatalf("activeSubtask after l = %+v ok=%v, want dev-bucket child", got, ok)
	}
	m.moveSubtaskColumn(1)
	if m.subtaskColIdx != 2 {
		t.Fatalf("after second moveSubtaskColumn(+1) = %d, want 2 (done column)", m.subtaskColIdx)
	}
	m.moveSubtaskColumn(1) // clamped at n-1
	if m.subtaskColIdx != 2 {
		t.Fatalf("moveSubtaskColumn past last clamped = %d, want 2", m.subtaskColIdx)
	}
	m.moveSubtaskColumn(-1)
	if m.subtaskColIdx != 1 {
		t.Fatalf("after moveSubtaskColumn(-1) = %d, want 1", m.subtaskColIdx)
	}
}

// TestSubtasksPanelFitsAnnouncedBoxHeight pins the post-review fix
// for the height regression: the rendered panel total row count
// must NOT exceed the boxHeight the caller announced via the
// TaskViewBudget — otherwise the panel overruns the outer slice and
// the form / activity panes get pushed off-screen.
func TestSubtasksPanelFitsAnnouncedBoxHeight(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
	}{
		{"wide side-by-side", 200, 50},
		{"medium side-by-side", 160, 40},
		{"stacked narrow", 90, 36},
		{"stacked focus full screen", 100, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := subtaskBoardFixture(t)
			m.width = tc.width
			m.height = tc.height
			m.applyTaskFocus(taskFocusSubtasks)
			parent, ok := m.activeTask()
			if !ok {
				t.Fatal("fixture missing active task")
			}
			children := m.directChildren(parent.ID)
			lyt := m.computeTaskViewLayout(m.availableWidth(), true)
			formHeight := m.cachedTaskDetailsBoxHeight(parent, lyt)
			budget := m.taskViewBudget(lyt, formHeight)
			boxHeight := budget.SubtasksBoxHeight()
			if boxHeight <= 0 {
				t.Skipf("SubtasksBoxHeight = %d at width=%d height=%d — sub-tasks panel dropped at this size", boxHeight, tc.width, tc.height)
			}
			rendered := m.renderSubtasksPanel(children, lyt, boxHeight)
			got := strings.Count(rendered, "\n") + 1
			if got > boxHeight {
				t.Fatalf("rendered panel total rows = %d, want ≤ %d (panel overruns announced boxHeight) at width=%d height=%d", got, boxHeight, tc.width, tc.height)
			}
		})
	}
}

// TestSubtaskCountReturnsDirectChildrenOnly pins AC §6: the root-card
// sub-task badge counts direct children only. Grandchildren must not
// inflate the badge on the root — otherwise the badge claims "3
// sub-tasks" when only one is directly attached.
func TestSubtaskCountReturnsDirectChildrenOnly(t *testing.T) {
	parent := domain.Task{ID: 500, Title: "Root", BucketKey: "backlog"}
	child := domain.Task{ID: 501, Title: "Child", BucketKey: "backlog", ParentID: ptrInt64(500)}
	grand := domain.Task{ID: 502, Title: "Grand", BucketKey: "backlog", ParentID: ptrInt64(501)}
	greatGrand := domain.Task{ID: 503, Title: "Great", BucketKey: "backlog", ParentID: ptrInt64(502)}
	m := Model{
		styles: newStyles(config.Theme{}),
		tasks:  []domain.Task{parent, child, grand, greatGrand},
	}
	if got := m.subtaskCount(parent.ID); got != 1 {
		t.Fatalf("subtaskCount(parent) = %d, want 1 (direct children only)", got)
	}
	if got := m.subtaskCount(child.ID); got != 1 {
		t.Fatalf("subtaskCount(child) = %d, want 1", got)
	}
	if got := m.subtaskCount(grand.ID); got != 1 {
		t.Fatalf("subtaskCount(grand) = %d, want 1", got)
	}
	if got := m.subtaskCount(greatGrand.ID); got != 0 {
		t.Fatalf("subtaskCount(greatGrand) = %d, want 0", got)
	}
}
