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
	store := openStoreFixture(t, t.TempDir()+"/test.db")

	bundle, _ := testfixtures.LoadBundle(t, "kanban_three_buckets.yaml")
	store.applyBundle(bundle)

	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	workflow := app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.snap())
	task, err := workflow.CreateTask(ctx, project.ID, "Nova task", "Desc", domain.Priority(2), "", nil)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if task.BucketKey != "todo" {
		t.Fatalf("CreateTask().BucketKey = %q, want %q", task.BucketKey, "todo")
	}
}
