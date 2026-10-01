package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// DependencyOperations reads and changes task dependencies.
type DependencyOperations interface {
	ListDependencies(ctx context.Context, input contract.ListDependenciesInput) (contract.DependenciesResponse, error)
	AddDependency(ctx context.Context, input contract.AddDependencyInput) (contract.DependencyResponse, error)
	RemoveDependency(ctx context.Context, input contract.RemoveDependencyInput) (contract.RemoveDependencyResponse, error)
}

// DependencyBody names the task the path task waits on.
type DependencyBody struct {
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

func (s *Server) dependencyRoutes() []route {
	return []route{
		query("listDependencies", http.MethodGet, projectPath+"/dependencies", "dependency.list", "Task dependencies of a project.", []param{
			projectParam,
			queryParam("task", "Limit to one task id.", idSchema),
		}, s.listDependencies),
		command("addDependency", http.MethodPost, taskPath+"/dependencies", "dependency.add", "Make a task wait on another task.", []param{projectParam, taskParam}, s.addDependency),
		query("removeDependency", http.MethodDelete, taskPath+"/dependencies/{depends_on}", "dependency.remove", "Stop a task waiting on another task; requires confirmation.", []param{
			projectParam, taskParam,
			pathParam("depends_on", "Id of the task waited on.", idSchema),
			confirmedParam,
		}, s.removeDependency),
	}
}

func (s *Server) listDependencies(r *http.Request) (contract.DependenciesResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.DependenciesResponse{}, err
	}
	input := contract.ListDependenciesInput{ProjectSelector: selector}
	if raw := r.URL.Query().Get("task"); raw != "" {
		if input.TaskID, err = parseID(raw, "task"); err != nil {
			return contract.DependenciesResponse{}, err
		}
	}
	return ops.ListDependencies(r.Context(), input)
}

func (s *Server) addDependency(r *http.Request, body DependencyBody) (contract.DependencyResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.DependencyResponse{}, err
	}
	return ops.AddDependency(r.Context(), contract.AddDependencyInput{ProjectSelector: selector, TaskID: taskID, DependsOnTaskID: body.DependsOnTaskID})
}

func (s *Server) removeDependency(r *http.Request) (contract.RemoveDependencyResponse, error) {
	dependsOn, err := parseID(r.PathValue("depends_on"), "depends_on")
	if err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	ok, err := confirmed(r)
	if err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.RemoveDependencyResponse{}, err
	}
	return ops.RemoveDependency(r.Context(), contract.RemoveDependencyInput{ProjectSelector: selector, TaskID: taskID, DependsOnTaskID: dependsOn, Confirmed: ok})
}
