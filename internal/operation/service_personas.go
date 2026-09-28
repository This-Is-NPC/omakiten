package operation

import (
	"context"
	"sort"
	"strings"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// ListPersonas returns every persona wired in the active config personas: block,
// ordered by slug. Bodies and expanded references are omitted — callers fetch
// one persona via ShowPersona. Read-only.
func (s *Service) ListPersonas(_ context.Context, _ contract.ListPersonasInput) (contract.ListPersonasResponse, error) {
	if err := s.allow("persona.list"); err != nil {
		return contract.ListPersonasResponse{}, err
	}
	if s.personaCatalog == nil {
		return contract.ListPersonasResponse{Personas: []contract.PersonaSummary{}}, nil
	}
	all := s.personaCatalog()
	out := make([]contract.PersonaSummary, 0, len(all))
	for _, p := range all {
		out = append(out, contract.PersonaSummary{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return contract.ListPersonasResponse{Personas: out}, nil
}

// ShowPersona returns one persona by slug with body and every explicitly
// referenced law/skill expanded inline. Read-only.
func (s *Service) ShowPersona(_ context.Context, input contract.ShowPersonaInput) (contract.ShowPersonaResponse, error) {
	if err := s.allow("persona.get"); err != nil {
		return contract.ShowPersonaResponse{}, err
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return contract.ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	if s.personaCatalog == nil {
		return contract.ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona catalog not initialized", map[string]any{"slug": slug})
	}
	var found *contract.PersonaInfo
	for _, p := range s.personaCatalog() {
		if p.Slug == slug {
			copy := p
			found = &copy
			break
		}
	}
	if found == nil {
		return contract.ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona not found", map[string]any{"slug": slug})
	}
	laws, err := resolveLawSlugs(found.Laws, s.lawCatalog)
	if err != nil {
		return contract.ShowPersonaResponse{}, err
	}
	repertoire, err := resolveSkillSlugs(found.SkillRepertoire, s.skillCatalog)
	if err != nil {
		return contract.ShowPersonaResponse{}, err
	}
	return contract.ShowPersonaResponse{Persona: contract.PersonaDetail{
		Slug:            found.Slug,
		Name:            found.Name,
		Description:     found.Description,
		Body:            found.Body,
		Laws:            laws,
		SkillRepertoire: repertoire,
	}}, nil
}
