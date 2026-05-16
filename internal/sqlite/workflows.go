package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

// CurrentTaskBucket returns the bucket id and key for a task. The bucket
// id comes from the `tasks` row; the key is resolved through the
// caller-supplied resolver so renames in the active bundle propagate
// without a schema migration. Returns ErrTaskNotFound when the task is
// not in the project; returns ("", nil) when the task carries no bucket
// assignment (legacy rows pre workflow gating) so callers can fall
// through to the workflow's default-bucket logic.
func (s *Store) CurrentTaskBucket(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (int64, string, error) {
	var bucketID int64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(t.bucket_id, 0)
FROM tasks t
WHERE t.project_id = ? AND t.id = ?
`, projectID, taskID).Scan(&bucketID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return 0, "", err
	}
	if bucketID == 0 {
		return 0, "", nil
	}
	if isNilResolver(buckets) {
		return bucketID, "", nil
	}
	bucket, ok := buckets.BucketByID(bucketID)
	if !ok {
		return bucketID, "", nil
	}
	return bucketID, bucket.Key, nil
}

// TaskState returns the active|archived flag for a task.
func (s *Store) TaskState(ctx context.Context, projectID, taskID int64) (domain.TaskState, error) {
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID).Scan(&state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return "", err
	}
	return domain.TaskState(state), nil
}
