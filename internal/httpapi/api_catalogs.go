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
}

func (s *Server) catalogRoutes() []route {
	return []route{
		query("listLaws", http.MethodGet, projectPath+"/laws", "law.list", "Laws the project configuration loads, without bodies.", []param{
			projectParam,
			queryParam("scope", "Law scope filter.", stringSchema),
			queryParam("scope_project", "Owner project slug filter.", stringSchema),
			queryParam("persona", "Owner persona slug filter.", stringSchema),
		}, s.listLaws),
	}
}

func (s *Server) listLaws(r *http.Request) (contract.ListLawsResponse, error) {
	ops, _, err := s.project(r)
	if err != nil {
		return contract.ListLawsResponse{}, err
	}
	values := r.URL.Query()
	return ops.ListLaws(r.Context(), contract.ListLawsInput{Scope: values.Get("scope"), Project: values.Get("scope_project"), Persona: values.Get("persona")})
}
