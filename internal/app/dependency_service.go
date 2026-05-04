package app

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
	"omakiten/internal/graph"
)

type DependencyService struct {
	repo DependencyRepository
}

func NewDependencyService(repo DependencyRepository) *DependencyService {
	return &DependencyService{repo: repo}
}

func (s *DependencyService) Add(ctx context.Context, project domain.ProjectContext, taskID, dependsOnTaskID int64) (dependency domain.TaskDependency, err error) {
	finish := activity.Track(ctx, "app.DependencyService.Add", project, map[string]any{"task_id": taskID, "depends_on": dependsOnTaskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 || dependsOnTaskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task ids must be positive", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
		return
	}
	if taskID == dependsOnTaskID {
		err = domain.NewError(domain.ErrDependencyInvalid, "task cannot depend on itself", map[string]any{"task_id": taskID})
		return
	}

	dependencies, err := s.repo.ListTaskDependencies(ctx, project.ID, 0)
	if err != nil {
		return
	}
	edges := make([]graph.Edge, 0, len(dependencies)+1)
	for _, d := range dependencies {
		edges = append(edges, graph.Edge{From: d.TaskID, To: d.DependsOnTaskID})
	}
	edges = append(edges, graph.Edge{From: taskID, To: dependsOnTaskID})
	if graph.HasCycle(edges) {
		err = domain.NewError(domain.ErrDependencyInvalid, "dependency would create a cycle", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
		return
	}

	dependency, err = s.repo.AddTaskDependency(ctx, project.ID, taskID, dependsOnTaskID)
	return
}

func (s *DependencyService) Remove(ctx context.Context, project domain.ProjectContext, taskID, dependsOnTaskID int64) (err error) {
	finish := activity.Track(ctx, "app.DependencyService.Remove", project, map[string]any{"task_id": taskID, "depends_on": dependsOnTaskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 || dependsOnTaskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task ids must be positive", map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
		return
	}
	err = s.repo.RemoveTaskDependency(ctx, project.ID, taskID, dependsOnTaskID)
	return
}

func (s *DependencyService) List(ctx context.Context, project domain.ProjectContext, taskID int64) (dependencies []domain.TaskDependency, err error) {
	finish := activity.Track(ctx, "app.DependencyService.List", project, map[string]any{"task_id": taskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID < 0 {
		err = domain.NewError(domain.ErrValidation, "task id cannot be negative", nil)
		return
	}
	dependencies, err = s.repo.ListTaskDependencies(ctx, project.ID, taskID)
	return
}
