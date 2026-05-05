package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

func (s *Store) FindOrCreateTag(ctx context.Context, name, label string) (domain.Tag, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Tag{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(name, label) VALUES (?, ?)`, name, label); err != nil {
		return domain.Tag{}, err
	}

	var tag domain.Tag
	if err := tx.QueryRowContext(ctx, `SELECT id, name, label FROM tags WHERE name = ?`, name).
		Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
		return domain.Tag{}, err
	}

	return tag, tx.Commit()
}

func (s *Store) ListAllTags(ctx context.Context) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.name, t.label,
  (SELECT COUNT(*) FROM task_tags WHERE tag_id = t.id) +
  (SELECT COUNT(*) FROM project_tags WHERE tag_id = t.id) +
  (SELECT COUNT(*) FROM comment_tags WHERE tag_id = t.id) AS usage_count
FROM tags t
ORDER BY usage_count DESC, t.name
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []domain.Tag
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Label, &tag.UsageCount); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *Store) RenameTag(ctx context.Context, tagID int64, newLabel string) (domain.Tag, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE tags SET label = ? WHERE id = ?
RETURNING id, name, label
`, newLabel, tagID)

	var tag domain.Tag
	if err := row.Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Tag{}, domain.NewError(domain.ErrTagNotFound, "tag not found", map[string]any{"tag_id": tagID})
		}
		return domain.Tag{}, err
	}
	return tag, nil
}

func (s *Store) MergeTags(ctx context.Context, sourceTagID, targetTagID int64) (domain.Tag, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Tag{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// Reassign task_tags from source to target (ignore conflicts — already linked)
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO task_tags(project_id, task_id, tag_id)
SELECT project_id, task_id, ? FROM task_tags WHERE tag_id = ?
`, targetTagID, sourceTagID); err != nil {
		return domain.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_tags WHERE tag_id = ?`, sourceTagID); err != nil {
		return domain.Tag{}, err
	}

	// Reassign project_tags
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO project_tags(project_id, tag_id)
SELECT project_id, ? FROM project_tags WHERE tag_id = ?
`, targetTagID, sourceTagID); err != nil {
		return domain.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_tags WHERE tag_id = ?`, sourceTagID); err != nil {
		return domain.Tag{}, err
	}

	// Reassign comment_tags
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO comment_tags(comment_id, tag_id)
SELECT comment_id, ? FROM comment_tags WHERE tag_id = ?
`, targetTagID, sourceTagID); err != nil {
		return domain.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comment_tags WHERE tag_id = ?`, sourceTagID); err != nil {
		return domain.Tag{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, sourceTagID); err != nil {
		return domain.Tag{}, err
	}

	var tag domain.Tag
	if err := tx.QueryRowContext(ctx, `SELECT id, name, label FROM tags WHERE id = ?`, targetTagID).
		Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Tag{}, domain.NewError(domain.ErrTagNotFound, "target tag not found", map[string]any{"tag_id": targetTagID})
		}
		return domain.Tag{}, err
	}

	return tag, tx.Commit()
}

func (s *Store) DeleteOrphanTags(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM tags WHERE id NOT IN (
  SELECT tag_id FROM task_tags
  UNION
  SELECT tag_id FROM project_tags
  UNION
  SELECT tag_id FROM comment_tags
)
`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) AddTaskTag(ctx context.Context, projectID, taskID, tagID int64) error {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO task_tags(project_id, task_id, tag_id)
VALUES (?, ?, ?)
ON CONFLICT(project_id, task_id, tag_id) DO NOTHING
`, projectID, taskID, tagID)
	return err
}

func (s *Store) RemoveTaskTag(ctx context.Context, projectID, taskID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM task_tags WHERE project_id = ? AND task_id = ? AND tag_id = ?`, projectID, taskID, tagID)
	return err
}

func (s *Store) ListTaskTags(ctx context.Context, projectID, taskID int64) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.name, t.label
FROM tags t
JOIN task_tags tt ON tt.tag_id = t.id
WHERE tt.project_id = ? AND tt.task_id = ?
ORDER BY t.name
`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []domain.Tag
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *Store) ListTaskTagsByProject(ctx context.Context, projectID int64) (map[int64][]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tt.task_id, t.id, t.name, t.label
FROM task_tags tt
JOIN tags t ON t.id = tt.tag_id
WHERE tt.project_id = ?
ORDER BY tt.task_id, t.name
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Tag{}
	for rows.Next() {
		var taskID int64
		var tag domain.Tag
		if err := rows.Scan(&taskID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		result[taskID] = append(result[taskID], tag)
	}
	return result, rows.Err()
}

func (s *Store) AddProjectTag(ctx context.Context, projectID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_tags(project_id, tag_id)
VALUES (?, ?)
ON CONFLICT(project_id, tag_id) DO NOTHING
`, projectID, tagID)
	return err
}

func (s *Store) RemoveProjectTag(ctx context.Context, projectID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_tags WHERE project_id = ? AND tag_id = ?`, projectID, tagID)
	return err
}

func (s *Store) ListProjectTags(ctx context.Context, projectID int64) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.name, t.label
FROM tags t
JOIN project_tags pt ON pt.tag_id = t.id
WHERE pt.project_id = ?
ORDER BY t.name
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []domain.Tag
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
