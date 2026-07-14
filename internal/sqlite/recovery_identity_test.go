package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/app"
)

func TestReindexWithBackupRejectsRecoveryPathChangesAtDestructiveBoundaries(t *testing.T) {
	tests := map[string]struct {
		install func(*reindexBackupHooks, func())
		mutate  func(string) error
	}{
		"removal after backup": {
			install: func(hooks *reindexBackupHooks, mutate func()) { hooks.Generation.AfterBackup = func(int) { mutate() } },
			mutate:  os.Remove,
		},
		"replacement under begin immediate": {
			install: func(hooks *reindexBackupHooks, mutate func()) { hooks.Generation.AfterBegin = func(int) { mutate() } },
			mutate: func(path string) error {
				if err := os.Remove(path); err != nil {
					return err
				}
				return os.WriteFile(path, []byte("replacement"), 0o600)
			},
		},
		"replacement immediately before commit": {
			install: func(hooks *reindexBackupHooks, mutate func()) { hooks.BeforeCommit = func(int) { mutate() } },
			mutate: func(path string) error {
				if err := os.Remove(path); err != nil {
					return err
				}
				return os.WriteFile(path, []byte("replacement"), 0o600)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "maintenance.db")
			setup := openStoreFixture(t, dbPath)
			execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('recovery identity evidence', 'retired_private_type', 501, 0)`)
			if err := setup.Close(); err != nil {
				t.Fatalf("close setup: %v", err)
			}
			maintenance, err := OpenSearchMaintenance(ctx, dbPath)
			if err != nil {
				t.Fatalf("OpenSearchMaintenance: %v", err)
			}
			defer func() { _ = maintenance.Close() }()
			backup := app.NewBackupService(app.BackupOptions{SourcePath: dbPath, DestDir: filepath.Join(dir, "backups")})
			var backupPath string
			var mutationErr error
			hooks := reindexBackupHooks{}
			test.install(&hooks, func() {
				if mutationErr == nil {
					mutationErr = test.mutate(backupPath)
				}
			})
			var resultErr error
			err = backup.WithLease(ctx, func(lease app.BackupLease) error {
				create := func(ctx context.Context, write func(string) error) (string, error) {
					path, err := lease.WriteSnapshot(ctx, write)
					backupPath = path
					return path, err
				}
				_, _, resultErr = maintenance.reindexSearchConfirmedWithBackup(ctx, create, lease.Discard, lease.Validate, hooks)
				return resultErr
			})
			if mutationErr != nil {
				t.Fatalf("mutate recovery path: %v", mutationErr)
			}
			if resultErr == nil || err == nil {
				t.Fatal("reindex committed with a changed recovery pathname")
			}
			report, checkErr := maintenance.CheckSearchIndex(ctx)
			if checkErr != nil {
				t.Fatalf("CheckSearchIndex after abort: %v", checkErr)
			}
			var evidence int64
			for _, typeReport := range report.Types {
				evidence += typeReport.Unsupported.Count
			}
			if evidence != 1 {
				t.Fatalf("reindex changed pre-repair evidence after recovery-path abort: %+v", report)
			}
		})
	}
}

func TestReindexWithBackupRetainsRecoveryWhenInvalidationRollbackIsUnproven(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "maintenance.db")
	setup := openStoreFixture(t, dbPath)
	execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('unproven rollback evidence', 'retired_private_type', 601, 0)`)
	if err := setup.Close(); err != nil {
		t.Fatalf("close setup: %v", err)
	}
	maintenance, err := OpenSearchMaintenance(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSearchMaintenance: %v", err)
	}
	backup := app.NewBackupService(app.BackupOptions{SourcePath: dbPath, DestDir: filepath.Join(dir, "backups")})
	var backupPath, retainedPath string
	var resultErr, hookErr error
	movedPath := dbPath + ".original"
	hooks := reindexBackupHooks{BeforeCommit: func(int) {
		if hookErr = os.Rename(dbPath, movedPath); hookErr != nil {
			return
		}
		if hookErr = os.WriteFile(dbPath, []byte("replacement"), 0o600); hookErr != nil {
			return
		}
		hookErr = maintenance.maintenanceConn.Close()
	}}
	err = backup.WithLease(ctx, func(lease app.BackupLease) error {
		create := func(ctx context.Context, write func(string) error) (string, error) {
			path, err := lease.WriteSnapshot(ctx, write)
			backupPath = path
			return path, err
		}
		_, retainedPath, resultErr = maintenance.reindexSearchConfirmedWithBackup(ctx, create, lease.Discard, lease.Validate, hooks)
		return resultErr
	})
	if hookErr != nil {
		t.Fatalf("force source invalidation and rollback failure: %v", hookErr)
	}
	if err == nil || !errors.Is(resultErr, errRollbackUnproven) {
		t.Fatalf("reindex error = %v, want rollback-unproven classification", err)
	}
	if retainedPath != backupPath || backupPath == "" {
		t.Fatalf("unproven rollback retained path = %q, want recovery %q", retainedPath, backupPath)
	}
	recovery, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatalf("open retained recovery: %v", err)
	}
	defer func() { _ = recovery.Close() }()
	var evidence int
	if err := recovery.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE content = 'unproven rollback evidence'`).Scan(&evidence); err != nil || evidence != 1 {
		t.Fatalf("retained noncanonical evidence = %d, %v", evidence, err)
	}
}
