package app

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// OrphanRepository exposes the read + rebind primitives the OrphanService
// composes. PreviewOrphanedTasks is pure read; RebindOrphanedTasks applies
// the rebind and emits task.migrated events.
type OrphanRepository interface {
	PreviewOrphanedTasks(ctx context.Context, projectID int64) (domain.OrphanReport, error)
	RebindOrphanedTasks(ctx context.Context, projectID int64) (domain.OrphanReport, error)
}

type OrphanService struct {
	repo OrphanRepository
}

func NewOrphanService(repo OrphanRepository) *OrphanService {
	return &OrphanService{repo: repo}
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

	report, err = s.repo.PreviewOrphanedTasks(ctx, project.ID)
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

	report, err = s.repo.RebindOrphanedTasks(ctx, project.ID)
	return
}
