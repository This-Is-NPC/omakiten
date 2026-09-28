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
		ShouldLog: func() bool { return s.shouldLogEvent(projectID, domain.EventTypeTaskCreated) },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.Task, error) {
			// parent_id lands in the same INSERT as the row itself so sub-task
			// creation is atomic — no two-step INSERT-then-UPDATE that could
			// leave an orphan root visible (and an audit event already emitted)
			// when the second statement fails.
			var parentArg any
			if parentID != nil {
				parentArg = *parentID
			}
			// depth is computed from parent.depth + 1 via a correlated
			// SELECT; COALESCE handles the root case (NULL parent_id →
			// NULL subquery → 0). #299 §A makes this column the source
			// of truth for `subject_depth` event payloads so every
			// INSERT path must keep it in lockstep with parent_id.
			row := tx.QueryRowContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, parent_id, depth)
VALUES (?, ?, ?, ?, ?, ?, COALESCE((SELECT depth + 1 FROM tasks WHERE id = ?), 0))
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at, parent_id, depth
`, projectID, bucketID, title, description, int(priority), parentArg, parentArg)
			return scanTask(row, bucketKey)
		},
		Payload: func(task domain.Task) (string, error) {
			fields := map[string]any{"bucket": bucketKey}
			if parentID != nil {
				fields["parent_id"] = *parentID
			}
			return taskEventPayload(task, buckets, fields)
		},
	})
}

func (s *Store) ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error) {
	query, args, ok := taskListQuery(filter, buckets, projectID)
	if !ok {
		return nil, nil
	}
	rows, err := s.query(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanTaskList(s, rows, buckets)
}

func taskListQuery(filter domain.TaskFilter, buckets domain.BucketResolver, projectID int64) (string, []any, bool) {
	query := `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id, tasks.depth
FROM tasks
WHERE tasks.project_id = ?`
	args := []any{projectID}
	query, args, ok := appendTaskFilters(query, args, filter, buckets)
	if !ok {
		return "", nil, false
	}
	return query + " ORDER BY " + taskOrderClause(filter.Sort), args, true
}

func appendTaskFilters(query string, args []any, filter domain.TaskFilter, buckets domain.BucketResolver) (string, []any, bool) {
	if !filter.IncludeArchived {
		query += " AND tasks.state = 'active'"
	}
	var ok bool
	query, args, ok = appendTaskBucketFilters(query, args, filter, buckets)
	if !ok {
		return "", nil, false
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
	return query, args, true
}

func appendTaskBucketFilters(query string, args []any, filter domain.TaskFilter, buckets domain.BucketResolver) (string, []any, bool) {
	if filter.BucketKey != "" {
		if isNilResolver(buckets) {
			return "", nil, false
		}
		bucket, ok := buckets.BucketByKey(filter.BucketKey)
		if !ok {
			return "", nil, false
		}
		query += " AND tasks.bucket_id = ?"
		args = append(args, bucket.ID)
	}
	if len(filter.BucketKeys) == 0 {
		return query, args, true
	}
	if isNilResolver(buckets) {
		return "", nil, false
	}
	ids := make([]int64, 0, len(filter.BucketKeys))
	for _, key := range filter.BucketKeys {
		if bucket, ok := buckets.BucketByKey(key); ok {
			ids = append(ids, bucket.ID)
		}
	}
	if len(ids) == 0 {
		return "", nil, false
	}
	query += " AND tasks.bucket_id IN (" + placeholders(len(ids)) + ")"
	for _, id := range ids {
		args = append(args, id)
	}
	return query, args, true
}

func scanTaskList(s *Store, rows *sql.Rows, buckets domain.BucketResolver) ([]domain.Task, error) {
	var tasks []domain.Task
	for rows.Next() {
		var (
			task     domain.Task
			parentID sql.NullInt64
		)
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID, &task.Depth); err != nil {
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
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return domain.Task{}, err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	result, err := s.moveTaskTx(ctx, tx, projectID, taskID, targetBucketKey, buckets)
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.commitTransaction(ctx, tx); err != nil {
		return domain.Task{}, err
	}
	if result.moveEvent.EventType != "" {
		s.publishEvent(ctx, result.moveEvent)
	}
	if result.unassignEvent.EventType != "" {
		s.publishEvent(ctx, result.unassignEvent)
	}
	return result.task, nil
}

type taskMoveResult struct {
	task          domain.Task
	moveEvent     domain.Event
	unassignEvent domain.Event
}

func (s *Store) moveTaskTx(ctx context.Context, tx *sql.Tx, projectID, taskID int64, targetBucketKey string, buckets domain.BucketResolver) (taskMoveResult, error) {
	var currentBucketID int64
	var prevAssignedTo sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(bucket_id, 0), assigned_to FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&currentBucketID, &prevAssignedTo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return taskMoveResult{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return taskMoveResult{}, err
	}

	targetBucketID, err := s.activeBucketID(ctx, targetBucketKey, buckets)
	if err != nil {
		return taskMoveResult{}, err
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
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at, parent_id, depth
`, targetBucketID, isFinal, targetBucketID, projectID, taskID)

	task, err := scanTask(row, targetBucketKey)
	if err != nil {
		return taskMoveResult{}, err
	}
	moveEv, unassignEv, err := s.taskMoveEvents(ctx, tx, projectID, taskID, task, buckets, currentBucketID, currentBucketKey, targetBucketID, targetBucketKey, prevAssignedTo)
	if err != nil {
		return taskMoveResult{}, err
	}
	return taskMoveResult{task: task, moveEvent: moveEv, unassignEvent: unassignEv}, nil
}

func (s *Store) taskMoveEvents(ctx context.Context, tx *sql.Tx, projectID, taskID int64, task domain.Task, buckets domain.BucketResolver, currentBucketID int64, currentBucketKey string, targetBucketID int64, targetBucketKey string, prevAssignedTo sql.NullString) (domain.Event, domain.Event, error) {
	var moveEv domain.Event
	var unassignEv domain.Event
	var err error
	if currentBucketID != targetBucketID {
		movePayload, payloadErr := taskEventPayload(task, buckets, map[string]any{"from": currentBucketKey, "to": targetBucketKey})
		if payloadErr != nil {
			return domain.Event{}, domain.Event{}, payloadErr
		}
		moveEv, err = s.persistTaskMoveEvent(ctx, tx, projectID, taskID, movePayload)
		if err != nil {
			return domain.Event{}, domain.Event{}, err
		}
		if prevAssignedTo.Valid && prevAssignedTo.String != "" {
			unassignPayload := fmt.Sprintf(`{"former_assignee":%q,"source":"task.moved"}`, prevAssignedTo.String)
			unassignEv, err = s.persistTaskUnassignEvent(ctx, tx, projectID, taskID, unassignPayload)
			if err != nil {
				return domain.Event{}, domain.Event{}, err
			}
		}
	}
	return moveEv, unassignEv, nil
}

func (s *Store) persistTaskMoveEvent(ctx context.Context, tx *sql.Tx, projectID, taskID int64, payload string) (domain.Event, error) {
	if s.shouldLogEvent(projectID, domain.EventTypeTaskMoved) {
		return insertTaskEvent(ctx, tx, projectID, taskID, domain.EventTypeTaskMoved, "", payload)
	}
	return domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: domain.EventTypeTaskMoved, Payload: payload}, nil
}

func (s *Store) persistTaskUnassignEvent(ctx context.Context, tx *sql.Tx, projectID, taskID int64, payload string) (domain.Event, error) {
	if s.shouldLogEvent(projectID, domain.EventTypeTaskUnassigned) {
		return insertEntityEvent(ctx, tx, domain.EventEntityTask, taskID, projectID, domain.EventTypeTaskUnassigned, payload)
	}
	return domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: domain.EventTypeTaskUnassigned, Payload: payload}, nil
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
		result, err := s.query(ctx).ExecContext(ctx, "UPDATE tasks SET "+strings.Join(sets, ", ")+
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
	if err := s.query(ctx).QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanTask(row *sql.Row, bucketKey string) (domain.Task, error) {
	var (
		task     domain.Task
		parentID sql.NullInt64
	)
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID, &task.Depth); err != nil {
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
	row := s.query(ctx).QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at, tasks.parent_id, tasks.depth
FROM tasks
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var (
		task     domain.Task
		parentID sql.NullInt64
	)
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt, &parentID, &task.Depth); err != nil {
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
	if err := s.query(ctx).QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
	}
	return nil
}
