package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// CreateTask inserts a task into the given bucket and emits the matching
// task.created event in the same transaction. The bucket key must be
// non-empty — default-bucket selection is an app-layer concern (see
// app.WorkflowService.ResolveDefaultBucket); the store enforces only the
// foreign-key existence of the bucket in the active workflow.
func (s *Store) CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string) (domain.Task, error) {
	if bucketKey == "" {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "bucket key is required", nil)
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

	// Priority must be resolved by the app layer (WorkflowService.
	// CreateTask substitutes domain.DefaultPriority() when the input is
	// PriorityZero). Migration 017 dropped the SQL DEFAULT — every
	// INSERT must carry an explicit priority_id. Reaching this point
	// with PriorityZero is a programming error and we error out loud.
	if priority == domain.PriorityZero {
		return domain.Task{}, domain.NewError(domain.ErrValidation,
			"task priority unresolved at the storage layer; the app must substitute domain.DefaultPriority() before calling CreateTask",
			map[string]any{"project_id": projectID, "bucket": bucketKey})
	}
	row := tx.QueryRowContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id)
VALUES (?, ?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at
`, projectID, bucketID, title, description, int(priority))

	task, err := scanTask(row, bucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	if s.shouldLogEvent(domain.EventTypeTaskCreated) {
		payload := fmt.Sprintf(`{"bucket":%q}`, bucketKey)
		if _, err := insertTaskEvent(ctx, tx, projectID, task.ID, domain.EventTypeTaskCreated, "", payload); err != nil {
			return domain.Task{}, err
		}
	}

	return task, tx.Commit()
}

func (s *Store) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error) {
	query := `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ?`
	args := []any{projectID}
	if !filter.IncludeArchived {
		query += " AND tasks.state = 'active'"
	}
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
		query += " AND tasks.priority_id IN (" + placeholders(len(filter.Priorities)) + ")"
		for _, p := range filter.Priorities {
			args = append(args, int(p))
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
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt); err != nil {
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
		// priority_id is the natural sort weight — config authors order
		// the priorities table low→high by id, so ASC gives the
		// historical "low first" semantics and DESC gives "high first"
		// without a hardcoded CASE expression. Renaming a label in
		// config.priorities never breaks this sort.
		column = "tasks.priority_id"
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

// MoveTask is the pure-persistence move: it resolves the target bucket id,
// updates the task row, and emits a task.moved event when the bucket actually
// changes. Workflow policy (transition allowed?, guards, task.completed on
// final bucket) lives in app.WorkflowService — this method does not enforce
// any of those rules so the adapter stays decision-free.
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

	currentBucketKey, err := bucketKeyByIDTx(ctx, tx, currentBucketID)
	if err != nil {
		return domain.Task{}, err
	}

	row := tx.QueryRowContext(ctx, `
UPDATE tasks SET bucket_id = ?, updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at
`, targetBucketID, projectID, taskID)

	task, err := scanTask(row, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	if currentBucketID != targetBucketID && s.shouldLogEvent(domain.EventTypeTaskMoved) {
		movePayload := fmt.Sprintf(`{"from":%q,"to":%q}`, currentBucketKey, targetBucketKey)
		if _, err := insertTaskEvent(ctx, tx, projectID, taskID, domain.EventTypeTaskMoved, "", movePayload); err != nil {
			return domain.Task{}, err
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
		sets = append(sets, "priority_id = ?")
		args = append(args, int(*update.Priority))
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
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt); err != nil {
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
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), COALESCE(workflow_buckets.key, ''), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at
FROM tasks
LEFT JOIN workflow_buckets ON workflow_buckets.id = tasks.bucket_id
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.BucketKey, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt); err != nil {
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
