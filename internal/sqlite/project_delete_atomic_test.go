package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func TestDeleteProjectWithBackupAbortsContinuousGenerationChurnWithoutCandidates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, dbPath, project := atomicDeleteFixture(t, ctx)
	external := openAtomicDeleteWriter(t, dbPath)
	create, discard, attempts := atomicDeleteBackupCallbacks(t, dbPath)
	hooks := projectDeleteBackupHooks{Generation: exactGenerationHooks{BeforeBegin: func(attempt int) {
		if _, err := external.ExecContext(ctx, `INSERT INTO events(entity_type, entity_id, project_id, event_type, payload) VALUES ('project', ?, ?, ?, '{}')`, project.ID, project.ID, fmt.Sprintf("churn-%d", attempt)); err != nil {
			t.Errorf("external churn insert: %v", err)
		}
	}}}

	backupPath, err := store.deleteProjectWithBackup(ctx, project.ID, create, discard, func() error { return nil }, hooks)
	if err == nil {
		t.Fatal("continuous churn delete succeeded")
	}
	if backupPath != "" {
		t.Fatalf("continuous churn retained pre-mutation candidate %q", backupPath)
	}
	if *attempts != exactGenerationAttempts {
		t.Fatalf("backup attempts = %d, want %d", *attempts, exactGenerationAttempts)
	}
	if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
		t.Fatalf("project missing after churn abort: %v", err)
	}
	assertMatchingBackupCount(t, filepath.Join(filepath.Dir(dbPath), "backups"), 0)
}

func TestDeleteProjectWithBackupDiscardsAndRetriesCandidateAfterBeginFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, dbPath, project := atomicDeleteFixture(t, ctx)
	locker := openAtomicDeleteWriter(t, dbPath)
	create, discard, attempts := atomicDeleteBackupCallbacks(t, dbPath)
	var pinned *sql.Conn
	hooks := projectDeleteBackupHooks{
		AfterConnect: func(conn *sql.Conn) {
			pinned = conn
			if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 1`); err != nil {
				t.Errorf("set project-delete busy timeout: %v", err)
			}
		},
		Generation: exactGenerationHooks{BeforeBegin: func(attempt int) {
			if attempt == 1 {
				if _, err := locker.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
					t.Errorf("acquire competing writer lock: %v", err)
				}
			}
		}},
	}

	backupPath, err := store.deleteProjectWithBackup(ctx, project.ID, create, discard, func() error { return nil }, hooks)
	if _, rollbackErr := locker.ExecContext(ctx, `ROLLBACK`); rollbackErr != nil {
		t.Fatalf("release competing writer lock: %v", rollbackErr)
	}
	if err == nil {
		t.Fatal("project delete under competing writer lock succeeded")
	}
	if pinned == nil {
		t.Fatal("project-delete connection was not captured")
	}
	if backupPath != "" {
		t.Fatalf("begin failure retained candidate %q, want discard", backupPath)
	}
	if *attempts != exactGenerationAttempts {
		t.Fatalf("backup attempts = %d, want %d", *attempts, exactGenerationAttempts)
	}
	assertMatchingBackupCount(t, filepath.Join(filepath.Dir(dbPath), "backups"), 0)
}

func TestDeleteProjectWithBackupDiscardsCandidateAfterVersionReadFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*projectDeleteBackupHooks, func()){
		"after snapshot": func(hooks *projectDeleteBackupHooks, closeConn func()) {
			hooks.Generation.AfterBackup = func(int) { closeConn() }
		},
		"under writer lock": func(hooks *projectDeleteBackupHooks, closeConn func()) {
			hooks.Generation.AfterBegin = func(int) { closeConn() }
		},
	}
	for name, install := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store, dbPath, project := atomicDeleteFixture(t, ctx)
			create, discard, _ := atomicDeleteBackupCallbacks(t, dbPath)
			var pinned *sql.Conn
			var closeErr error
			hooks := projectDeleteBackupHooks{AfterConnect: func(conn *sql.Conn) { pinned = conn }}
			install(&hooks, func() {
				if closeErr == nil {
					closeErr = pinned.Close()
				}
			})

			backupPath, err := store.deleteProjectWithBackup(ctx, project.ID, create, discard, func() error { return nil }, hooks)
			if closeErr != nil {
				t.Fatalf("close project-delete connection: %v", closeErr)
			}
			if err == nil {
				t.Fatal("project delete survived injected data_version read failure")
			}
			if backupPath != "" {
				t.Fatalf("data_version read failure retained candidate %q, want discard", backupPath)
			}
			assertMatchingBackupCount(t, filepath.Join(filepath.Dir(dbPath), "backups"), 0)
			if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
				t.Fatalf("project missing after pre-mutation read failure: %v", err)
			}
		})
	}
}

func TestDeleteProjectWithBackupRollsBackFailureAndRetainsRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, dbPath, project := atomicDeleteFixture(t, ctx)
	if err := store.RecordEntityEvent(ctx, domain.EventEntityProject, project.ID, project.ID, "rollback-evidence", `{}`); err != nil {
		t.Fatalf("RecordEntityEvent: %v", err)
	}
	create, discard, _ := atomicDeleteBackupCallbacks(t, dbPath)
	sentinel := errors.New("forced pre-commit failure")
	hooks := projectDeleteBackupHooks{BeforeCommit: func(int) error { return sentinel }}

	backupPath, err := store.deleteProjectWithBackup(ctx, project.ID, create, discard, func() error { return nil }, hooks)
	if !errors.Is(err, sentinel) {
		t.Fatalf("delete error = %v, want %v", err, sentinel)
	}
	if backupPath == "" {
		t.Fatal("mutation-attempt failure did not retain recovery backup")
	}
	if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
		t.Fatalf("project deletion was not rolled back: %v", err)
	}
	var liveEvents int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE project_id = ? AND event_type = 'rollback-evidence'`, project.ID).Scan(&liveEvents); err != nil || liveEvents != 1 {
		t.Fatalf("live rollback evidence count = %d, err=%v", liveEvents, err)
	}
	assertSnapshotRowCount(t, backupPath, `SELECT COUNT(*) FROM events WHERE project_id = ? AND event_type = 'rollback-evidence'`, project.ID, 1)
}

func atomicDeleteFixture(t *testing.T, ctx context.Context) (*Store, string, domain.Project) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "live.db")
	store := openStoreFixture(t, dbPath)
	project, err := store.UpsertProject(ctx, "Doomed", "doomed", filepath.Join(t.TempDir(), "doomed"))
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (?, 1, 'seed', '', 2, 'active')`, project.ID); err != nil {
		t.Fatalf("insert seed task: %v", err)
	}
	return store.Store, dbPath, project
}

func openAtomicDeleteWriter(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsnWithPragmas(path, 5000, 0, -1))
	if err != nil {
		t.Fatalf("open external writer: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func atomicDeleteBackupCallbacks(t *testing.T, sourcePath string) (
	func(context.Context, func(string) error) (string, error),
	func(string) error,
	*int,
) {
	t.Helper()
	destDir := filepath.Join(filepath.Dir(sourcePath), "backups")
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	attempts := 0
	create := func(ctx context.Context, write func(string) error) (string, error) {
		attempts++
		svc := app.NewBackupService(app.BackupOptions{
			SourcePath: sourcePath,
			DestDir:    destDir,
			Now: func() time.Time {
				return time.Date(2026, 7, 13, 12, 0, 0, attempts, time.UTC)
			},
		})
		var path string
		err := svc.WithLease(ctx, func(lease app.BackupLease) error {
			var err error
			path, err = lease.WriteSnapshot(ctx, write)
			return err
		})
		return path, err
	}
	discard := func(path string) error { return os.Remove(path) }
	return create, discard, &attempts
}

func assertSnapshotRowCount(t *testing.T, path, query string, id int64, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = db.Close() }()
	var got int
	if err := db.QueryRowContext(context.Background(), query, id).Scan(&got); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if got != want {
		t.Fatalf("snapshot row count = %d, want %d", got, want)
	}
}

func assertMatchingBackupCount(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) && want == 0 {
			return
		}
		t.Fatalf("read backup dir: %v", err)
	}
	got := 0
	for _, entry := range entries {
		if backupFilenamePatternForAtomicDeleteTest(entry.Name()) {
			got++
		}
	}
	if got != want {
		t.Fatalf("matching backups = %d, want %d", got, want)
	}
}

func backupFilenamePatternForAtomicDeleteTest(name string) bool {
	return len(name) == len("2026-07-13T12-00-00.000000001Z.db") && filepath.Ext(name) == ".db"
}
