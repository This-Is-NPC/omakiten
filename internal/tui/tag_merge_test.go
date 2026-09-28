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

func TestTagMergeViaFacade(t *testing.T) {
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
	source, err := store.FindOrCreateTag(ctx, "golang", "Golang")
	if err != nil {
		t.Fatalf("FindOrCreateTag source: %v", err)
	}
	target, err := store.FindOrCreateTag(ctx, "go", "Go")
	if err != nil {
		t.Fatalf("FindOrCreateTag target: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "tagged", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AddTaskTag(ctx, project.ID, task.ID, source.ID); err != nil {
		t.Fatalf("AddTaskTag: %v", err)
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
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	m.confirmTagMerge("golang", "go")
	assertTagMerge(t, ctx, store, project, task, source, target, m)
}

func assertTagMerge(t *testing.T, ctx context.Context, store *snapstore.Store, project domain.Project, task domain.Task, source, target domain.Tag, m Model) {
	if !strings.Contains(strings.ToLower(m.status), "merge") && !strings.Contains(m.status, "Go") && !strings.Contains(m.status, "go") {
		t.Fatalf("status = %q, want merge confirmation", m.status)
	}
	tags, err := store.ListTaskTags(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListTaskTags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != target.ID {
		t.Fatalf("task tags after merge = %+v, want [%+v]", tags, target)
	}
	all, err := store.ListAllTags(ctx)
	if err != nil {
		t.Fatalf("ListAllTags: %v", err)
	}
	for _, tag := range all {
		if tag.ID == source.ID {
			t.Fatalf("source tag %d still present after merge", source.ID)
		}
	}
}

func TestTagMergeRequiresFacade(t *testing.T) {
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
	if _, err := store.FindOrCreateTag(ctx, "golang", "Golang"); err != nil {
		t.Fatalf("FindOrCreateTag source: %v", err)
	}
	if _, err := store.FindOrCreateTag(ctx, "go", "Go"); err != nil {
		t.Fatalf("FindOrCreateTag target: %v", err)
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
	m.confirmTagMerge("golang", "go")
	if m.status == "" || strings.Contains(strings.ToLower(m.status), "merged") {
		t.Fatalf("status = %q, want merge unavailable", m.status)
	}
}

func TestTagMergeScreenActions(t *testing.T) {
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
	if _, err := store.FindOrCreateTag(ctx, "alpha", "Alpha"); err != nil {
		t.Fatalf("FindOrCreateTag alpha: %v", err)
	}
	if _, err := store.FindOrCreateTag(ctx, "beta", "Beta"); err != nil {
		t.Fatalf("FindOrCreateTag beta: %v", err)
	}
	svc := operation.NewService(store, contract.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(snap)
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{Service: svc, Snapshot: snap})

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

	m.applyScreenOutcome(screenhost.Outcome{
		Action: screenhost.Action{Kind: screenhost.ActionPrepareTagMerge, Value: "alpha"},
	})
	if !strings.Contains(m.status, "Alpha") && !strings.Contains(strings.ToLower(m.status), "merge") {
		t.Fatalf("prepare status = %q", m.status)
	}
	m.applyScreenOutcome(screenhost.Outcome{
		Action: screenhost.Action{Kind: screenhost.ActionMergeTags, SourceValue: "alpha", Value: "beta"},
	})
	all, err := store.ListAllTags(ctx)
	if err != nil {
		t.Fatalf("ListAllTags: %v", err)
	}
	for _, tag := range all {
		if tag.Name == "alpha" {
			t.Fatalf("alpha still present after merge action: %+v", all)
		}
	}
}
