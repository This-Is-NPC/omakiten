package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// PlanService wraps the plan persistence layer with input normalisation,
// activity tracking, and (eventually) plan-status transitions. v1 ships
// the create + list + show primitives; add-wave / assign-task / claim-next
// land in subsequent slices.
type PlanService struct {
	repo PlanRepository
}

func NewPlanService(repo PlanRepository) *PlanService {
	return &PlanService{repo: repo}
}

// Create normalises the slug / name pair and delegates to the repo. The
// repo emits plan.created in the same transaction as the insert; this
// wrapper exists so the agent layer (which composes against the app port)
// can grow business rules without touching the sqlite adapter.
func (s *PlanService) Create(ctx context.Context, project domain.ProjectContext, slug, name, goalBody string) (plan domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.Create", project, map[string]any{"slug": slug})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	plan, err = s.repo.CreatePlan(ctx, project.ID, slug, name, goalBody)
	return
}

// List returns every plan for the project, ordered by id ascending.
func (s *PlanService) List(ctx context.Context, project domain.ProjectContext) (plans []domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.List", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	plans, err = s.repo.ListPlans(ctx, project.ID)
	return
}

// GetBySlug resolves a plan by its slug, returning ErrPlanNotFound when
// the slug does not exist in the active project.
func (s *PlanService) GetBySlug(ctx context.Context, project domain.ProjectContext, slug string) (domain.Plan, error) {
	return s.repo.GetPlanBySlug(ctx, project.ID, strings.TrimSpace(slug))
}
