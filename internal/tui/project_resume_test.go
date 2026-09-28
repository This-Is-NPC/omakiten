package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/projectresume"
)

func TestProjectResumeViaFacade(t *testing.T) {
	fixture := setupProjectResumeFixture(t)
	m, task, blocked := fixture.model, fixture.task, fixture.blocked

	m.openProjectResume()
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.ProjectResume {
		t.Fatalf("screen stack = %v, want ProjectResume on top", m.screenStack)
	}
	payload := m.projectResumeScreen.Payload()
	if payload.Err != nil {
		t.Fatalf("resume payload err = %v", payload.Err)
	}
	if !m.projectResumeScreen.Loaded() {
		t.Fatal("expected resume screen loaded")
	}
	assertProjectResumePayload(t, m, task, blocked)
}

func assertProjectResumePayload(t *testing.T, m Model, task, blocked domain.Task) {
	payload := m.projectResumeScreen.Payload()
	foundLikely := false
	for _, item := range payload.LikelyNextWork {
		if item.ID == task.ID {
			foundLikely = true
		}
	}
	if !foundLikely {
		t.Fatalf("likely next missing task #%d: %+v", task.ID, payload.LikelyNextWork)
	}
	foundBlocked := false
	for _, item := range payload.BlockedWork {
		if item.ID == blocked.ID {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatalf("blocked missing task #%d: %+v", blocked.ID, payload.BlockedWork)
	}
	foundDep := false
	for _, dep := range payload.Dependencies {
		if dep.TaskID == blocked.ID && dep.DependsOnTaskID == task.ID {
			foundDep = true
		}
	}
	if !foundDep {
		t.Fatalf("dependencies missing edge: %+v", payload.Dependencies)
	}
}

type projectResumeFixture struct {
	model   Model
	task    domain.Task
	blocked domain.Task
}

func setupProjectResumeFixture(t *testing.T) projectResumeFixture {
	t.Helper()
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
	task, err := store.CreateTask(ctx, project.ID, "likely next", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	blocked, err := store.CreateTask(ctx, project.ID, "blocked work", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask blocked: %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, blocked.ID, task.ID); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}
	svc := operation.NewService(store, operation.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(snap)
	svc.SetSettings(operation.ServiceSettings{RecentCommentLimit: 5, IncludeWorkflow: true, NextWorkLimit: 5, SimilarTaskLimit: 5})
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{Service: svc, Snapshot: snap})
	m, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Events: store, Tags: store, Cache: cache, ProjectID: project.ID}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return projectResumeFixture{model: m, task: task, blocked: blocked}
}

func TestProjectResumeRequiresFacade(t *testing.T) {
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
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(project.ID, &agentruntime.ProjectRuntime{Snapshot: snap, Service: nil})

	m, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store, Events: store, Tags: store,
		Cache: cache, ProjectID: project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.openProjectResume()
	if m.status == "" || !strings.Contains(strings.ToLower(m.status), "unavailable") && m.projectResumeScreen.Payload().Err == nil {
		t.Fatalf("status=%q payload.err=%v, want resume unavailable", m.status, m.projectResumeScreen.Payload().Err)
	}
}

func TestProjectResumeActionFromProjectScreen(t *testing.T) {
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
	svc := operation.NewService(store, operation.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(snap)
	svc.SetSettings(operation.ServiceSettings{NextWorkLimit: 5})
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
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{Kind: screenhost.ActionOpenProjectResume}})
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.ProjectResume {
		t.Fatalf("stack=%v, want ProjectResume", m.screenStack)
	}
}

func TestProjectResumeFailureFinalRenderSanitizesGlobalStatus(t *testing.T) {
	failure := errors.New(hostileGlobalStatus)
	model := Model{
		styles:              newStyles(config.Theme{}),
		width:               80,
		height:              24,
		projectResumeScreen: projectresume.New().Apply(projectresume.Payload{Err: failure}),
		status:              failure.Error(),
		screenStack:         []screenhost.ID{screenhost.ProjectResume},
	}

	assertHostileStatusIsSafe(t, model.View())
}
