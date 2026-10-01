package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listLaws", http.MethodGet, "/api/v1/projects/alpha/laws?scope=project&scope_project=beta&persona=reviewer", "", "ListLaws", contract.ListLawsInput{Scope: "project", Project: "beta", Persona: "reviewer"}},
	{"getLaw", http.MethodGet, "/api/v1/projects/alpha/laws/no-secrets", "", "ShowLaw", contract.ShowLawInput{Slug: "no-secrets"}},
	{"listPersonas", http.MethodGet, "/api/v1/projects/alpha/personas", "", "ListPersonas", contract.ListPersonasInput{}},
	{"getPersona", http.MethodGet, "/api/v1/projects/alpha/personas/reviewer", "", "ShowPersona", contract.ShowPersonaInput{Slug: "reviewer"}},
})

func (f *fakeOps) ListLaws(_ context.Context, in contract.ListLawsInput) (contract.ListLawsResponse, error) {
	f.record("ListLaws", in)
	return contract.ListLawsResponse{}, nil
}

func (f *fakeOps) ShowLaw(_ context.Context, in contract.ShowLawInput) (contract.ShowLawResponse, error) {
	f.record("ShowLaw", in)
	return contract.ShowLawResponse{}, nil
}

func (f *fakeOps) ListPersonas(_ context.Context, in contract.ListPersonasInput) (contract.ListPersonasResponse, error) {
	f.record("ListPersonas", in)
	return contract.ListPersonasResponse{}, nil
}

func (f *fakeOps) ShowPersona(_ context.Context, in contract.ShowPersonaInput) (contract.ShowPersonaResponse, error) {
	f.record("ShowPersona", in)
	return contract.ShowPersonaResponse{}, nil
}
