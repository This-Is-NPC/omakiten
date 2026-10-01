package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// RecordOperations records errors, their solutions, and task progress.
type RecordOperations interface {
	RecordError(ctx context.Context, input contract.RecordErrorInput) (contract.ErrorRecordResponse, error)
	AddSolution(ctx context.Context, input contract.AddSolutionInput) (contract.SolutionResponse, error)
	ConfirmSolution(ctx context.Context, input contract.ConfirmSolutionInput) (contract.SolutionResponse, error)
	ListTopSolutions(ctx context.Context, input contract.ListTopSolutionsInput) (contract.TopSolutionsResponse, error)
}

// ErrorBody is the recordError request body.
type ErrorBody struct {
	Description string   `json:"description"`
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// SolutionBody is the addSolution request body.
type SolutionBody struct {
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	// TaskID links the solution to the task that applied it.
	TaskID int64 `json:"task_id,omitempty"`
}

// ConfirmationBody reports whether applying a solution worked.
type ConfirmationBody struct {
	Success bool `json:"success"`
}

func (s *Server) recordRoutes() []route {
	return []route{
		command("recordError", http.MethodPost, projectPath+"/errors", "error.record", "Record an error met while working.", []param{projectParam}, s.recordError),
		command("addSolution", http.MethodPost, projectPath+"/errors/{error}/solutions", "solution.add", "Add a solution to a recorded error.", []param{
			projectParam,
			pathParam("error", "Error id.", idSchema),
		}, s.addSolution),
		command("confirmSolution", http.MethodPost, projectPath+"/solutions/{solution}/confirmations", "solution.confirm", "Report whether a solution worked.", []param{
			projectParam,
			pathParam("solution", "Solution id.", idSchema),
		}, s.confirmSolution),
		query("listTopSolutions", http.MethodGet, projectPath+"/solutions", "solution.list_top", "Best-ranked solutions of a project.", []param{
			projectParam,
			queryParam("limit", "Row cap; the configured default applies when absent.", intSchema),
		}, s.listTopSolutions),
	}
}

func (s *Server) recordError(r *http.Request, body ErrorBody) (contract.ErrorRecordResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ErrorRecordResponse{}, err
	}
	return ops.RecordError(r.Context(), contract.RecordErrorInput{ProjectSelector: selector, Description: body.Description, Context: body.Context, Tags: body.Tags})
}

func (s *Server) addSolution(r *http.Request, body SolutionBody) (contract.SolutionResponse, error) {
	errorID, err := parseID(r.PathValue("error"), "error")
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	return ops.AddSolution(r.Context(), contract.AddSolutionInput{ProjectSelector: selector, ErrorID: errorID, Description: body.Description, Steps: body.Steps, TaskID: body.TaskID})
}

func (s *Server) confirmSolution(r *http.Request, body ConfirmationBody) (contract.SolutionResponse, error) {
	solutionID, err := parseID(r.PathValue("solution"), "solution")
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	return ops.ConfirmSolution(r.Context(), contract.ConfirmSolutionInput{ProjectSelector: selector, SolutionID: solutionID, Success: body.Success})
}

func (s *Server) listTopSolutions(r *http.Request) (contract.TopSolutionsResponse, error) {
	limit, err := optionalInt(r, "limit")
	if err != nil {
		return contract.TopSolutionsResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.TopSolutionsResponse{}, err
	}
	return ops.ListTopSolutions(r.Context(), contract.ListTopSolutionsInput{ProjectSelector: selector, Limit: limit})
}
