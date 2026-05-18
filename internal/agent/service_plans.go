package agent

import (
	"context"

	"omakiten/internal/app"
)

// newPlanService composes the per-project app.PlanService. Mirrors the
// other newXService helpers; the agent layer never holds a long-lived
// reference because the underlying repo is the only state.
func (s *Service) newPlanService() *app.PlanService {
	return app.NewPlanService(s.repo)
}

// CreatePlan creates a plan in the resolved project, returning the wire
// projection. Validation lives in app.PlanService → sqlite.Store; this
// layer only resolves the project and projects the response.
func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (CreatePlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	plan, err := s.newPlanService().Create(ctx, project, input.Slug, input.Name, input.GoalBody)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	return CreatePlanResponse{
		Project: projectSummary(project),
		Plan:    planSummary(plan),
	}, nil
}

// ListPlans returns every plan in the resolved project, oldest first.
// GoalBody is stripped from each entry to keep payloads compact — the
// goal body is full markdown and unbounded by design.
func (s *Service) ListPlans(ctx context.Context, input ListPlansInput) (ListPlansResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListPlansResponse{}, err
	}
	plans, err := s.newPlanService().List(ctx, project)
	if err != nil {
		return ListPlansResponse{}, err
	}
	summaries := make([]PlanSummary, 0, len(plans))
	for _, p := range plans {
		s := planSummary(p)
		s.GoalBody = ""
		summaries = append(summaries, s)
	}
	return ListPlansResponse{
		Project: projectSummary(project),
		Plans:   summaries,
	}, nil
}
