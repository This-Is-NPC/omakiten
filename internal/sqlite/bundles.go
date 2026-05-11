package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func (s *Store) ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Deactivate any previously active bundle so that only the
	// newly imported one is visible to queries. The config selection
	// is single-tenant: one active yaml file -> one active bundle.
	if _, err := tx.ExecContext(ctx, "UPDATE config_bundles SET active = 0"); err != nil {
		return err
	}

	bundleID, err := upsertBundle(ctx, tx, bundle, sourcePath, sourceHash)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "UPDATE settings SET active = 0 WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	settings := map[string]string{
		"output.json_minified":  fmt.Sprintf("%t", bundle.Config.Output.JSONMinified),
		"output.omit_empty":     fmt.Sprintf("%t", bundle.Config.Output.OmitEmpty),
		"context.default_level": fmt.Sprintf("%d", bundle.Config.Context.DefaultLevel),
		"context.max_tokens":    fmt.Sprintf("%d", bundle.Config.Context.MaxTokens),
		"workflow.active":       bundle.Config.Workflow.Active,
		"theme.active":          bundle.Config.Theme.Active,
	}
	for key, value := range settings {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings(bundle_id, key, value, active) VALUES (?, ?, ?, 1)
ON CONFLICT(bundle_id, key) DO UPDATE SET value = excluded.value, active = 1
`, bundleID, key, value); err != nil {
			return err
		}
	}

	// persona_skills must be cleared before skills are deleted to avoid FK
	// violations; importPersonas re-creates them after personas are inserted.
	if err := clearPersonaSkills(ctx, tx, bundleID); err != nil {
		return err
	}
	if err := importSkills(ctx, tx, bundleID, bundle.Skills); err != nil {
		return err
	}
	if err := importPersonas(ctx, tx, bundleID, bundle.Personas); err != nil {
		return err
	}
	personasByKey, err := loadPersonaIDs(ctx, tx, bundleID)
	if err != nil {
		return err
	}
	projectsByKey, err := loadProjectIDsBySlug(ctx, tx)
	if err != nil {
		return err
	}
	if err := importLaws(ctx, tx, bundleID, bundle.Laws, personasByKey, projectsByKey); err != nil {
		return err
	}
	if err := importWorkflows(ctx, tx, bundleID, bundle.Workflows); err != nil {
		return err
	}

	return tx.Commit()
}

func loadPersonaIDs(ctx context.Context, tx *sql.Tx, bundleID int64) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, key FROM personas WHERE bundle_id = ? AND active = 1", bundleID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		out[key] = id
	}
	return out, rows.Err()
}

func loadProjectIDsBySlug(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, slug FROM projects WHERE archived_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, err
		}
		out[slug] = id
	}
	return out, rows.Err()
}

func upsertBundle(ctx context.Context, tx *sql.Tx, bundle config.Bundle, sourcePath, sourceHash string) (int64, error) {
	row := tx.QueryRowContext(ctx, `
INSERT INTO config_bundles(key, name, version, scope, source_path, source_hash, active, updated_at)
VALUES (?, ?, ?, 'global', ?, ?, 1, CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET
  name = excluded.name,
  version = excluded.version,
  source_path = excluded.source_path,
  source_hash = excluded.source_hash,
  active = 1,
  updated_at = CURRENT_TIMESTAMP
RETURNING id
`, bundle.Kit.Key, bundle.Kit.Name, bundle.Version, sourcePath, sourceHash)

	var id int64
	err := row.Scan(&id)
	return id, err
}

func importSkills(ctx context.Context, tx *sql.Tx, bundleID int64, skills []config.Skill) error {
	// Hard-delete prior rows for this bundle to avoid the UNIQUE(bundle_id,
	// local_id) collision when slugs are reordered or removed. persona_skills
	// is wiped earlier during personas import (see clearPersonaSkills).
	if _, err := tx.ExecContext(ctx, "DELETE FROM skills WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for index, skill := range skills {
		localID := index + 1
		if _, err := tx.ExecContext(ctx, `
INSERT INTO skills(bundle_id, local_id, key, name, description, body, source_path, active)
VALUES (?, ?, ?, ?, ?, ?, ?, 1)
`, bundleID, localID, skill.Slug, skill.Name, skill.Description, skill.Body, skill.SourcePath); err != nil {
			return err
		}
	}
	return nil
}

func importPersonas(ctx context.Context, tx *sql.Tx, bundleID int64, personas []config.Persona) error {
	if err := clearPersonaSkills(ctx, tx, bundleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM personas WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for index, persona := range personas {
		localID := index + 1
		var personaID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO personas(bundle_id, local_id, key, name, description, active)
VALUES (?, ?, ?, ?, ?, 1)
RETURNING id
`, bundleID, localID, persona.Slug, persona.Name, persona.Description).Scan(&personaID); err != nil {
			return err
		}

		for _, skillSlug := range persona.Skills {
			var skillID int64
			if err := tx.QueryRowContext(ctx, "SELECT id FROM skills WHERE bundle_id = ? AND key = ? AND active = 1", bundleID, skillSlug).Scan(&skillID); err != nil {
				return fmt.Errorf("persona %s references skill %s: %w", persona.Slug, skillSlug, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO persona_skills(persona_id, skill_id) VALUES (?, ?)", personaID, skillID); err != nil {
				return err
			}
		}
	}
	return nil
}

func clearPersonaSkills(ctx context.Context, tx *sql.Tx, bundleID int64) error {
	_, err := tx.ExecContext(ctx, `
DELETE FROM persona_skills
WHERE persona_id IN (SELECT id FROM personas WHERE bundle_id = ?)
`, bundleID)
	return err
}

func importLaws(ctx context.Context, tx *sql.Tx, bundleID int64, laws []config.Law, personasByKey map[string]int64, projectsByKey map[string]int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM laws WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for index, law := range laws {
		localID := index + 1
		scope := law.Scope
		if scope == "" {
			scope = "global"
		}
		var projectID, personaID *int64
		if scope == "project" && law.ProjectSlug != "" {
			if id, ok := projectsByKey[law.ProjectSlug]; ok {
				projectID = &id
			}
		}
		if scope == "persona" && law.PersonaSlug != "" {
			if id, ok := personasByKey[law.PersonaSlug]; ok {
				personaID = &id
			}
		}
		// Resolve the frontmatter severity label to its configured id
		// via the active registry. The validator runs against the
		// loaded bundle BEFORE ImportBundle, so unknown labels at this
		// point are a contract violation — fail rigid instead of
		// silently substituting a default. The runtime composition
		// root populates the registry from the user's config, so this
		// never trips in production.
		severityID, ok := domain.SeverityFromLabel(law.Severity)
		if !ok {
			return fmt.Errorf("law %q: severity %q not in active registry (validator should have caught this)", law.Slug, law.Severity)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO laws(bundle_id, local_id, key, severity_id, body, scope, project_id, persona_id, active)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
`, bundleID, localID, law.Slug, int(severityID), law.Body, scope, projectID, personaID); err != nil {
			return err
		}
	}
	return nil
}

func importWorkflows(ctx context.Context, tx *sql.Tx, bundleID int64, workflows []config.Workflow) error {
	if _, err := tx.ExecContext(ctx, "UPDATE workflows SET active = 0 WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}

	for _, workflow := range workflows {
		operationsJSON, err := json.Marshal(workflow.Operations)
		if err != nil {
			return err
		}
		// defaults_json is "{}" when the workflow declares no defaults
		// block — the resolver treats both empty object and a populated
		// block uniformly because the WorkflowDefaults pointer/field
		// chain falls through to the implicit "true" when fields are nil.
		defaultsJSON := "{}"
		if workflow.Defaults != nil {
			raw, err := json.Marshal(workflow.Defaults)
			if err != nil {
				return err
			}
			defaultsJSON = string(raw)
		}
		var workflowID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO workflows(bundle_id, local_id, key, name, operations_json, defaults_json, active) VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(bundle_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, operations_json = excluded.operations_json, defaults_json = excluded.defaults_json, active = 1
RETURNING id
`, bundleID, workflow.ID, workflow.Key, workflow.Name, string(operationsJSON), defaultsJSON).Scan(&workflowID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "UPDATE workflow_buckets SET active = 0 WHERE workflow_id = ?", workflowID); err != nil {
			return err
		}
		bucketIDs := map[int]int64{}
		for _, bucket := range workflow.Buckets {
			permissionsJSON := "{}"
			if bucket.Permissions != nil {
				raw, err := json.Marshal(bucket.Permissions)
				if err != nil {
					return err
				}
				permissionsJSON = string(raw)
			}
			var bucketID int64
			if err := tx.QueryRowContext(ctx, `
INSERT INTO workflow_buckets(workflow_id, local_id, key, name, position, permissions_json, active) VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(workflow_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, position = excluded.position, permissions_json = excluded.permissions_json, active = 1
RETURNING id
`, workflowID, bucket.ID, bucket.Key, bucket.Name, bucket.Position, permissionsJSON).Scan(&bucketID); err != nil {
				return err
			}
			bucketIDs[bucket.ID] = bucketID
		}

		if _, err := tx.ExecContext(ctx, "UPDATE workflow_transitions SET active = 0 WHERE workflow_id = ?", workflowID); err != nil {
			return err
		}
		for _, transition := range workflow.Transitions {
			fromID := bucketIDs[transition.From]
			toID := bucketIDs[transition.To]
			guards := transition.Guards
			if guards == nil {
				guards = []config.TransitionGuard{}
			}
			guardsJSON, err := json.Marshal(guards)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO workflow_transitions(workflow_id, from_bucket_id, to_bucket_id, guards_json, active) VALUES (?, ?, ?, ?, 1)
ON CONFLICT(workflow_id, from_bucket_id, to_bucket_id) DO UPDATE SET guards_json = excluded.guards_json, active = 1
`, workflowID, fromID, toID, string(guardsJSON)); err != nil {
				return err
			}
		}
	}

	return nil
}
