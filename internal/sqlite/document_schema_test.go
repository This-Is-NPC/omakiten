package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDocumentSchemaUpgradePreservesWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.UpsertProject(ctx, "Example", "example", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.CreatePlan(ctx, project.ID, "portable", "Portable", "## Goal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE document_metadata; PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	got, err := store.GetPlanBySlug(ctx, project.ID, plan.Slug)
	if err != nil || got.ID != plan.ID || got.GoalBody != plan.GoalBody {
		t.Fatalf("work changed: %+v, %v", got, err)
	}
	if err := store.SaveWorkMetadata(ctx, "plan", plan.ID, []byte(`{"document":{"type":"Omakiten Plan"}}`)); err != nil {
		t.Fatal(err)
	}
}
