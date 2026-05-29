package agent

import (
	"context"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// ListSkills returns every loaded skill (slug + name + description), ordered by
// slug for a stable response. Bodies are omitted so the listing stays compact —
// callers fetch a single body via ShowSkill. Read-only: skills are user-authored
// and MCP exposes no create/edit/delete path.
func (s *Service) ListSkills(_ context.Context, _ ListSkillsInput) (ListSkillsResponse, error) {
	if s.skillCatalog == nil {
		return ListSkillsResponse{Skills: []SkillSummary{}}, nil
	}
	all := s.skillCatalog()
	out := make([]SkillSummary, 0, len(all))
	for _, sk := range all {
		out = append(out, SkillSummary{
			Slug:        sk.Slug,
			Name:        sk.Name,
			Description: sk.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return ListSkillsResponse{Skills: out}, nil
}

// ShowSkill returns one skill by slug, body included. Read-only — there is no
// mutation counterpart. An unknown slug rejects cleanly with a validation
// error naming the missing slug so the caller can correct without a guess.
func (s *Service) ShowSkill(_ context.Context, input ShowSkillInput) (ShowSkillResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	if s.skillCatalog == nil {
		return ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill catalog not initialized", map[string]any{"slug": slug})
	}
	for _, sk := range s.skillCatalog() {
		if sk.Slug == slug {
			return ShowSkillResponse{Skill: SkillSummary(sk)}, nil
		}
	}
	return ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill not found", map[string]any{"slug": slug})
}
