package app

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// OrphanRepository exposes the read + rebind primitives the OrphanService
// composes. PreviewOrphanedTasks is pure read; RebindOrphanedTasks applies
// the rebind and emits task.migrated events. Both accept the current and
// previous BucketResolver views (previous may be nil on the first import)
// so the adapter never imports config.
type OrphanRepository interface {
	PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
}

type OrphanService struct {
	repo     OrphanRepository
	current  domain.BucketResolver
	previous domain.BucketResolver
}

// NewOrphanService wires the orphan service with the current and previous
// per-project bucket resolvers. The previous resolver may be nil when
// the runtime has only seen one bundle for the project — the
// implementation degrades to "no orphans" in that case.
func NewOrphanService(repo OrphanRepository, current, previous domain.BucketResolver) *OrphanService {
	return &OrphanService{repo: repo, current: current, previous: previous}
}

// Preview returns the orphan report for the given project without mutating.
func (s *OrphanService) Preview(ctx context.Context, project domain.ProjectContext) (report domain.OrphanReport, err error) {
	finish := activity.Track(ctx, "app.OrphanService.Preview", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	report, err = s.repo.PreviewOrphanedTasks(ctx, project.ID, s.current, s.previous)
	return
}

// Migrate applies the rebind for every orphan task and returns the report
// describing what was migrated.
func (s *OrphanService) Migrate(ctx context.Context, project domain.ProjectContext) (report domain.OrphanReport, err error) {
	finish := activity.Track(ctx, "app.OrphanService.Migrate", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	report, err = s.repo.RebindOrphanedTasks(ctx, project.ID, s.current, s.previous)
	return
}
