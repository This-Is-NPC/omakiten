package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

func TestHandleOrphanMigrationAction_skipIsLabeledDismissal(t *testing.T) {
	m := newOrphanMigrationModel(t, nil)
	called := false
	m.repos.DispatchAction = func(context.Context, contract.ActionRequest) (contract.ActionResult, error) {
		called = true
		return contract.ActionResult{}, nil
	}
	m.handleOrphanMigrationAction(ActionMsg{
		Slug:     "kitten_orphan_migration",
		ActionID: "skip",
	})
	if called {
		t.Fatal("DispatchCommand must not run for orphan skip")
	}
	if !strings.Contains(m.status, "kitten_orphan_migration") || !strings.Contains(m.status, "skip") {
		t.Fatalf("status = %q, want skip hint", m.status)
	}
}

func TestHandleOrphanMigrationAction_requiresFacade(t *testing.T) {
	m := newOrphanMigrationModel(t, nil)
	runtimecache.InstallRuntime(m.repos.Cache, m.repos.ProjectID, &agentruntime.ProjectRuntime{
		Snapshot: m.repos.activeSnapshot(),
		Service:  nil,
	})
	m.handleOrphanMigrationAction(ActionMsg{
		Slug:     "kitten_orphan_migration",
		ActionID: "migrate",
	})
	if !strings.Contains(m.status, "migrate") {
		t.Fatalf("status = %q, want skipped hint naming migrate", m.status)
	}
}

func TestHandleOrphanMigrationAction_migratesViaFacade(t *testing.T) {
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	m := newOrphanMigrationModel(t, store)

	if _, err := store.CreateTask(m.ctx, m.project.ID, "orphan me", "", domain.Priority(2), "dev", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	rotateOrphanSnapshots(t, m, store)

	preview, err := m.repos.operationService().MigrateOrphans(m.ctx, contract.MigrateOrphansInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: m.project.ID},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Report.Total == 0 || !preview.Confirmation.RequiresConfirmation {
		t.Fatalf("preview must require confirm; report=%+v confirmation=%+v", preview.Report, preview.Confirmation)
	}

	m.handleOrphanMigrationAction(ActionMsg{
		Slug:     "kitten_orphan_migration",
		ActionID: "migrate",
	})
	if !strings.Contains(m.status, "1") {
		t.Fatalf("status = %q, want migrated count", m.status)
	}

	tasks, err := store.ListTasks(m.ctx, m.project.ID, domain.TaskFilter{}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.Title == "orphan me" && task.BucketKey == "backlog" {
			found = true
		}
	}
	if !found {
		t.Fatalf("orphan task not rebound to backlog; tasks=%+v", tasks)
	}
}

func TestPreviewOrphanReport_usesFacadeWhenWired(t *testing.T) {
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	m := newOrphanMigrationModel(t, store)
	if _, err := store.CreateTask(m.ctx, m.project.ID, "orphan me", "", domain.Priority(2), "dev", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	rotateOrphanSnapshots(t, m, store)

	report, err := m.previewOrphanReport("default")
	if err != nil {
		t.Fatalf("previewOrphanReport: %v", err)
	}
	if report.Total == 0 {
		t.Fatalf("facade preview Total = 0; report=%+v", report)
	}
}

func TestDispatchNotification_routesOrphanMigrateBySlug(t *testing.T) {
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	m := newOrphanMigrationModel(t, store)
	if _, err := store.CreateTask(m.ctx, m.project.ID, "orphan me", "", domain.Priority(2), "dev", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	rotateOrphanSnapshots(t, m, store)

	dispatched := false
	m.repos.DispatchAction = func(context.Context, contract.ActionRequest) (contract.ActionResult, error) {
		dispatched = true
		return contract.ActionResult{}, nil
	}
	next, _, handled := m.dispatchNotification(ActionMsg{
		Slug:     "kitten_orphan_migration",
		ActionID: "migrate",
		ID:       1,
	})
	if !handled {
		t.Fatal("dispatchNotification must handle kitten_orphan_migration")
	}
	if dispatched {
		t.Fatal("orphan migrate must not fall through to DispatchCommand")
	}
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("dispatchNotification returned %T, want Model", next)
	}
	tasks, err := store.ListTasks(got.ctx, got.project.ID, domain.TaskFilter{}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	migrated := false
	for _, task := range tasks {
		if task.Title == "orphan me" && task.BucketKey == "backlog" {
			migrated = true
		}
	}
	if !migrated {
		t.Fatalf("slug route did not migrate; tasks=%+v status=%q", tasks, got.status)
	}
}

func newOrphanMigrationModel(t *testing.T, store *snapstore.Store) Model {
	t.Helper()
	ctx := context.Background()
	if store == nil {
		store = snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	}
	bundle := tuiTestBundle(t)
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	svc := operation.NewService(store, contract.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(store.Snapshot())
	svc.WireOrphan(store.Snapshot(), nil)
	svc.SetSettings(operation.ServiceSettings{
		RecentCommentLimit: 5,
		MaxCommentChars:    0,
		IncludeWorkflow:    true,
		CachePrompts:       true,
		NextWorkLimit:      5,
		SimilarTaskLimit:   5,
	})

	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{
		Service:  svc,
		Snapshot: store.Snapshot(),
	})

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Dependencies: store,
		Comments:     store,
		Tags:         store,
		Events:       store,
		Orphans:      store,
		Cache:        cache,
		ProjectID:    project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, nil, nil, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return model
}

func rotateOrphanSnapshots(t *testing.T, m Model, store *snapstore.Store) {
	t.Helper()
	previous := store.Snapshot()
	bundle := tuiTestBundle(t)
	wf := bundle.Workflows[0]
	wf.Buckets = []config.Bucket{
		{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
	}
	wf.Transitions = nil
	bundle.Workflows = []config.Workflow{wf}
	if err := store.ImportBundle(m.ctx, bundle, "test.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle(remove dev): %v", err)
	}
	current := store.Snapshot()
	svc := runtimecache.Service(m.repos.Cache, m.repos.ProjectID)
	if svc == nil {
		t.Fatal("operationService nil after install")
	}
	svc.SetSnapshot(current)
	svc.WireOrphan(current, previous)
	runtimecache.InstallRuntime(m.repos.Cache, m.repos.ProjectID, &agentruntime.ProjectRuntime{
		Service:          svc,
		Snapshot:         current,
		PreviousSnapshot: previous,
	})
}
