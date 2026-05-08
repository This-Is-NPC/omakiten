package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"omakiten/internal/domain"
)

func (s *Store) ActiveWorkflow(ctx context.Context) (domain.Workflow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT workflows.id, workflows.key, workflows.name, COALESCE(workflows.operations_json, '{}')
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
	var operationsJSON string
	if err := row.Scan(&workflow.ID, &workflow.Key, &workflow.Name, &operationsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Workflow{}, domain.NewError(domain.ErrConfigInvalid, "active workflow not found", nil)
		}
		return domain.Workflow{}, err
	}
	if operationsJSON != "" && operationsJSON != "{}" {
		if err := json.Unmarshal([]byte(operationsJSON), &workflow.Operations); err != nil {
			return domain.Workflow{}, err
		}
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
	rows, err := s.db.QueryContext(ctx, "SELECT id, key, name, position, COALESCE(permissions_json, '{}') FROM workflow_buckets WHERE workflow_id = ? AND active = 1 ORDER BY position, id", workflowID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var buckets []domain.Bucket
	for rows.Next() {
		var bucket domain.Bucket
		var permissionsJSON string
		if err := rows.Scan(&bucket.ID, &bucket.Key, &bucket.Name, &bucket.Position, &permissionsJSON); err != nil {
			return nil, err
		}
		if permissionsJSON != "" && permissionsJSON != "{}" {
			var perms domain.BucketPermissions
			if err := json.Unmarshal([]byte(permissionsJSON), &perms); err != nil {
				return nil, err
			}
			bucket.Permissions = &perms
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

// ResolveActiveBucket looks up a bucket by key in the active workflow and
// returns its full record (id, key, name, position). The app workflow service
// uses this both to resolve the target of a move and to drive the default
// bucket selection on task create.
func (s *Store) ResolveActiveBucket(ctx context.Context, key string) (domain.Bucket, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT workflow_buckets.id, workflow_buckets.key, workflow_buckets.name, workflow_buckets.position
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
`, key)
	var bucket domain.Bucket
	if err := row.Scan(&bucket.ID, &bucket.Key, &bucket.Name, &bucket.Position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Bucket{}, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
		}
		return domain.Bucket{}, err
	}
	return bucket, nil
}

// IsFinalActiveBucket reports whether the given bucket is the highest-position
// bucket in its active workflow. The app workflow service uses this to decide
// whether a move into a bucket should additionally emit a task.completed event.
func (s *Store) IsFinalActiveBucket(ctx context.Context, bucketID int64) (bool, error) {
	var workflowID int64
	var position int
	err := s.db.QueryRowContext(ctx, `SELECT workflow_id, position FROM workflow_buckets WHERE id = ? AND active = 1`, bucketID).Scan(&workflowID, &position)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var maxPosition int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) FROM workflow_buckets WHERE workflow_id = ? AND active = 1`, workflowID).Scan(&maxPosition); err != nil {
		return false, err
	}
	return position == maxPosition, nil
}

// TransitionAllowed reports whether the active workflow declares a transition
// between the two bucket ids. App-layer workflow policy decides what to do
// when the answer is false (typically a domain.ErrWorkflowInvalidTransition).
func (s *Store) TransitionAllowed(ctx context.Context, fromBucketID, toBucketID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM workflow_transitions
WHERE from_bucket_id = ? AND to_bucket_id = ? AND active = 1
`, fromBucketID, toBucketID).Scan(&count)
	return count > 0, err
}

// LoadTransitionGuards returns the parsed guard list attached to the matching
// transition. An empty list (or "no rows") means no guards apply.
func (s *Store) LoadTransitionGuards(ctx context.Context, fromBucketID, toBucketID int64) ([]domain.TransitionGuard, error) {
	var guardsJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT guards_json FROM workflow_transitions
WHERE from_bucket_id = ? AND to_bucket_id = ? AND active = 1
LIMIT 1
`, fromBucketID, toBucketID).Scan(&guardsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if guardsJSON == "" {
		return nil, nil
	}
	var guards []domain.TransitionGuard
	if err := json.Unmarshal([]byte(guardsJSON), &guards); err != nil {
		return nil, err
	}
	return guards, nil
}

// CurrentTaskBucket returns the bucket id and key for a task. Returns
// ErrTaskNotFound if the task is not in the project.
func (s *Store) CurrentTaskBucket(ctx context.Context, projectID, taskID int64) (int64, string, error) {
	var bucketID int64
	var bucketKey string
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(t.bucket_id, 0), COALESCE(wb.key, '')
FROM tasks t
LEFT JOIN workflow_buckets wb ON wb.id = t.bucket_id
WHERE t.project_id = ? AND t.id = ?
`, projectID, taskID).Scan(&bucketID, &bucketKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return 0, "", err
	}
	return bucketID, bucketKey, nil
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

