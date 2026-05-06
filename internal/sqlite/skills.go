package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

func (s *Store) ListActiveSkills(ctx context.Context) ([]domain.Skill, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT skills.id, skills.key, skills.name
FROM skills
JOIN config_bundles ON config_bundles.id = skills.bundle_id
WHERE skills.active = 1 AND config_bundles.active = 1
ORDER BY skills.local_id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var skills []domain.Skill
	for rows.Next() {
		var skill domain.Skill
		if err := rows.Scan(&skill.ID, &skill.Key, &skill.Name); err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}
