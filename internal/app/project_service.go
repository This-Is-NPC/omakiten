package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type ProjectService struct {
	repo   ProjectRepository
	backup BackupRunner
	events EventRecorder
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
	return &ProjectService{repo: repo, backup: backup, events: events}
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
//  2. Collect counters for the audit payload + the caller's prompt.
//  3. Run BackupService — backup failure aborts before any rows are
//     touched. The user retries once the underlying issue is fixed.
//  4. Cascade-delete via the repository (events for the project come
//     out in the same transaction; FK CASCADE handles every other
//     dependent row).
//  5. Emit project.removed with slug/name/counters/backup_path.
//
// Returns ErrValidation when the service was constructed without a
// BackupRunner — the destructive flow refuses to run without the
// safety net.
func (s *ProjectService) Delete(ctx context.Context, projectID int64) (ProjectDeleteResult, error) {
	if s.backup == nil {
		return ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project delete requires a BackupRunner (composition root must inject one)", nil)
	}
	project, err := s.repo.FindProjectByID(ctx, projectID)
	if err != nil {
		return ProjectDeleteResult{}, err
	}
	counters, err := s.repo.ProjectDeleteCounts(ctx, projectID)
	if err != nil {
		return ProjectDeleteResult{}, fmt.Errorf("resolve project counters: %w", err)
	}

	backupPath, err := s.backup.Run(ctx)
	if err != nil {
		return ProjectDeleteResult{}, fmt.Errorf("backup before delete: %w", err)
	}

	if err := s.repo.DeleteProject(ctx, projectID); err != nil {
		return ProjectDeleteResult{}, err
	}

	if s.events != nil {
		payload, marshalErr := json.Marshal(map[string]any{
			"slug":        project.Slug,
			"name":        project.Name,
			"counters":    counters,
			"backup_path": backupPath,
		})
		if marshalErr == nil {
			_ = s.events.RecordEntityEvent(ctx, domain.EventEntityProject, project.ID, 0, domain.EventTypeProjectRemoved, string(payload))
		}
	}

	return ProjectDeleteResult{
		Project:    project,
		Counters:   counters,
		BackupPath: backupPath,
		EventType:  domain.EventTypeProjectRemoved,
	}, nil
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
