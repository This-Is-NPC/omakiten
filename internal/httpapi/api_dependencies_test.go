package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listDependencies", http.MethodGet, "/api/v1/projects/alpha/dependencies?task=5", "", "ListDependencies", contract.ListDependenciesInput{ProjectSelector: alpha, TaskID: 5}},
})

func (f *fakeOps) ListDependencies(_ context.Context, in contract.ListDependenciesInput) (contract.DependenciesResponse, error) {
	f.record("ListDependencies", in)
	return contract.DependenciesResponse{}, nil
}
