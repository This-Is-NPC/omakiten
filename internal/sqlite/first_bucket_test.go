package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/config"
)

func TestCreateTaskDefaultsToFirstWorkflowBucket(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	bundle := config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Workflow: config.WorkflowSettings{Active: "kanban"},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "kanban",
			Name: "Kanban",
			Buckets: []config.Bucket{
				{ID: 1, Key: "todo", Name: "To Do", Position: 1},
				{ID: 2, Key: "wip", Name: "In Progress", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
			Transitions: []config.Transition{{From: 1, To: 2}, {From: 2, To: 3}},
		}},
	}

	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, project.ID, "Nova task", "Desc", "", "")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if task.BucketKey != "todo" {
		t.Fatalf("CreateTask().BucketKey = %q, want %q", task.BucketKey, "todo")
	}
}
