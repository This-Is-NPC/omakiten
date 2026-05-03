package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	if err := importSkills(ctx, tx, bundleID, bundle.Skills); err != nil {
		return err
	}
	if err := importPersonas(ctx, tx, bundleID, bundle.Personas); err != nil {
		return err
	}
	if err := importLaws(ctx, tx, bundleID, bundle.Laws); err != nil {
		return err
	}
	if err := importWorkflows(ctx, tx, bundleID, bundle.Workflows); err != nil {
		return err
	}

	return tx.Commit()
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
	if _, err := tx.ExecContext(ctx, "UPDATE skills SET active = 0 WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for _, skill := range skills {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO skills(bundle_id, local_id, key, name, active) VALUES (?, ?, ?, ?, 1)
ON CONFLICT(bundle_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, active = 1
`, bundleID, skill.ID, skill.Key, skill.Name); err != nil {
			return err
		}
	}
	return nil
}

func importPersonas(ctx context.Context, tx *sql.Tx, bundleID int64, personas []config.Persona) error {
	if _, err := tx.ExecContext(ctx, "UPDATE personas SET active = 0 WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for _, persona := range personas {
		var personaID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO personas(bundle_id, local_id, key, name, active) VALUES (?, ?, ?, ?, 1)
ON CONFLICT(bundle_id, local_id) DO UPDATE SET key = excluded.key, name = excluded.name, active = 1
RETURNING id
`, bundleID, persona.ID, persona.Key, persona.Name).Scan(&personaID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM persona_skills WHERE persona_id = ?", personaID); err != nil {
			return err
		}
		for _, skillLocalID := range persona.SkillIDs {
			var skillID int64
			if err := tx.QueryRowContext(ctx, "SELECT id FROM skills WHERE bundle_id = ? AND local_id = ? AND active = 1", bundleID, skillLocalID).Scan(&skillID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO persona_skills(persona_id, skill_id) VALUES (?, ?)", personaID, skillID); err != nil {
				return err
			}
		}
	}
	return nil
}

func importLaws(ctx context.Context, tx *sql.Tx, bundleID int64, laws []config.Law) error {
	if _, err := tx.ExecContext(ctx, "UPDATE laws SET active = 0 WHERE bundle_id = ?", bundleID); err != nil {
		return err
	}
	for _, law := range laws {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO laws(bundle_id, local_id, key, severity, body, active) VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT(bundle_id, local_id) DO UPDATE SET key = excluded.key, severity = excluded.severity, body = excluded.body, active = 1
`, bundleID, law.ID, law.Key, law.Severity, law.Body); err != nil {
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
			if _, err := tx.ExecContext(ctx, `
INSERT INTO workflow_transitions(workflow_id, from_bucket_id, to_bucket_id, active) VALUES (?, ?, ?, 1)
ON CONFLICT(workflow_id, from_bucket_id, to_bucket_id) DO UPDATE SET active = 1
`, workflowID, fromID, toID); err != nil {
				return err
			}
		}
	}

	return nil
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

func (s *Store) CreateTask(ctx context.Context, projectID int64, title, description, bucketKey string) (domain.Task, error) {
	if bucketKey == "" {
		bucketKey = "backlog"
	}

	bucketID, err := s.activeBucketID(ctx, bucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority
`, projectID, bucketID, title, description)

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

func (s *Store) TaskCount(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
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
		return domain.Task{}, err
	}
	task.BucketKey = bucketKey
	return task, nil
}
