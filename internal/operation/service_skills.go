package operation

import (
	"context"
	"sort"
	"strings"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func (s *Service) ListSkills(_ context.Context, _ contract.ListSkillsInput) (contract.ListSkillsResponse, error) {
	if err := s.allow("skill.list"); err != nil {
		return contract.ListSkillsResponse{}, err
	}
	if s.skillCatalog == nil {
		return contract.ListSkillsResponse{Skills: []contract.SkillSummary{}}, nil
	}
	all := s.skillCatalog()
	out := make([]contract.SkillSummary, 0, len(all))
	for _, sk := range all {
		out = append(out, contract.SkillSummary{
			Slug:        sk.Slug,
			Name:        sk.Name,
			Description: sk.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return contract.ListSkillsResponse{Skills: out}, nil
}

// ShowSkill returns one skill by slug, body included. Read-only — there is no
// mutation counterpart. An unknown slug rejects cleanly with a validation
// error naming the missing slug so the caller can correct without a guess.
func (s *Service) ShowSkill(_ context.Context, input contract.ShowSkillInput) (contract.ShowSkillResponse, error) {
	if err := s.allow("skill.get"); err != nil {
		return contract.ShowSkillResponse{}, err
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return contract.ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	if s.skillCatalog == nil {
		return contract.ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill catalog not initialized", map[string]any{"slug": slug})
	}
	for _, sk := range s.skillCatalog() {
		if sk.Slug == slug {
			return contract.ShowSkillResponse{Skill: contract.SkillSummary(sk)}, nil
		}
	}
	return contract.ShowSkillResponse{}, domain.NewError(domain.ErrValidation, "skill not found", map[string]any{"slug": slug})
}
