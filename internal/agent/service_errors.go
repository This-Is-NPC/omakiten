package agent

import (
	"context"

	"omakiten/internal/app"
)

func (s *Service) RecordError(ctx context.Context, input RecordErrorInput) (ErrorRecordResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ErrorRecordResponse{}, err
	}
	record, err := app.NewErrorService(s.repo).Record(ctx, project, input.Description, input.Context, input.Tags)
	if err != nil {
		return ErrorRecordResponse{}, err
	}
	return ErrorRecordResponse{Project: projectSummary(project), Error: errorSummary(record)}, nil
}

func (s *Service) SearchErrors(ctx context.Context, input SearchErrorsInput) (SearchErrorsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SearchErrorsResponse{}, err
	}
	records, err := app.NewErrorService(s.repo).Search(ctx, project, input.Query, input.Tags)
	if err != nil {
		return SearchErrorsResponse{}, err
	}
	out := make([]ErrorSummary, 0, len(records))
	for _, r := range records {
		out = append(out, errorSummary(r))
	}
	return SearchErrorsResponse{Project: projectSummary(project), Errors: out}, nil
}

func (s *Service) AddSolution(ctx context.Context, input AddSolutionInput) (SolutionResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SolutionResponse{}, err
	}
	var taskID *int64
	if input.TaskID > 0 {
		v := input.TaskID
		taskID = &v
	}
	solution, err := app.NewErrorService(s.repo).AddSolution(ctx, project, input.ErrorID, input.Description, input.Steps, taskID)
	if err != nil {
		return SolutionResponse{}, err
	}
	return SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ConfirmSolution(ctx context.Context, input ConfirmSolutionInput) (SolutionResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SolutionResponse{}, err
	}
	solution, err := app.NewErrorService(s.repo).ConfirmSolution(ctx, project, input.SolutionID, input.Success)
	if err != nil {
		return SolutionResponse{}, err
	}
	return SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ListTopSolutions(ctx context.Context, input ListTopSolutionsInput) (TopSolutionsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TopSolutionsResponse{}, err
	}
	solutions, err := app.NewErrorService(s.repo).ListTopSolutions(ctx, project, input.Limit)
	if err != nil {
		return TopSolutionsResponse{}, err
	}
	out := make([]SolutionSummary, 0, len(solutions))
	for _, sol := range solutions {
		out = append(out, solutionSummary(sol))
	}
	return TopSolutionsResponse{Project: projectSummary(project), Solutions: out}, nil
}
