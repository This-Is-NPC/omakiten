package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// TestShouldRealtimeRefreshGate pins which view states the per-second tick
// is allowed to reload. The single-task read view (taskScreenView) is the
// state newly permitted by this change; every edit/overlay/modal state must
// still suppress the tick so a passive reload never lands on top of an input.
func TestShouldRealtimeRefreshGate(t *testing.T) {
	// Zero-value Model is a live board (top 0 != topHome, modeNormal,
	// taskScreenClosed, no overlays) — the baseline that must refresh.
	base := func() Model { return Model{} }

	if !base().shouldRealtimeRefresh() {
		t.Fatal("baseline board view: shouldRealtimeRefresh() = false, want true")
	}

	cases := []struct {
		name string
		mut  func(*Model)
		want bool
	}{
		{"task read view refreshes", func(m *Model) { m.taskScreen = taskScreenView }, true},
		{"plan-network open refreshes", func(m *Model) { m.planNetworkOpen = true }, true},
		{"task drilled over plan-network refreshes", func(m *Model) {
			m.planNetworkOpen = true
			m.taskScreen = taskScreenView
		}, true},
		{"palette open blocks", func(m *Model) { m.paletteOpen = true }, false},
		{"task edit view blocks", func(m *Model) { m.taskScreen = taskScreenEdit }, false},
		{"comment overlay blocks", func(m *Model) { m.commentScreenOpen = true }, false},
		{"description overlay blocks", func(m *Model) { m.descriptionScreenOpen = true }, false},
		{"plan-goal overlay blocks", func(m *Model) { m.planGoalScreenOpen = true }, false},
		{"project-form overlay blocks", func(m *Model) { m.projectFormScreenOpen = true }, false},
		{"entity screen blocks", func(m *Model) { m.entityScreen = entityScreenView }, false},
		{"help open blocks", func(m *Model) { m.helpOpen = true }, false},
		{"move mode blocks", func(m *Model) { m.moveMode = true }, false},
		{"non-normal mode blocks", func(m *Model) { m.mode = modeComment }, false},
		{"home blocks", func(m *Model) { m.top = topHome }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.mut(&m)
			if got := m.shouldRealtimeRefresh(); got != tc.want {
				t.Fatalf("shouldRealtimeRefresh() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRealtimeTickReloadsPlanNetwork proves the tick reloads the open plan's
// projection — not just the board snapshot. A task assigned to the plan from
// another writer after the view opened must appear without a keypress, and the
// cursor must survive the reload.
func TestRealtimeTickReloadsPlanNetwork(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	w1, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1)
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	tOpen, err := store.CreateTask(ctx, project.ID, "foundation-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, tOpen.ID, plan.ID, w1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("after enter: planNetworkOpen = false, want true")
	}
	if n := planWaveTaskCount(opened, w1.ID); n != 1 {
		t.Fatalf("wave task count before tick = %d, want 1", n)
	}
	cursorBefore := opened.planNetworkCursor.Cursor()

	// A second writer assigns another task to the same wave after the view
	// is already open — invisible until the projection reloads.
	tLate, err := store.CreateTask(ctx, project.ID, "late-task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(late) error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, tLate.ID, plan.ID, w1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan(late) error = %v", err)
	}

	ticked, _ := opened.Update(refreshTickMsg{})
	after := ticked.(Model)
	if n := planWaveTaskCount(after, w1.ID); n != 2 {
		t.Fatalf("wave task count after realtime tick = %d, want 2", n)
	}
	if !strings.Contains(ansi.Strip(after.View()), "late-task") {
		t.Fatalf("plan view missing late-task after tick\n%s", ansi.Strip(after.View()))
	}
	if got := after.planNetworkCursor.Cursor(); got != cursorBefore {
		t.Fatalf("plan cursor moved on tick: got %d, want %d (state must survive)", got, cursorBefore)
	}
}

func planWaveTaskCount(m Model, waveID int64) int {
	for _, w := range m.planNetworkShow.Waves {
		if w.Wave.ID == waveID {
			return len(w.Tasks)
		}
	}
	return -1
}

// TestRealtimeTickReloadsTaskActivity proves the tick reloads the open task's
// activity feed. A comment written by another session after the task view
// opened must appear without a keypress — previously the tick was suppressed
// entirely while a task view was open.
func TestRealtimeTickReloadsTaskActivity(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()
	task, err := store.CreateTask(ctx, project.ID, "live-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	model.openTaskView(task)
	if model.taskScreen != taskScreenView {
		t.Fatalf("openTaskView: taskScreen = %v, want taskScreenView", model.taskScreen)
	}
	// Focus the activity pane and land the cursor on the first card so the
	// tick has live view state (cursor + scroll) to preserve, mirroring the
	// plan-network test that asserts the cursor survives the reload.
	model.applyTaskFocus(taskFocusActivity)
	if model.activityCursor < 0 {
		t.Fatalf("applyTaskFocus(activity): activityCursor = %d, want >= 0", model.activityCursor)
	}
	before := len(model.activity)
	cursorBefore := model.activityCursor
	scrollBefore := model.activityLines.Scroll()
	anchoredEventID := model.activity[model.activityCursor].ID

	if _, err := store.AddComment(ctx, project.ID, task.ID, "live comment from another session", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	ticked, cmd := model.Update(refreshTickMsg{})
	if cmd == nil {
		t.Fatal("Update(refreshTickMsg) command = nil, want next realtime tick")
	}
	got := ticked.(Model)
	if len(got.activity) <= before {
		t.Fatalf("activity len after tick = %d, want > %d", len(got.activity), before)
	}
	if !strings.Contains(ansi.Strip(got.View()), "live comment from another session") {
		t.Fatalf("task view missing live comment after tick\n%s", ansi.Strip(got.View()))
	}
	// State preservation: the activity cursor and scroll offset must survive
	// the tick (asc feed appends below the held cursor, so both are unchanged).
	if got.activityCursor != cursorBefore {
		t.Fatalf("activityCursor moved on tick: got %d, want %d (state must survive)", got.activityCursor, cursorBefore)
	}
	if got.activityLines.Scroll() != scrollBefore {
		t.Fatalf("activity scroll moved on tick: got %d, want %d (state must survive)", got.activityLines.Scroll(), scrollBefore)
	}
	// The cursor must still name the same event it named before the reload.
	if got.activity[got.activityCursor].ID != anchoredEventID {
		t.Fatalf("activity cursor names a different event after tick: got id %d, want %d", got.activity[got.activityCursor].ID, anchoredEventID)
	}
}

// TestRealtimeTickActivityCursorSurvivesInsertAbove pins the id-anchoring fix:
// with a newest-first (desc) feed a comment from another session lands at
// index 0, pushing every existing row down by one. The index-based cursor
// would then name the wrong card; anchoring it to the focused event id keeps
// the same card selected across the reload.
func TestRealtimeTickActivityCursorSurvivesInsertAbove(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	// Force newest-first at the bundle level so the order survives the
	// refresh()-driven m.views reset that runs at the start of every tick — a
	// fresh comment then inserts ABOVE the held cursor.
	bundle := tuiTestBundle(t)
	bundle.Config.Views.TaskActivity.Sort.Order = "desc"
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()
	task, err := store.CreateTask(ctx, project.ID, "anchor-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	// Two comments so the feed has an interior card to hold a cursor on.
	if _, err := store.AddComment(ctx, project.ID, task.ID, "older comment", "human", nil); err != nil {
		t.Fatalf("AddComment(older) error = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "newer comment", "human", nil); err != nil {
		t.Fatalf("AddComment(newer) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	model.openTaskView(task)
	model.applyTaskFocus(taskFocusActivity)
	if model.views.TaskActivity.Sort.Order != "desc" {
		t.Fatalf("task activity order = %q, want desc (insert-above precondition)", model.views.TaskActivity.Sort.Order)
	}

	// Hold the cursor on the oldest event (last row in a desc feed).
	model.activityCursor = len(model.activity) - 1
	anchoredEventID := model.activity[model.activityCursor].ID
	indexBefore := model.activityCursor

	// Another session adds a comment — newest-first, it lands at index 0 and
	// shifts the held card down by one.
	if _, err := store.AddComment(ctx, project.ID, task.ID, "intruder from another session", "human", nil); err != nil {
		t.Fatalf("AddComment(intruder) error = %v", err)
	}

	ticked, _ := model.Update(refreshTickMsg{})
	got := ticked.(Model)

	if got.activityCursor == indexBefore {
		t.Fatalf("cursor index did not shift after insert-above: still %d (anchor not exercised)", got.activityCursor)
	}
	if got.activity[got.activityCursor].ID != anchoredEventID {
		t.Fatalf("cursor names wrong card after insert-above: got id %d, want %d", got.activity[got.activityCursor].ID, anchoredEventID)
	}
}
