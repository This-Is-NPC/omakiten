package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSchemaUpgradePreservesWork verifies databases at each earlier schema
// version open as the current schema and keep their rows.
func TestSchemaUpgradePreservesWork(t *testing.T) {
	t.Parallel()
	for name, downgrade := range map[string]string{
		"version 1": "DROP TABLE agent_sessions; DROP TABLE agent_teams; DROP INDEX idx_events_agent_session; DROP TABLE document_metadata; PRAGMA user_version = 1",
		"version 2": "DROP TABLE agent_sessions; DROP TABLE agent_teams; DROP INDEX idx_events_agent_session; PRAGMA user_version = 2",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertUpgradeFrom(t, downgrade)
		})
	}
}

// assertUpgradeFrom seeds a plan, applies downgrade, reopens, and checks the
// plan survives on the current schema.
func assertUpgradeFrom(t *testing.T, downgrade string) {
	t.Helper()
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
	if _, err := store.db.ExecContext(ctx, downgrade); err != nil {
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
	if err := verifyCurrentOmakitenSchema(ctx, store.db); err != nil {
		t.Fatalf("upgraded schema: %v", err)
	}
}
