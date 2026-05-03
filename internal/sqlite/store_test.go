package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestStoreProjectScopedTasks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	projectA, err := store.UpsertProject(ctx, "Project A", "a", "/work/a")
	if err != nil {
		t.Fatalf("UpsertProject(A) error = %v", err)
	}
	projectB, err := store.UpsertProject(ctx, "Project B", "b", "/work/b")
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}

	if _, err := store.CreateTask(ctx, projectA.ID, "A task", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, projectB.ID, "B task", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}

	tasks, err := store.ListTasks(ctx, projectA.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListTasks() len = %d, want 1", len(tasks))
	}
	if tasks[0].Title != "A task" {
		t.Fatalf("ListTasks()[0].Title = %q, want A task", tasks[0].Title)
	}
}

func TestStoreMoveTaskEnforcesWorkflow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	tests := []struct {
		name    string
		to      string
		wantErr bool
	}{
		{name: "allowed backlog to dev", to: "dev"},
		{name: "blocked dev to done", to: "done", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.MoveTask(ctx, project.ID, task.ID, tt.to)
			if tt.wantErr && err == nil {
				t.Fatalf("MoveTask() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("MoveTask() error = %v", err)
			}
		})
	}
}

func TestStoreOperationalDataIsProjectScoped(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	projectA, err := store.UpsertProject(ctx, "Project A", "a", "/work/a")
	if err != nil {
		t.Fatalf("UpsertProject(A) error = %v", err)
	}
	projectB, err := store.UpsertProject(ctx, "Project B", "b", "/work/b")
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}

	taskA1, err := store.CreateTask(ctx, projectA.ID, "A first", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A1) error = %v", err)
	}
	taskA2, err := store.CreateTask(ctx, projectA.ID, "A second", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A2) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, projectB.ID, "B first", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}

	if _, err := store.AddComment(ctx, projectA.ID, taskA1.ID, "A note", "human"); err != nil {
		t.Fatalf("AddComment(A) error = %v", err)
	}
	if _, err := store.AddComment(ctx, projectB.ID, taskB.ID, "B note", "human"); err != nil {
		t.Fatalf("AddComment(B) error = %v", err)
	}
	comments, err := store.ListComments(ctx, projectA.ID, 0)
	if err != nil {
		t.Fatalf("ListComments(A) error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "A note" {
		t.Fatalf("ListComments(A) = %#v, want only A note", comments)
	}

	if _, err := store.AddTaskDependency(ctx, projectA.ID, taskA2.ID, taskA1.ID); err != nil {
		t.Fatalf("AddTaskDependency(A) error = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, projectA.ID, taskA2.ID, taskB.ID); err == nil {
		t.Fatalf("AddTaskDependency(cross project) error = nil, want error")
	}
	dependencies, err := store.ListTaskDependencies(ctx, projectA.ID, 0)
	if err != nil {
		t.Fatalf("ListTaskDependencies(A) error = %v", err)
	}
	if len(dependencies) != 1 || dependencies[0].DependsOnTaskID != taskA1.ID {
		t.Fatalf("ListTaskDependencies(A) = %#v, want only A dependency", dependencies)
	}

	if _, err := store.AddContextEntry(ctx, projectA.ID, "A context", 2); err != nil {
		t.Fatalf("AddContextEntry(A) error = %v", err)
	}
	if _, err := store.AddContextEntry(ctx, projectB.ID, "B context", 2); err != nil {
		t.Fatalf("AddContextEntry(B) error = %v", err)
	}
	entries, err := store.ListContextEntries(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("ListContextEntries(A) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "A context" {
		t.Fatalf("ListContextEntries(A) = %#v, want only A context", entries)
	}
}

func TestDependencyServiceRejectsCycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	taskA, err := store.CreateTask(ctx, project.ID, "A", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, project.ID, "B", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}
	service := app.NewDependencyService(store)
	if _, err := service.Add(ctx, project.Context(), taskA.ID, taskB.ID); err != nil {
		t.Fatalf("Add(A->B) error = %v", err)
	}
	if _, err := service.Add(ctx, project.Context(), taskB.ID, taskA.ID); err == nil {
		t.Fatalf("Add(B->A) error = nil, want cycle error")
	}
}

func TestStoreActiveWorkflow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	workflow, err := store.ActiveWorkflow(ctx)
	if err != nil {
		t.Fatalf("ActiveWorkflow() error = %v", err)
	}
	if workflow.Key != "default" {
		t.Fatalf("ActiveWorkflow().Key = %q, want default", workflow.Key)
	}
	if len(workflow.Buckets) != 3 {
		t.Fatalf("ActiveWorkflow().Buckets len = %d, want 3", len(workflow.Buckets))
	}
	if len(workflow.Transitions) != 1 {
		t.Fatalf("ActiveWorkflow().Transitions len = %d, want 1", len(workflow.Transitions))
	}
}

func sqliteTestBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{Slug: "go", Name: "Go"}},
		Personas: []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}
