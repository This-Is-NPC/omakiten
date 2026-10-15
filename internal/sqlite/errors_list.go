package sqlite

import (
	"context"
	"strings"

	"omakiten/internal/domain"
)

const errorListLimit = 200

// ListErrors returns the project's errors, newest first, each with its tags.
// A non-empty tagName keeps only errors carrying that tag. Results are capped
// at errorListLimit.
func (s *Store) ListErrors(ctx context.Context, projectID int64, tagName string) ([]domain.ErrorRecord, error) {
	query := `
SELECT e.id, e.description, e.context, COALESCE(e.project_id, 0), e.created_at
FROM errors e
WHERE e.project_id = ?`
	args := []any{projectID}
	if tagName != "" {
		query += `
  AND EXISTS (SELECT 1 FROM error_tags et JOIN tags t ON t.id = et.tag_id WHERE et.error_id = e.id AND t.name = ?)`
		args = append(args, tagName)
	}
	query += `
ORDER BY e.id DESC
LIMIT ?`
	args = append(args, errorListLimit)
	rows, err := s.query(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []domain.ErrorRecord
	index := map[int64]int{}
	for rows.Next() {
		var record domain.ErrorRecord
		if err := rows.Scan(&record.ID, &record.Description, &record.Context, &record.ProjectID, &record.CreatedAt); err != nil {
			return nil, err
		}
		index[record.ID] = len(records)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil || len(records) == 0 {
		return records, err
	}
	return records, s.attachErrorTags(ctx, records, index)
}

// attachErrorTags loads the tags of every record in one query.
func (s *Store) attachErrorTags(ctx context.Context, records []domain.ErrorRecord, index map[int64]int) error {
	args := make([]any, len(records))
	for i, record := range records {
		args[i] = record.ID
	}
	rows, err := s.query(ctx).QueryContext(ctx, `
SELECT et.error_id, t.id, t.name, t.label
FROM error_tags et
JOIN tags t ON t.id = et.tag_id
WHERE et.error_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)
ORDER BY et.error_id, t.name`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var errorID int64
		var tag domain.Tag
		if err := rows.Scan(&errorID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return err
		}
		i := index[errorID]
		records[i].Tags = append(records[i].Tags, tag)
	}
	return rows.Err()
}
