package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/taskform"
)

func setupTaskEditStore(t *testing.T) (context.Context, *snapstore.Store, domain.Project) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatal(err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, project
}

func TestTaskFormScreenIsRegisteredAndCreatesThroughHost(t *testing.T) {
	ctx, store, project := setupTaskEditStore(t)
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	if _, ok := screenRegistry.ByID(screenhost.TaskForm); !ok {
		t.Fatal("task form screen is not registered")
	}

	m.openTaskCreate()
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.TaskForm {
		t.Fatalf("screen stack = %v, want task form", m.screenStack)
	}
	for _, r := range "Created from screen" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, m.repos.activeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Created from screen" {
		t.Fatalf("created tasks = %#v", tasks)
	}
	if len(m.screenStack) != 1 || m.screenStack[0] != screenhost.TaskDetail || m.taskDetailScreen.Payload().Task.ID != tasks[0].ID {
		t.Fatalf("post-save route stack=%v task=%d", m.screenStack, m.taskDetailScreen.Payload().Task.ID)
	}
}

func TestTaskFormHostRejectsStaleOutcomeBeforeStoringScreen(t *testing.T) {
	ctx, store, project := setupTaskEditStore(t)
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskCreate()
	current := m.taskFormScreen
	stale := taskform.New().Bind(m.taskFormDeps()).Open(taskform.Payload{
		Mode: taskform.Create, Generation: m.taskFormGeneration - 1,
		Values:     taskform.Values{Title: "stale", Priority: "2"},
		Priorities: m.taskFormPriorities(),
	}, m.screenFrame())
	m.applyScreenOutcome(screenhost.Outcome{Screen: stale, Action: screenhost.Action{
		Kind: screenhost.ActionSaveTaskForm, Generation: m.taskFormGeneration - 1,
		TaskFormMode: taskform.Create.String(), TaskTitle: "stale", TaskPriority: "2",
	}})
	if got := m.taskFormScreen.Values().Title; got != current.Values().Title {
		t.Fatalf("stale outcome stored title %q, want %q", got, current.Values().Title)
	}
	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, m.repos.activeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("stale save persisted tasks %#v", tasks)
	}
}

func TestTaskFormSubtaskCancelReturnsToParent(t *testing.T) {
	ctx, store, project := setupTaskEditStore(t)
	parent, err := store.CreateTask(ctx, project.ID, "Parent", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskView(parent)
	m.openSubTaskCreate(parent)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if len(m.screenStack) != 1 || m.screenStack[0] != screenhost.TaskDetail || m.taskDetailScreen.Payload().Task.ID != parent.ID {
		t.Fatalf("cancel route stack=%v task=%d, want parent #%d", m.screenStack, m.taskDetailScreen.Payload().Task.ID, parent.ID)
	}
}

func TestTaskFormEditOwnsFooterAndHelpAboveTaskDetail(t *testing.T) {
	ctx, store, project := setupTaskEditStore(t)
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	m := newTaskEditIntegrationModel(t, ctx, store, project)
	m.openTaskEdit(task)
	footer := m.footerTokens()
	if len(footer) == 0 || footer[0].key != "ctrl+s" {
		t.Fatalf("task form footer = %#v", footer)
	}
	if got := m.currentHelpTitles(); len(got) != 1 || got[0] != "task_form" {
		t.Fatalf("task form help titles = %v", got)
	}
}
