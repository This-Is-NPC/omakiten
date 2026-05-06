package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

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

func (s *Store) activeBucketID(ctx context.Context, key string) (int64, error) {
	return activeBucketIDQuery(ctx, s.db, key)
}

func activeBucketIDTx(ctx context.Context, tx *sql.Tx, key string) (int64, error) {
	return activeBucketIDQuery(ctx, tx, key)
}

// queryer abstracts *sql.DB and *sql.Tx so the bucket lookup can be reused
// from both the connection pool and a transaction without duplicating SQL.
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

// bucketKeyByIDTx resolves a bucket id to its key inside the active workflow.
// Returns "" when the id is 0 (task had no bucket yet — happens only with
// legacy data; the move event records "" as the source bucket then).
func bucketKeyByIDTx(ctx context.Context, tx *sql.Tx, bucketID int64) (string, error) {
	if bucketID == 0 {
		return "", nil
	}
	var key string
	err := tx.QueryRowContext(ctx, "SELECT key FROM workflow_buckets WHERE id = ? AND active = 1", bucketID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return key, nil
}

// isFinalBucketTx reports whether the given bucket is the highest-position
// bucket in its active workflow — i.e. the destination that triggers a
// task.completed event in addition to task.moved.
func isFinalBucketTx(ctx context.Context, tx *sql.Tx, bucketID int64) (bool, error) {
	var workflowID int64
	var position int
	err := tx.QueryRowContext(ctx, `SELECT workflow_id, position FROM workflow_buckets WHERE id = ? AND active = 1`, bucketID).Scan(&workflowID, &position)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var maxPosition int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) FROM workflow_buckets WHERE workflow_id = ? AND active = 1`, workflowID).Scan(&maxPosition); err != nil {
		return false, err
	}
	return position == maxPosition, nil
}
