package app

import (
	"context"

	"omakiten/internal/domain"
	"omakiten/internal/graph"
)

type DependencyService struct {
	repo DependencyRepository
}

func NewDependencyService(repo DependencyRepository) *DependencyService {
	return &DependencyService{repo: repo}
}

func (s *DependencyService) Add(ctx context.Context, project domain.ProjectContext, taskID, dependsOnTaskID int64) (domain.TaskDependency, error) {
	if taskID <= 0 || dependsOnTaskID <= 0 {
		return domain.TaskDependency{}, domain.NewError(domain.ErrValidation, "task ids must be positive", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
	}
	if taskID == dependsOnTaskID {
		return domain.TaskDependency{}, domain.NewError(domain.ErrDependencyInvalid, "task cannot depend on itself", map[string]any{"task_id": taskID})
	}

	dependencies, err := s.repo.ListTaskDependencies(ctx, project.ID, 0)
	if err != nil {
		return domain.TaskDependency{}, err
	}
	edges := make([]graph.Edge, 0, len(dependencies)+1)
	for _, dependency := range dependencies {
		edges = append(edges, graph.Edge{From: dependency.TaskID, To: dependency.DependsOnTaskID})
	}
	edges = append(edges, graph.Edge{From: taskID, To: dependsOnTaskID})
	if graph.HasCycle(edges) {
		return domain.TaskDependency{}, domain.NewError(domain.ErrDependencyInvalid, "dependency would create a cycle", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
	}

	return s.repo.AddTaskDependency(ctx, project.ID, taskID, dependsOnTaskID)
}

func (s *DependencyService) Remove(ctx context.Context, project domain.ProjectContext, taskID, dependsOnTaskID int64) error {
	if taskID <= 0 || dependsOnTaskID <= 0 {
		return domain.NewError(domain.ErrValidation, "task ids must be positive", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
	}
	return s.repo.RemoveTaskDependency(ctx, project.ID, taskID, dependsOnTaskID)
}

func (s *DependencyService) List(ctx context.Context, project domain.ProjectContext, taskID int64) ([]domain.TaskDependency, error) {
	if taskID < 0 {
		return nil, domain.NewError(domain.ErrValidation, "task id cannot be negative", nil)
	}
	return s.repo.ListTaskDependencies(ctx, project.ID, taskID)
}
