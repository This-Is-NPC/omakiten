package tui

import (
	"context"
	"sync/atomic"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// stubWatermark is a deterministic DataVersionReader for the tick-gate tests.
// version is the value every probe returns; calls counts how many times the
// tick probed it. Setting version simulates an external write (the watermark
// moving); leaving it fixed simulates idle seconds.
type stubWatermark struct {
	version int64
	calls   atomic.Int64
}

func (s *stubWatermark) DataVersion(context.Context) (int64, error) {
	s.calls.Add(1)
	return s.version, nil
}

// countingTaskRepo wraps the real store-backed TaskRepository and counts
// ListTasks calls — the board reload's signature query. An idle tick that is
// correctly gated never reloads the board, so this counter must not move.
type countingTaskRepo struct {
	app.TaskRepository
	listCalls atomic.Int64
}

func (c *countingTaskRepo) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error) {
	c.listCalls.Add(1)
	return c.TaskRepository.ListTasks(ctx, projectID, filter, buckets)
}

// TestRealtimeTickIdleSkipsReload proves the success metric: across N idle
// ticks (watermark unchanged) the tick runs exactly N cheap probes and ZERO
// board reloads. This is the core of the #123 idle-waste fix.
func TestRealtimeTickIdleSkipsReload(t *testing.T) {
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
	watermark := &stubWatermark{version: 42}

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
	model.height = 40
	model.width = 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	// The FIRST tick always reloads (no trusted baseline yet) — drive it once
	// so the watermark baseline is established, then reset the ListTasks
	// counter so the idle assertion measures only the steady state.
	first, _ := model.Update(refreshTickMsg{})
	model = first.(Model)
	tasks.listCalls.Store(0)
	probesBefore := watermark.calls.Load()

	const idleTicks = 5
	for i := 0; i < idleTicks; i++ {
		next, cmd := model.Update(refreshTickMsg{})
		if cmd == nil {
			t.Fatalf("idle tick %d: command = nil, want next realtime tick", i)
		}
		model = next.(Model)
	}

	if got := tasks.listCalls.Load(); got != 0 {
		t.Fatalf("idle ticks triggered %d board reload(s) (ListTasks calls), want 0", got)
	}
	if got := watermark.calls.Load() - probesBefore; got != idleTicks {
		t.Fatalf("idle ticks ran %d watermark probes, want exactly %d (one cheap probe per tick)", got, idleTicks)
	}
}

// TestRealtimeTickReloadsWhenWatermarkAdvances proves the other side of the
// gate: when the watermark moves (an external write landed), the tick DOES
// reload the board.
func TestRealtimeTickReloadsWhenWatermarkAdvances(t *testing.T) {
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
	model.height = 40
	model.width = 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	// Establish the baseline (first tick always reloads), then clear the
	// counter so the assertion isolates the watermark-advance reload.
	first, _ := model.Update(refreshTickMsg{})
	model = first.(Model)
	tasks.listCalls.Store(0)

	// Watermark moves → next tick must reload the board exactly once.
	watermark.version = 2
	next, _ := model.Update(refreshTickMsg{})
	model = next.(Model)
	if got := tasks.listCalls.Load(); got == 0 {
		t.Fatal("watermark advanced but the tick did not reload the board (ListTasks not called)")
	}
}

// TestSelfWriteRepaintsInlineWithoutTick is the invariant regression guard
// (AC4). A TUI self-write commits on the pool, NOT on the pinned probe
// connection, so PRAGMA data_version deliberately does not move for it. The
// write path therefore MUST repaint inline via the synchronous m.refresh();
// the watermark gate must never be relied on to surface a self-write. Here we
// freeze the watermark (idle, never advances) and assert a self-write is
// visible immediately — without any refreshTickMsg being delivered.
func TestSelfWriteRepaintsInlineWithoutTick(t *testing.T) {
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
	task, err := store.CreateTask(ctx, project.ID, "self-write-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// A watermark frozen at a constant value models "no external write ever".
	// If the self-write incorrectly depended on the watermark to repaint, the
	// new activity would not appear because the gate would skip every tick.
	watermark := &stubWatermark{version: 7}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
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
	model.height = 40
	model.width = 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	model.openTaskView(task)
	model.applyTaskFocus(taskFocusActivity)
	before := len(model.activity)

	// Simulate the self-write path: write then call the synchronous refresh
	// that input.go / render_comment.go invoke inline after a self-write.
	if _, err := store.AddComment(ctx, project.ID, task.ID, "my own comment", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("inline refresh() error = %v", err)
	}
	if err := model.refreshTaskActivity(task.ID); err != nil {
		t.Fatalf("refreshTaskActivity() error = %v", err)
	}

	if len(model.activity) <= before {
		t.Fatalf("self-write not visible after inline refresh: activity len = %d, want > %d", len(model.activity), before)
	}
	// And the watermark never moved — proving the repaint did NOT depend on it.
	if model.dataVersionSynced && model.lastDataVersion != 0 {
		// lastDataVersion is only set by a tick probe; no tick ran here.
		t.Fatalf("watermark state changed without a tick: synced=%v last=%d", model.dataVersionSynced, model.lastDataVersion)
	}
}
