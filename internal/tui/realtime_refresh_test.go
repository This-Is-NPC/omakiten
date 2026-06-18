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

func updateRealtimeTick(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(refreshTickMsg{})
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(refreshTickMsg) returned %T, want Model", updated)
	}
	if cmd == nil {
		t.Fatal("Update(refreshTickMsg) command = nil, want next realtime tick")
	}
	return model, cmd
}

// driveRealtimeTick delivers a refreshTickMsg, then completes every off-thread
// reload the same way the Bubble Tea runtime would: it executes the returned
// cmd, finds realtimeReloadMsg workers produced by the tick (batched with the
// next-tick scheduler), and folds those msgs back through Update. It returns the
// post-fold model. When the tick reloaded nothing (gated out or an empty view)
// the reload cmd is absent and the model is returned as-ticked.
func driveRealtimeTick(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	model, _ := driveRealtimeTickAll(t, m)
	return model, nil
}

func foldRealtimeReload(t *testing.T, m Model, cmd tea.Cmd) (Model, realtimeReloadMsg, bool, tea.Cmd) {
	t.Helper()
	reload := findRealtimeReloadCmd(cmd)
	if reload == nil {
		return m, realtimeReloadMsg{}, false, nil
	}
	msg := reload()
	rmsg, ok := msg.(realtimeReloadMsg)
	if !ok {
		t.Fatalf("realtime reload cmd produced %T, want realtimeReloadMsg", msg)
	}
	folded, foldCmd := m.Update(rmsg)
	model, ok := folded.(Model)
	if !ok {
		t.Fatalf("Update(realtimeReloadMsg) returned %T, want Model", folded)
	}
	return model, rmsg, true, foldCmd
}

func mustFoldRealtimeReload(t *testing.T, m Model, cmd tea.Cmd) (Model, realtimeReloadMsg, tea.Cmd) {
	t.Helper()
	folded, rmsg, ok, foldCmd := foldRealtimeReload(t, m, cmd)
	if !ok {
		t.Fatal("returned cmd carries no realtime reload cmd; the reload was not handed off-thread")
	}
	return folded, rmsg, foldCmd
}

func assertRealtimeBaselineEstablished(t *testing.T, m Model, wantVersion int64) {
	t.Helper()
	assertRealtimeDomainBaselineEstablished(t, m, realtimeReloadBundle, wantVersion)
}

func assertRealtimeDomainBaselineEstablished(t *testing.T, m Model, kind realtimeReloadKind, wantVersion int64) {
	t.Helper()
	version, ok := m.dataVersionBaseline(kind)
	if !ok {
		t.Fatalf("realtime baseline for domain %v was not established", kind)
	}
	if version != wantVersion {
		t.Fatalf("realtime baseline version for domain %v = %d, want %d", kind, version, wantVersion)
	}
	builtGen := m.realtimeReloadGen[kind]
	if builtGen == 0 {
		t.Fatalf("realtime baseline for domain %v was not established: no reload generation was built", kind)
	}
	appliedGen := m.lastAppliedRealtimeReloadGen(kind)
	if appliedGen != builtGen {
		t.Fatalf("realtime baseline reload for domain %v was not folded: applied gen %d, built gen %d", kind, appliedGen, builtGen)
	}
}

// findRealtimeReloadCmd flattens a (possibly batched) cmd and returns the
// realtime reload worker cmd, recognised via the realtimeReloadRegistry, or nil
// when none is present. tea.Batch packs sub-cmds into a tea.BatchMsg ([]tea.Cmd)
// when executed; the registry lets the helper pick the reload cmd out of that
// batch without executing the next-tick scheduler (which would block on a
// timer).
func findRealtimeReloadCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	if isRealtimeReloadCmd(cmd) {
		return cmd
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, sub := range msg {
			if isRealtimeReloadCmd(sub) {
				return sub
			}
		}
	}
	return nil
}

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

	after, _ := driveRealtimeTick(t, opened)
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

	got, _ := driveRealtimeTick(t, model)
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

	got, _ := driveRealtimeTick(t, model)

	if got.activityCursor == indexBefore {
		t.Fatalf("cursor index did not shift after insert-above: still %d (anchor not exercised)", got.activityCursor)
	}
	if got.activity[got.activityCursor].ID != anchoredEventID {
		t.Fatalf("cursor names wrong card after insert-above: got id %d, want %d", got.activity[got.activityCursor].ID, anchoredEventID)
	}
}

// TestRealtimeTickReturnsReloadCmdNotInlineMutation proves AC1/AC5: the
// changed-tick reload is handed back as a tea.Cmd, never run inline in Update.
// A slow board query therefore cannot stall a keystroke — Update returns
// immediately and the heavy ListTasks read only fires when the worker cmd runs.
// The assertion: after Update(refreshTickMsg) on a changed watermark, the board
// query has NOT been issued yet (it runs off-thread), and the returned cmd
// carries a realtimeReloadMsg that, once executed and folded, performs the load.
func TestRealtimeTickReturnsReloadCmdNotInlineMutation(t *testing.T) {
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
	if _, err := store.CreateTask(ctx, project.ID, "live-task", "", domain.Priority(2), "backlog", nil, snap); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	tasks := &countingTaskRepo{TaskRepository: store}
	watermark := &stubWatermark{version: 1}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        tasks,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Watermark:    watermark,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height, model.width = 40, 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	// Establish baseline watermark (first tick), then isolate the steady state.
	model, _ = driveRealtimeTick(t, model)
	assertRealtimeBaselineEstablished(t, model, 1)
	tasks.listCalls.Store(0)

	// Changed watermark → Update must return a reload cmd WITHOUT having issued
	// the board query inline.
	watermark.version = 2
	model, cmd := updateRealtimeTick(t, model)
	if got := tasks.listCalls.Load(); got != 0 {
		t.Fatalf("board query ran inline during Update (ListTasks calls = %d, want 0); the reload must be off-thread", got)
	}

	// Running the worker now issues the query and produces a fold-ready msg.
	folded, rmsg, _ := mustFoldRealtimeReload(t, model, cmd)
	if rmsg.kind != realtimeReloadBundle || !rmsg.snapValid {
		t.Fatalf("board reload msg kind=%v snapValid=%v, want board/true", rmsg.kind, rmsg.snapValid)
	}
	if got := tasks.listCalls.Load(); got == 0 {
		t.Fatal("worker cmd ran but ListTasks was not issued")
	}
	if len(folded.tasks) != 1 || folded.tasks[0].Title != "live-task" {
		t.Fatalf("post-fold tasks = %#v, want live-task", folded.tasks)
	}
}

// TestRealtimeTickTaskViewSkipsBoardRebuild proves AC2: when the single-task
// view is on screen the changed-tick reload loads ONLY the activity feed — the
// board snapshot (ListTasks) is never rebuilt, because renderTaskScreen fully
// occludes the board. An external board write made while the task view is open
// must NOT trigger a board reload this tick.
func TestRealtimeTickTaskViewSkipsBoardRebuild(t *testing.T) {
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
	task, err := store.CreateTask(ctx, project.ID, "open-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	tasks := &countingTaskRepo{TaskRepository: store}
	watermark := &stubWatermark{version: 1}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        tasks,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Watermark:    watermark,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height, model.width = 40, 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	model.openTaskView(task)
	if model.taskScreen != taskScreenView {
		t.Fatalf("openTaskView: taskScreen = %v, want taskScreenView", model.taskScreen)
	}

	// External writes: a new board task AND a comment on the open task. Only the
	// comment (the task feed) should be picked up this tick.
	if _, err := store.CreateTask(ctx, project.ID, "external-board-task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask(external) error = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "feed comment", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	tasks.listCalls.Store(0)
	watermark.version = 2

	// Inspect the reload cmd kind directly: it must be the task-view scope.
	model, cmd := updateRealtimeTick(t, model)
	folded, rmsg, _ := mustFoldRealtimeReload(t, model, cmd)
	if rmsg.kind != realtimeReloadActivity {
		t.Fatalf("task-view tick reload kind = %v, want realtimeReloadActivity (board must not rebuild)", rmsg.kind)
	}
	if got := tasks.listCalls.Load(); got != 0 {
		t.Fatalf("task-view tick rebuilt the board (ListTasks calls = %d, want 0)", got)
	}

	// Folding the feed result must surface the comment without disturbing the
	// (untouched) board task slice.
	boardTasksBefore := len(model.tasks)
	got := folded
	if len(got.tasks) != boardTasksBefore {
		t.Fatalf("board task slice changed under task-view tick: got %d, want %d", len(got.tasks), boardTasksBefore)
	}
	foundComment := false
	for _, e := range got.activity {
		if e.Body == "feed comment" {
			foundComment = true
		}
	}
	if !foundComment {
		t.Fatal("task-view tick did not load the new comment into the activity feed")
	}
}

// TestRealtimeTickBoardCatchesUpAfterTaskViewScopedReload proves the per-domain
// watermark regression: a board-visible write made while the task view is open
// must not be consumed by the activity-only tick. Returning to the board must
// still observe the same DB watermark movement and rebuild the board.
func TestRealtimeTickBoardCatchesUpAfterTaskViewScopedReload(t *testing.T) {
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
	task, err := store.CreateTask(ctx, project.ID, "open-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	tasks := &countingTaskRepo{TaskRepository: store}
	watermark := &stubWatermark{version: 1}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        tasks,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Watermark:    watermark,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height, model.width = 40, 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	model, _ = driveRealtimeTick(t, model)
	assertRealtimeBaselineEstablished(t, model, 1)
	model.openTaskView(task)

	if _, err := store.CreateTask(ctx, project.ID, "external-board-task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask(external) error = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "activity-only change", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	watermark.version = 2
	tasks.listCalls.Store(0)

	model, cmd := updateRealtimeTick(t, model)
	folded, rmsg, _ := mustFoldRealtimeReload(t, model, cmd)
	if rmsg.kind != realtimeReloadActivity {
		t.Fatalf("task-view tick reload kind = %v, want realtimeReloadActivity", rmsg.kind)
	}
	if got := tasks.listCalls.Load(); got != 0 {
		t.Fatalf("task-view tick rebuilt the board (ListTasks calls = %d, want 0)", got)
	}
	model = folded
	assertRealtimeDomainBaselineEstablished(t, model, realtimeReloadActivity, 2)
	if version, ok := model.dataVersionBaseline(realtimeReloadBundle); !ok || version != 1 {
		t.Fatalf("activity-only tick advanced bundle baseline: got %d (ok=%v), want 1", version, ok)
	}
	for _, task := range model.tasks {
		if task.Title == "external-board-task" {
			t.Fatal("board task slice changed while task view was open; task-view tick must stay activity-scoped")
		}
	}

	model.closeTaskScreen("")
	tasks.listCalls.Store(0)
	model, _ = driveRealtimeTick(t, model)
	if got := tasks.listCalls.Load(); got == 0 {
		t.Fatal("returning to board did not rebuild after activity-only tick; bundle baseline should still lag")
	}
	foundBoardTask := false
	for _, task := range model.tasks {
		if task.Title == "external-board-task" {
			foundBoardTask = true
			break
		}
	}
	if !foundBoardTask {
		t.Fatalf("board did not catch up after returning from task view; tasks = %#v", model.tasks)
	}
}
