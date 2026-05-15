package sqlite

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// ListActivePersonas delegates to the in-memory provider snapshot. The
// SQL `personas` table was dropped in migration 020; ids are now
// synthesised from the persona's slot in the bundle so older callers
// that filter by id keep working (the ids are stable within a snapshot
// but rotate on every Swap, which mirrors the previous behaviour of
// `personas.local_id` resetting on every ImportBundle).
func (s *Store) ListActivePersonas(ctx context.Context) ([]domain.Persona, error) {
	personas := s.Providers().Personas()
	skills := s.Providers().Skills()
	skillIDBySlug := make(map[string]int64, len(skills))
	for i, sk := range skills {
		skillIDBySlug[sk.Slug] = int64(i + 1)
	}
	out := make([]domain.Persona, 0, len(personas))
	for i, p := range personas {
		out = append(out, toDomainPersona(p, int64(i+1), skillIDBySlug))
	}
	return out, nil
}

func toDomainPersona(p config.Persona, id int64, skillIDBySlug map[string]int64) domain.Persona {
	skillKeys := append([]string(nil), p.Skills...)
	lawKeys := append([]string(nil), p.Laws...)
	skillIDs := make([]int64, 0, len(p.Skills))
	for _, slug := range p.Skills {
		if id, ok := skillIDBySlug[slug]; ok {
			skillIDs = append(skillIDs, id)
		}
	}
	return domain.Persona{
		ID:          id,
		Key:         p.Slug,
		Name:        p.Name,
		Description: p.Description,
		Body:        p.Body,
		SkillIDs:    skillIDs,
		SkillKeys:   skillKeys,
		LawKeys:     lawKeys,
		SourcePath:  p.SourcePath,
		IsCustom:    p.IsCustom,
	}
}
