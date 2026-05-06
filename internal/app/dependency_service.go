package app

import (
	"context"
	"sort"

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

// SyncBlockers reconciles the dependency set for taskID against the desired
// blockerIDs slice: each blocker not currently present is added (cycle
// guard included), and each currently-present blocker missing from the
// desired set is removed. The diff was previously inlined in the TUI;
// owning it here keeps the picker code free of dependency-graph logic and
// makes the behavior unit-testable.
//
// Order is deterministic: removals first (sorted by id) so the cycle check
// for each subsequent add sees a smaller graph; adds in id order match the
// previous TUI behavior so any side-effecting tests/screens stay stable.
func (s *DependencyService) SyncBlockers(ctx context.Context, project domain.ProjectContext, taskID int64, blockerIDs []int64) (err error) {
	finish := activity.Track(ctx, "app.DependencyService.SyncBlockers", project, map[string]any{"task_id": taskID, "count": len(blockerIDs)})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task id must be positive", nil)
		return
	}

	current, err := s.repo.ListTaskDependencies(ctx, project.ID, taskID)
	if err != nil {
		return
	}
	have := make(map[int64]struct{}, len(current))
	for _, dep := range current {
		have[dep.DependsOnTaskID] = struct{}{}
	}
	want := make(map[int64]struct{}, len(blockerIDs))
	for _, id := range blockerIDs {
		want[id] = struct{}{}
	}

	var added, removed []int64
	for id := range want {
		if _, ok := have[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range have {
		if _, ok := want[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })

	for _, depID := range removed {
		if err = s.Remove(ctx, project, taskID, depID); err != nil {
			return
		}
	}
	for _, depID := range added {
		if _, err = s.Add(ctx, project, taskID, depID); err != nil {
			return
		}
	}
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
