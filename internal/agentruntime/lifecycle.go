package agentruntime

import (
	"bytes"
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// Backup wraps app.BackupService so CLI/TUI composition does not name app types.
type Backup struct {
	inner *app.BackupService
}

// BackupOptions is the CLI-facing constructor input for NewBackup.
type BackupOptions struct {
	SourcePath     string
	DestDir        string
	Retention      int
	SnapshotWriter func(context.Context, string, string) error
	PruneWarn      func(error)
}

func NewBackup(opts BackupOptions) *Backup {
	return &Backup{inner: app.NewBackupService(app.BackupOptions{
		SourcePath:     opts.SourcePath,
		DestDir:        opts.DestDir,
		Retention:      opts.Retention,
		SnapshotWriter: opts.SnapshotWriter,
		PruneWarn:      opts.PruneWarn,
	})}
}

func (b *Backup) Run(ctx context.Context) (string, error) {
	if b == nil || b.inner == nil {
		return "", domain.NewError(domain.ErrValidation, "backup service is required", nil)
	}
	return b.inner.Run(ctx)
}

// RecoveryLease is the subset of app.RecoveryLease CLI destructive flows need.
type RecoveryLease interface {
	WriteSnapshot(ctx context.Context, write func(destinationPath string) error) (string, error)
	Discard(path string) error
	Validate() error
}

// DestructiveResult separates committed mutations from retryable failures.
type DestructiveResult struct {
	BackupPath        string
	MutationCompleted bool
	Err               error
}

// RunLeased holds one backup-directory lease across recovery, mutation, and pruning.
func RunLeased(ctx context.Context, backup *Backup, run func(RecoveryLease) DestructiveResult) (DestructiveResult, error) {
	if backup == nil || backup.inner == nil {
		return DestructiveResult{}, domain.NewError(domain.ErrValidation, "backup service is required", nil)
	}
	op, err := app.RunLeasedDestructiveOperation(ctx, backup.inner, func(lease app.RecoveryLease) app.DestructiveOperationResult {
		got := run(lease)
		return app.DestructiveOperationResult{
			BackupPath:        got.BackupPath,
			MutationCompleted: got.MutationCompleted,
			Err:               got.Err,
		}
	})
	return DestructiveResult{
		BackupPath:        op.BackupPath,
		MutationCompleted: op.MutationCompleted,
		Err:               op.Err,
	}, err
}

// InitProject registers a project row. Sysadmin — not an MCP product tool.
func InitProject(ctx context.Context, repo app.ProjectRepository, name, slug, rootPath string) (domain.Project, error) {
	return app.NewProjectService(repo, nil, nil).Init(ctx, name, slug, rootPath)
}

// ProjectDeleteResult is the success payload DeleteProject returns.
type ProjectDeleteResult struct {
	Project    domain.Project               `json:"project"`
	Counters   domain.ProjectDeleteCounters `json:"counters"`
	BackupPath string                       `json:"backup_path"`
	EventType  string                       `json:"event_type"`
	Audit      string                       `json:"-"`
}

// ProjectStore is the composite sqlite.Store satisfies for project deletion.
type ProjectStore interface {
	app.ProjectRepository
	app.EventRecorder
	app.Checkpointer
}

// Checkpointer is the optional WAL checkpoint hook DeleteProjectChecked uses.
type Checkpointer interface {
	Checkpoint(ctx context.Context) error
}

// EventRecorder is the audit hook DeleteProjectChecked uses after a successful delete.
type EventRecorder interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error
}

// DeleteProject hard-deletes a project after writing a recovery snapshot.
// The store is also used as the checkpointer and event recorder (CLI / default path).
func DeleteProject(ctx context.Context, store ProjectStore, backup *Backup, projectID int64, counters domain.ProjectDeleteCounters) (ProjectDeleteResult, error) {
	return DeleteProjectChecked(ctx, store, backup, store, store, projectID, counters)
}

// DeleteProjectChecked is DeleteProject with an explicit checkpointer and
// event recorder. A nil checkpointer skips WithCheckpointer so the backup
// SnapshotWriter path runs. A nil event recorder falls back to store.
func DeleteProjectChecked(ctx context.Context, store ProjectStore, backup *Backup, cp Checkpointer, events EventRecorder, projectID int64, counters domain.ProjectDeleteCounters) (ProjectDeleteResult, error) {
	if backup == nil || backup.inner == nil {
		return ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project delete requires a backup", nil)
	}
	if events == nil {
		events = store
	}
	var audit bytes.Buffer
	svc := app.NewProjectService(store, backup.inner, events).SetAuditWarnWriter(&audit)
	if cp != nil {
		svc = svc.WithCheckpointer(cp)
	}
	result, err := svc.Delete(ctx, projectID, counters)
	out := ProjectDeleteResult{Audit: strings.TrimSpace(audit.String())}
	if err != nil {
		return out, err
	}
	out.Project = result.Project
	out.Counters = result.Counters
	out.BackupPath = result.BackupPath
	out.EventType = result.EventType
	return out, nil
}
