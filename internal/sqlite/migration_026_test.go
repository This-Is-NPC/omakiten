package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration026AddsParentIDColumn boots a fresh DB and asserts the
// parent_id column lands on the tasks table with the expected
// nullability and self-referencing FK.
func TestMigration026AddsParentIDColumn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.db

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(tasks)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		found    bool
		nullable bool
	)
	for rows.Next() {
		var (
			cid          int
			name         string
			ctype        string
			notnull      int
			dfltValue    any
			pk           int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "parent_id" {
			found = true
			nullable = notnull == 0
		}
	}
	if !found {
		t.Fatal("tasks.parent_id column missing after migration 026")
	}
	if !nullable {
		t.Fatal("tasks.parent_id must be nullable so existing rows stay roots")
	}

	fkRows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list(tasks)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list: %v", err)
	}
	defer func() { _ = fkRows.Close() }()

	var parentFKOnDelete string
	for fkRows.Next() {
		var (
			id        int
			seq       int
			table     string
			from      string
			to        string
			onUpdate  string
			onDelete  string
			match     string
		)
		if err := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan fk: %v", err)
		}
		if from == "parent_id" && table == "tasks" {
			parentFKOnDelete = onDelete
		}
	}
	if parentFKOnDelete != "CASCADE" {
		t.Fatalf("tasks.parent_id FK ON DELETE = %q, want CASCADE", parentFKOnDelete)
	}
}

// TestMigration026CascadesSubtree seeds a parent with two children and a
// grandchild then deletes the parent — every descendant row must be
// gone in one operation.
func TestMigration026CascadesSubtree(t *testing.T) {
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
		`INSERT INTO projects (id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')`,
		`INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES
		   (1, 1, 1, 'root', 2),
		   (2, 1, 1, 'child-a', 2),
		   (3, 1, 1, 'child-b', 2),
		   (4, 1, 1, 'grandchild', 2),
		   (99, 1, 1, 'sibling-root', 2)`,
		`UPDATE tasks SET parent_id = 1 WHERE id IN (2, 3)`,
		`UPDATE tasks SET parent_id = 2 WHERE id = 4`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE id = 1`); err != nil {
		t.Fatalf("delete root: %v", err)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id IN (1, 2, 3, 4)`).Scan(&remaining); err != nil {
		t.Fatalf("count subtree: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("subtree rows after cascade = %d, want 0", remaining)
	}

	var sibling int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = 99`).Scan(&sibling); err != nil {
		t.Fatalf("count sibling: %v", err)
	}
	if sibling != 1 {
		t.Fatalf("sibling root after cascade = %d, want 1 (unrelated subtrees must survive)", sibling)
	}
}
