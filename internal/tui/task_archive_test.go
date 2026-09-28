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
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
)

func TestTaskArchiveUnarchiveViaFacade(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	snap := store.Snapshot()
	task, err := store.CreateTask(ctx, project.ID, "archive me", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	svc := operation.NewService(store, contract.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(snap)
	svc.SetSettings(operation.ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true,
		NextWorkLimit:      5,
		SimilarTaskLimit:   5,
	})
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{
		Service:  svc,
		Snapshot: snap,
	})

	m, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Events:       store,
		Tags:         store,
		Cache:        cache,
		ProjectID:    project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.height, m.width = 40, 160
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m.openTaskView(task)
	if !m.inTaskDetail() {
		t.Fatal("expected task detail open")
	}

	m.executeTaskArchiveFromDetail(screenhost.Action{
		Kind:       screenhost.ActionArchiveTaskFromDetail,
		TaskID:     task.ID,
		Generation: m.taskDetailGeneration,
	})
	assertArchived(t, ctx, store, project, task, m)
	m.executeTaskUnarchiveFromDetail(screenhost.Action{
		Kind:       screenhost.ActionUnarchiveTaskFromDetail,
		TaskID:     task.ID,
		Generation: m.taskDetailGeneration,
	})
	got, err := store.GetTaskByID(ctx, project.ID, task.ID, nil)
	if err != nil {
		t.Fatalf("GetTaskByID after unarchive: %v", err)
	}
	if got.State != domain.TaskStateActive {
		t.Fatalf("state = %q, want active", got.State)
	}
	if m.taskDetailScreen.Payload().Task.State != domain.TaskStateActive {
		t.Fatalf("detail payload state = %q, want active", m.taskDetailScreen.Payload().Task.State)
	}
}

func assertArchived(t *testing.T, ctx context.Context, store *snapstore.Store, project domain.Project, task domain.Task, m Model) {
	if !strings.Contains(m.status, "Archived") && !strings.Contains(m.status, "arquiv") {
		if !strings.Contains(m.status, "#") {
			t.Fatalf("archive status = %q", m.status)
		}
	}
	got, err := store.GetTaskByID(ctx, project.ID, task.ID, nil)
	if err != nil {
		t.Fatalf("GetTaskByID after archive: %v", err)
	}
	if got.State != domain.TaskStateArchived {
		t.Fatalf("state = %q, want archived", got.State)
	}
	if m.taskDetailScreen.Payload().Task.State != domain.TaskStateArchived {
		t.Fatalf("detail payload state = %q, want archived", m.taskDetailScreen.Payload().Task.State)
	}
}

func TestTaskArchiveRequiresFacade(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	snap := store.Snapshot()
	task, err := store.CreateTask(ctx, project.ID, "no facade", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{Snapshot: snap, Service: nil})

	m, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store, Events: store, Tags: store,
		Cache: cache, ProjectID: project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m.openTaskView(task)
	m.executeTaskArchiveFromDetail(screenhost.Action{
		Kind: screenhost.ActionArchiveTaskFromDetail, TaskID: task.ID, Generation: m.taskDetailGeneration,
	})
	if m.status == "" || strings.Contains(strings.ToLower(m.status), "archived task") {
		t.Fatalf("status = %q, want archive unavailable", m.status)
	}
	got, err := store.GetTaskByID(ctx, project.ID, task.ID, nil)
	if err != nil {
		t.Fatalf("GetTaskByID: %v", err)
	}
	if got.State != domain.TaskStateActive {
		t.Fatalf("task mutated without facade: state=%q", got.State)
	}
}
