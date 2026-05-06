package agent

import (
	"context"
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
func (s *Service) ShowTemplate(_ context.Context, input ShowTemplateInput) (ShowTemplateResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template slug is required", nil)
	}
	if s.templateCatalog == nil {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template catalog not initialized", map[string]any{"slug": slug})
	}
	for _, t := range s.templateCatalog() {
		if t.Slug == slug {
			return ShowTemplateResponse{Template: t}, nil
		}
	}
	return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template not found", map[string]any{"slug": slug})
}
