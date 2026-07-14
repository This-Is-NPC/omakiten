package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"omakiten/internal/domain"
)

func TestBackupLeaseHonorsContextWhileAnotherHandleOwnsLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	first := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	second := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest})
	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.WithLease(context.Background(), func(BackupLease) error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := second.WithLease(ctx, func(BackupLease) error {
		return errors.New("second lease unexpectedly acquired")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second WithLease error = %v, want context deadline", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first WithLease: %v", err)
	}
}

type finalizationTestLease struct {
	BackupLease
	prune string
}

func (l *finalizationTestLease) PruneRetaining(string) error {
	l.prune = "normal"
	return nil
}

func (l *finalizationTestLease) PruneFailedRetaining(string) error {
	l.prune = "failed"
	return nil
}

type finalizationTestLeaser struct {
	BackupRunner
	lease *finalizationTestLease
	err   error
}

func (l *finalizationTestLeaser) WithLease(ctx context.Context, run func(BackupLease) error) error {
	return errors.Join(run(l.lease), l.err)
}

func TestRunLeasedDestructiveOperationSeparatesFailureFromPostCommitWarning(t *testing.T) {
	t.Parallel()

	releaseErr := errors.New("lease release failed")
	tests := map[string]DestructiveOperationResult{
		"operation failure": {
			BackupPath: "/recovery.db",
			Err:        errors.New("operation failed"),
		},
		"committed release failure": {
			BackupPath:        "/recovery.db",
			MutationCompleted: true,
		},
	}
	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lease := &finalizationTestLease{}
			got, leaseErr := RunLeasedDestructiveOperation(context.Background(), &finalizationTestLeaser{lease: lease, err: releaseErr}, func(RecoveryLease) DestructiveOperationResult {
				return operation
			})
			wantPrune := "failed"
			if operation.MutationCompleted {
				wantPrune = "normal"
			}
			if got.Err != operation.Err || !errors.Is(leaseErr, releaseErr) || lease.prune != wantPrune {
				t.Fatalf("result = %+v, lease error = %v, prune = %q", got, leaseErr, lease.prune)
			}
		})
	}
}

func TestRunLeasedDestructiveOperationDoesNotPruneWithoutBackup(t *testing.T) {
	t.Parallel()
	lease := &finalizationTestLease{}
	operation, err := RunLeasedDestructiveOperation(context.Background(), &finalizationTestLeaser{lease: lease}, func(RecoveryLease) DestructiveOperationResult {
		return DestructiveOperationResult{MutationCompleted: true}
	})
	if err != nil || !operation.MutationCompleted || lease.prune != "" {
		t.Fatalf("empty-backup finalization = operation:%+v leaseErr:%v prune:%q", operation, err, lease.prune)
	}
}

type blockingAtomicProjectRepository struct {
	ProjectRepository
	created chan string
	release chan struct{}
}

func (r *blockingAtomicProjectRepository) DeleteProjectWithBackup(
	ctx context.Context,
	_ int64,
	create func(context.Context, func(string) error) (string, error),
	_ func(string) error,
	validate func() error,
) (string, error) {
	path, err := create(ctx, func(destinationPath string) error {
		return os.WriteFile(destinationPath, []byte("recovery"), 0o600)
	})
	if err != nil {
		return "", err
	}
	r.created <- path
	<-r.release
	if err := validate(); err != nil {
		return path, err
	}
	return path, nil
}

func TestProjectDeleteLeasePreventsRetentionPruningUntilMutationCompletes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	t.Cleanup(func() { _ = store.Close() })
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	repo := &blockingAtomicProjectRepository{
		ProjectRepository: store,
		created:           make(chan string, 1),
		release:           make(chan struct{}),
	}
	deleteBackup := NewBackupService(BackupOptions{
		SourcePath: source,
		DestDir:    dest,
		Retention:  1,
		Now: func() time.Time {
			return time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
		},
	})
	deleteDone := make(chan error, 1)
	go func() {
		_, err := NewProjectService(repo, deleteBackup, nil).Delete(ctx, project.ID, domain.ProjectDeleteCounters{})
		deleteDone <- err
	}()
	recoveryPath := <-repo.created

	secondWriterStarted := make(chan struct{})
	secondBackup := NewBackupService(BackupOptions{
		SourcePath: source,
		DestDir:    dest,
		Retention:  1,
		Now: func() time.Time {
			return time.Date(2026, 7, 13, 10, 0, 1, 0, time.UTC)
		},
		SnapshotWriter: func(_ context.Context, sourcePath, destinationPath string) error {
			close(secondWriterStarted)
			return AtomicCopyFile(sourcePath, destinationPath, destinationPath+".tmp")
		},
	})
	secondDone := make(chan error, 1)
	go func() {
		_, err := secondBackup.Run(ctx)
		secondDone <- err
	}()
	select {
	case <-secondWriterStarted:
		t.Fatal("second backup acquired the lease before project mutation completed")
	case <-time.After(75 * time.Millisecond):
	}
	if _, err := os.Stat(recoveryPath); err != nil {
		t.Fatalf("project recovery backup was pruned while mutation was blocked: %v", err)
	}

	close(repo.release)
	if err := <-deleteDone; err != nil {
		t.Fatalf("project delete: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second backup: %v", err)
	}
}

func TestBackupPruneRejectsReplacedDirectoryAndAttackerTimestampFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit renaming this test's open directory handle")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	originalNames := []string{
		"2026-07-13T10-00-00.000000000Z.db",
		"2026-07-13T10-00-01.000000000Z.db",
	}
	for _, name := range originalNames {
		if err := os.WriteFile(filepath.Join(dest, name), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	attacker := filepath.Join(dir, "attacker")
	if err := os.Mkdir(attacker, 0o700); err != nil {
		t.Fatal(err)
	}
	attackerName := "2026-07-13T10-00-02.000000000Z.db"
	attackerPath := filepath.Join(attacker, attackerName)
	if err := os.WriteFile(attackerPath, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, "original-backups")
	svc := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest, Retention: 1})
	if err := svc.WithLease(context.Background(), func(lease BackupLease) error {
		if err := os.Rename(dest, moved); err != nil {
			return err
		}
		if err := os.Symlink(attacker, dest); err != nil {
			return err
		}
		if err := lease.PruneRetaining(""); err == nil {
			return errors.New("prune accepted a replaced backup directory")
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLease: %v", err)
	}
	if body, err := os.ReadFile(attackerPath); err != nil || string(body) != "unrelated" {
		t.Fatalf("attacker timestamp file changed: body=%q err=%v", body, err)
	}
	for _, name := range originalNames {
		if _, err := os.Stat(filepath.Join(moved, name)); err != nil {
			t.Fatalf("original backup %s was removed after replacement: %v", name, err)
		}
	}
}

func TestBackupFailedPruneBoundsDisabledRetentionAndProtectsRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "backups")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(dest, "2026-07-13T10-00-00.000000000Z.db"),
		filepath.Join(dest, "2026-07-13T10-00-01.000000000Z.db"),
		filepath.Join(dest, "2026-07-13T10-00-02.000000000Z.db"),
	}
	for index, path := range paths {
		if err := os.WriteFile(path, []byte{byte(index)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewBackupService(BackupOptions{SourcePath: source, DestDir: dest, Retention: 0})
	if err := svc.WithLease(context.Background(), func(lease BackupLease) error {
		return lease.PruneFailedRetaining(paths[0])
	}); err != nil {
		t.Fatalf("PruneFailedRetaining: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	entries = matchingBackupEntries(entries)
	if len(entries) != 1 || entries[0].Name() != filepath.Base(paths[0]) {
		t.Fatalf("failed-operation backups = %v, want only protected %q", entries, filepath.Base(paths[0]))
	}
}
