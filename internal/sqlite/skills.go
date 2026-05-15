package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

// ListActiveSkills delegates to the in-memory provider snapshot. The
// SQL `skills` table was dropped in migration 020; ids are now
// synthesised from the skill's slot in the bundle (positional, 1-based).
// Stable within a snapshot; rotates on every Swap — mirrors the legacy
// behaviour where `skills.local_id` reset on every ImportBundle.
func (s *Store) ListActiveSkills(ctx context.Context) ([]domain.Skill, error) {
	skills := s.Providers().Skills()
	out := make([]domain.Skill, 0, len(skills))
	for i, sk := range skills {
		out = append(out, domain.Skill{
			ID:          int64(i + 1),
			Key:         sk.Slug,
			Name:        sk.Name,
			Description: sk.Description,
			Body:        sk.Body,
			SourcePath:  sk.SourcePath,
			IsCustom:    sk.IsCustom,
		})
	}
	return out, nil
}
