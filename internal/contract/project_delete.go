package contract

import (
	"context"

	"omakiten/internal/domain"
)

// ProjectDeleteInput names the project to delete and the counters the caller
// confirmed; the counters populate the audit event.
type ProjectDeleteInput struct {
	ProjectID int64                        `json:"project_id"`
	Counters  domain.ProjectDeleteCounters `json:"counters"`
}

// ProjectDeleteResult is the success payload DeleteProject returns.
type ProjectDeleteResult struct {
	Project       domain.Project               `json:"project"`
	Counters      domain.ProjectDeleteCounters `json:"counters"`
	BackupPath    string                       `json:"backup_path"`
	EventType     string                       `json:"event_type"`
	PruneWarnings []string                     `json:"prune_warnings,omitempty"`
	Audit         string                       `json:"-"`
}

type ProjectDeleter func(context.Context, ProjectDeleteInput) (ProjectDeleteResult, error)
