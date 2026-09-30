package agentruntime

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// InitProject registers a project row. Sysadmin — not an agent product tool.
func InitProject(ctx context.Context, repo app.ProjectRepository, name, slug, rootPath string) (domain.Project, error) {
	return app.NewProjectService(repo, nil, nil).Init(ctx, name, slug, rootPath)
}

// ProjectStore is the composite sqlite.Store satisfies for project deletion.
type ProjectStore interface {
	app.ProjectRepository
	app.EventRecorder
}

// EventRecorder is the audit hook project deletion uses after a successful delete.
type EventRecorder interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error
}
