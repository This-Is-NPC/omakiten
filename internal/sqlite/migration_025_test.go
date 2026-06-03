package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration025CascadesProjectDelete seeds a project with at least
// one row in every table that holds a project_id FK plus indirect
// children (task_dependencies, plan_waves, error_tags, solutions,
// project_tags, task_tags), deletes the project row directly via SQL,
// and asserts every child table reaches count(*)=0 for that project.
//
// The test exercises the FK cascade chain installed by migration 025
// end-to-end so the service layer can rely on plain DELETE FROM
// projects WHERE id = ? cleaning up every dependent row in a single
// transaction.
func TestMigration025CascadesProjectDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.db

	// Seed a single project with one row in every cascading child
	// table. priority_id defaults are dropped so we pass an explicit
	// value (2 = the canonical default per the YAML kit).
	for _, stmt := range []string{
		`INSERT INTO projects (id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')`,
		`INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES
		   (1, 1, 1, 'T1', 2),
		   (2, 1, 1, 'T2', 2)`,
		`INSERT INTO task_dependencies (project_id, task_id, depends_on_task_id) VALUES (1, 2, 1)`,
		`INSERT INTO plans (id, project_id, slug, name) VALUES (1, 1, 'plan1', 'Plan 1')`,
		`INSERT INTO plan_waves (id, plan_id, name, position) VALUES (1, 1, 'Wave 1', 1)`,
		`INSERT INTO errors (id, description, project_id) VALUES (1, 'boom', 1)`,
		`INSERT INTO solutions (id, error_id, description) VALUES (1, 1, 'fix it')`,
		`INSERT INTO tags (id, name, label) VALUES (1, 'urgent', 'urgent'), (2, 'tech-debt', 'tech-debt'), (3, 'sql', 'sql')`,
		`INSERT INTO project_tags (project_id, tag_id) VALUES (1, 1)`,
		`INSERT INTO task_tags (project_id, task_id, tag_id) VALUES (1, 1, 2)`,
		`INSERT INTO error_tags (error_id, tag_id) VALUES (1, 3)`,
		`INSERT INTO events (entity_type, entity_id, project_id, event_type) VALUES ('task', 1, 1, 'task.created')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = 1`); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	// Every FK-bearing child must reach 0 rows for project_id=1 (or
	// for the cascaded-via-tasks/plans/errors tables, 0 rows total
	// because the seed only touched project 1).
	checks := map[string]string{
		"tasks":              `SELECT COUNT(*) FROM tasks WHERE project_id = 1`,
		"task_dependencies":  `SELECT COUNT(*) FROM task_dependencies WHERE project_id = 1`,
		"task_tags":          `SELECT COUNT(*) FROM task_tags WHERE project_id = 1`,
		"plans":              `SELECT COUNT(*) FROM plans WHERE project_id = 1`,
		"plan_waves":         `SELECT COUNT(*) FROM plan_waves WHERE plan_id IN (SELECT id FROM plans)`,
		"errors":             `SELECT COUNT(*) FROM errors WHERE project_id = 1`,
		"solutions_via_err":  `SELECT COUNT(*) FROM solutions WHERE error_id NOT IN (SELECT id FROM errors)`,
		"error_tags_via_err": `SELECT COUNT(*) FROM error_tags WHERE error_id NOT IN (SELECT id FROM errors)`,
		"project_tags":       `SELECT COUNT(*) FROM project_tags WHERE project_id = 1`,
	}
	for label, query := range checks {
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		if n != 0 {
			t.Fatalf("%s rows after cascade = %d, want 0", label, n)
		}
	}

	// Tags themselves are project-agnostic — they stay. project_tags
	// is the bridge that cascades.
	var tagCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&tagCount); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tagCount != 3 {
		t.Fatalf("tags rows after cascade = %d, want 3 (tags are not project-scoped)", tagCount)
	}
}

// TestMigration025KeepsCrossProjectRowsIntact seeds two projects with
// independent child rows and asserts that deleting one project does
// not touch the other's data.
func TestMigration025KeepsCrossProjectRowsIntact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.db

	for _, stmt := range []string{
		`INSERT INTO projects (id, name, slug, root_path) VALUES (1, 'A', 'a', '/a'), (2, 'B', 'b', '/b')`,
		`INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES
		   (1, 1, 1, 'A1', 2),
		   (2, 2, 1, 'B1', 2)`,
		`INSERT INTO errors (id, description, project_id) VALUES (1, 'a-boom', 1), (2, 'b-boom', 2)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = 1`); err != nil {
		t.Fatalf("delete project A: %v", err)
	}

	survivors := map[string]int{
		"tasks":  1,
		"errors": 1,
	}
	for table, want := range survivors {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE project_id = 2`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != want {
			t.Fatalf("%s rows for project B = %d, want %d", table, n, want)
		}
	}

	// Project A's row count → 0; project B → 1.
	var aTasks int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE project_id = 1`).Scan(&aTasks); err != nil {
		t.Fatalf("count A tasks: %v", err)
	}
	if aTasks != 0 {
		t.Fatalf("project A tasks after cascade = %d, want 0", aTasks)
	}
}

// readableInt is a no-op helper retained for parity with neighbouring
// migration tests that read counts via sql.Scan; keeps the surface
// uniform when the test grows.
var _ = sql.ErrNoRows
