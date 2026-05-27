package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"omakiten/internal/domain"
)

// ListDirectChildren returns the tasks whose parent_id points at the
// given parentID, ordered by id. Used by the detail-view sub-tasks
// panel and any caller that only cares about one level of the tree —
// recursive walks live in CountDescendants and DescendsFrom.
func (s *Store) ListDirectChildren(ctx context.Context, projectID, parentID int64, buckets domain.BucketResolver) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id, tasks.depth
FROM tasks
WHERE tasks.project_id = ? AND tasks.parent_id = ?
ORDER BY tasks.id
`, projectID, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var children []domain.Task
	for rows.Next() {
		var (
			task     domain.Task
			parentFK sql.NullInt64
		)
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentFK, &task.Depth); err != nil {
			return nil, err
		}
		assignParentID(&task, parentFK)
		task.BucketKey = s.bucketKeyByID(task.BucketID, buckets)
		children = append(children, task)
	}
	return children, rows.Err()
}

// FirstChildNotInBucket returns the first direct child (lowest id) whose
// bucket_id is not finalBucketID. The boolean reports whether such a
// child exists: callers fan out to a "guard satisfied" branch when the
// flag is false. Used by the subtasks_complete guard — only direct
// children are checked because deeper levels gate themselves on their
// own promotions.
func (s *Store) FirstChildNotInBucket(ctx context.Context, projectID, parentID, finalBucketID int64, buckets domain.BucketResolver) (domain.Task, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id, tasks.depth
FROM tasks
WHERE tasks.project_id = ? AND tasks.parent_id = ?
  AND tasks.state = 'active'
  AND COALESCE(tasks.bucket_id, 0) != ?
ORDER BY tasks.id
LIMIT 1
`, projectID, parentID, finalBucketID)

	var (
		task     domain.Task
		parentFK sql.NullInt64
	)
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentFK, &task.Depth); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, false, nil
		}
		return domain.Task{}, false, err
	}
	assignParentID(&task, parentFK)
	task.BucketKey = s.bucketKeyByID(task.BucketID, buckets)
	return task, true, nil
}

// CountDescendants returns the total number of tasks in the subtree
// rooted at parentID (children + grandchildren + ...). Recursive CTE
// over parent_id — runs off the hot path (delete-confirmation prompts,
// admin scripts) so the extra walk is acceptable.
func (s *Store) CountDescendants(ctx context.Context, projectID, parentID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
WITH RECURSIVE subtree(id, depth) AS (
    SELECT id, 0 FROM tasks WHERE project_id = ? AND parent_id = ?
    UNION ALL
    SELECT t.id, s.depth + 1 FROM tasks t INNER JOIN subtree s ON t.parent_id = s.id
    WHERE t.project_id = ? AND s.depth < 1024
)
SELECT COUNT(*) FROM subtree
`, projectID, parentID, projectID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// IsDescendantOf reports whether candidateID has ancestorID anywhere in
// its parent chain. Used by SetTaskParent / Edit to reject reparents
// that would create a cycle: setting T.parent = P is unsafe iff P is
// already a descendant of T. The walk follows parent_id upward starting
// at candidateID — terminates at the first hit or at a root row.
func (s *Store) IsDescendantOf(ctx context.Context, projectID, candidateID, ancestorID int64) (bool, error) {
	if candidateID == ancestorID {
		return true, nil
	}
	var hit int
	err := s.db.QueryRowContext(ctx, `
WITH RECURSIVE ancestors(id, parent_id, depth) AS (
    SELECT id, parent_id, 0 FROM tasks WHERE project_id = ? AND id = ?
    UNION ALL
    SELECT t.id, t.parent_id, a.depth + 1 FROM tasks t INNER JOIN ancestors a ON t.id = a.parent_id
    WHERE t.project_id = ? AND a.depth < 1024
)
SELECT COUNT(*) FROM ancestors WHERE id = ?
`, projectID, candidateID, projectID, ancestorID).Scan(&hit)
	if err != nil {
		return false, err
	}
	return hit > 0, nil
}

// SetTaskParent updates tasks.parent_id for the given task. parentID
// nil clears the column (the task becomes a root); a non-nil pointer
// sets the FK. The caller is responsible for cycle prevention via
// IsDescendantOf — this method is the pure-persistence write. When
// parentID is non-nil the parent lookup and the UPDATE share a single
// transaction so the project-scope assertion is not TOCTOU-vulnerable
// to a concurrent re-parent racing between SELECT and UPDATE.
func (s *Store) SetTaskParent(ctx context.Context, projectID, taskID int64, parentID *int64) error {
	if parentID != nil && *parentID == taskID {
		return domain.NewError(domain.ErrValidation, "task cannot be its own parent", map[string]any{"task_id": taskID})
	}
	if parentID == nil {
		return s.clearTaskParent(ctx, projectID, taskID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var parentProjectID int64
	switch err := tx.QueryRowContext(ctx, `SELECT project_id FROM tasks WHERE id = ?`, *parentID).Scan(&parentProjectID); {
	case errors.Is(err, sql.ErrNoRows):
		return domain.NewError(domain.ErrValidation, "parent task not found", map[string]any{"task_id": taskID, "parent_id": *parentID})
	case err != nil:
		return err
	}
	if parentProjectID != projectID {
		return domain.NewError(domain.ErrValidation, "parent task belongs to a different project", map[string]any{
			"task_id":           taskID,
			"parent_id":         *parentID,
			"project_id":        projectID,
			"parent_project_id": parentProjectID,
		})
	}
	result, err := tx.ExecContext(ctx, `
UPDATE tasks SET parent_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE project_id = ? AND id = ?
`, *parentID, projectID, taskID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
	}
	if err := recomputeSubtreeDepth(ctx, tx, projectID, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// recomputeSubtreeDepth resets `tasks.depth` for taskID and every
// descendant after a reparent. The new depth for taskID is
// `parent.depth + 1` (or 0 when parent_id is NULL); each descendant
// inherits parent.depth + 1 via a recursive UPDATE. Called inside the
// reparent transaction so depth stays in lockstep with parent_id. #299
// §A.4 covers the contract; the recursive UPDATE caps at
// orphanDepthLimit (64) so a cycle escapes in bounded time.
func recomputeSubtreeDepth(ctx context.Context, tx *sql.Tx, projectID, rootID int64) error {
	_, err := tx.ExecContext(ctx, `
WITH RECURSIVE subtree(id, depth) AS (
    SELECT t.id, COALESCE((SELECT p.depth + 1 FROM tasks p WHERE p.id = t.parent_id), 0)
      FROM tasks t
      WHERE t.project_id = ? AND t.id = ?
    UNION ALL
    SELECT t.id, s.depth + 1
      FROM tasks t
      INNER JOIN subtree s ON t.parent_id = s.id
      WHERE t.project_id = ? AND s.depth < 64
)
UPDATE tasks SET depth = (SELECT depth FROM subtree WHERE subtree.id = tasks.id)
WHERE id IN (SELECT id FROM subtree)
`, projectID, rootID, projectID)
	return err
}

// clearTaskParent is the parent-clear branch of SetTaskParent split out
// so the parent-set path can run inside a transaction without forking
// every UPDATE statement. Wrapped in a transaction now so the
// depth-recompute (#299 §A.4) lands atomically with the parent_id =
// NULL update — without the transaction, a crash between the two
// statements could leave a sub-tree with stale depth values.
func (s *Store) clearTaskParent(ctx context.Context, projectID, taskID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE tasks SET parent_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE project_id = ? AND id = ?
`, projectID, taskID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
	}
	if err := recomputeSubtreeDepth(ctx, tx, projectID, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// CountDirectChildren returns the number of direct children for parentID
// — used by the board badge slot to avoid a per-card subtree walk.
func (s *Store) CountDirectChildren(ctx context.Context, projectID, parentID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE project_id = ? AND parent_id = ?`, projectID, parentID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count direct children: %w", err)
	}
	return count, nil
}
