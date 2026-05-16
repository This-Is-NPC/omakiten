package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// Search is the MCP entry for the unified FTS5 search tool. Project
// resolution differs from the rest of the agent surface: a missing
// project / project_id pair means "cross-project" instead of falling
// back to the CWD-resolved selector. Callers searching cross-entity
// institutional memory typically want every project's content, not
// the project the agent happens to be running under.
func (s *Service) Search(ctx context.Context, input SearchInput) (SearchResponse, error) {
	project, err := s.resolveSearchProject(ctx, input.ProjectSelector)
	if err != nil {
		return SearchResponse{}, err
	}

	hits, err := app.NewSearchService(s.repo, s.repo).Search(ctx, project, input.Query, input.EntityTypes)
	if err != nil {
		return SearchResponse{}, err
	}

	out := make([]SearchHitDTO, 0, len(hits))
	for _, h := range hits {
		out = append(out, searchHitDTO(h))
	}
	return SearchResponse{Project: projectSummary(project), Hits: out}, nil
}

// resolveSearchProject mirrors Service.resolveProject for the explicit-
// project case (project_id > 0 or non-empty slug), but skips the
// CWD-based fallback so omitting the selector means cross-project. The
// returned ProjectContext is zero-valued (ID 0) when cross-project is
// requested, which the SearchService propagates as the "no filter"
// sentinel to the adapter.
func (s *Service) resolveSearchProject(ctx context.Context, selector ProjectSelector) (domain.ProjectContext, error) {
	if selector.ProjectID > 0 {
		project, err := s.repo.FindProjectByID(ctx, selector.ProjectID)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}
	if slug := strings.TrimSpace(selector.Project); slug != "" {
		project, err := s.repo.FindProjectBySlug(ctx, slug)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}
	return domain.ProjectContext{}, nil
}
