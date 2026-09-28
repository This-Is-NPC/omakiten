package contract

import (
	"context"
)

// RecoveryLease exposes only the image operations needed during destruction;
// the app finalizer retains standalone-write and pruning ownership.
type RecoveryLease interface {
	WriteSnapshot(ctx context.Context, write func(destinationPath string) error) (string, error)
	Discard(path string) error
	Validate() error
}

// BackupLease is the full app-owned lease; operations receive RecoveryLease.
type BackupLease interface {
	RecoveryLease
	Write(ctx context.Context) (string, error)
	PruneRetaining(path string) error
	PruneFailedRetaining(path string) error
}

// BackupLeaser serializes snapshot creation, mutation and retention across processes.
type BackupLeaser interface {
	WithLease(ctx context.Context, run func(BackupLease) error) error
}
