package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

func (s *Store) CreateTask(ctx context.Context, projectID int64, title, description, priority, bucketKey string) (domain.Task, error) {
	if bucketKey == "" {
		workflow, err := s.ActiveWorkflow(ctx)
		if err != nil {
			return domain.Task{}, err
		}
		if len(workflow.Buckets) == 0 {
			return domain.Task{}, domain.NewError(domain.ErrConfigInvalid, "active workflow has no buckets", nil)
		}
		bucketKey = workflow.Buckets[0].Key
	}

	bucketID, err := s.activeBucketID(ctx, bucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()

	query := `
INSERT INTO tasks(project_id, bucket_id, title, description, priority)
VALUES (?, ?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority, created_at
`
	args := []any{projectID, bucketID, title, description, priority}
	if priority == "" {
		query = `
INSERT INTO tasks(project_id, bucket_id, title, description)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority, created_at
`
		args = []any{projectID, bucketID, title, description}
	}
	row := tx.QueryRowContext(ctx, query, args...)

	task, err := scanTask(row, bucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	payload := fmt.Sprintf(`{"bucket":%q}`, bucketKey)
	if _, err := insertTaskEvent(ctx, tx, projectID, task.ID, domain.EventTypeTaskCreated, "", payload); err != nil {
		return domain.Task{}, err
	}

	return task, tx.Commit()
}

func (s *Store) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error) {
	query := `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority, tasks.created_at
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ?`
	args := []any{projectID}
	if filter.BucketKey != "" {
		query += " AND workflow_buckets.key = ?"
		args = append(args, filter.BucketKey)
	}
	if len(filter.BucketKeys) > 0 {
		query += " AND workflow_buckets.key IN (" + placeholders(len(filter.BucketKeys)) + ")"
		for _, key := range filter.BucketKeys {
			args = append(args, key)
		}
	}
	if len(filter.Priorities) > 0 {
		query += " AND tasks.priority IN (" + placeholders(len(filter.Priorities)) + ")"
		for _, p := range filter.Priorities {
			args = append(args, string(p))
		}
	}
	query += " ORDER BY " + taskOrderClause(filter.Sort)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []domain.Task
	for rows.Next() {
		var task domain.Task
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority, &task.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// taskOrderClause maps a TaskSort to a literal ORDER BY fragment. We never
// interpolate Field/Order directly into SQL — they are validated against a
// fixed allowlist here, and unknown values silently fall back to "tasks.id"
// to keep the legacy ordering as the safe default.
func taskOrderClause(sort domain.TaskSort) string {
	column := "tasks.id"
	switch sort.Field {
	case "title":
		column = "tasks.title COLLATE NOCASE"
	case "priority":
		// CASE expression so the ordering is semantic (low < normal < high)
		// rather than alphabetical, which would put "high" before "low".
		column = "CASE tasks.priority WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 ELSE 0 END"
	case "created_at":
		column = "tasks.created_at"
	case "id", "":
		column = "tasks.id"
	}
	direction := "ASC"
	if sort.Order == "desc" {
		direction = "DESC"
	}
	return column + " " + direction + ", tasks.id ASC"
}

func (s *Store) MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentBucketID int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(bucket_id, 0) FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&currentBucketID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}

	targetBucketID, err := activeBucketIDTx(ctx, tx, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	if currentBucketID != targetBucketID {
		allowed, err := transitionAllowed(ctx, tx, currentBucketID, targetBucketID)
		if err != nil {
			return domain.Task{}, err
		}
		if !allowed {
			return domain.Task{}, domain.NewError(domain.ErrWorkflowInvalidTransition, "transition not allowed", map[string]any{"task_id": taskID, "from": currentBucketID, "to": targetBucketID})
		}
		if err := evaluateTransitionGuards(ctx, tx, projectID, taskID, currentBucketID, targetBucketID); err != nil {
			return domain.Task{}, err
		}
	}

	currentBucketKey, err := bucketKeyByIDTx(ctx, tx, currentBucketID)
	if err != nil {
		return domain.Task{}, err
	}

	row := tx.QueryRowContext(ctx, `
UPDATE tasks SET bucket_id = ?, updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?
RETURNING id, project_id, bucket_id, title, description, priority, created_at
`, targetBucketID, projectID, taskID)

	task, err := scanTask(row, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	if currentBucketID != targetBucketID {
		movePayload := fmt.Sprintf(`{"from":%q,"to":%q}`, currentBucketKey, targetBucketKey)
		if _, err := insertTaskEvent(ctx, tx, projectID, taskID, domain.EventTypeTaskMoved, "", movePayload); err != nil {
			return domain.Task{}, err
		}

		isFinal, err := isFinalBucketTx(ctx, tx, targetBucketID)
		if err != nil {
			return domain.Task{}, err
		}
		if isFinal {
			completedPayload := fmt.Sprintf(`{"bucket":%q}`, targetBucketKey)
			if _, err := insertTaskEvent(ctx, tx, projectID, taskID, domain.EventTypeTaskCompleted, "", completedPayload); err != nil {
				return domain.Task{}, err
			}
		}
	}

	return task, tx.Commit()
}

func (s *Store) UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate) (domain.Task, error) {
	sets := []string{}
	args := []any{}
	if update.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *update.Title)
	}
	if update.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *update.Description)
	}
	if update.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, string(*update.Priority))
	}
	if len(sets) > 0 {
		args = append(args, projectID, taskID)
		result, err := s.db.ExecContext(ctx, "UPDATE tasks SET "+strings.Join(sets, ", ")+
			", updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?", args...)
		if err != nil {
			return domain.Task{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return domain.Task{}, err
		}
		if changed == 0 {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
	}

	return s.taskByID(ctx, projectID, taskID)
}

func (s *Store) TaskCount(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanTask(row *sql.Row, bucketKey string) (domain.Task, error) {
	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", nil)
		}
		return domain.Task{}, err
	}
	task.BucketKey = bucketKey
	return task, nil
}

func (s *Store) taskByID(ctx context.Context, projectID, taskID int64) (domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority, tasks.created_at
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority, &task.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}
	return task, nil
}

func (s *Store) ensureTaskExists(ctx context.Context, projectID, taskID int64) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
	}
	return nil
}
