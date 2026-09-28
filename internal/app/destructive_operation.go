package app

import (
	"context"

	"omakiten/internal/contract"
)

// DestructiveOperationResult separates committed mutations from retryable failures.
type DestructiveOperationResult struct {
	BackupPath        string
	MutationCompleted bool
	Err               error
}

// RunLeasedDestructiveOperation holds one lease across recovery, mutation, and pruning.
func RunLeasedDestructiveOperation(ctx context.Context, backup contract.BackupLeaser, run func(contract.RecoveryLease) DestructiveOperationResult) (DestructiveOperationResult, error) {
	var operation DestructiveOperationResult
	leaseErr := backup.WithLease(ctx, func(lease contract.BackupLease) error {
		operation = run(lease)
		if operation.BackupPath != "" {
			if operation.MutationCompleted {
				_ = lease.PruneRetaining(operation.BackupPath)
			} else {
				_ = lease.PruneFailedRetaining(operation.BackupPath)
			}
		}
		return nil
	})
	return operation, leaseErr
}
