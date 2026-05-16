package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

// ListTaskBlockerBuckets returns the (id, title, bucket_key) triples for every
// task that taskID depends on. Used by the blockers_in workflow guard at the
// app layer to decide which blockers still sit in disallowed buckets.
func (s *Store) ListTaskBlockerBuckets(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) ([]domain.TaskBlocker, error) {
	// `workflow_buckets` was dropped in migration 020; the previous JOIN
	// resolved bucket key per blocker. We now scan blocker rows with
	// bucket_id and resolve key via the in-memory provider after the
	// SQL completes.
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.title, COALESCE(t.bucket_id, 0)
FROM task_dependencies td
JOIN tasks t ON t.project_id = td.project_id AND t.id = td.depends_on_task_id
WHERE td.project_id = ? AND td.task_id = ?
ORDER BY t.id
`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var blockers []domain.TaskBlocker
	for rows.Next() {
		var b domain.TaskBlocker
		var bucketID int64
		if err := rows.Scan(&b.TaskID, &b.Title, &bucketID); err != nil {
			return nil, err
		}
		b.BucketKey = s.bucketKeyByID(bucketID, buckets)
		blockers = append(blockers, b)
	}
	return blockers, rows.Err()
}

// CountTaskComments returns the count of comment events for a task. Used by
// the comments_min workflow guard at the app layer.
func (s *Store) CountTaskComments(ctx context.Context, projectID, taskID int64) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1) FROM events WHERE entity_type = 'task' AND event_type = 'comment' AND project_id = ? AND entity_id = ?
`, projectID, taskID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// CountTaskCommentsTagged returns the count of distinct comment events on a
// task that carry the given tag. Used by the comments_tagged workflow guard.
func (s *Store) CountTaskCommentsTagged(ctx context.Context, projectID, taskID int64, tagName string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT e.id)
FROM events e
JOIN event_tags et ON et.event_id = e.id
JOIN tags t ON t.id = et.tag_id
WHERE e.entity_type = 'task' AND e.event_type = 'comment' AND e.project_id = ? AND e.entity_id = ? AND t.name = ?
`, projectID, taskID, tagName).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
