package sqlite

import (
	"context"
	"database/sql"
	"io"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// TestProjectDeleteSnapshotsRealWALData closes the integration gap the
// fake-ledger checkpoint tests left open (comment 7959 info row 5):
// the in-process fake checkpointer + fake backup runner can pin call
// order but cannot prove that WAL frames committed from this process
// actually land in the .db file BackupService copies.
//
// This test wires the real *sqlite.Store as Checkpointer, the real
// *app.BackupService as BackupRunner, and runs the full
// ProjectService.Delete cascade against a fresh DB. Tasks are inserted
// through the store's normal write path (the inserts hit the WAL
// sidecar in WAL mode), then ProjectService.Delete fires its
// `PRAGMA wal_checkpoint(TRUNCATE)` before BackupService.Run copies
// the .db file. We re-open the resulting snapshot as a fresh *sql.DB
// and assert the task rows survived the round-trip — proving that
// committed-but-uncheckpointed WAL data really does end up in the
// backup, not just in some ordering ledger.
func TestProjectDeleteSnapshotsRealWALData(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/live.db"
	store := openStoreFixture(t, dbPath)
	store.applyBundle(sqliteTestBundle(t))

	project, err := store.UpsertProject(ctx, "Doomed", "doomed", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	const taskCount = 3
	for i := 0; i < taskCount; i++ {
		if _, err := store.CreateTask(ctx, project.ID, "wal-bound", "", domain.Priority(2), "backlog", store.snap()); err != nil {
			t.Fatalf("CreateTask(%d): %v", i, err)
		}
	}

	backupDir := t.TempDir()
	backup := app.NewBackupService(app.BackupOptions{
		SourcePath: dbPath,
		DestDir:    backupDir,
		Retention:  0,
	})

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	if counters.Tasks != taskCount {
		t.Fatalf("pre-delete counters.Tasks = %d, want %d", counters.Tasks, taskCount)
	}

	svc := app.NewProjectService(store, backup, store).
		WithCheckpointer(store).
		SetAuditWarnWriter(io.Discard)
	result, err := svc.Delete(ctx, project.ID, counters)
	if err != nil {
		t.Fatalf("ProjectService.Delete: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatalf("Delete returned empty BackupPath; backup did not run")
	}

	// Live store: project + tasks must be gone (cascade FKs).
	if _, err := store.FindProjectByID(ctx, project.ID); err == nil {
		t.Fatalf("project still present in live store after delete")
	}

	// Snapshot: re-open as a fresh *sql.DB and assert the task rows
	// survived the WAL → main → backup → fresh-handle round-trip.
	// Without the checkpoint, the inserts could sit only in the WAL
	// sidecar at the moment of copy and the snapshot would see zero
	// rows.
	snap, err := sql.Open("sqlite", result.BackupPath)
	if err != nil {
		t.Fatalf("open backup as sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = snap.Close() })

	var snapTaskCount int
	if err := snap.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE project_id = ?", project.ID,
	).Scan(&snapTaskCount); err != nil {
		t.Fatalf("count tasks in backup: %v", err)
	}
	if snapTaskCount != taskCount {
		t.Fatalf("tasks in backup = %d, want %d — WAL frames did not land in the snapshot", snapTaskCount, taskCount)
	}

	var snapProjectName string
	if err := snap.QueryRowContext(ctx,
		"SELECT name FROM projects WHERE id = ?", project.ID,
	).Scan(&snapProjectName); err != nil {
		t.Fatalf("read project from backup: %v", err)
	}
	if snapProjectName != "Doomed" {
		t.Fatalf("project name in backup = %q, want %q", snapProjectName, "Doomed")
	}
}
