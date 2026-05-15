package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

// ActiveWorkflow delegates to the in-memory provider snapshot. The SQL
// workflow tables (`workflows`, `workflow_buckets`, `workflow_transitions`,
// `transition_guards`) were dropped in migration 020. Returns
// ErrConfigInvalid when no workflow is loaded — production always loads
// one via ConfigService.Import before any reader hits this path.
func (s *Store) ActiveWorkflow(ctx context.Context) (domain.Workflow, error) {
	wf := s.Providers().Workflow()
	if wf.Key == "" {
		return domain.Workflow{}, domain.NewError(domain.ErrConfigInvalid, "active workflow not found", nil)
	}
	return wf, nil
}

// ResolveActiveBucket delegates to the in-memory provider.
func (s *Store) ResolveActiveBucket(ctx context.Context, key string) (domain.Bucket, error) {
	b, ok := s.Providers().BucketByKey(key)
	if !ok {
		return domain.Bucket{}, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
	}
	return b, nil
}

// IsFinalActiveBucket delegates to the in-memory provider.
func (s *Store) IsFinalActiveBucket(ctx context.Context, bucketID int64) (bool, error) {
	return s.Providers().IsFinalBucket(bucketID), nil
}

// TransitionAllowed delegates to the in-memory provider.
func (s *Store) TransitionAllowed(ctx context.Context, fromBucketID, toBucketID int64) (bool, error) {
	return s.Providers().TransitionAllowed(fromBucketID, toBucketID), nil
}

// LoadTransitionGuards delegates to the in-memory provider.
func (s *Store) LoadTransitionGuards(ctx context.Context, fromBucketID, toBucketID int64) ([]domain.TransitionGuard, error) {
	return s.Providers().Guards(fromBucketID, toBucketID), nil
}

// CurrentTaskBucket returns the bucket id and key for a task. Reads
// `tasks` (state), resolves the key via the provider (no JOIN against
// the dropped `workflow_buckets` table). Returns ErrTaskNotFound when
// the task is not in the project.
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
	bucket, ok := s.Providers().BucketByID(bucketID)
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
// provider. Used by both the connection-pool path and the in-tx path —
// the provider call does not touch the SQL connection so it is safe
// from either context.
func (s *Store) activeBucketID(_ context.Context, key string) (int64, error) {
	b, ok := s.Providers().BucketByKey(key)
	if !ok {
		return 0, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
	}
	return b.ID, nil
}

// bucketKeyByID resolves a bucket id to its key via the in-memory
// provider. Returns "" when the id is 0 or the bucket is missing from
// the snapshot (matches the pre-migration "no rows -> empty key"
// behaviour the move-event recorder depends on).
func (s *Store) bucketKeyByID(bucketID int64) string {
	if bucketID == 0 {
		return ""
	}
	bucket, ok := s.Providers().BucketByID(bucketID)
	if !ok {
		return ""
	}
	return bucket.Key
}
