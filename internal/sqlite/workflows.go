package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

// CurrentTaskBucket returns the bucket id and key for a task. The bucket
// id comes from the `tasks` row; the key is resolved through the
// per-Store Snapshot so renames in the active bundle propagate without a
// schema migration. Returns ErrTaskNotFound when the task is not in the
// project; returns ("", nil) when the task carries no bucket assignment
// (legacy rows pre workflow gating) so callers can fall through to the
// workflow's default-bucket logic.
func (s *Store) CurrentTaskBucket(ctx context.Context, projectID, taskID int64) (int64, string, error) {
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
	bucket, ok := s.Snapshot().BucketByID(bucketID)
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

// activeBucketID resolves a bucket key to its id via the in-memory
// Snapshot. Used by the connection-pool and in-tx code paths alike —
// the snapshot read does not touch the SQL connection so it is safe
// from either context.
func (s *Store) activeBucketID(_ context.Context, key string) (int64, error) {
	b, ok := s.Snapshot().BucketByKey(key)
	if !ok {
		return 0, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
	}
	return b.ID, nil
}

// bucketKeyByID resolves a bucket id to its key via the in-memory
// Snapshot. Returns "" when the id is 0 or the bucket is missing from
// the snapshot (matches the pre-migration "no rows -> empty key"
// behaviour the move-event recorder depends on).
func (s *Store) bucketKeyByID(bucketID int64) string {
	if bucketID == 0 {
		return ""
	}
	bucket, ok := s.Snapshot().BucketByID(bucketID)
	if !ok {
		return ""
	}
	return bucket.Key
}
