package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/taskform"
)

func TestTaskEditParentLookupRejectsForeignProject(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	foreignProject, _ := store.UpsertProject(ctx, "Foreign", "foreign", "/work/foreign")
	task, _ := store.CreateTask(ctx, project.ID, "Subject", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	foreign, _ := store.CreateTask(ctx, foreignProject.ID, "Foreign parent", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskEdit(task)

	setTaskFormValues(&m, taskform.Values{Title: task.Title, Priority: "2", Parent: fmt.Sprint(foreign.ID)})
	m.lookupTaskFormParent(screenhost.Action{Generation: m.taskFormGeneration, TaskFormMode: taskform.Edit.String(), TaskID: task.ID, TaskParent: fmt.Sprint(foreign.ID)})
	if got := m.taskFormScreen.Err(); !strings.Contains(strings.ToLower(got), "no task") {
		t.Fatalf("foreign-project parent error = %q, want same-project not-found hint", got)
	}
}

func TestTaskEditEmptyParentSavesAsRoot(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	parent, _ := store.CreateTask(ctx, project.ID, "Parent", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	child, _ := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", &parent.ID, store.Snapshot())
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskEdit(child)
	setTaskFormValues(&m, taskform.Values{Title: child.Title, Priority: "2", TagsCSV: "alpha, beta"})

	m.saveTaskForm()
	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{IncludeArchived: true}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	var saved domain.Task
	for _, task := range tasks {
		if task.ID == child.ID {
			saved = task
			break
		}
	}
	if saved.ParentID != nil {
		t.Fatalf("saved parent = %v, want root nil", *saved.ParentID)
	}
	tags, err := store.ListTaskTags(ctx, project.ID, child.ID)
	if err != nil {
		t.Fatalf("ListTaskTags() error = %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "alpha" || tags[1].Name != "beta" {
		t.Fatalf("saved tags = %+v, want alpha and beta", tags)
	}
}

func TestTaskEditTagDiffUsesCanonicalNames(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	task, _ := store.CreateTask(ctx, project.ID, "Subject", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	svc := operation.NewService(store, contract.ProjectSelector{ProjectID: project.ID})
	svc.SetSnapshot(store.Snapshot())
	if _, err := svc.AddTag(ctx, contract.AddTagInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: project.ID},
		EntityType:      "task",
		EntityID:        task.ID,
		TagName:         "alpha",
	}); err != nil {
		t.Fatalf("Add(alpha) error = %v", err)
	}
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskEdit(task)
	setTaskFormValues(&m, taskform.Values{Title: task.Title, Priority: "2", TagsCSV: "Alpha"})

	m.saveTaskForm()
	tags, err := store.ListTaskTags(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListTaskTags() error = %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "alpha" {
		t.Fatalf("saved tags = %+v, want canonical alpha preserved", tags)
	}
}

func TestTaskEditCycleErrorNamesConflictingDescendant(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	root, _ := store.CreateTask(ctx, project.ID, "Root", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	child, _ := store.CreateTask(ctx, project.ID, "Conflicting child", "", domain.Priority(2), "backlog", &root.ID, store.Snapshot())
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskEdit(root)
	setTaskFormValues(&m, taskform.Values{
		Title:    root.Title,
		Priority: "2",
		Parent:   fmt.Sprint(child.ID),
	})

	m.saveTaskForm()
	for _, want := range []string{fmt.Sprintf("#%d", child.ID), child.Title} {
		if !strings.Contains(m.status, want) {
			t.Fatalf("cycle status = %q, want conflicting descendant %q", m.status, want)
		}
	}
}

func setTaskFormValues(m *Model, values taskform.Values) {
	payload := m.taskFormScreen.Payload()
	payload.Values = values
	m.taskFormScreen = m.taskFormScreen.Open(payload, m.screenFrame())
}

func newTaskEditIntegrationModel(t *testing.T, ctx context.Context, store *snapstore.Store, project domain.Project) Model {
	t.Helper()
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Tags:         store,
		Events:       store,
		ActivityLogs: store,
		Cache:        runtimecache.InstallWithStore(0, store),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return model
}
