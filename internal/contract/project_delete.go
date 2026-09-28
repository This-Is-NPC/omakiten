package contract

import (
	"context"

	"omakiten/internal/domain"
)

// ProjectDeleteResult is the success payload DeleteProject returns.
type ProjectDeleteResult struct {
	Project    domain.Project               `json:"project"`
	Counters   domain.ProjectDeleteCounters `json:"counters"`
	BackupPath string                       `json:"backup_path"`
	EventType  string                       `json:"event_type"`
	Audit      string                       `json:"-"`
}

type ProjectDeleter func(context.Context, int64, domain.ProjectDeleteCounters, func(error)) (ProjectDeleteResult, error)
