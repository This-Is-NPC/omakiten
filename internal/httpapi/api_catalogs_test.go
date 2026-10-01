package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listLaws", http.MethodGet, "/api/v1/projects/alpha/laws?scope=project&scope_project=beta&persona=reviewer", "", "ListLaws", contract.ListLawsInput{Scope: "project", Project: "beta", Persona: "reviewer"}},
})

func (f *fakeOps) ListLaws(_ context.Context, in contract.ListLawsInput) (contract.ListLawsResponse, error) {
	f.record("ListLaws", in)
	return contract.ListLawsResponse{}, nil
}
