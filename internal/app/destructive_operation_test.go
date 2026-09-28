package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/recovery"
)

type finalizationTestLease struct {
	contract.BackupLease
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
	lease *finalizationTestLease
	err   error
}

func (l *finalizationTestLeaser) WithLease(ctx context.Context, run func(contract.BackupLease) error) error {
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
			got, leaseErr := RunLeasedDestructiveOperation(context.Background(), &finalizationTestLeaser{lease: lease, err: releaseErr}, func(contract.RecoveryLease) DestructiveOperationResult {
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
	operation, err := RunLeasedDestructiveOperation(context.Background(), &finalizationTestLeaser{lease: lease}, func(contract.RecoveryLease) DestructiveOperationResult {
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
	deleteBackup := recovery.NewBackupService(recovery.BackupOptions{
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
	secondBackup := recovery.NewBackupService(recovery.BackupOptions{
		SourcePath: source,
		DestDir:    dest,
		Retention:  1,
		Now: func() time.Time {
			return time.Date(2026, 7, 13, 10, 0, 1, 0, time.UTC)
		},
		SnapshotWriter: func(_ context.Context, sourcePath, destinationPath string) error {
			close(secondWriterStarted)
			return os.WriteFile(destinationPath, []byte("source"), 0o600)
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
