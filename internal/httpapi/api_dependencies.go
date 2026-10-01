package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// DependencyOperations reads and changes task dependencies.
type DependencyOperations interface {
	ListDependencies(ctx context.Context, input contract.ListDependenciesInput) (contract.DependenciesResponse, error)
}

func (s *Server) dependencyRoutes() []route {
	return []route{
		query("listDependencies", http.MethodGet, projectPath+"/dependencies", "dependency.list", "Task dependencies of a project.", []param{
			projectParam,
			queryParam("task", "Limit to one task id.", idSchema),
		}, s.listDependencies),
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
