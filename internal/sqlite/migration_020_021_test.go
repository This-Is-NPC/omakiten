package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigration021RecoversOrphanBucketIDsFromEvents covers the
// real-world failure mode: a user ran the pre-rebind shape of
// migration 020 against an existing DB. workflow_buckets is gone,
// tasks still carry SQL-era bucket_ids (>1000), and every view is
// empty because Snapshot.BucketByID cannot resolve those ids. The
// recovery migration walks the events table for each task's last
// known bucket key and remaps via the canonical preset CASE so the
// task lands on a sensible bucket id without any Go-side or manual
// intervention.
func TestMigration021RecoversOrphanBucketIDsFromEvents(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	// 1. Build the DB through migration 019 so we still have an
	//    events table but the broken-shape of 020 hasn't run yet.
	rawSeed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := rawSeed.ExecContext(ctx, pragma); err != nil {
			t.Fatalf("seed pragma %s: %v", pragma, err)
		}
	}
	if _, err := rawSeed.ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed schema_migrations: %v", err)
	}
	// Seed the broken state directly: projects + tasks with SQL-era
	// bucket_ids, plus the events rows that carry each task's last
	// known bucket key. The events schema mirrors what migration 009
	// installs.
	for _, stmt := range []string{
		`CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT NOT NULL UNIQUE, name TEXT NOT NULL, root_path TEXT NOT NULL)`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL REFERENCES projects(id), bucket_id INTEGER, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', priority_id INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, completed_at TEXT)`,
		`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, entity_id INTEGER NOT NULL, project_id INTEGER, event_type TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		// Mark every migration up through 020 as applied so the next
		// Open only fires 021. This is the exact shape the user's DB
		// is in: pre-rebind 020 already ran, workflow_buckets is gone.
		`INSERT INTO schema_migrations(version) VALUES
		   ('001_initial.sql'),
		   ('002_entities.sql'),
		   ('003_activity_logs.sql'),
		   ('004_tags.sql'),
		   ('005_transition_guards.sql'),
		   ('006_comment_tags.sql'),
		   ('007_errors.sql'),
		   ('008_solution_likes.sql'),
		   ('009_events.sql'),
		   ('010_agent_attribution.sql'),
		   ('011_purge_tui_summary_pollution.sql'),
		   ('012_task_state.sql'),
		   ('013_bucket_permissions_operations.sql'),
		   ('014_workflow_defaults.sql'),
		   ('015_priority_id.sql'),
		   ('016_severity_id.sql'),
		   ('017_drop_priority_severity_defaults.sql'),
		   ('018_drop_legacy_event_payloads.sql'),
		   ('019_unify_tool_call_events.sql'),
		   ('020_drop_config_tables.sql'),
		   ('022_search_index.sql'),
		   ('023_plans.sql'),
		   ('024_search_index_plans.sql')`,
		`INSERT INTO projects (id, slug, name, root_path) VALUES (1, 'p', 'P', '/p')`,
		// Tasks pointing at SQL-era PKs (the broken state).
		`INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES
		   (10, 1, 2761, 'T-backlog', 2),
		   (11, 1, 2763, 'T-review', 2),
		   (12, 1, 2764, 'T-done', 2),
		   (13, 1, 2764, 'T-orphan-no-events', 2)`,
		// Events carrying the bucket key for each task. Latest event
		// per task wins via ORDER BY id DESC LIMIT 1.
		`INSERT INTO events (entity_type, entity_id, project_id, event_type, payload) VALUES
		   ('task', 10, 1, 'task.created', '{"bucket":"backlog"}'),
		   ('task', 11, 1, 'task.created', '{"bucket":"backlog"}'),
		   ('task', 11, 1, 'task.moved', '{"from":"backlog","to":"review"}'),
		   ('task', 12, 1, 'task.created', '{"bucket":"backlog"}'),
		   ('task', 12, 1, 'task.moved', '{"from":"backlog","to":"done"}')`,
	} {
		if _, err := rawSeed.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed stmt %q: %v", stmt, err)
		}
	}
	if err := rawSeed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	// 2. Re-open through Store.Open so applyMigrations runs the new
	//    021 against the seeded broken state.
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	want := map[int64]int64{
		10: 1, // backlog
		11: 3, // review
		12: 4, // done
		13: 1, // no events → fallback to first bucket
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id, bucket_id FROM tasks ORDER BY id`)
	if err != nil {
		t.Fatalf("query tasks: %v", err)
	}
	defer rows.Close()
	got := map[int64]int64{}
	for rows.Next() {
		var id, bucket int64
		if err := rows.Scan(&id, &bucket); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = bucket
	}
	if rows.Err() != nil {
		t.Fatalf("rows.Err: %v", rows.Err())
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("task %d bucket_id = %d, want %d (got map=%v)", id, got[id], w, got)
		}
	}
}
