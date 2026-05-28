package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"omakiten/migrations"
)

// seedPreMigration029DB builds a database staged at post-028, pre-029. The
// tasks table already carries the depth column + index that 028 introduced,
// and every migration up to and including 028 is recorded in
// schema_migrations. Migration 029 itself is left for Open() to apply.
//
// The synthetic schema mirrors seedPreMigration028DB but lands the depth
// column inline so the planted rows can carry the same drifted state the
// bug produced on real DBs (parent_id != NULL while depth defaults to 0).
func seedPreMigration029DB(t testing.TB, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	for _, stmt := range []string{
		"PRAGMA foreign_keys = ON",
		"CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE, root_path TEXT NOT NULL UNIQUE)",
		`CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL REFERENCES projects(id),
			bucket_id INTEGER,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			priority_id INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at TEXT,
			parent_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
			depth INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id, id)
		)`,
		"CREATE INDEX idx_tasks_depth ON tasks(depth)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed stmt %q: %v", stmt, err)
		}
	}
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || name == "029_repair_tasks_depth.sql" {
			continue
		}
		if _, err := db.Exec("INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			t.Fatalf("mark migration %s applied: %v", name, err)
		}
	}
	if _, err := db.Exec("INSERT INTO projects (id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return db
}

// seedDriftedTaskChain plants a parent → child chain via raw INSERTs that
// omit the depth column, reproducing the bug the pre-028-aware INSERT
// statement caused: parent_id is set but depth lands on the column default
// (0) instead of the computed depth.
func seedDriftedTaskChain(t testing.TB, db *sql.DB, length int) {
	t.Helper()
	for id := 1; id <= length; id++ {
		var parent any
		if id > 1 {
			parent = id - 1
		}
		if _, err := db.Exec(
			"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id, parent_id) VALUES (?, 1, 1, ?, 2, ?)",
			id,
			fmt.Sprintf("task-%d", id),
			parent,
		); err != nil {
			t.Fatalf("seed drifted task %d: %v", id, err)
		}
	}
}

// seedBackfilledTaskChain plants the same chain seedDriftedTaskChain does
// but writes depth explicitly to mimic a DB whose 028 backfill landed
// correctly. Used to assert 029's idempotence.
func seedBackfilledTaskChain(t testing.TB, db *sql.DB, length int) {
	t.Helper()
	for id := 1; id <= length; id++ {
		var parent any
		if id > 1 {
			parent = id - 1
		}
		if _, err := db.Exec(
			"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id, parent_id, depth) VALUES (?, 1, 1, ?, 2, ?, ?)",
			id,
			fmt.Sprintf("task-%d", id),
			parent,
			id-1,
		); err != nil {
			t.Fatalf("seed backfilled task %d: %v", id, err)
		}
	}
}

func TestMigration029ReBackfillsDriftedDepth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	seedDriftedTaskChain(t, db, 4)

	pre := map[int]int{}
	rows, err := db.Query("SELECT id, depth FROM tasks ORDER BY id")
	if err != nil {
		t.Fatalf("query pre depths: %v", err)
	}
	for rows.Next() {
		var id, depth int
		if err := rows.Scan(&id, &depth); err != nil {
			t.Fatalf("scan pre depth: %v", err)
		}
		pre[id] = depth
	}
	_ = rows.Close()
	for _, id := range []int{2, 3, 4} {
		if pre[id] != 0 {
			t.Fatalf("pre-029 drifted depth on task %d = %d, want 0 (fixture is wrong)", id, pre[id])
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got := taskDepths(t, store)
	want := map[int]int{1: 0, 2: 1, 3: 2, 4: 3}
	for id, depth := range want {
		if got[id] != depth {
			t.Fatalf("task %d depth = %d, want %d (all depths: %#v)", id, got[id], depth, got)
		}
	}
}

func TestMigration029IdempotentOnAlreadyCorrectRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	seedBackfilledTaskChain(t, db, 4)

	pre := map[int]int{}
	rows, err := db.Query("SELECT id, depth FROM tasks ORDER BY id")
	if err != nil {
		t.Fatalf("query pre depths: %v", err)
	}
	for rows.Next() {
		var id, depth int
		if err := rows.Scan(&id, &depth); err != nil {
			t.Fatalf("scan pre depth: %v", err)
		}
		pre[id] = depth
	}
	_ = rows.Close()

	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got := taskDepths(t, store)
	if len(got) != len(pre) {
		t.Fatalf("post-029 row count = %d, pre = %d", len(got), len(pre))
	}
	for id, depth := range pre {
		if got[id] != depth {
			t.Fatalf("task %d depth changed: pre=%d post=%d", id, depth, got[id])
		}
	}
}

func TestMigration029Preserves64Truncation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	chainLen := orphanDepthLimit + 6
	seedDriftedTaskChain(t, db, chainLen)
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got := taskDepths(t, store)
	for id := 1; id <= orphanDepthLimit+1; id++ {
		want := id - 1
		if got[id] != want {
			t.Fatalf("task %d depth = %d, want %d", id, got[id], want)
		}
	}
	for id := orphanDepthLimit + 2; id <= chainLen; id++ {
		if got[id] != 0 {
			t.Fatalf("task %d above cap depth = %d, want 0", id, got[id])
		}
	}
}

func TestTriggerAutoComputesDepthOnInsertWithoutDepth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES (1, 1, 1, 'root', 2)",
	); err != nil {
		t.Fatalf("insert root: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id, parent_id) VALUES (2, 1, 1, 'child', 2, 1)",
	); err != nil {
		t.Fatalf("insert child without depth: %v", err)
	}

	got := taskDepths(t, store)
	if got[1] != 0 {
		t.Fatalf("root depth = %d, want 0", got[1])
	}
	if got[2] != 1 {
		t.Fatalf("child depth = %d, want 1 (trigger should have computed parent.depth + 1)", got[2])
	}
}

func TestTriggerNoOpWhenDepthAlreadySet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES (1, 1, 1, 'root', 2)",
	); err != nil {
		t.Fatalf("insert root: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id, parent_id, depth) VALUES (2, 1, 1, 'child', 2, 1, 5)",
	); err != nil {
		t.Fatalf("insert child with explicit depth: %v", err)
	}

	got := taskDepths(t, store)
	if got[2] != 5 {
		t.Fatalf("child depth = %d, want 5 (trigger WHEN clause should have skipped row)", got[2])
	}
}

func TestTriggerNoOpOnRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration029DB(t, dbPath)
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES (1, 1, 1, 'root', 2)",
	); err != nil {
		t.Fatalf("insert root without depth: %v", err)
	}

	got := taskDepths(t, store)
	if got[1] != 0 {
		t.Fatalf("root depth = %d, want 0 (trigger WHEN requires parent_id != NULL)", got[1])
	}
}
