package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type ProjectService struct {
	repo         ProjectRepository
	backup       BackupRunner
	events       EventRecorder
	checkpointer Checkpointer
	// auditWarn receives warnings about audit-trail emission failures
	// (json.Marshal or RecordEntityEvent errors that happen AFTER the
	// destructive transaction committed). Defaults to os.Stderr so the
	// audit gap stays visible to operators; tests inject io.Discard to
	// keep output deterministic.
	auditWarn io.Writer
}

// NewProjectService constructs the service against a ProjectRepository
// plus the optional collaborators consumed by destructive flows.
// `backup` and `events` may be nil — Init does not touch either, so
// the CLI bootstrap that only calls Init wires the constructor with
// (repo, nil, nil) and the bundled tests follow the same shape.
// Delete returns an error when called with backup=nil so the
// invariant "every destructive flow writes a snapshot first" is
// enforced at the API boundary rather than in the caller's wiring.
func NewProjectService(repo ProjectRepository, backup BackupRunner, events EventRecorder) *ProjectService {
	return &ProjectService{repo: repo, backup: backup, events: events, auditWarn: os.Stderr}
}

// SetAuditWarnWriter overrides the writer that receives post-commit
// audit-trail emission warnings. Production wiring keeps the os.Stderr
// default so operators see audit gaps; tests pass io.Discard to keep
// the test runner clean. Returns the service for fluent wiring.
func (s *ProjectService) SetAuditWarnWriter(w io.Writer) *ProjectService {
	if w == nil {
		w = io.Discard
	}
	s.auditWarn = w
	return s
}

// WithCheckpointer attaches the legacy pre-snapshot checkpoint used by callers
// whose BackupService retains the generic file-copy writer. SQLite-aware CLI
// composition injects an online snapshot writer and does not depend on this
// checkpoint for WAL consistency. Returns the service for fluent wiring.
func (s *ProjectService) WithCheckpointer(c Checkpointer) *ProjectService {
	s.checkpointer = c
	return s
}

func (s *ProjectService) Init(ctx context.Context, name, slug, rootPath string) (project domain.Project, err error) {
	finish := activity.Track(ctx, "app.ProjectService.Init", domain.ProjectContext{}, map[string]any{"slug": slug, "root": rootPath})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(absRoot)
	}

	slug = normalizeSlug(slug)
	if slug == "" {
		slug = normalizeSlug(name)
	}
	if slug == "" {
		err = domain.NewError(domain.ErrValidation, "project slug is required", nil)
		return
	}

	project, err = s.repo.UpsertProject(ctx, name, slug, absRoot)
	return
}

// ProjectDeleteResult is the success payload Delete returns. counters
// is the pre-delete snapshot rendered to the user; backup_path is the
// snapshot the backup pass wrote (always populated on success); event
// is the project.removed row that landed in the audit log after the
// commit.
type ProjectDeleteResult struct {
	Project    domain.Project               `json:"project"`
	Counters   domain.ProjectDeleteCounters `json:"counters"`
	BackupPath string                       `json:"backup_path"`
	EventType  string                       `json:"event_type"`
}

// Delete hard-deletes a project after writing a recovery snapshot. Production
// SQLite composition selects AtomicProjectDeleteRepository + BackupLeaser: one
// cross-process directory lease spans an exact-generation, connection-bound
// snapshot, BEGIN IMMEDIATE cascade, commit, and rooted retention pass. Fakes
// and non-SQLite repositories retain the legacy sequence:
//
//  1. Resolve the project (load slug/name for the payload + error
//     reporting).
//  2. Checkpoint the live WAL (best-effort, when a legacy Checkpointer was
//     attached). SQLite-aware snapshot writers include committed WAL frames
//     independently. Checkpoint failure is logged via auditWarn.
//  3. Run BackupService — backup failure aborts before any rows are
//     touched. The user retries once the underlying issue is fixed.
//  4. Cascade-delete via the repository (events for the project come
//     out in the same transaction; FK CASCADE handles every other
//     dependent row).
//  5. Emit project.removed with the caller-provided counters
//     snapshot + slug/name/backup_path.
//
// counters is the pre-delete row-count snapshot the caller resolved
// to render the prompt/overlay. Threading it through here removes the
// duplicate ProjectDeleteCounts round-trip the destructive flow used
// to issue (once for the prompt, once for the audit payload). Pass
// the zero-value when no prompt was rendered — callers that only
// need the side effect can fetch counters via ProjectDeleteCounts and
// pass them in.
//
// Returns ErrValidation when the service was constructed without a
// BackupRunner — the destructive flow refuses to run without the
// safety net.
func (s *ProjectService) Delete(ctx context.Context, projectID int64, counters domain.ProjectDeleteCounters) (ProjectDeleteResult, error) {
	if s.backup == nil {
		return ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project delete requires a BackupRunner (composition root must inject one)", nil)
	}
	project, err := s.repo.FindProjectByID(ctx, projectID)
	if err != nil {
		return ProjectDeleteResult{}, err
	}

	if atomicRepo, ok := s.repo.(AtomicProjectDeleteRepository); ok {
		if backupLeaser, ok := s.backup.(BackupLeaser); ok {
			backupPath, err := s.deleteAtomic(ctx, atomicRepo, backupLeaser, projectID)
			if err != nil {
				return ProjectDeleteResult{}, err
			}
			if s.events != nil {
				s.recordProjectRemoved(ctx, project, counters, backupPath)
			}
			return ProjectDeleteResult{
				Project:    project,
				Counters:   counters,
				BackupPath: backupPath,
				EventType:  domain.EventTypeProjectRemoved,
			}, nil
		}
	}

	// Retain the legacy checkpoint ordering for generic file-copy writers.
	// SQLite-aware writers remain consistent when this best-effort checkpoint
	// is busy because they read the database and WAL through SQLite itself.
	if s.checkpointer != nil {
		if cerr := s.checkpointer.Checkpoint(ctx); cerr != nil {
			fmt.Fprintf(s.auditWarn, "warning: wal_checkpoint before backup failed for project_id=%d: %s\n", projectID, cerr.Error())
		}
	}

	backupPath, err := s.backup.Run(ctx)
	if err != nil {
		return ProjectDeleteResult{}, fmt.Errorf("backup before delete: %w", err)
	}

	if err := s.repo.DeleteProject(ctx, projectID); err != nil {
		return ProjectDeleteResult{}, err
	}

	if s.events != nil {
		s.recordProjectRemoved(ctx, project, counters, backupPath)
	}

	return ProjectDeleteResult{
		Project:    project,
		Counters:   counters,
		BackupPath: backupPath,
		EventType:  domain.EventTypeProjectRemoved,
	}, nil
}

func (s *ProjectService) deleteAtomic(
	ctx context.Context,
	repo AtomicProjectDeleteRepository,
	backup BackupLeaser,
	projectID int64,
) (string, error) {
	operation, leaseErr := RunLeasedDestructiveOperation(ctx, backup, func(lease RecoveryLease) DestructiveOperationResult {
		backupPath, operationErr := repo.DeleteProjectWithBackup(
			ctx,
			projectID,
			lease.WriteSnapshot,
			lease.Discard,
			lease.Validate,
		)
		return DestructiveOperationResult{
			BackupPath:        backupPath,
			MutationCompleted: operationErr == nil,
			Err:               operationErr,
		}
	})
	if !operation.MutationCompleted {
		if operation.Err != nil {
			return operation.BackupPath, fmt.Errorf("atomic backup and project delete: %w", errors.Join(operation.Err, leaseErr))
		}
		return "", fmt.Errorf("acquire project-delete backup lease: %w", leaseErr)
	}
	if leaseErr != nil {
		// The delete committed before lease release failed. Reporting the
		// operation as failed would invite an unsafe retry, so preserve success
		// and surface the release discrepancy through the existing audit channel.
		fmt.Fprintf(s.auditWarn, "warning: backup lease release failed after project delete committed for project_id=%d: %s\n", projectID, leaseErr.Error())
	}
	return operation.BackupPath, nil
}

// recordProjectRemoved marshals the project.removed payload and writes
// the audit row. Failures at either step (json.Marshal returning an
// error, or RecordEntityEvent rejecting the row) are surfaced on
// s.auditWarn so operators see the audit gap — the destructive
// transaction already committed, so the rollback ship has sailed; the
// only remediation is logging the discrepancy so reconciliation work
// can backfill the event manually.
func (s *ProjectService) recordProjectRemoved(ctx context.Context, project domain.Project, counters domain.ProjectDeleteCounters, backupPath string) {
	payload, marshalErr := json.Marshal(map[string]any{
		"slug":        project.Slug,
		"name":        project.Name,
		"counters":    counters,
		"backup_path": backupPath,
	})
	if marshalErr != nil {
		fmt.Fprintf(s.auditWarn, "warning: project.removed payload marshal failed for project_id=%d slug=%q: %s\n", project.ID, project.Slug, marshalErr.Error())
		return
	}
	if err := s.events.RecordEntityEvent(ctx, domain.EventEntityProject, project.ID, 0, domain.EventTypeProjectRemoved, string(payload)); err != nil {
		fmt.Fprintf(s.auditWarn, "warning: project.removed audit emission failed for project_id=%d slug=%q: %s\n", project.ID, project.Slug, err.Error())
	}
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
