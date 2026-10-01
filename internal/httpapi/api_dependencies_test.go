package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listDependencies", http.MethodGet, "/api/v1/projects/alpha/dependencies?task=5", "", "ListDependencies", contract.ListDependenciesInput{ProjectSelector: alpha, TaskID: 5}},
	{"addDependency", http.MethodPost, "/api/v1/projects/alpha/tasks/5/dependencies", `{"depends_on_task_id":6}`, "AddDependency", contract.AddDependencyInput{ProjectSelector: alpha, TaskID: 5, DependsOnTaskID: 6}},
})

func (f *fakeOps) ListDependencies(_ context.Context, in contract.ListDependenciesInput) (contract.DependenciesResponse, error) {
	f.record("ListDependencies", in)
	return contract.DependenciesResponse{}, nil
}

func (f *fakeOps) AddDependency(_ context.Context, in contract.AddDependencyInput) (contract.DependencyResponse, error) {
	f.record("AddDependency", in)
	return contract.DependencyResponse{}, nil
}
