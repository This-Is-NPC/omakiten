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
	CreatePlan(ctx context.Context, input contract.CreatePlanInput) (contract.CreatePlanResponse, error)
	EditPlan(ctx context.Context, input contract.EditPlanInput) (contract.EditPlanResponse, error)
}

// CreatePlanBody is the createPlan request body.
type CreatePlanBody struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	GoalBody string `json:"goal_body,omitempty"`
}

// EditPlanBody is the editPlan request body; absent fields stay unchanged.
type EditPlanBody struct {
	Name     *string `json:"name,omitempty"`
	Slug     *string `json:"slug,omitempty"`
	Status   *string `json:"status,omitempty"`
	GoalBody *string `json:"goal_body,omitempty"`
}

func (s *Server) planRoutes() []route {
	return []route{
		query("listPlans", http.MethodGet, projectPath+"/plans", "plan.list", "Plans of a project.", []param{projectParam}, s.listPlans),
		command("createPlan", http.MethodPost, projectPath+"/plans", "plan.create", "Create a plan.", []param{projectParam}, s.createPlan),
		query("getPlan", http.MethodGet, planPath, "plan.show", "A plan with its waves and tasks.", []param{projectParam, planParam}, s.showPlan),
		command("editPlan", http.MethodPatch, planPath, "plan.edit", "Edit plan name, slug, status, or goal.", []param{projectParam, planParam}, s.editPlan),
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

func (s *Server) createPlan(r *http.Request, body CreatePlanBody) (contract.CreatePlanResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.CreatePlanResponse{}, err
	}
	return ops.CreatePlan(r.Context(), contract.CreatePlanInput{
		ProjectSelector: selector,
		Slug:            body.Slug,
		Name:            body.Name,
		GoalBody:        body.GoalBody,
	})
}

func (s *Server) editPlan(r *http.Request, body EditPlanBody) (contract.EditPlanResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.EditPlanResponse{}, err
	}
	return ops.EditPlan(r.Context(), contract.EditPlanInput{
		ProjectSelector: selector,
		Slug:            r.PathValue("plan"),
		Name:            body.Name,
		NewSlug:         body.Slug,
		Status:          body.Status,
		GoalBody:        body.GoalBody,
	})
}
