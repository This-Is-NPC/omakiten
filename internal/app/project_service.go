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
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

type ProjectService struct {
	repo   ProjectRepository
	backup contract.BackupLeaser
	events EventRecorder
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
func NewProjectService(repo ProjectRepository, backup contract.BackupLeaser, events EventRecorder) *ProjectService {
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

	slug = domain.Slugify(slug)
	if slug == "" {
		slug = domain.Slugify(name)
	}
	if slug == "" {
		err = domain.NewError(domain.ErrValidation, "project slug is required", nil)
		return
	}

	project, err = s.repo.UpsertProject(ctx, name, slug, absRoot)
	return
}

// Delete requires atomic repository deletion under a backup-directory lease.
// counters is the snapshot used to confirm deletion and populate its audit event.
func (s *ProjectService) Delete(ctx context.Context, projectID int64, counters domain.ProjectDeleteCounters) (contract.ProjectDeleteResult, error) {
	if s.backup == nil {
		return contract.ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project delete requires a backup lease", nil)
	}
	project, err := s.repo.FindProjectByID(ctx, projectID)
	if err != nil {
		return contract.ProjectDeleteResult{}, err
	}

	repo, ok := s.repo.(AtomicProjectDeleteRepository)
	if !ok {
		return contract.ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project store does not support atomic deletion", nil)
	}
	return s.deleteAtomicProject(ctx, project, counters, projectID, repo, s.backup)
}

func (s *ProjectService) deleteAtomicProject(
	ctx context.Context,
	project domain.Project,
	counters domain.ProjectDeleteCounters,
	projectID int64,
	repo AtomicProjectDeleteRepository,
	backup contract.BackupLeaser,
) (contract.ProjectDeleteResult, error) {
	backupPath, err := s.deleteAtomic(ctx, repo, backup, projectID)
	if err != nil {
		return contract.ProjectDeleteResult{}, err
	}
	if s.events != nil {
		s.recordProjectRemoved(ctx, project, counters, backupPath)
	}
	return contract.ProjectDeleteResult{
		Project:    project,
		Counters:   counters,
		BackupPath: backupPath,
		EventType:  domain.EventTypeProjectRemoved,
	}, nil
}

func (s *ProjectService) deleteAtomic(
	ctx context.Context,
	repo AtomicProjectDeleteRepository,
	backup contract.BackupLeaser,
	projectID int64,
) (string, error) {
	operation, leaseErr := RunLeasedDestructiveOperation(ctx, backup, func(lease contract.RecoveryLease) DestructiveOperationResult {
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
