package tui

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
)

func TestHomeProjectDeleteUsesSQLiteSnapshotWriterWithPinnedWALReader(t *testing.T) {
	ctx, store, dbPath, raw, reader, project := homeBackupWALFixture(t)
	if _, err := raw.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (?, 1, 'tui committed wal row', '', 2, 'active')`, project.ID); err != nil {
		t.Fatalf("insert WAL task: %v", err)
	}

	model := Model{ctx: ctx, repos: Repositories{
		Projects:     store,
		Events:       store,
		Checkpointer: store,
		SnapshotWriter: func(snapshotCtx context.Context, _, destinationPath string) error {
			return store.Snapshot(snapshotCtx, destinationPath)
		},
		DBPath: dbPath,
	}}
	backup, err := model.buildHomeBackupService(nil)
	if err != nil {
		t.Fatalf("buildHomeBackupService: %v", err)
	}
	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	result, err := agentruntime.DeleteProject(ctx, store, backup, project.ID, counters)
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

func TestHomeProjectDeleteAbortsWhenInjectedSnapshotFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project, err := store.UpsertProject(ctx, "Preserved", "preserved", "/work/preserved")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	model := Model{ctx: ctx, repos: Repositories{
		Projects: store,
		Events:   store,
		SnapshotWriter: func(context.Context, string, string) error {
			return errors.New("forced snapshot failure")
		},
		DBPath: dbPath,
	}}
	backup, err := model.buildHomeBackupService(nil)
	if err != nil {
		t.Fatalf("buildHomeBackupService: %v", err)
	}
	if _, err := agentruntime.DeleteProjectChecked(ctx, legacyProjectStore{store}, backup, nil, store, project.ID, domain.ProjectDeleteCounters{}); err == nil {
		t.Fatal("project deletion succeeded after snapshot failure")
	}
	if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
		t.Fatalf("project missing after snapshot failure: %v", err)
	}
}
