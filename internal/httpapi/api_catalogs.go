package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// CatalogOperations reads the laws, personas, skills, templates, and agent
// commands a project's configuration loads.
type CatalogOperations interface {
	ListLaws(ctx context.Context, input contract.ListLawsInput) (contract.ListLawsResponse, error)
	ShowLaw(ctx context.Context, input contract.ShowLawInput) (contract.ShowLawResponse, error)
	ListPersonas(ctx context.Context, input contract.ListPersonasInput) (contract.ListPersonasResponse, error)
	ShowPersona(ctx context.Context, input contract.ShowPersonaInput) (contract.ShowPersonaResponse, error)
	ListSkills(ctx context.Context, input contract.ListSkillsInput) (contract.ListSkillsResponse, error)
}

func (s *Server) catalogRoutes() []route {
	return []route{
		query("listLaws", http.MethodGet, projectPath+"/laws", "law.list", "Laws the project configuration loads, without bodies.", []param{
			projectParam,
			queryParam("scope", "Law scope filter.", stringSchema),
			queryParam("scope_project", "Owner project slug filter.", stringSchema),
			queryParam("persona", "Owner persona slug filter.", stringSchema),
		}, s.listLaws),
		query("getLaw", http.MethodGet, projectPath+"/laws/{law}", "law.get", "A law with its body.", []param{projectParam, pathParam("law", "Law slug.", stringSchema)}, s.showLaw),
		query("listPersonas", http.MethodGet, projectPath+"/personas", "persona.list", "Personas the project configuration loads, without bodies.", []param{projectParam}, s.listPersonas),
		query("getPersona", http.MethodGet, projectPath+"/personas/{persona}", "persona.get", "A persona with its body and expanded laws and skills.", []param{projectParam, pathParam("persona", "Persona slug.", stringSchema)}, s.showPersona),
		query("listSkills", http.MethodGet, projectPath+"/skills", "skill.list", "Skills the project configuration loads, without bodies.", []param{projectParam}, s.listSkills),
	}
}

func (s *Server) listSkills(r *http.Request) (contract.ListSkillsResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ListSkillsResponse{}, err
	}
	return ops.ListSkills(r.Context(), contract.ListSkillsInput{})
}

func (s *Server) showPersona(r *http.Request) (contract.ShowPersonaResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ShowPersonaResponse{}, err
	}
	return ops.ShowPersona(r.Context(), contract.ShowPersonaInput{Slug: r.PathValue("persona")})
}

func (s *Server) listPersonas(r *http.Request) (contract.ListPersonasResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ListPersonasResponse{}, err
	}
	return ops.ListPersonas(r.Context(), contract.ListPersonasInput{})
}

func (s *Server) showLaw(r *http.Request) (contract.ShowLawResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ShowLawResponse{}, err
	}
	return ops.ShowLaw(r.Context(), contract.ShowLawInput{Slug: r.PathValue("law")})
}

func (s *Server) listLaws(r *http.Request) (contract.ListLawsResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ListLawsResponse{}, err
	}
	values := r.URL.Query()
	return ops.ListLaws(r.Context(), contract.ListLawsInput{Scope: values.Get("scope"), Project: values.Get("scope_project"), Persona: values.Get("persona")})
}
