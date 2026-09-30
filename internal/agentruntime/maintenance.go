package agentruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/recovery"
	"omakiten/internal/sqlite"
)

// forbiddenBackupOutRoots are the system trees a pinned backup destination
// may not enter: the snapshot carries every project's data.
var forbiddenBackupOutRoots = []string{"/etc", "/usr", "/proc", "/sys", "/dev"}

// MaintenanceOptions binds database-wide recovery to one database file.
type MaintenanceOptions struct {
	DBPath    string
	Retention int
	// Projects and Events serve project deletion; database-only callers leave
	// them nil. A nil Events records through Projects.
	Projects ProjectStore
	Events   EventRecorder
	// OpenSearchStore opens the search maintenance connection. Nil opens DBPath.
	OpenSearchStore func(context.Context) (*sqlite.Store, error)
	// Catalog resolves validation messages. Nil returns the message keys.
	Catalog *config.Catalog
}

// Maintenance implements contract.Maintenance against SQLite and the recovery
// backup directory.
type Maintenance struct {
	opts MaintenanceOptions
}

var _ contract.Maintenance = (*Maintenance)(nil)

func NewMaintenance(opts MaintenanceOptions) *Maintenance {
	if opts.OpenSearchStore == nil {
		dbPath := opts.DBPath
		opts.OpenSearchStore = func(ctx context.Context) (*sqlite.Store, error) {
			return sqlite.OpenSearchMaintenance(ctx, dbPath)
		}
	}
	if opts.Events == nil && opts.Projects != nil {
		opts.Events = opts.Projects
	}
	return &Maintenance{opts: opts}
}

func (m *Maintenance) BackupDatabase(ctx context.Context, input contract.DatabaseBackupInput) (contract.DatabaseBackupResult, error) {
	if input.Out != "" {
		return m.backupToPath(ctx, input)
	}
	var warnings []string
	backup, err := m.backupService(&warnings)
	if err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	path, err := backup.Run(ctx)
	if err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	retention := m.opts.Retention
	return contract.DatabaseBackupResult{Path: path, Pruned: true, Retention: &retention, PruneWarnings: warnings}, nil
}

func (m *Maintenance) backupToPath(ctx context.Context, input contract.DatabaseBackupInput) (contract.DatabaseBackupResult, error) {
	finalPath, err := filepath.Abs(input.Out)
	if err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	finalPath = filepath.Clean(finalPath)
	if root, blocked := blockedBackupOutRoot(finalPath); blocked {
		return contract.DatabaseBackupResult{}, domain.NewError(domain.ErrValidation, fmt.Sprintf(m.opts.Catalog.Get("cli.db.backup.error.system_path_fmt"), finalPath, root), map[string]any{"path": finalPath, "root": root})
	}
	if !input.Force {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			return contract.DatabaseBackupResult{}, domain.NewError(domain.ErrValidation, fmt.Sprintf(m.opts.Catalog.Get("cli.db.backup.error.exists_fmt"), finalPath), map[string]any{"path": finalPath})
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return contract.DatabaseBackupResult{}, fmt.Errorf("backup --out stat: %w", statErr)
		}
	}
	snapshot := sqlite.SnapshotDatabase
	if input.Force {
		snapshot = sqlite.SnapshotDatabaseReplace
	}
	if err := snapshot(ctx, m.opts.DBPath, finalPath); err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	return contract.DatabaseBackupResult{Path: finalPath}, nil
}

// blockedBackupOutRoot reports the forbidden root containing absClean.
// "/etc/foo" matches "/etc"; "/etcetera" does not.
func blockedBackupOutRoot(absClean string) (string, bool) {
	for _, root := range forbiddenBackupOutRoots {
		if absClean == root || strings.HasPrefix(absClean, root+string(filepath.Separator)) {
			return root, true
		}
	}
	return "", false
}

func (m *Maintenance) ReindexSearch(ctx context.Context, input contract.SearchReindexInput) (contract.SearchReindexResult, error) {
	store, err := m.opts.OpenSearchStore(ctx)
	if err != nil {
		return contract.SearchReindexResult{}, err
	}
	defer func() { _ = store.Close() }()
	if !input.Confirm {
		report, err := store.ReindexSearchConfirmed(ctx, false)
		if err != nil {
			return contract.SearchReindexResult{}, err
		}
		return contract.SearchReindexResult{SearchIndexReindexReport: report, DatabasePath: m.opts.DBPath}, nil
	}

	var warnings []string
	backup, err := m.backupService(&warnings)
	if err != nil {
		return contract.SearchReindexResult{}, err
	}
	var report domain.SearchIndexReindexReport
	operation, leaseErr := app.RunLeasedDestructiveOperation(ctx, backup, func(lease contract.RecoveryLease) app.DestructiveOperationResult {
		createBackup := func(backupCtx context.Context, write func(string) error) (string, error) {
			return lease.WriteSnapshot(backupCtx, write)
		}
		var backupPath string
		var operationErr error
		report, backupPath, operationErr = store.ReindexSearchConfirmedWithBackup(ctx, createBackup, lease.Discard, lease.Validate)
		return app.DestructiveOperationResult{BackupPath: backupPath, MutationCompleted: operationErr == nil, Err: operationErr}
	})
	if !operation.MutationCompleted {
		if operation.Err != nil {
			return contract.SearchReindexResult{}, fmt.Errorf("verified backup and search reindex: %w", errors.Join(operation.Err, leaseErr))
		}
		return contract.SearchReindexResult{}, fmt.Errorf("acquire reindex backup lease: %w", leaseErr)
	}
	result := contract.SearchReindexResult{
		SearchIndexReindexReport: report,
		DatabasePath:             m.opts.DBPath,
		BackupPath:               operation.BackupPath,
		PruneWarnings:            warnings,
	}
	if leaseErr != nil {
		result.LeaseReleaseWarning = leaseErr.Error()
	}
	return result, nil
}

func (m *Maintenance) DeleteProject(ctx context.Context, input contract.ProjectDeleteInput) (contract.ProjectDeleteResult, error) {
	if m.opts.Projects == nil {
		return contract.ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project delete requires a project store", nil)
	}
	var warnings []string
	backup, err := m.backupService(&warnings)
	if err != nil {
		return contract.ProjectDeleteResult{}, err
	}
	var audit bytes.Buffer
	svc := app.NewProjectService(m.opts.Projects, backup, m.opts.Events).SetAuditWarnWriter(&audit)
	result, err := svc.Delete(ctx, input.ProjectID, input.Counters)
	result.Audit = strings.TrimSpace(audit.String())
	result.PruneWarnings = warnings
	return result, err
}

func (m *Maintenance) backupService(warnings *[]string) (*recovery.BackupService, error) {
	dir, err := paths.BackupDir()
	if err != nil {
		return nil, fmt.Errorf("resolve backup dir: %w", err)
	}
	return recovery.NewBackupService(recovery.BackupOptions{
		SourcePath:     m.opts.DBPath,
		DestDir:        dir,
		Retention:      m.opts.Retention,
		SnapshotWriter: sqlite.SnapshotDatabase,
		PruneWarn: func(pruneErr error) {
			*warnings = append(*warnings, pruneErr.Error())
		},
	}), nil
}
