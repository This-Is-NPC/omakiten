package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"omakiten/internal/domain"
)

// HardDeleteTask removes a task and every dependent row (events including
// comments, event_tags via FK cascade, task_dependencies in both directions,
// task_tags via FK cascade). Returns a snapshot Event of type task.removed
// emitted as an entity_type='system' row so the audit trail survives the
// cascade — the per-task activity feed is gone with the task itself.
//
// Order matters: task_dependencies has no FK cascade onto tasks(project_id,id)
// for the depends_on_task_id column, so we delete those rows manually before
// the task. Events have no FK to tasks at all (entity_id is opaque), so we
// delete those manually too.
func (s *Store) HardDeleteTask(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	task, err := s.taskByIDTx(ctx, tx, projectID, taskID, buckets)
	if err != nil {
		return domain.Event{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE entity_type = 'task' AND project_id = ? AND entity_id = ?`, projectID, taskID); err != nil {
		return domain.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE project_id = ? AND (task_id = ? OR depends_on_task_id = ?)`, projectID, taskID, taskID); err != nil {
		return domain.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID); err != nil {
		return domain.Event{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"task_id":     task.ID,
		"title":       task.Title,
		"description": task.Description,
		"priority":    int(task.Priority),
		"bucket_key":  task.BucketKey,
		"state":       task.State,
	})
	if err != nil {
		return domain.Event{}, err
	}

	var event domain.Event
	if s.shouldLogEvent(domain.EventTypeTaskRemoved) {
		if err := tx.QueryRowContext(ctx, `
INSERT INTO events(entity_type, project_id, event_type, body, payload)
VALUES ('system', ?, ?, '', ?)
RETURNING id, entity_type, COALESCE(entity_id, 0), project_id, event_type, body, payload, created_at
`, projectID, domain.EventTypeTaskRemoved, string(payload)).Scan(
			&event.ID, &event.EntityType, &event.EntityID, &event.ProjectID,
			&event.EventType, &event.Body, &event.Payload, &event.CreatedAt,
		); err != nil {
			return domain.Event{}, fmt.Errorf("emit task.removed: %w", err)
		}
	} else {
		event = domain.Event{EntityType: domain.EventEntitySystem, ProjectID: projectID, EventType: domain.EventTypeTaskRemoved, Payload: string(payload)}
	}

	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	s.publishEvent(ctx, event)
	return event, nil
}

// BackfillTaskCompletedAt sets tasks.completed_at = updated_at for every
// task currently sitting in the workflow's final bucket whose
// completed_at column is still NULL. The MoveTask + SetTaskState write
// paths now keep the column populated going forward (see migration 023
// follow-up); this helper closes the gap for rows that landed in the
// terminal bucket before that wiring existed.
//
// Best-effort timestamp: updated_at is the closest stable signal we
// have for "when did this task last move". Tasks that bounced in and
// out of the final bucket lose the original completion moment, which
// the plan #124 risk register calls out explicitly.
//
// Idempotent: the WHERE clause means a second invocation against the
// same row is a no-op (completed_at IS NOT NULL after the first run).
// Returns the row count updated so callers can surface the result in
// telemetry without joining against the table.
func (s *Store) BackfillTaskCompletedAt(ctx context.Context, projectID int64, buckets domain.BucketResolver) (int64, error) {
	if buckets == nil {
		return 0, nil
	}
	finalKey := buckets.Workflow().FinalBucketKey()
	finalBucket, ok := buckets.BucketByKey(finalKey)
	if !ok {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks SET completed_at = updated_at
WHERE project_id = ?
  AND completed_at IS NULL
  AND COALESCE(bucket_id, 0) = ?
`, projectID, finalBucket.ID)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// SetTaskState flips the active|archived flag and emits the matching
// task.archived / task.unarchived event in the same transaction. When
// targetBucketKey is non-empty the task is also moved into that bucket
// (used by archive to move into the final 'done' bucket atomically).
// MoveTask events are NOT emitted for the archive-side bucket change — the
// task.archived event already records the destination bucket in its payload.
func (s *Store) SetTaskState(ctx context.Context, projectID, taskID int64, state domain.TaskState, targetBucketKey string, buckets domain.BucketResolver) (domain.Task, domain.Event, error) {
	if state != domain.TaskStateActive && state != domain.TaskStateArchived {
		return domain.Task{}, domain.Event{}, domain.NewError(domain.ErrValidation, "invalid task state", map[string]any{"state": string(state)})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, domain.Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	prev, err := s.taskByIDTx(ctx, tx, projectID, taskID, buckets)
	if err != nil {
		return domain.Task{}, domain.Event{}, err
	}

	var prevAssignedTo sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID).Scan(&prevAssignedTo); err != nil {
		return domain.Task{}, domain.Event{}, err
	}

	bucketKey := prev.BucketKey
	bucketArg := any(prev.BucketID)
	if targetBucketKey != "" && targetBucketKey != prev.BucketKey {
		targetBucketID, err := s.activeBucketID(ctx, targetBucketKey, buckets)
		if err != nil {
			return domain.Task{}, domain.Event{}, err
		}
		bucketArg = targetBucketID
		bucketKey = targetBucketKey
	}

	// Same completed_at rule as MoveTask: archive into the final bucket
	// stamps the timestamp; unarchive out of it clears. Active vs archived
	// state does not gate the column — bucket membership does. assigned_to
	// follows the same release-on-bucket-change rule MoveTask applies, so
	// archiving a claimed task drops the assignment cleanly.
	isFinal := boolToInt(buckets.Workflow().FinalBucketKey() == bucketKey)
	row := tx.QueryRowContext(ctx, `
UPDATE tasks SET state = ?, bucket_id = ?, updated_at = CURRENT_TIMESTAMP,
  completed_at = CASE WHEN ? = 1 THEN COALESCE(completed_at, CURRENT_TIMESTAMP) ELSE NULL END,
  assigned_to  = CASE WHEN bucket_id != ? THEN NULL ELSE assigned_to END
WHERE project_id = ? AND id = ?
RETURNING id, project_id, bucket_id, title, description, priority_id, state, created_at
`, string(state), bucketArg, isFinal, bucketArg, projectID, taskID)
	task, err := scanTask(row, bucketKey)
	if err != nil {
		return domain.Task{}, domain.Event{}, err
	}

	eventType := domain.EventTypeTaskArchived
	if state == domain.TaskStateActive {
		eventType = domain.EventTypeTaskUnarchived
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"from_bucket": prev.BucketKey,
		"to_bucket":   bucketKey,
		"from_state":  prev.State,
		"to_state":    state,
	})
	if marshalErr != nil {
		return domain.Task{}, domain.Event{}, marshalErr
	}
	var event domain.Event
	if s.shouldLogEvent(eventType) {
		event, err = insertTaskEvent(ctx, tx, projectID, taskID, eventType, "", string(payload))
		if err != nil {
			return domain.Task{}, domain.Event{}, err
		}
	} else {
		event = domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: eventType, Payload: string(payload)}
	}

	var unassignEv domain.Event
	if prev.BucketID != 0 && prev.BucketKey != bucketKey && prevAssignedTo.Valid && prevAssignedTo.String != "" {
		unassignPayload := fmt.Sprintf(`{"former_assignee":%q,"source":%q}`, prevAssignedTo.String, "task."+string(state))
		if s.shouldLogEvent(domain.EventTypeTaskUnassigned) {
			var err error
			unassignEv, err = insertEntityEvent(ctx, tx, domain.EventEntityTask, taskID, projectID, domain.EventTypeTaskUnassigned, unassignPayload)
			if err != nil {
				return domain.Task{}, domain.Event{}, err
			}
		} else {
			unassignEv = domain.Event{EntityType: domain.EventEntityTask, EntityID: taskID, ProjectID: projectID, EventType: domain.EventTypeTaskUnassigned, Payload: unassignPayload}
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.Task{}, domain.Event{}, err
	}
	s.publishEvent(ctx, event)
	if unassignEv.EventType != "" {
		s.publishEvent(ctx, unassignEv)
	}
	return task, event, nil
}

// EmitTaskEditedEvent records a task.edited event with a payload describing
// the edited fields. Service layer calls it after a successful UpdateTask.
func (s *Store) EmitTaskEditedEvent(ctx context.Context, projectID, taskID int64, before, after domain.Task) (domain.Event, error) {
	payload := map[string]any{}
	if before.Title != after.Title {
		payload["title"] = map[string]any{"from": before.Title, "to": after.Title}
	}
	if before.Description != after.Description {
		payload["description"] = map[string]any{"from": before.Description, "to": after.Description}
	}
	if before.Priority != after.Priority {
		payload["priority"] = map[string]any{"from": int(before.Priority), "to": int(after.Priority)}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	return s.RecordTaskEvent(ctx, projectID, taskID, domain.EventTypeTaskEdited, "", string(body))
}

func (s *Store) taskByIDTx(ctx context.Context, tx *sql.Tx, projectID, taskID int64, buckets domain.BucketResolver) (domain.Task, error) {
	row := tx.QueryRowContext(ctx, `
SELECT tasks.id, tasks.project_id, COALESCE(tasks.bucket_id, 0), tasks.title, tasks.description, tasks.priority_id, tasks.state, tasks.created_at
FROM tasks
WHERE tasks.project_id = ? AND tasks.id = ?
`, projectID, taskID)

	var task domain.Task
	if err := row.Scan(&task.ID, &task.ProjectID, &task.BucketID, &task.Title, &task.Description, &task.Priority, &task.State, &task.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, err
	}
	task.BucketKey = s.bucketKeyByID(task.BucketID, buckets)
	return task, nil
}
