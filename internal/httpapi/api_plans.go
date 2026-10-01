package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

// wavePath addresses a wave by its id alone: wave ids are unique in a
// project and the wave operations take no plan.
const wavePath = projectPath + "/waves/{wave}"

var waveParam = pathParam("wave", "Wave id.", idSchema)

// PlanOperations reads and changes plans, their waves, and their tasks.
type PlanOperations interface {
	ListPlans(ctx context.Context, input contract.ListPlansInput) (contract.ListPlansResponse, error)
	ShowPlan(ctx context.Context, input contract.ShowPlanInput) (contract.ShowPlanResponse, error)
	CreatePlan(ctx context.Context, input contract.CreatePlanInput) (contract.CreatePlanResponse, error)
	EditPlan(ctx context.Context, input contract.EditPlanInput) (contract.EditPlanResponse, error)
	DeletePlan(ctx context.Context, input contract.DeletePlanInput) (contract.DeletePlanResponse, error)
	ContinuePlan(ctx context.Context, input contract.ContinuePlanInput) (contract.ContinuePlanResponse, error)
	AddPlanWave(ctx context.Context, input contract.AddPlanWaveInput) (contract.AddPlanWaveResponse, error)
	RemovePlanWave(ctx context.Context, input contract.RemovePlanWaveInput) (contract.RemovePlanWaveResponse, error)
	RenamePlanWave(ctx context.Context, input contract.RenamePlanWaveInput) (contract.RenamePlanWaveResponse, error)
	ReorderPlanWave(ctx context.Context, input contract.ReorderPlanWaveInput) (contract.ReorderPlanWaveResponse, error)
	AssignPlanTask(ctx context.Context, input contract.AssignPlanTaskInput) (contract.AssignPlanTaskResponse, error)
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

// AddPlanWaveBody is the addPlanWave request body. Position 0 appends;
// a 1-based position inserts there.
type AddPlanWaveBody struct {
	Name     string `json:"name"`
	Position int    `json:"position,omitempty"`
}

// WaveNameBody sets the name of a wave.
type WaveNameBody struct {
	Name string `json:"name"`
}

// WavePositionBody moves a wave to a 1-based position in its plan; an
// occupied position swaps the two waves.
type WavePositionBody struct {
	Position int `json:"position"`
}

// PlanTaskBody names the wave of the plan that a task joins.
type PlanTaskBody struct {
	WaveID int64 `json:"wave_id"`
}

func (s *Server) planRoutes() []route {
	return []route{
		query("listPlans", http.MethodGet, projectPath+"/plans", "plan.list", "Plans of a project.", []param{projectParam}, s.listPlans),
		command("createPlan", http.MethodPost, projectPath+"/plans", "plan.create", "Create a plan.", []param{projectParam}, s.createPlan),
		query("getPlan", http.MethodGet, planPath, "plan.show", "A plan with its waves and tasks.", []param{projectParam, planParam}, s.showPlan),
		command("editPlan", http.MethodPatch, planPath, "plan.edit", "Edit plan name, slug, status, or goal.", []param{projectParam, planParam}, s.editPlan),
		query("deletePlan", http.MethodDelete, planPath, "plan.delete", "Delete a plan and its waves; its tasks stay, detached.", []param{projectParam, planParam, confirmedParam}, s.deletePlan),
		query("getPlanContinuation", http.MethodGet, planPath+"/continuation", "plan.continue", "A plan with a preview of the task a claim would take next.", []param{projectParam, planParam}, s.continuePlan),
		command("addPlanWave", http.MethodPost, planPath+"/waves", "plan.wave.add", "Append or insert a wave into a plan.", []param{projectParam, planParam}, s.addPlanWave),
		query("removePlanWave", http.MethodDelete, wavePath, "plan.wave.remove", "Delete a wave; its tasks stay in the plan, unscheduled.", []param{projectParam, waveParam, confirmedParam}, s.removePlanWave),
		command("renamePlanWave", http.MethodPut, wavePath+"/name", "plan.wave.rename", "Rename a wave.", []param{projectParam, waveParam}, s.renamePlanWave),
		command("reorderPlanWave", http.MethodPut, wavePath+"/position", "plan.wave.reorder", "Move a wave to another position in its plan.", []param{projectParam, waveParam}, s.reorderPlanWave),
		command("assignPlanTask", http.MethodPut, planPath+"/tasks/{task}", "plan.task.assign", "Put a task into a wave of a plan.", []param{projectParam, planParam, taskParam}, s.assignPlanTask),
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

func (s *Server) deletePlan(r *http.Request) (contract.DeletePlanResponse, error) {
	ok, err := confirmed(r)
	if err != nil {
		return contract.DeletePlanResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.DeletePlanResponse{}, err
	}
	return ops.DeletePlan(r.Context(), contract.DeletePlanInput{ProjectSelector: selector, Slug: r.PathValue("plan"), Confirmed: ok})
}

func (s *Server) continuePlan(r *http.Request) (contract.ContinuePlanResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ContinuePlanResponse{}, err
	}
	return ops.ContinuePlan(r.Context(), contract.ContinuePlanInput{ProjectSelector: selector, Slug: r.PathValue("plan")})
}

func (s *Server) addPlanWave(r *http.Request, body AddPlanWaveBody) (contract.AddPlanWaveResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.AddPlanWaveResponse{}, err
	}
	return ops.AddPlanWave(r.Context(), contract.AddPlanWaveInput{
		ProjectSelector: selector,
		Slug:            r.PathValue("plan"),
		Name:            body.Name,
		Position:        body.Position,
	})
}

func (s *Server) wave(r *http.Request) (Operations, contract.ProjectSelector, int64, error) {
	waveID, err := parseID(r.PathValue("wave"), "wave")
	if err != nil {
		return nil, contract.ProjectSelector{}, 0, err
	}
	ops, selector, err := s.project(r)
	return ops, selector, waveID, err
}

func (s *Server) removePlanWave(r *http.Request) (contract.RemovePlanWaveResponse, error) {
	ok, err := confirmed(r)
	if err != nil {
		return contract.RemovePlanWaveResponse{}, err
	}
	ops, selector, waveID, err := s.wave(r)
	if err != nil {
		return contract.RemovePlanWaveResponse{}, err
	}
	return ops.RemovePlanWave(r.Context(), contract.RemovePlanWaveInput{ProjectSelector: selector, WaveID: waveID, Confirmed: ok})
}

func (s *Server) renamePlanWave(r *http.Request, body WaveNameBody) (contract.RenamePlanWaveResponse, error) {
	ops, selector, waveID, err := s.wave(r)
	if err != nil {
		return contract.RenamePlanWaveResponse{}, err
	}
	return ops.RenamePlanWave(r.Context(), contract.RenamePlanWaveInput{ProjectSelector: selector, WaveID: waveID, Name: body.Name})
}

func (s *Server) reorderPlanWave(r *http.Request, body WavePositionBody) (contract.ReorderPlanWaveResponse, error) {
	ops, selector, waveID, err := s.wave(r)
	if err != nil {
		return contract.ReorderPlanWaveResponse{}, err
	}
	return ops.ReorderPlanWave(r.Context(), contract.ReorderPlanWaveInput{ProjectSelector: selector, WaveID: waveID, Position: body.Position})
}

func (s *Server) assignPlanTask(r *http.Request, body PlanTaskBody) (contract.AssignPlanTaskResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.AssignPlanTaskResponse{}, err
	}
	return ops.AssignPlanTask(r.Context(), contract.AssignPlanTaskInput{
		ProjectSelector: selector,
		TaskID:          taskID,
		Slug:            r.PathValue("plan"),
		WaveID:          body.WaveID,
	})
}
