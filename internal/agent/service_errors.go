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
	record, err := s.newErrorService().Record(ctx, project, input.Description, input.Context, input.Tags)
	if err != nil {
		return ErrorRecordResponse{}, err
	}
	return ErrorRecordResponse{Project: projectSummary(project), Error: errorSummary(record)}, nil
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
	solution, err := s.newErrorService().AddSolution(ctx, project, input.ErrorID, input.Description, input.Steps, taskID)
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
	solution, err := s.newErrorService().ConfirmSolution(ctx, project, input.SolutionID, input.Success)
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
	es := s.newErrorService()
	es.SetSolutionsDefaults(app.SolutionsDefaults{
		TopLimitDefault: s.settings.SolutionsTopLimitDefault,
		TopLimitMax:     s.settings.SolutionsTopLimitMax,
	})
	solutions, err := es.ListTopSolutions(ctx, project, input.Limit)
	if err != nil {
		return TopSolutionsResponse{}, err
	}
	out := make([]SolutionSummary, 0, len(solutions))
	for _, sol := range solutions {
		out = append(out, solutionSummary(sol))
	}
	return TopSolutionsResponse{Project: projectSummary(project), Solutions: out}, nil
}
