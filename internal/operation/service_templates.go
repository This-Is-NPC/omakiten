package operation

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// ListTemplates returns the templates relevant for the requested filters.
//
// When `project` is set, the response is project-aware: per default kind we
// return the project-scoped template if one exists, otherwise the global
// fallback, never both. When `project` is empty the call returns every matching
// loaded template without resolving project fallback precedence.
func (s *Service) ListTemplates(_ context.Context, input contract.ListTemplatesInput) (contract.ListTemplatesResponse, error) {
	if err := s.allow("template.list"); err != nil {
		return contract.ListTemplatesResponse{}, err
	}
	if s.templateCatalog == nil {
		return contract.ListTemplatesResponse{Templates: []contract.TemplateSummary{}}, nil
	}
	all := s.templateCatalog()

	if input.Project == "" {
		return contract.ListTemplatesResponse{Templates: listAllTemplates(all, input)}, nil
	}
	return contract.ListTemplatesResponse{Templates: listProjectTemplates(all, input)}, nil
}

func listAllTemplates(all []contract.TemplateSummary, input contract.ListTemplatesInput) []contract.TemplateSummary {
	out := make([]contract.TemplateSummary, 0, len(all))
	for _, t := range all {
		if input.Kind != "" && t.Default != input.Kind {
			continue
		}
		out = append(out, templateForList(t, input.IncludeBody))
	}
	return out
}

func listProjectTemplates(all []contract.TemplateSummary, input contract.ListTemplatesInput) []contract.TemplateSummary {
	scoped := map[string]contract.TemplateSummary{}
	global := map[string]contract.TemplateSummary{}
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
	out := make([]contract.TemplateSummary, 0, len(scoped)+len(global))
	for kind, t := range scoped {
		out = append(out, templateForList(t, input.IncludeBody))
		delete(global, kind)
	}
	for _, t := range global {
		out = append(out, templateForList(t, input.IncludeBody))
	}
	return out
}

func templateForList(template contract.TemplateSummary, includeBody bool) contract.TemplateSummary {
	if !includeBody {
		template.Body = ""
	}
	return template
}

// ShowTemplate returns one template by slug, with body included.
//
// When a project context resolves (explicitly via project_id/project, or via
// CWD/service-default), the call hard-rejects any global slug that is shadowed
// by a project-scoped override of the same default kind. The rejection message
// names the active slug so the agent can re-call without a clarification
// round-trip — same pattern as the _agent_model coercion. Calls outside any
// registered project (no resolution) fall back to the slug-only lookup so
// `okt mcp tools` discovery and CLI debug calls keep working.
func (s *Service) ShowTemplate(ctx context.Context, input contract.ShowTemplateInput) (contract.ShowTemplateResponse, error) {
	if err := s.allow("template.show"); err != nil {
		return contract.ShowTemplateResponse{}, err
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return contract.ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template slug is required", nil)
	}
	if s.templateCatalog == nil {
		return contract.ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template catalog not initialized", map[string]any{"slug": slug})
	}

	project, err := s.resolveTemplateProject(ctx, input)
	if err != nil {
		return contract.ShowTemplateResponse{}, err
	}

	catalog := s.templateCatalog()
	requested := findTemplate(catalog, slug)
	if requested == nil {
		return contract.ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template not found", map[string]any{"slug": slug})
	}

	if err := shadowedTemplateError(catalog, *requested, project, slug); err != nil {
		return contract.ShowTemplateResponse{}, err
	}

	return contract.ShowTemplateResponse{Template: *requested}, nil
}

func (s *Service) resolveTemplateProject(ctx context.Context, input contract.ShowTemplateInput) (domain.ProjectContext, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err == nil {
		return project, nil
	}
	// Explicit project selectors must propagate ErrProjectNotFound. CWD-only /
	// service-default failures are tolerated so non-project contexts keep working.
	if input.ProjectID > 0 || strings.TrimSpace(input.Project) != "" {
		return domain.ProjectContext{}, err
	}
	return domain.ProjectContext{}, nil
}

func findTemplate(catalog []contract.TemplateSummary, slug string) *contract.TemplateSummary {
	for i := range catalog {
		if catalog[i].Slug == slug {
			return &catalog[i]
		}
	}
	return nil
}

func shadowedTemplateError(catalog []contract.TemplateSummary, requested contract.TemplateSummary, project domain.ProjectContext, slug string) error {
	if project.Slug == "" || requested.Project != "" || requested.Default == "" {
		return nil
	}
	for _, template := range catalog {
		if template.Default == requested.Default && template.Project == project.Slug {
			return domain.NewError(
				domain.ErrValidation,
				fmt.Sprintf(
					"template %q is shadowed in project %q by %q; use slug %q instead",
					slug, project.Slug, template.Slug, template.Slug,
				),
				map[string]any{
					"requested_slug": slug,
					"active_slug":    template.Slug,
					"project":        project.Slug,
				},
			)
		}
	}
	return nil
}
