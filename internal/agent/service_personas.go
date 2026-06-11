package agent

import (
	"context"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// ListPersonas returns every persona wired in the active config personas: block,
// ordered by slug. Bodies and expanded references are omitted — callers fetch
// one persona via ShowPersona. Read-only.
func (s *Service) ListPersonas(_ context.Context, _ ListPersonasInput) (ListPersonasResponse, error) {
	if s.personaCatalog == nil {
		return ListPersonasResponse{Personas: []PersonaSummary{}}, nil
	}
	all := s.personaCatalog()
	out := make([]PersonaSummary, 0, len(all))
	for _, p := range all {
		out = append(out, PersonaSummary{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return ListPersonasResponse{Personas: out}, nil
}

// ShowPersona returns one persona by slug with body and every explicitly
// referenced law/skill expanded inline. Read-only.
func (s *Service) ShowPersona(_ context.Context, input ShowPersonaInput) (ShowPersonaResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	if s.personaCatalog == nil {
		return ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona catalog not initialized", map[string]any{"slug": slug})
	}
	var found *PersonaInfo
	for _, p := range s.personaCatalog() {
		if p.Slug == slug {
			copy := p
			found = &copy
			break
		}
	}
	if found == nil {
		return ShowPersonaResponse{}, domain.NewError(domain.ErrValidation, "persona not found", map[string]any{"slug": slug})
	}
	laws, err := resolveLawSlugs(found.Laws, s.lawCatalog)
	if err != nil {
		return ShowPersonaResponse{}, err
	}
	skills, err := resolveSkillSlugs(found.Skills, s.skillCatalog)
	if err != nil {
		return ShowPersonaResponse{}, err
	}
	repertoire, err := resolveSkillSlugs(found.SkillRepertoire, s.skillCatalog)
	if err != nil {
		return ShowPersonaResponse{}, err
	}
	return ShowPersonaResponse{Persona: PersonaDetail{
		Slug:            found.Slug,
		Name:            found.Name,
		Description:     found.Description,
		Body:            found.Body,
		Laws:            laws,
		Skills:          skills,
		SkillRepertoire: repertoire,
	}}, nil
}
