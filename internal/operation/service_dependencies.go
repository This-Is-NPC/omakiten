package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
)

func (s *Service) AddDependency(ctx context.Context, input contract.AddDependencyInput) (contract.DependencyResponse, error) {
	if err := s.allow("dependency.add"); err != nil {
		return contract.DependencyResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.DependencyResponse{}, err
	}
	dependency, err := app.NewDependencyServiceWithEvents(s.repo, s.repo).Add(ctx, project, input.TaskID, input.DependsOnTaskID)
	if err != nil {
		return contract.DependencyResponse{}, err
	}
	return contract.DependencyResponse{Project: projectSummary(project), Dependency: dependencySummary(dependency)}, nil
}

func (s *Service) RemoveDependency(ctx context.Context, input contract.RemoveDependencyInput) (contract.RemoveDependencyResponse, error) {
	if err := s.allow("dependency.remove"); err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	if !input.Confirmed {
		return contract.RemoveDependencyResponse{
			Project: projectSummary(project),
			Confirmation: contract.Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a dependency changes task ordering and requires explicit confirmation.",
				Options:              []contract.ConfirmationOption{{Action: "remove_dependency", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := app.NewDependencyServiceWithEvents(s.repo, s.repo).Remove(ctx, project, input.TaskID, input.DependsOnTaskID); err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	return contract.RemoveDependencyResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListDependencies(ctx context.Context, input contract.ListDependenciesInput) (contract.DependenciesResponse, error) {
	if err := s.allow("dependency.list"); err != nil {
		return contract.DependenciesResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.DependenciesResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return contract.DependenciesResponse{}, err
	}
	return contract.DependenciesResponse{Project: projectSummary(project), Dependencies: dependencySummaries(dependencies)}, nil
}
