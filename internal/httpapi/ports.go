package httpapi

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// Operations is the operation facade the adapter drives, pinned to the
// HTTP surface by the composition root.
type Operations interface {
	ProjectOperations
	TaskOperations
	CommentOperations
	DependencyOperations
	PlanOperations
	WorkDocumentOperations
	InsightOperations
	MaintenanceOperations
	RecordOperations
	TagOperations
}

// Runtimes resolves the operation facade for each request.
type Runtimes interface {
	// Project returns the facade and selector of the project with slug.
	Project(ctx context.Context, slug string) (Operations, contract.ProjectSelector, error)
	// Global returns the facade for project-independent operations.
	Global(ctx context.Context) (Operations, error)
	// Knowledge reads the file-backed knowledge of project slug and of the
	// projects it names as related.
	Knowledge(ctx context.Context, slug string) (domain.KnowledgeSnapshot, error)
	// Snapshot returns the configuration project slug runs with.
	Snapshot(ctx context.Context, slug string) (*config.Snapshot, error)
	// Catalog returns the catalog for the GUI language preference.
	Catalog() *config.Catalog
}

// EventLog replays committed events for SSE resumption.
type EventLog interface {
	// EventsAfter returns up to limit visible rows with id > afterID,
	// oldest first.
	EventsAfter(ctx context.Context, afterID int64, limit int) ([]contract.LogsRow, error)
}
