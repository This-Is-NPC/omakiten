package agent

import (
	"context"

	"omakiten/internal/app"
)

func (s *Service) AddDependency(ctx context.Context, input AddDependencyInput) (DependencyResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DependencyResponse{}, err
	}
	dependency, err := app.NewDependencyService(s.repo).Add(ctx, project, input.TaskID, input.DependsOnTaskID)
	if err != nil {
		return DependencyResponse{}, err
	}
	return DependencyResponse{Project: projectSummary(project), Dependency: dependencySummary(dependency)}, nil
}

func (s *Service) RemoveDependency(ctx context.Context, input RemoveDependencyInput) (RemoveDependencyResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RemoveDependencyResponse{}, err
	}
	if !input.Confirmed {
		return RemoveDependencyResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a dependency changes task ordering and requires explicit confirmation.",
				Options:              []ConfirmationOption{{Action: "remove_dependency", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := app.NewDependencyService(s.repo).Remove(ctx, project, input.TaskID, input.DependsOnTaskID); err != nil {
		return RemoveDependencyResponse{}, err
	}
	return RemoveDependencyResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListDependencies(ctx context.Context, input ListDependenciesInput) (DependenciesResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DependenciesResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return DependenciesResponse{}, err
	}
	return DependenciesResponse{Project: projectSummary(project), Dependencies: dependencySummaries(dependencies)}, nil
}
