package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// failingOnceTaskRepo wraps the store-backed TaskRepository and makes the first
// ListTasks call after arm() return an error, then succeeds. It models a board
// reload whose heavy DB query fails transiently — the F2 regression scenario.
type failingOnceTaskRepo struct {
	app.TaskRepository
	failNext atomic.Bool
	calls    atomic.Int64
}

func (r *failingOnceTaskRepo) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error) {
	r.calls.Add(1)
	if r.failNext.CompareAndSwap(true, false) {
		return nil, errors.New("transient board query failure")
	}
	return r.TaskRepository.ListTasks(ctx, projectID, filter, buckets)
}

// TestRealtimeTickConfigReloadIndependentOfWatermark proves F1: a config-file
// change with NO DB write (watermark frozen) is picked up on the next honored
// tick, because reloadBundleIfChanged now runs every tick regardless of the
// data_version gate. Before the fix the early `if !dataVersionChanged` return
// fired before the config reload, so the edit was invisible until an unrelated
// DB write moved the watermark.
func TestRealtimeTickConfigReloadIndependentOfWatermark(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle(t)); err != nil {
		t.Fatalf("SaveFullBundle: %v", err)
	}
	writeThemeFile(t, filepath.Join(tmp, "themes", "catppuccin.yaml"), "catppuccin", "Catppuccin")

	store := snapstore.Open(t, dbPath)
	files := configstore.New()
	editor := app.NewBundleEditor(files, configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	bus := events.NewInProcessBus(config.EventsSettings{})
	cache := agentruntime.NewBundleCache(store.Store, bus, files)
	if _, err := cache.Resolve(ctx, project.ID, configPath); err != nil {
		t.Fatalf("cache.Resolve initial: %v", err)
	}

	// Watermark frozen forever — no DB write will ever move it. If the config
	// reload were still gated on the watermark, the edit below would be invisible.
	watermark := &stubWatermark{version: 99}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Editor:       editor,
		BundleStore:  files,
		EntityFiles:  files,
		Slugger:      files,
		Watermark:    watermark,
		Cache:        cache,
		ProjectID:    project.ID,
		ConfigPath:   configPath,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height, model.width = 40, 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// Drive the first tick to establish the watermark baseline (first probe
	// always reloads). dataVersionSynced is now true and frozen at 99.
	first, _ := model.Update(refreshTickMsg{})
	model = first.(Model)
	if model.languages.AgentOutput != "" {
		t.Fatalf("initial AgentOutput = %q, want empty", model.languages.AgentOutput)
	}

	// Edit the config on disk WITHOUT any DB write. The watermark stays at 99.
	bundle, err := config.LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	bundle.Config.Languages.AgentOutput = "Português (Brasil)"
	if err := config.SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle: %v", err)
	}
	futureT := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(configPath, futureT, futureT); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Next tick: watermark unchanged (idle DB), but the config-mtime gate must
	// still run and apply the edit.
	next, _ := model.Update(refreshTickMsg{})
	model = next.(Model)
	if model.languages.AgentOutput != "Português (Brasil)" {
		t.Fatalf("config-only edit not picked up on tick: AgentOutput = %q, want Português (Brasil) (F1: config reload still gated on watermark)", model.languages.AgentOutput)
	}
}

// TestRealtimeTickFailedReloadRetainsWatermark proves F2: a failing board
// reload must leave m.lastDataVersion unadvanced so the next tick re-observes
// the same external write and retries it. Before the fix dataVersionChanged
// committed the version up-front, so a failed reload consumed the watermark and
// the write was lost until an unrelated write moved it again.
func TestRealtimeTickFailedReloadRetainsWatermark(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	snap := store.Snapshot()
	if _, err := store.CreateTask(ctx, project.ID, "live-task", "", domain.Priority(2), "backlog", nil, snap); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	tasks := &failingOnceTaskRepo{TaskRepository: store}
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
		t.Fatalf("NewModel: %v", err)
	}
	model.height, model.width = 40, 160
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// First tick: establishes the baseline at version 1 (a successful reload).
	model, _ = driveRealtimeTick(t, model)
	if model.lastDataVersion != 1 {
		t.Fatalf("after baseline tick: lastDataVersion = %d, want 1", model.lastDataVersion)
	}

	// External write moves the watermark; arm the next board query to fail.
	watermark.version = 2
	tasks.failNext.Store(true)
	model, _ = driveRealtimeTick(t, model)
	// The reload failed (board worker returned r.err), so the watermark baseline
	// must NOT have advanced to 2 — the write is still pending.
	if model.lastDataVersion == 2 {
		t.Fatal("failed reload committed the watermark (lastDataVersion = 2) — the external write would be lost (F2)")
	}
	if model.lastDataVersion != 1 {
		t.Fatalf("after failed reload: lastDataVersion = %d, want 1 (unadvanced)", model.lastDataVersion)
	}

	// Next tick still sees version 2 != baseline 1 → retries. This time the
	// query succeeds and the baseline advances.
	model, _ = driveRealtimeTick(t, model)
	if model.lastDataVersion != 2 {
		t.Fatalf("retry tick did not commit watermark: lastDataVersion = %d, want 2 (write must be observed on retry)", model.lastDataVersion)
	}
}

// TestApplyRealtimeReloadDropsStaleGeneration proves F3: an out-of-order
// (older-generation) reload msg is dropped and does not fold over the newer
// snapshot already applied. It also asserts the F2 interaction — the dropped
// stale msg never commits its watermark version.
func TestApplyRealtimeReloadDropsStaleGeneration(t *testing.T) {
	var m Model

	// Two board reloads in flight. The newer one (gen 2, two tasks) lands first
	// and is applied; the older one (gen 1, one task) arrives late and must be
	// dropped so it cannot regress the view.
	newer := realtimeReloadMsg{
		kind:             realtimeReloadBoard,
		gen:              2,
		dataVersion:      20,
		dataVersionValid: true,
		snap:             app.TUISnapshot{Tasks: []domain.Task{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}}},
		snapValid:        true,
	}
	older := realtimeReloadMsg{
		kind:             realtimeReloadBoard,
		gen:              1,
		dataVersion:      10,
		dataVersionValid: true,
		snap:             app.TUISnapshot{Tasks: []domain.Task{{ID: 1, Title: "a"}}},
		snapValid:        true,
	}

	m.applyRealtimeReload(newer)
	if len(m.tasks) != 2 {
		t.Fatalf("after newer apply: %d tasks, want 2", len(m.tasks))
	}
	if m.lastAppliedReloadGen != 2 {
		t.Fatalf("after newer apply: lastAppliedReloadGen = %d, want 2", m.lastAppliedReloadGen)
	}
	if m.lastDataVersion != 20 {
		t.Fatalf("after newer apply: lastDataVersion = %d, want 20", m.lastDataVersion)
	}

	// The stale older msg arrives — must be dropped, leaving tasks + watermark
	// untouched.
	m.applyRealtimeReload(older)
	if len(m.tasks) != 2 {
		t.Fatalf("stale gen folded over newer snapshot: %d tasks, want 2 (F3)", len(m.tasks))
	}
	if m.lastDataVersion != 20 {
		t.Fatalf("stale gen committed its watermark: lastDataVersion = %d, want 20 (F2/F3 interaction)", m.lastDataVersion)
	}
	if m.lastAppliedReloadGen != 2 {
		t.Fatalf("stale gen advanced lastAppliedReloadGen: %d, want 2", m.lastAppliedReloadGen)
	}
}
