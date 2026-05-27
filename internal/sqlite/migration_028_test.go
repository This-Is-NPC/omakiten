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

func seedPreMigration028DB(t testing.TB, dbPath string) *sql.DB {
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
			UNIQUE(project_id, id)
		)`,
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
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || name == "028_tasks_depth.sql" {
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

func seedRawTaskChain(t testing.TB, db *sql.DB, length int) {
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
			t.Fatalf("seed task %d: %v", id, err)
		}
	}
}

func taskDepths(t testing.TB, store *Store) map[int]int {
	t.Helper()
	rows, err := store.db.Query("SELECT id, depth FROM tasks ORDER BY id")
	if err != nil {
		t.Fatalf("query depths: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[int]int{}
	for rows.Next() {
		var id, depth int
		if err := rows.Scan(&id, &depth); err != nil {
			t.Fatalf("scan depth: %v", err)
		}
		got[id] = depth
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("depth rows: %v", err)
	}
	return got
}

func TestMigration028BackfillsTaskDepth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration028DB(t, dbPath)
	seedRawTaskChain(t, db, 3)
	if _, err := db.Exec("INSERT INTO tasks (id, project_id, bucket_id, title, priority_id) VALUES (99, 1, 1, 'sibling-root', 2)"); err != nil {
		t.Fatalf("seed sibling root: %v", err)
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
	want := map[int]int{1: 0, 2: 1, 3: 2, 99: 0}
	for id, depth := range want {
		if got[id] != depth {
			t.Fatalf("task %d depth = %d, want %d (all depths: %#v)", id, got[id], depth, got)
		}
	}
}

func TestMigration028WarnsAndLeavesTruncatedDepthRowsAtZero(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedPreMigration028DB(t, dbPath)
	seedRawTaskChain(t, db, orphanDepthLimit+3)
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	logs := captureSlog(t)
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got := taskDepths(t, store)
	if got[orphanDepthLimit+1] != orphanDepthLimit {
		t.Fatalf("task at cap depth = %d, want %d", got[orphanDepthLimit+1], orphanDepthLimit)
	}
	if got[orphanDepthLimit+2] != 0 || got[orphanDepthLimit+3] != 0 {
		t.Fatalf("truncated depths = (%d, %d), want both 0", got[orphanDepthLimit+2], got[orphanDepthLimit+3])
	}
	if !strings.Contains(logs.String(), "tasks depth backfill truncated; descendants > 64 retain depth=0") {
		t.Fatalf("slog output = %q, want migration truncation warning", logs.String())
	}

	logs.Reset()
	if _, err := store.queryRecursiveOrphans(ctx, 1, "all", orphanScopedQueries[scopeAllTasks]); err != nil {
		t.Fatalf("queryRecursiveOrphans: %v", err)
	}
	if !strings.Contains(logs.String(), "orphan depth CTE truncated; deeper rows report depth=0") {
		t.Fatalf("slog output = %q, want orphan path truncation warning", logs.String())
	}
}
