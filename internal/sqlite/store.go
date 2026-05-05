package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/migrations"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = store.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.applyMigrations(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) applyMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		return err
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE version = ?", name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO projects(name, slug, root_path, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(root_path) DO UPDATE SET
  name = excluded.name,
  slug = excluded.slug,
  updated_at = CURRENT_TIMESTAMP
RETURNING id, name, slug, root_path
`, name, slug, rootPath)

	var project domain.Project
	if err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
		return domain.Project{}, err
	}
	return project, nil
}

func (s *Store) FindProjectByID(ctx context.Context, id int64) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE id = ? AND archived_at IS NULL", id))
}

func (s *Store) FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE slug = ? AND archived_at IS NULL", slug))
}

func (s *Store) FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE archived_at IS NULL ORDER BY length(root_path) DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []domain.Project
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
			return nil, err
		}
		if pathWithinRoot(path, project.RootPath) {
			projects = append(projects, project)
		}
	}
	return projects, rows.Err()
}

func (s *Store) scanProject(row *sql.Row) (domain.Project, error) {
	var project domain.Project
	if err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
		}
		return domain.Project{}, err
	}
	return project, nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func (s *Store) ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

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
		if _, err := tx.ExecContext(ctx, `
INSERT INTO laws(bundle_id, local_id, key, severity, body, scope, project_id, persona_id, active)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
`, bundleID, localID, law.Slug, law.Severity, law.Body, scope, projectID, personaID); err != nil {
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
		var workflowID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO workflows(bundle_id, local_id, key, name, active) VALUES (?, ?, ?, ?, 1)
ON CONFLICT(bundle_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, active = 1
RETURNING id
`, bundleID, workflow.ID, workflow.Key, workflow.Name).Scan(&workflowID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "UPDATE workflow_buckets SET active = 0 WHERE workflow_id = ?", workflowID); err != nil {
			return err
		}
		bucketIDs := map[int]int64{}
		for _, bucket := range workflow.Buckets {
			var bucketID int64
			if err := tx.QueryRowContext(ctx, `
INSERT INTO workflow_buckets(workflow_id, local_id, key, name, position, active) VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT(workflow_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, position = excluded.position, active = 1
RETURNING id
`, workflowID, bucket.ID, bucket.Key, bucket.Name, bucket.Position).Scan(&bucketID); err != nil {
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

	for index := range personas {
		ids, keys, err := s.personaSkills(ctx, personas[index].ID)
		if err != nil {
			return nil, err
		}
		personas[index].SkillIDs = ids
		personas[index].SkillKeys = keys
	}
	return personas, nil
}

func (s *Store) personaSkills(ctx context.Context, personaID int64) ([]int64, []string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT skills.id, skills.key
FROM persona_skills
JOIN skills ON skills.id = persona_skills.skill_id
WHERE persona_skills.persona_id = ? AND skills.active = 1
ORDER BY skills.local_id
`, personaID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	var keys []string
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		keys = append(keys, key)
	}
	return ids, keys, rows.Err()
}

func (s *Store) ListActiveLaws(ctx context.Context) ([]domain.Law, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT laws.id, laws.key, laws.severity, laws.body
FROM laws
JOIN config_bundles ON config_bundles.id = laws.bundle_id
WHERE laws.active = 1 AND config_bundles.active = 1
ORDER BY laws.local_id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var laws []domain.Law
	for rows.Next() {
		var law domain.Law
		if err := rows.Scan(&law.ID, &law.Key, &law.Severity, &law.Body); err != nil {
			return nil, err
		}
		laws = append(laws, law)
	}
	return laws, rows.Err()
}

func (s *Store) ActiveWorkflow(ctx context.Context) (domain.Workflow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT workflows.id, workflows.key, workflows.name
FROM workflows
JOIN config_bundles ON config_bundles.id = workflows.bundle_id
JOIN settings ON settings.bundle_id = config_bundles.id
  AND settings.key = 'workflow.active'
  AND settings.value = workflows.key
  AND settings.active = 1
WHERE workflows.active = 1 AND config_bundles.active = 1
ORDER BY config_bundles.id DESC, workflows.id DESC
LIMIT 1
`)

	var workflow domain.Workflow
	if err := row.Scan(&workflow.ID, &workflow.Key, &workflow.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Workflow{}, domain.NewError(domain.ErrConfigInvalid, "active workflow not found", nil)
		}
		return domain.Workflow{}, err
	}

	buckets, err := s.workflowBuckets(ctx, workflow.ID)
	if err != nil {
		return domain.Workflow{}, err
	}
	workflow.Buckets = buckets

	transitions, err := s.workflowTransitions(ctx, workflow.ID)
	if err != nil {
		return domain.Workflow{}, err
	}
	workflow.Transitions = transitions

	return workflow, nil
}

func (s *Store) workflowBuckets(ctx context.Context, workflowID int64) ([]domain.Bucket, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, key, name, position FROM workflow_buckets WHERE workflow_id = ? AND active = 1 ORDER BY position, id", workflowID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var buckets []domain.Bucket
	for rows.Next() {
		var bucket domain.Bucket
		if err := rows.Scan(&bucket.ID, &bucket.Key, &bucket.Name, &bucket.Position); err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

func (s *Store) workflowTransitions(ctx context.Context, workflowID int64) ([]domain.WorkflowTransition, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT from_bucket.id, from_bucket.key, to_bucket.id, to_bucket.key
FROM workflow_transitions
JOIN workflow_buckets AS from_bucket ON from_bucket.id = workflow_transitions.from_bucket_id
JOIN workflow_buckets AS to_bucket ON to_bucket.id = workflow_transitions.to_bucket_id
WHERE workflow_transitions.workflow_id = ?
  AND workflow_transitions.active = 1
  AND from_bucket.active = 1
  AND to_bucket.active = 1
ORDER BY from_bucket.position, to_bucket.position
`, workflowID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var transitions []domain.WorkflowTransition
	for rows.Next() {
		var transition domain.WorkflowTransition
		if err := rows.Scan(&transition.FromBucketID, &transition.FromBucketKey, &transition.ToBucketID, &transition.ToBucketKey); err != nil {
			return nil, err
		}
		transitions = append(transitions, transition)
	}
	return transitions, rows.Err()
}

func (s *Store) ContextSettings(ctx context.Context) (domain.ContextSettings, error) {
	settings := domain.ContextSettings{DefaultLevel: 2, MaxTokens: 12000}
	rows, err := s.db.QueryContext(ctx, `
SELECT settings.key, settings.value
FROM settings
JOIN config_bundles ON config_bundles.id = settings.bundle_id
WHERE settings.active = 1
  AND config_bundles.active = 1
  AND settings.key IN ('context.default_level', 'context.max_tokens')
ORDER BY config_bundles.id DESC
`)
	if err != nil {
		return domain.ContextSettings{}, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return domain.ContextSettings{}, err
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return domain.ContextSettings{}, domain.NewError(domain.ErrConfigInvalid, "context setting must be numeric", map[string]any{"key": key, "value": value})
		}
		switch key {
		case "context.default_level":
			settings.DefaultLevel = parsed
		case "context.max_tokens":
			settings.MaxTokens = parsed
		}
	}
	return settings, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, projectID int64, title, description, priority, bucketKey string) (domain.Task, error) {
	if bucketKey == "" {
		workflow, err := s.ActiveWorkflow(ctx)
		if err != nil {
			return domain.Task{}, err
		}
		if len(workflow.Buckets) == 0 {
			return domain.Task{}, domain.NewError(domain.ErrConfigInvalid, "active workflow has no buckets", nil)
		}
		bucketKey = workflow.Buckets[0].Key
	}

	bucketID, err := s.activeBucketID(ctx, bucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	query := `
INSERT INTO tasks(project_id, bucket_id, title, description, priority)
VALUES (?, ?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority
`
	args := []any{projectID, bucketID, title, description, priority}
	if priority == "" {
		query = `
INSERT INTO tasks(project_id, bucket_id, title, description)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority
`
		args = []any{projectID, bucketID, title, description}
	}
	row := s.db.QueryRowContext(ctx, query, args...)

	return scanTask(row, bucketKey)
}

func (s *Store) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error) {
	query := `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ?`
	args := []any{projectID}
	if filter.BucketKey != "" {
		query += " AND workflow_buckets.key = ?"
		args = append(args, filter.BucketKey)
	}
	query += " ORDER BY tasks.id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []domain.Task
	for rows.Next() {
		var task domain.Task
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentBucketID int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(bucket_id, 0) FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&currentBucketID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}

	targetBucketID, err := activeBucketIDTx(ctx, tx, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	if currentBucketID != targetBucketID {
		allowed, err := transitionAllowed(ctx, tx, currentBucketID, targetBucketID)
		if err != nil {
			return domain.Task{}, err
		}
		if !allowed {
			return domain.Task{}, domain.NewError(domain.ErrWorkflowInvalidTransition, "transition not allowed", map[string]any{"task_id": taskID, "from": currentBucketID, "to": targetBucketID})
		}
		if err := evaluateTransitionGuards(ctx, tx, projectID, taskID, currentBucketID, targetBucketID); err != nil {
			return domain.Task{}, err
		}
	}

	row := tx.QueryRowContext(ctx, `
UPDATE tasks SET bucket_id = ?, updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?
RETURNING id, project_id, bucket_id, title, description, priority
`, targetBucketID, projectID, taskID)

	task, err := scanTask(row, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	return task, tx.Commit()
}

func (s *Store) UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate) (domain.Task, error) {
	sets := []string{}
	args := []any{}
	if update.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *update.Title)
	}
	if update.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *update.Description)
	}
	if update.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, string(*update.Priority))
	}
	if len(sets) > 0 {
		args = append(args, projectID, taskID)
		result, err := s.db.ExecContext(ctx, "UPDATE tasks SET "+strings.Join(sets, ", ")+
			", updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?", args...)
		if err != nil {
			return domain.Task{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return domain.Task{}, err
		}
		if changed == 0 {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
	}

	return s.taskByID(ctx, projectID, taskID)
}

func (s *Store) TaskCount(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) AddComment(ctx context.Context, projectID, taskID int64, body, authorType string, tags []domain.Tag) (domain.Comment, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.Comment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Comment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var comment domain.Comment
	if err := tx.QueryRowContext(ctx, `
INSERT INTO comments(project_id, task_id, body, author_type)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, task_id, body, author_type, created_at
`, projectID, taskID, body, authorType).Scan(&comment.ID, &comment.ProjectID, &comment.TaskID, &comment.Body, &comment.AuthorType, &comment.CreatedAt); err != nil {
		return domain.Comment{}, err
	}

	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(name, label) VALUES (?, ?)`, tag.Name, tag.Label); err != nil {
			return domain.Comment{}, err
		}
		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, tag.Name).Scan(&tagID); err != nil {
			return domain.Comment{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO comment_tags(comment_id, tag_id) VALUES (?, ?)`, comment.ID, tagID); err != nil {
			return domain.Comment{}, err
		}
		comment.Tags = append(comment.Tags, domain.Tag{ID: tagID, Name: tag.Name, Label: tag.Label})
	}

	return comment, tx.Commit()
}

func (s *Store) ListComments(ctx context.Context, projectID, taskID int64) ([]domain.Comment, error) {
	query := "SELECT id, project_id, task_id, body, author_type, created_at FROM comments WHERE project_id = ?"
	args := []any{projectID}
	if taskID > 0 {
		if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
			return nil, err
		}
		query += " AND task_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var comments []domain.Comment
	for rows.Next() {
		var comment domain.Comment
		if err := rows.Scan(&comment.ID, &comment.ProjectID, &comment.TaskID, &comment.Body, &comment.AuthorType, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(comments) > 0 {
		ids := make([]int64, len(comments))
		for i, c := range comments {
			ids[i] = c.ID
		}
		tagsByComment, err := s.commentTagsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i := range comments {
			if tags, ok := tagsByComment[comments[i].ID]; ok {
				comments[i].Tags = tags
			}
		}
	}

	return comments, nil
}

func (s *Store) commentTagsByIDs(ctx context.Context, commentIDs []int64) (map[int64][]domain.Tag, error) {
	if len(commentIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(commentIDs))
	args := make([]any, len(commentIDs))
	for i, id := range commentIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT ct.comment_id, t.id, t.name, t.label FROM comment_tags ct JOIN tags t ON t.id = ct.tag_id WHERE ct.comment_id IN ("+strings.Join(placeholders, ",")+")",
		args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Tag{}
	for rows.Next() {
		var commentID int64
		var tag domain.Tag
		if err := rows.Scan(&commentID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		result[commentID] = append(result[commentID], tag)
	}
	return result, rows.Err()
}

func (s *Store) AddTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) (domain.TaskDependency, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.TaskDependency{}, err
	}
	if err := s.ensureTaskExists(ctx, projectID, dependsOnTaskID); err != nil {
		return domain.TaskDependency{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_dependencies(project_id, task_id, depends_on_task_id)
VALUES (?, ?, ?)
ON CONFLICT(project_id, task_id, depends_on_task_id) DO NOTHING
`, projectID, taskID, dependsOnTaskID); err != nil {
		return domain.TaskDependency{}, err
	}
	return domain.TaskDependency{ProjectID: projectID, TaskID: taskID, DependsOnTaskID: dependsOnTaskID}, nil
}

func (s *Store) RemoveTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM task_dependencies WHERE project_id = ? AND task_id = ? AND depends_on_task_id = ?", projectID, taskID, dependsOnTaskID)
	return err
}

func (s *Store) ListTaskDependencies(ctx context.Context, projectID, taskID int64) ([]domain.TaskDependency, error) {
	query := "SELECT project_id, task_id, depends_on_task_id FROM task_dependencies WHERE project_id = ?"
	args := []any{projectID}
	if taskID > 0 {
		if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
			return nil, err
		}
		query += " AND task_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY task_id, depends_on_task_id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var dependencies []domain.TaskDependency
	for rows.Next() {
		var dependency domain.TaskDependency
		if err := rows.Scan(&dependency.ProjectID, &dependency.TaskID, &dependency.DependsOnTaskID); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}

func (s *Store) AddContextEntry(ctx context.Context, projectID int64, body string, tokenEstimate int) (domain.ContextEntry, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO context_entries(project_id, body, token_estimate)
VALUES (?, ?, ?)
RETURNING id, project_id, body, token_estimate, created_at
`, projectID, body, tokenEstimate)
	return scanContextEntry(row)
}

func (s *Store) ListContextEntries(ctx context.Context, projectID int64) ([]domain.ContextEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, project_id, body, token_estimate, created_at FROM context_entries WHERE project_id = ? ORDER BY id DESC", projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.ContextEntry
	for rows.Next() {
		var entry domain.ContextEntry
		if err := rows.Scan(&entry.ID, &entry.ProjectID, &entry.Body, &entry.TokenEstimate, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) activeBucketID(ctx context.Context, key string) (int64, error) {
	return activeBucketIDQuery(ctx, s.db, key)
}

func activeBucketIDTx(ctx context.Context, tx *sql.Tx, key string) (int64, error) {
	return activeBucketIDQuery(ctx, tx, key)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func activeBucketIDQuery(ctx context.Context, q queryer, key string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `
SELECT workflow_buckets.id
FROM workflow_buckets
JOIN workflows ON workflows.id = workflow_buckets.workflow_id
JOIN config_bundles ON config_bundles.id = workflows.bundle_id
JOIN settings ON settings.bundle_id = config_bundles.id
  AND settings.key = 'workflow.active'
  AND settings.value = workflows.key
  AND settings.active = 1
WHERE workflow_buckets.key = ?
  AND workflow_buckets.active = 1
  AND workflows.active = 1
  AND config_bundles.active = 1
ORDER BY config_bundles.id DESC, workflows.id DESC
LIMIT 1
`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
	}
	return id, err
}

func transitionAllowed(ctx context.Context, tx *sql.Tx, fromBucketID, toBucketID int64) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM workflow_transitions
WHERE from_bucket_id = ? AND to_bucket_id = ? AND active = 1
`, fromBucketID, toBucketID).Scan(&count)
	return count > 0, err
}

func scanTask(row *sql.Row, bucketKey string) (domain.Task, error) {
	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", nil)
		}
		return domain.Task{}, err
	}
	task.BucketKey = bucketKey
	return task, nil
}

func (s *Store) taskByID(ctx context.Context, projectID, taskID int64) (domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}
	return task, nil
}

func (s *Store) ensureTaskExists(ctx context.Context, projectID, taskID int64) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
	}
	return nil
}

func scanComment(row *sql.Row) (domain.Comment, error) {
	var comment domain.Comment
	if err := row.Scan(&comment.ID, &comment.ProjectID, &comment.TaskID, &comment.Body, &comment.AuthorType, &comment.CreatedAt); err != nil {
		return domain.Comment{}, err
	}
	return comment, nil
}

func scanContextEntry(row *sql.Row) (domain.ContextEntry, error) {
	var entry domain.ContextEntry
	if err := row.Scan(&entry.ID, &entry.ProjectID, &entry.Body, &entry.TokenEstimate, &entry.CreatedAt); err != nil {
		return domain.ContextEntry{}, err
	}
	return entry, nil
}

type transitionGuard struct {
	Type    string   `json:"type"`
	Buckets []string `json:"buckets,omitempty"`
	Count   int      `json:"count,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

func evaluateTransitionGuards(ctx context.Context, tx *sql.Tx, projectID, taskID, fromBucketID, toBucketID int64) error {
	var guardsJSON string
	err := tx.QueryRowContext(ctx, `
SELECT guards_json FROM workflow_transitions
WHERE from_bucket_id = ? AND to_bucket_id = ? AND active = 1
LIMIT 1
`, fromBucketID, toBucketID).Scan(&guardsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	var guards []transitionGuard
	if err := json.Unmarshal([]byte(guardsJSON), &guards); err != nil {
		return err
	}

	for _, guard := range guards {
		switch guard.Type {
		case "blockers_in":
			if err := checkBlockersIn(ctx, tx, projectID, taskID, guard.Buckets, guard.Hint); err != nil {
				return err
			}
		case "comments_min":
			if err := checkCommentsMin(ctx, tx, projectID, taskID, guard.Count, guard.Hint); err != nil {
				return err
			}
		case "comments_tagged":
			if err := checkCommentsTagged(ctx, tx, projectID, taskID, guard.Tag, guard.Count, guard.Hint); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkBlockersIn(ctx context.Context, tx *sql.Tx, projectID, taskID int64, allowedKeys []string, hint string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT t.id, t.title, COALESCE(wb.key, '') AS bucket_key
FROM task_dependencies td
JOIN tasks t ON t.project_id = td.project_id AND t.id = td.depends_on_task_id
LEFT JOIN workflow_buckets wb ON wb.id = t.bucket_id AND wb.active = 1
WHERE td.project_id = ? AND td.task_id = ?
ORDER BY t.id
`, projectID, taskID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, k := range allowedKeys {
		allowed[k] = struct{}{}
	}

	var pending []string
	for rows.Next() {
		var id int64
		var title, bucketKey string
		if err := rows.Scan(&id, &title, &bucketKey); err != nil {
			return err
		}
		if _, ok := allowed[bucketKey]; !ok {
			pending = append(pending, fmt.Sprintf("#%d %q (in %q)", id, title, bucketKey))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) > 0 {
		msg := fmt.Sprintf("blockers_in guard: pending blockers: %s", strings.Join(pending, ", "))
		details := map[string]any{"pending_blockers": pending}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}

func checkCommentsMin(ctx context.Context, tx *sql.Tx, projectID, taskID int64, minCount int, hint string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(1) FROM comments WHERE project_id = ? AND task_id = ?
`, projectID, taskID).Scan(&count); err != nil {
		return err
	}
	if count < minCount {
		msg := fmt.Sprintf("comments_min guard: task has %d comment(s); transition requires at least %d", count, minCount)
		details := map[string]any{"count": count, "required": minCount}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}

func checkCommentsTagged(ctx context.Context, tx *sql.Tx, projectID, taskID int64, tagName string, minCount int, hint string) error {
	if minCount < 1 {
		minCount = 1
	}
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT c.id)
FROM comments c
JOIN comment_tags ct ON ct.comment_id = c.id
JOIN tags t ON t.id = ct.tag_id
WHERE c.project_id = ? AND c.task_id = ? AND t.name = ?
`, projectID, taskID, tagName).Scan(&count); err != nil {
		return err
	}
	if count < minCount {
		msg := fmt.Sprintf("comments_tagged guard: task has %d comment(s) tagged %q; transition requires at least %d", count, tagName, minCount)
		details := map[string]any{"count": count, "required": minCount, "tag": tagName}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}
