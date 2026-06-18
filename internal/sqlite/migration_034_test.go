package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// explainQueryPlan returns the joined `detail` column of EXPLAIN QUERY PLAN
// for the given query, so a test can assert which index (if any) the planner
// chose. The bound args are immaterial to plan selection but are supplied so
// the statement prepares cleanly.
func explainQueryPlan(t *testing.T, store *Store, query string, args ...any) string {
	t.Helper()
	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return strings.Join(details, "\n")
}

// TestMigration034IndexesServeRealtimeReadPath asserts the two indexes added
// in migration 034 are actually chosen by the planner for the read paths they
// target. Migration 034 was justified by an EXPLAIN before/after diff (task
// #1263): without these indexes both target queries full-SCAN. The migration
// is pure schema, so the only meaningful regression guard is that the planner
// keeps using the indexes — a plan that reverts to SCAN means the index stopped
// pulling its weight and should be revisited, not shipped speculatively.
func TestMigration034IndexesServeRealtimeReadPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	// Open applies the full migration chain on a fresh DB — this is also
	// the "applies clean on a fresh DB" assertion from the DoD.
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedRealtimeReadPathRows(t, store)

	t.Run("events project-filtered uses idx_events_project_type", func(t *testing.T) {
		plan := explainQueryPlan(t, store,
			"SELECT id FROM events WHERE project_id = ? AND event_type IN ('comment','operation') "+
				"AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 200",
			1, "2000-01-01 00:00:00")
		if !strings.Contains(plan, "USING INDEX idx_events_project_type") &&
			!strings.Contains(plan, "USING COVERING INDEX idx_events_project_type") {
			t.Fatalf("project-filtered events plan does not use idx_events_project_type:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("project-filtered events plan still full-scans events:\n%s", plan)
		}
	})

	t.Run("events project-only uses idx_events_project_type", func(t *testing.T) {
		plan := explainQueryPlan(t, store,
			"SELECT id FROM events WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 200", 1)
		if !strings.Contains(plan, "idx_events_project_type") {
			t.Fatalf("project-only events plan does not use idx_events_project_type:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("project-only events plan still full-scans events:\n%s", plan)
		}
	})

	t.Run("reverse dependency lookup uses idx_task_deps_depends_on", func(t *testing.T) {
		// The cascade-delete OR branch in DeleteTask and the plan-network
		// edge resolution key task_dependencies on depends_on_task_id, which
		// the PK (project_id, task_id, depends_on_task_id) cannot prefix.
		plan := explainQueryPlan(t, store,
			"SELECT project_id, task_id, depends_on_task_id FROM task_dependencies "+
				"WHERE project_id = ? AND depends_on_task_id = ?", 1, 500)
		if !strings.Contains(plan, "USING INDEX idx_task_deps_depends_on") &&
			!strings.Contains(plan, "USING COVERING INDEX idx_task_deps_depends_on") {
			t.Fatalf("reverse dependency plan does not use idx_task_deps_depends_on:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN task_dependencies") {
			t.Fatalf("reverse dependency plan still full-scans task_dependencies:\n%s", plan)
		}
	})
}

// seedRealtimeReadPathRows inserts enough events, tasks, and dependency edges
// for the planner to have a real row population to reason about, then ANALYZEs
// so plan selection reflects statistics rather than an empty table.
func seedRealtimeReadPathRows(t *testing.T, store *Store) {
	t.Helper()
	stmts := []string{
		"INSERT INTO projects (id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')",
		`WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i < 2000)
		 INSERT INTO tasks (id, project_id, bucket_id, title, priority_id, state)
		 SELECT i, 1, 1, 'task-' || i, 2, 'active' FROM c`,
		`WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i < 5000)
		 INSERT INTO events (entity_type, entity_id, project_id, event_type, body)
		 SELECT 'task', (i % 2000) + 1, 1,
		   CASE WHEN i % 3 = 0 THEN 'comment' WHEN i % 3 = 1 THEN 'operation' ELSE 'transition' END,
		   'b' || i FROM c`,
		`WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i < 1900)
		 INSERT INTO task_dependencies (project_id, task_id, depends_on_task_id)
		 SELECT 1, i, i + 1 FROM c`,
		"ANALYZE",
	}
	for _, stmt := range stmts {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatalf("seed stmt %q: %v", stmt, err)
		}
	}
}
