package app

import (
	"context"
	"encoding/json"
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

// WithCheckpointer attaches a Checkpointer the service invokes
// immediately before BackupService.Run, so the on-disk .db copy
// reflects every committed WAL frame from this process. Returns the
// service for fluent wiring. Pass nil from callers that have no live
// store handle (e.g. the standalone CLI flows that only compose
// BackupService directly).
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
	Project    domain.Project              `json:"project"`
	Counters   domain.ProjectDeleteCounters `json:"counters"`
	BackupPath string                      `json:"backup_path"`
	EventType  string                      `json:"event_type"`
}

// Delete hard-deletes a project after writing a recovery snapshot.
// Sequence:
//
//  1. Resolve the project (load slug/name for the payload + error
//     reporting).
//  2. Checkpoint the live WAL (best-effort, when a Checkpointer was
//     attached) so committed frames from this process land in the
//     main .db file the snapshot copies. Checkpoint failure is logged
//     via auditWarn and the flow continues.
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

	// Checkpoint the WAL before the snapshot so every committed
	// transaction from this process lands in the main .db file the
	// BackupService will copy. Best-effort — a checkpoint failure
	// (typically SQLITE_BUSY under concurrent writers) is logged and
	// the snapshot continues; the file copy still reflects the
	// on-disk DB+WAL pair at that instant.
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
