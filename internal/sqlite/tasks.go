package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// CreateTask inserts a task into the given bucket and emits the matching
// task.created event in the same transaction. The bucket key must be
// non-empty — default-bucket selection is an app-layer concern (see
// app.WorkflowService.ResolveDefaultBucket); the store enforces only the
// foreign-key existence of the bucket in the active workflow via the
// caller-supplied BucketResolver.
func (s *Store) CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string, parentID *int64, buckets domain.BucketResolver) (domain.Task, error) {
	if bucketKey == "" {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "bucket key is required", nil)
	}

	bucketID, err := s.activeBucketID(ctx, bucketKey, buckets)
	if err != nil {
		return domain.Task{}, err
	}

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
	return txMutateAndEmit(ctx, s, TxMutation[domain.Task]{
		Scope:     EventScopeTask,
		EventType: domain.EventTypeTaskCreated,
		ProjectID: projectID,
		EntityID:  func(t domain.Task) int64 { return t.ID },
		ShouldLog: func() bool { return s.shouldLogEvent(domain.EventTypeTaskCreated) },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.Task, error) {
			// parent_id lands in the same INSERT as the row itself so sub-task
			// creation is atomic — no two-step INSERT-then-UPDATE that could
			// leave an orphan root visible (and an audit event already emitted)
			// when the second statement fails.
			var parentArg any
			if parentID != nil {
				parentArg = *parentID
			}
			row := tx.QueryRowContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, parent_id)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at, parent_id
`, projectID, bucketID, title, description, int(priority), parentArg)
			return scanTask(row, bucketKey)
		},
		Payload: func(_ domain.Task) (string, error) {
			if parentID != nil {
				return fmt.Sprintf(`{"bucket":%q,"parent_id":%d}`, bucketKey, *parentID), nil
			}
			return fmt.Sprintf(`{"bucket":%q}`, bucketKey), nil
		},
	})
}

func (s *Store) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error) {
	// `workflow_buckets` was dropped in migration 020; the join previously
	// resolved bucket.key for filter and projection. We now resolve
	// key→id via the caller-supplied resolver before issuing the SQL so
	// the query stays a pure tasks-table read, and resolve id→key in Go
	// after the scan. When buckets is nil any filter that needs a
	// resolver short-circuits to an empty result and the post-scan key
	// resolution returns empty strings — matches the pre-migration JOIN
	// semantics for orphaned rows.
	query := `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id
FROM tasks
WHERE tasks.project_id = ?`
	args := []any{projectID}
	if !filter.IncludeArchived {
		query += " AND tasks.state = 'active'"
	}
	if filter.BucketKey != "" {
		if isNilResolver(buckets) {
			return nil, nil
		}
		b, ok := buckets.BucketByKey(filter.BucketKey)
		if !ok {
			// unknown bucket — return empty result rather than error;
			// matches the pre-migration JOIN semantics (no rows match).
			return nil, nil
		}
		query += " AND tasks.bucket_id = ?"
		args = append(args, b.ID)
	}
	if len(filter.BucketKeys) > 0 {
		if isNilResolver(buckets) {
			return nil, nil
		}
		ids := make([]int64, 0, len(filter.BucketKeys))
		for _, key := range filter.BucketKeys {
			if b, ok := buckets.BucketByKey(key); ok {
				ids = append(ids, b.ID)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		query += " AND tasks.bucket_id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if len(filter.Priorities) > 0 {
		query += " AND tasks.priority_id IN (" + placeholders(len(filter.Priorities)) + ")"
		for _, p := range filter.Priorities {
			args = append(args, int(p))
		}
	}
	switch filter.ParentMode {
	case domain.ParentRoots:
		query += " AND tasks.parent_id IS NULL"
	case domain.ParentChildren:
		query += " AND tasks.parent_id = ?"
		args = append(args, filter.ParentValue)
	}
	query += " ORDER BY " + taskOrderClause(filter.Sort)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []domain.Task
	for rows.Next() {
		var (
			task     domain.Task
			parentID sql.NullInt64
		)
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID); err != nil {
			return nil, err
		}
		assignParentID(&task, parentID)
		task.BucketKey = s.bucketKeyByID(task.BucketID, buckets)
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// taskOrderClause maps a TaskSort to a literal ORDER BY fragment. We never
// interpolate Field/Order directly into SQL — they are validated against a
// fixed allowlist here, and unknown values silently fall back to "tasks.id"
// as the safe default ordering.
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
func (s *Store) MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string, buckets domain.BucketResolver) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentBucketID int64
	var prevAssignedTo sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(bucket_id, 0), assigned_to FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&currentBucketID, &prevAssignedTo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}

	targetBucketID, err := s.activeBucketID(ctx, targetBucketKey, buckets)
	if err != nil {
		return domain.Task{}, err
	}

	currentBucketKey := s.bucketKeyByID(currentBucketID, buckets)

	// completed_at records the first time the task reached the workflow's
	// terminal bucket. First-stamp wins: COALESCE stamps the column on the
	// initial entry into the final bucket, and the ELSE branch preserves
	// the existing value on any move out. Reopening a done task and moving
	// it back to done does not overwrite the original timestamp — cycle-time
	// metrics, retros, and plan auto-finalization keep reading the moment
	// the work first crossed the finish line.
	//
	// assigned_to clears whenever the bucket changes: claim ownership is
	// scoped to "currently being worked on" — any move (forward to review,
	// backward to backlog, sideways via re-claim) releases the assignment
	// so the next plans.claim_next sees a clean slot. The CASE WHEN bucket_id
	// != ? expression reads the OLD bucket_id (SQLite evaluates UPDATE
	// RHS against pre-mutation values), so the comparison is "old bucket
	// != target".
	isFinal := boolToInt(buckets.Workflow().FinalBucketKey() == targetBucketKey)
	row := tx.QueryRowContext(ctx, `
UPDATE tasks SET bucket_id = ?, updated_at = CURRENT_TIMESTAMP,
  completed_at = CASE WHEN ? = 1 THEN COALESCE(completed_at, CURRENT_TIMESTAMP) ELSE completed_at END,
  assigned_to  = CASE WHEN bucket_id != ? THEN NULL ELSE assigned_to END
WHERE project_id = ? AND id = ?
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at, parent_id
`, targetBucketID, isFinal, targetBucketID, projectID, taskID)

	task, err := scanTask(row, targetBucketKey)
	if err != nil {
		return domain.Task{}, err
	}

	var moveEv domain.Event
	var unassignEv domain.Event
	if currentBucketID != targetBucketID {
		movePayload := fmt.Sprintf(`{"from":%q,"to":%q}`, currentBucketKey, targetBucketKey)
		if s.shouldLogEvent(domain.EventTypeTaskMoved) {
			var err error
			moveEv, err = insertTaskEvent(ctx, tx, projectID, taskID, domain.EventTypeTaskMoved, "", movePayload)
			if err != nil {
				return domain.Task{}, err
			}
		} else {
			moveEv = domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: domain.EventTypeTaskMoved, Payload: movePayload}
		}
		if prevAssignedTo.Valid && prevAssignedTo.String != "" {
			unassignPayload := fmt.Sprintf(`{"former_assignee":%q,"source":"task.moved"}`, prevAssignedTo.String)
			if s.shouldLogEvent(domain.EventTypeTaskUnassigned) {
				var err error
				unassignEv, err = insertEntityEvent(ctx, tx, domain.EventEntityTask, taskID, projectID, domain.EventTypeTaskUnassigned, unassignPayload)
				if err != nil {
					return domain.Task{}, err
				}
			} else {
				unassignEv = domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: domain.EventTypeTaskUnassigned, Payload: unassignPayload}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	if moveEv.EventType != "" {
		s.publishEvent(ctx, moveEv)
	}
	if unassignEv.EventType != "" {
		s.publishEvent(ctx, unassignEv)
	}
	return task, nil
}

func (s *Store) UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate, buckets domain.BucketResolver) (domain.Task, error) {
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

	return s.taskByID(ctx, projectID, taskID, buckets)
}

func (s *Store) TaskCount(ctx context.Context, projectID int64) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanTask(row *sql.Row, bucketKey string) (domain.Task, error) {
	var (
		task     domain.Task
		parentID sql.NullInt64
	)
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", nil)
		}
		return domain.Task{}, err
	}
	assignParentID(&task, parentID)
	task.BucketKey = bucketKey
	return task, nil
}

// assignParentID promotes a nullable parent_id read into the *int64
// surface on domain.Task. Centralised so every scan path treats SQL NULL
// (root task) and a present FK (sub-task) the same way. Delegates to
// sqlutil.NullInt64Ptr so the *int64 contract stays in one place.
func assignParentID(task *domain.Task, n sql.NullInt64) {
	task.ParentID = sqlutil.NullInt64Ptr(n)
}

// GetTaskByID is the port-facing point lookup the app's TaskService
// reads through. Mirrors taskByID's body but exported so the
// TaskRepository interface contract can name it; internal callers
// (UpdateTask post-write read) still use taskByID to keep the
// hexagonal direction inward.
func (s *Store) GetTaskByID(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (domain.Task, error) {
	return s.taskByID(ctx, projectID, taskID, buckets)
}

func (s *Store) taskByID(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id
FROM tasks
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var (
		task     domain.Task
		parentID sql.NullInt64
	)
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}
	assignParentID(&task, parentID)
	task.BucketKey = s.bucketKeyByID(task.BucketID, buckets)
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
