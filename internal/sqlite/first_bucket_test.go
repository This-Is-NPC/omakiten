package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestWorkflowServiceCreateTaskDefaultsToFirstBucket(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	bundle := testfixtures.LoadBundle(t, "kanban_three_buckets.yaml")
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	workflow := app.NewWorkflowServiceFromStore(store)
	task, err := workflow.CreateTask(ctx, project.ID, "Nova task", "Desc", domain.PriorityZero, "")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if task.BucketKey != "todo" {
		t.Fatalf("CreateTask().BucketKey = %q, want %q", task.BucketKey, "todo")
	}
}
