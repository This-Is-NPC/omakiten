package tui

import (
	"context"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// Local ports so Repositories does not name internal/app types (D1 / D20).
// *sqlite.Store and contract.BundleEditor satisfy these structurally.

type TaskStore interface {
	ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error)
	GetTaskByID(ctx context.Context, projectID, id int64, buckets domain.BucketResolver) (domain.Task, error)
	CountDescendants(ctx context.Context, projectID, parentID int64) (int, error)
	IsDescendantOf(ctx context.Context, projectID, candidateID, ancestorID int64) (bool, error)
}

type ProjectStore interface {
	FindProjectByID(ctx context.Context, id int64) (domain.Project, error)
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error)
}

type CommentStore interface {
	CommentByID(ctx context.Context, projectID, commentID int64) (domain.Comment, error)
}

type TagStore interface {
	ListProjectTags(ctx context.Context, projectID int64) ([]domain.Tag, error)
	ListTaskTags(ctx context.Context, projectID, taskID int64) ([]domain.Tag, error)
	DeleteOrphanTags(ctx context.Context) (int64, error)
}

type EventStore interface {
	ListTaskActivity(ctx context.Context, projectID, taskID int64, order string) ([]domain.Event, error)
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, body string) error
	ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.EventRow, error)
	EventCategoryCounts(ctx context.Context, projectID int64, since time.Time) (map[domain.EventCategory]int, error)
}

type PlanStore interface {
	PeekNextClaimable(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.PlanTaskRow, bool, error)
}

type OrphanStore interface {
	PreviewOrphanedCascade(ctx context.Context, projectID int64, plan domain.OrphanCascadePlan) (domain.OrphanReport, error)
	PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
}

type BundleLoader interface {
	LoadBundle(path string) (config.Bundle, error)
}

type DataVersionReader interface {
	DataVersion(ctx context.Context) (int64, error)
}

type MetricsPort interface {
	Summary(ctx context.Context, project domain.ProjectContext, period string, projectID int64) (domain.MetricsSummary, error)
}

type InsightsPort interface {
	Today(ctx context.Context, project domain.ProjectContext, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error)
}

type SearchPort interface {
	Search(ctx context.Context, project domain.ProjectContext, query string, entityTypes []string) ([]domain.SearchHit, error)
}

// RuntimeCache supplies project views and transactional reloads.
type RuntimeCache interface {
	View(projectID int64) *contract.RuntimeView
	ResolveView(context.Context, int64, string) (*contract.RuntimeView, error)
	ReloadView(context.Context, int64, string) (*contract.RuntimeView, error)
	ApplyView(context.Context, int64, string, func(*contract.RuntimeView) (func() error, error)) (*contract.RuntimeView, error)
	ResolveApplyView(context.Context, int64, string, func(*contract.RuntimeView) (func() error, error)) (*contract.RuntimeView, bool, error)
	SetProjectSelector(contract.ProjectSelector)
}
