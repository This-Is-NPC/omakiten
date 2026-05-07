package agent

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// ListTemplates returns the templates relevant for the requested filters.
//
// When `project` is set, the response is project-aware: per default kind we
// return the project-scoped template if one exists, otherwise the global
// fallback, never both. When `project` is empty the call returns every matching
// loaded template without resolving project fallback precedence.
func (s *Service) ListTemplates(_ context.Context, input ListTemplatesInput) (ListTemplatesResponse, error) {
	if s.templateCatalog == nil {
		return ListTemplatesResponse{Templates: []TemplateSummary{}}, nil
	}
	all := s.templateCatalog()

	if input.Project == "" {
		out := make([]TemplateSummary, 0, len(all))
		for _, t := range all {
			if input.Kind != "" && t.Default != input.Kind {
				continue
			}
			if !input.IncludeBody {
				t.Body = ""
			}
			out = append(out, t)
		}
		return ListTemplatesResponse{Templates: out}, nil
	}

	scoped := map[string]TemplateSummary{}
	global := map[string]TemplateSummary{}
	for _, t := range all {
		if t.Default == "" {
			continue
		}
		if input.Kind != "" && t.Default != input.Kind {
			continue
		}
		switch t.Project {
		case input.Project:
			scoped[t.Default] = t
		case "":
			global[t.Default] = t
		}
	}
	out := make([]TemplateSummary, 0, len(scoped)+len(global))
	for kind, t := range scoped {
		if !input.IncludeBody {
			t.Body = ""
		}
		out = append(out, t)
		delete(global, kind)
	}
	for _, t := range global {
		if !input.IncludeBody {
			t.Body = ""
		}
		out = append(out, t)
	}
	return ListTemplatesResponse{Templates: out}, nil
}

// ShowTemplate returns one template by slug, with body included.
//
// When a project context resolves (explicitly via project_id/project, or via
// CWD/service-default), the call hard-rejects any global slug that is shadowed
// by a project-scoped override of the same default kind. The rejection message
// names the active slug so the agent can re-call without a clarification
// round-trip — same pattern as the _agent_model coercion. Calls outside any
// registered project (no resolution) preserve the legacy slug-only lookup so
// `okt mcp tools` discovery and CLI debug calls keep working.
func (s *Service) ShowTemplate(ctx context.Context, input ShowTemplateInput) (ShowTemplateResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template slug is required", nil)
	}
	if s.templateCatalog == nil {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template catalog not initialized", map[string]any{"slug": slug})
	}

	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		// Explicit project selectors must propagate ErrProjectNotFound — never
		// silently fall back to a global lookup. CWD-only / service-default
		// failures are tolerated so non-project contexts keep working.
		if input.ProjectID > 0 || strings.TrimSpace(input.Project) != "" {
			return ShowTemplateResponse{}, err
		}
		project = domain.ProjectContext{}
	}

	catalog := s.templateCatalog()
	var requested *TemplateSummary
	for i := range catalog {
		if catalog[i].Slug == slug {
			requested = &catalog[i]
			break
		}
	}
	if requested == nil {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template not found", map[string]any{"slug": slug})
	}

	// Shadow check: only when a project resolves AND the requested template is
	// global (Project == "") AND has a default kind AND a project-scoped row
	// exists for the same kind in the active project.
	if project.Slug != "" && requested.Project == "" && requested.Default != "" {
		for i := range catalog {
			t := catalog[i]
			if t.Default == requested.Default && t.Project == project.Slug {
				return ShowTemplateResponse{}, domain.NewError(
					domain.ErrValidation,
					fmt.Sprintf(
						"template %q is shadowed in project %q by %q; use slug %q instead",
						slug, project.Slug, t.Slug, t.Slug,
					),
					map[string]any{
						"requested_slug": slug,
						"active_slug":    t.Slug,
						"project":        project.Slug,
					},
				)
			}
		}
	}

	return ShowTemplateResponse{Template: *requested}, nil
}
