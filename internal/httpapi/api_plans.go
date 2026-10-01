package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// PlanOperations reads and changes plans, their waves, and their tasks.
type PlanOperations interface {
	ListPlans(ctx context.Context, input contract.ListPlansInput) (contract.ListPlansResponse, error)
	ShowPlan(ctx context.Context, input contract.ShowPlanInput) (contract.ShowPlanResponse, error)
}

func (s *Server) planRoutes() []route {
	return []route{
		query("listPlans", http.MethodGet, projectPath+"/plans", "plan.list", "Plans of a project.", []param{projectParam}, s.listPlans),
		query("getPlan", http.MethodGet, projectPath+"/plans/{plan}", "plan.show", "A plan with its waves and tasks.", []param{
			projectParam,
			pathParam("plan", "Plan slug.", stringSchema),
		}, s.showPlan),
	}
}

func (s *Server) listPlans(r *http.Request) (contract.ListPlansResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListPlansResponse{}, err
	}
	return ops.ListPlans(r.Context(), contract.ListPlansInput{ProjectSelector: selector})
}

func (s *Server) showPlan(r *http.Request) (contract.ShowPlanResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowPlanResponse{}, err
	}
	return ops.ShowPlan(r.Context(), contract.ShowPlanInput{ProjectSelector: selector, Slug: r.PathValue("plan")})
}
