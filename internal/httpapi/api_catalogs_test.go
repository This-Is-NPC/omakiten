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
	{"listSkills", http.MethodGet, "/api/v1/projects/alpha/skills", "", "ListSkills", contract.ListSkillsInput{}},
	{"getSkill", http.MethodGet, "/api/v1/projects/alpha/skills/tdd", "", "ShowSkill", contract.ShowSkillInput{Slug: "tdd"}},
	{"listTemplates", http.MethodGet, "/api/v1/projects/alpha/templates?kind=handoff&scope_project=alpha&include_body=true", "", "ListTemplates", contract.ListTemplatesInput{
		ProjectSelector: alpha, Kind: "handoff", Project: "alpha", IncludeBody: true,
	}},
	{"getTemplate", http.MethodGet, "/api/v1/projects/alpha/templates/handoff", "", "ShowTemplate", contract.ShowTemplateInput{ProjectSelector: alpha, Slug: "handoff"}},
	{"listCommands", http.MethodGet, "/api/v1/projects/alpha/commands", "", "ListCommands", nil},
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

func (f *fakeOps) ListSkills(_ context.Context, in contract.ListSkillsInput) (contract.ListSkillsResponse, error) {
	f.record("ListSkills", in)
	return contract.ListSkillsResponse{}, nil
}

func (f *fakeOps) ShowSkill(_ context.Context, in contract.ShowSkillInput) (contract.ShowSkillResponse, error) {
	f.record("ShowSkill", in)
	return contract.ShowSkillResponse{}, nil
}

func (f *fakeOps) ListTemplates(_ context.Context, in contract.ListTemplatesInput) (contract.ListTemplatesResponse, error) {
	f.record("ListTemplates", in)
	return contract.ListTemplatesResponse{}, nil
}

func (f *fakeOps) ShowTemplate(_ context.Context, in contract.ShowTemplateInput) (contract.ShowTemplateResponse, error) {
	f.record("ShowTemplate", in)
	return contract.ShowTemplateResponse{}, nil
}

func (f *fakeOps) ListCommands(context.Context) (contract.ListCommandsResponse, error) {
	f.record("ListCommands", nil)
	return contract.ListCommandsResponse{}, nil
}
