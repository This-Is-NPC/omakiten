package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaUpgradePreservesWork verifies databases at each earlier schema
// version open as the current schema and keep their rows.
func TestSchemaUpgradePreservesWork(t *testing.T) {
	t.Parallel()
	for name, downgrade := range map[string]string{
		"version 1": "DROP TABLE agent_sessions; DROP TABLE agent_teams; DROP INDEX idx_events_agent_session; DROP TABLE document_metadata; PRAGMA user_version = 1",
		"version 2": "DROP TABLE agent_sessions; DROP TABLE agent_teams; DROP INDEX idx_events_agent_session; PRAGMA user_version = 2",
		"version 3": "PRAGMA user_version = 3",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertUpgradeFrom(t, downgrade)
		})
	}
}

// dropSyncIdentitySQL turns a current database into its version 3 shape; the
// capture triggers go first because they name the columns it drops.
func dropSyncIdentitySQL(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT 'DROP TRIGGER ' || name || ';' FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'sync\_capture\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var drops strings.Builder
	for rows.Next() {
		var drop string
		if err := rows.Scan(&drop); err != nil {
			t.Fatal(err)
		}
		drops.WriteString(drop)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return drops.String() + dropSyncObjectsSQL
}

const dropSyncObjectsSQL = `DROP VIEW sync_capture_projects; DROP VIEW sync_activity_event_types; DROP TABLE sync_rows; DROP TABLE sync_projects; DROP TABLE sync_inbox; DROP TABLE sync_identity;
DROP INDEX idx_projects_uid; DROP INDEX idx_tasks_uid; DROP INDEX idx_events_uid; DROP INDEX idx_errors_uid;
DROP INDEX idx_solutions_uid; DROP INDEX idx_plans_uid; DROP INDEX idx_plan_waves_uid;
ALTER TABLE projects DROP COLUMN uid; ALTER TABLE tasks DROP COLUMN uid; ALTER TABLE events DROP COLUMN uid;
ALTER TABLE errors DROP COLUMN uid; ALTER TABLE solutions DROP COLUMN uid; ALTER TABLE plans DROP COLUMN uid;
ALTER TABLE plan_waves DROP COLUMN uid;
ALTER TABLE events DROP COLUMN hlc; ALTER TABLE events DROP COLUMN device_id; ALTER TABLE events DROP COLUMN actor_id;
ALTER TABLE projects DROP COLUMN sync_scope;
`

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
	if _, err := store.db.ExecContext(ctx, dropSyncIdentitySQL(t, store.db)+downgrade); err != nil {
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
