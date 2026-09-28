package agentruntime

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
)

func TestHomeProjectDeleteUsesSQLiteSnapshotWriterWithPinnedWALReader(t *testing.T) {
	ctx, store, dbPath, raw, reader, project := homeBackupWALFixture(t)
	if _, err := raw.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (?, 1, 'tui committed wal row', '', 2, 'active')`, project.ID); err != nil {
		t.Fatalf("insert WAL task: %v", err)
	}

	dir, err := paths.BackupDir()
	if err != nil {
		t.Fatal(err)
	}
	backup := NewBackup(BackupOptions{SourcePath: dbPath, DestDir: dir})
	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	result, err := DeleteProject(ctx, store, backup, project.ID, counters)
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	snapshot, err := sql.Open("sqlite", result.BackupPath)
	if err != nil {
		t.Fatalf("open recovery snapshot: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var snapshotCount int
	if err := snapshot.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE project_id = ? AND title = 'tui committed wal row'`, project.ID).Scan(&snapshotCount); err != nil || snapshotCount != 1 {
		t.Fatalf("TUI recovery snapshot WAL rows = %d, %v", snapshotCount, err)
	}
	runtime.KeepAlive(reader)
}

func homeBackupWALFixture(t *testing.T) (context.Context, *sqlite.Store, string, *sql.DB, *sql.Conn, domain.Project) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project, err := store.UpsertProject(ctx, "Doomed", "doomed", "/work/doomed")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(3)
	if _, err := raw.ExecContext(ctx, `PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatalf("disable autocheckpoint: %v", err)
	}
	if _, err := raw.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("baseline checkpoint: %v", err)
	}
	reader, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("reader conn: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := reader.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatalf("reader BEGIN: %v", err)
	}
	t.Cleanup(func() { _, _ = reader.ExecContext(context.Background(), `ROLLBACK`) })
	var baseline int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&baseline); err != nil {
		t.Fatalf("reader baseline: %v", err)
	}
	return ctx, store, dbPath, raw, reader, project
}
