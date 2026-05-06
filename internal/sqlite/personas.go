package sqlite

import (
	"context"
	"fmt"

	"omakiten/internal/domain"
)

func (s *Store) ListActivePersonas(ctx context.Context) ([]domain.Persona, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT personas.id, personas.key, personas.name
FROM personas
JOIN config_bundles ON config_bundles.id = personas.bundle_id
WHERE personas.active = 1 AND config_bundles.active = 1
ORDER BY personas.local_id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var personas []domain.Persona
	for rows.Next() {
		var persona domain.Persona
		if err := rows.Scan(&persona.ID, &persona.Key, &persona.Name); err != nil {
			return nil, err
		}
		personas = append(personas, persona)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(personas) == 0 {
		return personas, nil
	}
	personaIDs := make([]int64, len(personas))
	for i, p := range personas {
		personaIDs[i] = p.ID
	}
	skillsByPersona, err := s.personaSkillsByIDs(ctx, personaIDs)
	if err != nil {
		return nil, err
	}
	for index := range personas {
		bundle := skillsByPersona[personas[index].ID]
		personas[index].SkillIDs = bundle.ids
		personas[index].SkillKeys = bundle.keys
	}
	return personas, nil
}

type personaSkillBundle struct {
	ids  []int64
	keys []string
}

// personaSkillsByIDs replaces the per-persona N+1 with a single query that
// fans out the join across every persona. The result is grouped server-side
// (ORDER BY persona_id, local_id) so the in-memory grouping just appends as
// it walks the rows.
func (s *Store) personaSkillsByIDs(ctx context.Context, personaIDs []int64) (map[int64]personaSkillBundle, error) {
	out := map[int64]personaSkillBundle{}
	if len(personaIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(personaIDs))
	for i, id := range personaIDs {
		args[i] = id
	}
	query := fmt.Sprintf(`
SELECT persona_skills.persona_id, skills.id, skills.key
FROM persona_skills
JOIN skills ON skills.id = persona_skills.skill_id
WHERE persona_skills.persona_id IN (%s) AND skills.active = 1
ORDER BY persona_skills.persona_id, skills.local_id
`, placeholders(len(personaIDs)))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var personaID, skillID int64
		var key string
		if err := rows.Scan(&personaID, &skillID, &key); err != nil {
			return nil, err
		}
		bundle := out[personaID]
		bundle.ids = append(bundle.ids, skillID)
		bundle.keys = append(bundle.keys, key)
		out[personaID] = bundle
	}
	return out, rows.Err()
}
