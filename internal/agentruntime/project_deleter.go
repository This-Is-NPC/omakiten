package agentruntime

import (
	"context"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// ProjectDeleter binds backup policy and audit collaborators outside presentation.
func ProjectDeleter(store ProjectStore, recorder EventRecorder, sourcePath string, retention int) contract.ProjectDeleter {
	return func(ctx context.Context, id int64, counters domain.ProjectDeleteCounters, pruneWarn func(error)) (contract.ProjectDeleteResult, error) {
		dir, err := paths.BackupDir()
		if err != nil {
			return contract.ProjectDeleteResult{}, err
		}
		backup := NewBackup(BackupOptions{SourcePath: sourcePath, DestDir: dir, Retention: retention, PruneWarn: pruneWarn})
		return DeleteProjectChecked(ctx, store, backup, recorder, id, counters)
	}
}
