package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
)

func (s *Service) RecordError(ctx context.Context, input contract.RecordErrorInput) (contract.ErrorRecordResponse, error) {
	if err := s.allow("error.record"); err != nil {
		return contract.ErrorRecordResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ErrorRecordResponse{}, err
	}
	record, err := s.newErrorService().Record(ctx, project, input.Description, input.Context, input.Tags)
	if err != nil {
		return contract.ErrorRecordResponse{}, err
	}
	return contract.ErrorRecordResponse{Project: projectSummary(project), Error: errorSummary(record)}, nil
}

func (s *Service) AddSolution(ctx context.Context, input contract.AddSolutionInput) (contract.SolutionResponse, error) {
	if err := s.allow("solution.add"); err != nil {
		return contract.SolutionResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	var taskID *int64
	if input.TaskID > 0 {
		v := input.TaskID
		taskID = &v
	}
	solution, err := s.newErrorService().AddSolution(ctx, project, input.ErrorID, input.Description, input.Steps, taskID)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	return contract.SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ConfirmSolution(ctx context.Context, input contract.ConfirmSolutionInput) (contract.SolutionResponse, error) {
	if err := s.allow("solution.confirm"); err != nil {
		return contract.SolutionResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	solution, err := s.newErrorService().ConfirmSolution(ctx, project, input.SolutionID, input.Success)
	if err != nil {
		return contract.SolutionResponse{}, err
	}
	return contract.SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ListTopSolutions(ctx context.Context, input contract.ListTopSolutionsInput) (contract.TopSolutionsResponse, error) {
	if err := s.allow("solution.list_top"); err != nil {
		return contract.TopSolutionsResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.TopSolutionsResponse{}, err
	}
	es := s.newErrorService()
	es.SetSolutionsDefaults(app.SolutionsDefaults{
		TopLimitDefault: s.settings.SolutionsTopLimitDefault,
		TopLimitMax:     s.settings.SolutionsTopLimitMax,
	})
	solutions, err := es.ListTopSolutions(ctx, project, input.Limit)
	if err != nil {
		return contract.TopSolutionsResponse{}, err
	}
	out := make([]contract.SolutionSummary, 0, len(solutions))
	for _, sol := range solutions {
		out = append(out, solutionSummary(sol))
	}
	return contract.TopSolutionsResponse{Project: projectSummary(project), Solutions: out}, nil
}
