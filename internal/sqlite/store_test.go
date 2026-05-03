package sqlite

import (
	"context"
	"testing"

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
		Skills:   []config.Skill{{ID: 1, Key: "go", Name: "Go"}},
		Personas: []config.Persona{{ID: 1, Key: "agent", Name: "Agent", SkillIDs: []int{1}}},
		Laws:     []config.Law{{ID: 1, Key: "scope", Severity: "error", Body: "Stay scoped."}},
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
