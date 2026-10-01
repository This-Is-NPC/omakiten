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
	ShowSkill(ctx context.Context, input contract.ShowSkillInput) (contract.ShowSkillResponse, error)
	ListTemplates(ctx context.Context, input contract.ListTemplatesInput) (contract.ListTemplatesResponse, error)
	ShowTemplate(ctx context.Context, input contract.ShowTemplateInput) (contract.ShowTemplateResponse, error)
	ListCommands(ctx context.Context) (contract.ListCommandsResponse, error)
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
		query("getSkill", http.MethodGet, projectPath+"/skills/{skill}", "skill.get", "A skill with its body.", []param{projectParam, pathParam("skill", "Skill slug.", stringSchema)}, s.showSkill),
		query("listTemplates", http.MethodGet, projectPath+"/templates", "template.list", "Templates the project configuration loads.", []param{
			projectParam,
			queryParam("kind", "Default kind filter.", stringSchema),
			queryParam("scope_project", "Resolve each default kind for this project slug, preferring its own template over the global one.", stringSchema),
			queryParam("include_body", "`true` includes template bodies.", boolSchema),
		}, s.listTemplates),
		query("getTemplate", http.MethodGet, projectPath+"/templates/{template}", "template.show", "A template with its body; a global template the project overrides is rejected.", []param{projectParam, pathParam("template", "Template slug.", stringSchema)}, s.showTemplate),
		query("listCommands", http.MethodGet, projectPath+"/commands", "command.list", "Agent commands the project configuration binds.", []param{projectParam}, s.listCommands),
	}
}

func (s *Server) listCommands(r *http.Request) (contract.ListCommandsResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ListCommandsResponse{}, err
	}
	return ops.ListCommands(r.Context())
}

func (s *Server) showTemplate(r *http.Request) (contract.ShowTemplateResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowTemplateResponse{}, err
	}
	return ops.ShowTemplate(r.Context(), contract.ShowTemplateInput{ProjectSelector: selector, Slug: r.PathValue("template")})
}

func (s *Server) listTemplates(r *http.Request) (contract.ListTemplatesResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListTemplatesResponse{}, err
	}
	includeBody, err := optionalBool(r, "include_body")
	if err != nil {
		return contract.ListTemplatesResponse{}, err
	}
	return ops.ListTemplates(r.Context(), contract.ListTemplatesInput{
		ProjectSelector: selector,
		Kind:            r.URL.Query().Get("kind"),
		Project:         r.URL.Query().Get("scope_project"),
		IncludeBody:     includeBody != nil && *includeBody,
	})
}

func (s *Server) showSkill(r *http.Request) (contract.ShowSkillResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ShowSkillResponse{}, err
	}
	return ops.ShowSkill(r.Context(), contract.ShowSkillInput{Slug: r.PathValue("skill")})
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
