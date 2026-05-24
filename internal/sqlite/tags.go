package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// tagPivot enumerates the join tables that link a tag to an entity. The
// values are kept inside the package so attachTagsTx can switch on a closed
// set rather than concatenate caller-supplied table names into SQL.
type tagPivot int

const (
	tagPivotEvent tagPivot = iota
	tagPivotError
)

// attachTagsTx ensures each tag exists (insert-or-ignore by name), reads
// back the canonical (id, label) pair, and links it to the entity through
// the supplied pivot. Returns the resolved tags so the caller can echo them
// back on the entity it just wrote. Pivot and entity column are enumerated
// here so SQL stays string-literal — callers cannot inject a third table.
//
// Used by AddComment / UpdateComment (event_tags) and RecordError
// (error_tags); each previously inlined the same loop.
func attachTagsTx(ctx context.Context, tx *sql.Tx, pivot tagPivot, entityID int64, tags []domain.Tag) ([]domain.Tag, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	out := make([]domain.Tag, 0, len(tags))
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(name, label) VALUES (?, ?)`, tag.Name, tag.Label); err != nil {
			return nil, err
		}
		var stored domain.Tag
		if err := tx.QueryRowContext(ctx, `SELECT id, name, label FROM tags WHERE name = ?`, tag.Name).Scan(&stored.ID, &stored.Name, &stored.Label); err != nil {
			return nil, err
		}
		switch pivot {
		case tagPivotEvent:
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO event_tags(event_id, tag_id) VALUES (?, ?)`, entityID, stored.ID); err != nil {
				return nil, err
			}
		case tagPivotError:
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO error_tags(error_id, tag_id) VALUES (?, ?)`, entityID, stored.ID); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("attachTagsTx: unknown pivot")
		}
		out = append(out, stored)
	}
	return out, nil
}

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
  (SELECT COUNT(*) FROM event_tags WHERE tag_id = t.id) +
  (SELECT COUNT(*) FROM error_tags WHERE tag_id = t.id) AS usage_count
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

	// Reassign event_tags (covers comments, which now live in events)
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO event_tags(event_id, tag_id)
SELECT event_id, ? FROM event_tags WHERE tag_id = ?
`, targetTagID, sourceTagID); err != nil {
		return domain.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE tag_id = ?`, sourceTagID); err != nil {
		return domain.Tag{}, err
	}

	// Reassign error_tags
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO error_tags(error_id, tag_id)
SELECT error_id, ? FROM error_tags WHERE tag_id = ?
`, targetTagID, sourceTagID); err != nil {
		return domain.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM error_tags WHERE tag_id = ?`, sourceTagID); err != nil {
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
  SELECT tag_id FROM event_tags
  UNION
  SELECT tag_id FROM error_tags
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
	return mapTagAttachError(err, "tag_id", tagID)
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
	return mapTagAttachError(err, "tag_id", tagID)
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

// mapTagAttachError surfaces SQLITE_CONSTRAINT_FOREIGNKEY on the tag
// attach paths (AddTaskTag / AddProjectTag) as a typed
// domain.ErrValidation so caller-supplied phantom tag IDs no longer
// leak the driver message into the agent envelope as a generic
// internal error. Non-constraint errors pass through unchanged so
// genuine I/O failures still surface as such.
func mapTagAttachError(err error, fieldName string, fieldValue any) error {
	if err == nil {
		return nil
	}
	var ce *sqlutil.ConstraintError
	mapped := sqlutil.MapSQLiteError(err)
	if !errors.As(mapped, &ce) {
		return err
	}
	if ce.Violation == sqlutil.ViolationForeignKey {
		return domain.NewError(domain.ErrValidation, "referenced row does not exist",
			map[string]any{"field": fieldName, "value": fieldValue})
	}
	return err
}
