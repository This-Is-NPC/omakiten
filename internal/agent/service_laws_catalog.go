package agent

import (
	"context"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// ListLaws returns every loaded law (slug + name + severity + scope), ordered
// by slug. Bodies are omitted — callers fetch a single body via ShowLaw.
// Read-only.
func (s *Service) ListLaws(_ context.Context, _ ListLawsInput) (ListLawsResponse, error) {
	if s.lawCatalog == nil {
		return ListLawsResponse{Laws: []LawSummary{}}, nil
	}
	all := s.lawCatalog()
	out := make([]LawSummary, 0, len(all))
	for _, law := range all {
		out = append(out, LawSummary{
			Slug:     law.Slug,
			Name:     law.Name,
			Severity: law.Severity,
			Scope:    law.Scope,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return ListLawsResponse{Laws: out}, nil
}

// ShowLaw returns one law by slug, body included. Read-only.
func (s *Service) ShowLaw(_ context.Context, input ShowLawInput) (ShowLawResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowLawResponse{}, domain.NewError(domain.ErrValidation, "law slug is required", nil)
	}
	if s.lawCatalog == nil {
		return ShowLawResponse{}, domain.NewError(domain.ErrValidation, "law catalog not initialized", map[string]any{"slug": slug})
	}
	for _, law := range s.lawCatalog() {
		if law.Slug == slug {
			return ShowLawResponse{Law: LawSummary{
				Slug:     law.Slug,
				Name:     law.Name,
				Severity: law.Severity,
				Scope:    law.Scope,
				Body:     law.Body,
			}}, nil
		}
	}
	return ShowLawResponse{}, domain.NewError(domain.ErrValidation, "law not found", map[string]any{"slug": slug})
}
