package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// CreatePlan inserts a plan and emits plan.created in the same transaction.
// (project_id, slug) must be unique — a UNIQUE-constraint violation maps to
// ErrPlanSlugConflict so callers can distinguish "you picked a duplicate"
// from genuine I/O failures.
func (s *Store) CreatePlan(ctx context.Context, projectID int64, slug, name, goalBody string) (domain.Plan, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if slug == "" {
		return domain.Plan{}, domain.NewError(domain.ErrValidation, "plan slug is required", nil)
	}
	if name == "" {
		return domain.Plan{}, domain.NewError(domain.ErrValidation, "plan name is required", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
INSERT INTO plans(project_id, slug, name, goal_body)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
`, projectID, slug, name, goalBody)

	plan, err := scanPlan(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Plan{}, domain.NewError(domain.ErrPlanSlugConflict,
				"plan slug already exists for this project",
				map[string]any{"project_id": projectID, "slug": slug})
		}
		return domain.Plan{}, err
	}

	payload, _ := json.Marshal(map[string]any{
		"slug":       plan.Slug,
		"name":       plan.Name,
		"project_id": plan.ProjectID,
	})
	ev, err := insertEntityEvent(ctx, tx, domain.EventEntityPlan, plan.ID, projectID, domain.EventTypePlanCreated, string(payload))
	if err != nil {
		return domain.Plan{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Plan{}, err
	}
	s.publishEvent(ctx, ev)
	return plan, nil
}

// GetPlanBySlug resolves a plan by its (project_id, slug) pair. Errors with
// ErrPlanNotFound when no row matches.
func (s *Store) GetPlanBySlug(ctx context.Context, projectID int64, slug string) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
FROM plans WHERE project_id = ? AND slug = ?
`, projectID, slug)
	plan, err := scanPlan(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Plan{}, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"project_id": projectID, "slug": slug})
		}
		return domain.Plan{}, err
	}
	return plan, nil
}

// GetPlanByID resolves a plan by its primary key, still scoped to the active
// project so cross-project leaks stay impossible.
func (s *Store) GetPlanByID(ctx context.Context, projectID, planID int64) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
FROM plans WHERE project_id = ? AND id = ?
`, projectID, planID)
	plan, err := scanPlan(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Plan{}, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"project_id": projectID, "plan_id": planID})
		}
		return domain.Plan{}, err
	}
	return plan, nil
}

// ListPlans returns every plan for the project, ordered by id ascending so
// creation order is preserved without depending on string comparison of
// generated slugs.
func (s *Store) ListPlans(ctx context.Context, projectID int64) ([]domain.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
FROM plans WHERE project_id = ? ORDER BY id ASC
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var plans []domain.Plan
	for rows.Next() {
		plan, err := scanPlanRows(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

// UpdatePlanGoalBody rewrites the plan's goal_body column and bumps
// updated_at. Returns ErrPlanNotFound when the plan id belongs to a
// different project or does not exist; emits plan.goal_edited so
// metrics.summary and the hooks engine see the edit.
func (s *Store) UpdatePlanGoalBody(ctx context.Context, projectID, planID int64, goalBody string) (domain.Plan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var ownerProjectID int64
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM plans WHERE id = ?`, planID).Scan(&ownerProjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Plan{}, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"plan_id": planID})
		}
		return domain.Plan{}, err
	}
	if ownerProjectID != projectID {
		return domain.Plan{}, domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	}

	row := tx.QueryRowContext(ctx, `
UPDATE plans SET goal_body = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
`, goalBody, planID)
	plan, err := scanPlan(row)
	if err != nil {
		return domain.Plan{}, err
	}

	payload, _ := json.Marshal(map[string]any{"length": len(goalBody)})
	ev, err := insertEntityEvent(ctx, tx, domain.EventEntityPlan, planID, projectID, domain.EventTypePlanGoalEdited, string(payload))
	if err != nil {
		return domain.Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Plan{}, err
	}
	s.publishEvent(ctx, ev)
	return plan, nil
}

// AddPlanWave appends (position=0 / negative) or inserts (position>0) a wave
// onto a plan. When position is non-positive the wave lands after the
// current highest position; explicit positions are honoured verbatim and
// collide via the (plan_id, position) UNIQUE — surfaced as a validation
// error so the caller can retry with a different slot.
//
// Emits plan.wave_added with the wave id, name, and position so dashboards
// can group wave additions by plan via entity_id.
func (s *Store) AddPlanWave(ctx context.Context, projectID, planID int64, name string, position int) (domain.PlanWave, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.PlanWave{}, domain.NewError(domain.ErrValidation, "wave name is required", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PlanWave{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// Project-scope check before mutating: returns ErrPlanNotFound when
	// the plan id belongs to a different project or simply does not exist.
	var ownerProjectID int64
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM plans WHERE id = ?`, planID).Scan(&ownerProjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.PlanWave{}, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"plan_id": planID})
		}
		return domain.PlanWave{}, err
	}
	if ownerProjectID != projectID {
		return domain.PlanWave{}, domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	}

	if position <= 0 {
		var maxPos sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM plan_waves WHERE plan_id = ?`, planID).Scan(&maxPos); err != nil {
			return domain.PlanWave{}, err
		}
		position = int(maxPos.Int64) + 1
	}

	row := tx.QueryRowContext(ctx, `
INSERT INTO plan_waves(plan_id, name, position) VALUES (?, ?, ?)
RETURNING id, plan_id, name, position
`, planID, name, position)

	var wave domain.PlanWave
	if err := row.Scan(&wave.ID, &wave.PlanID, &wave.Name, &wave.Position); err != nil {
		if isUniqueViolation(err) {
			return domain.PlanWave{}, domain.NewError(domain.ErrValidation,
				"wave position already taken",
				map[string]any{"plan_id": planID, "position": position})
		}
		return domain.PlanWave{}, err
	}

	payload, _ := json.Marshal(map[string]any{
		"wave_id":  wave.ID,
		"name":     wave.Name,
		"position": wave.Position,
	})
	ev, err := insertEntityEvent(ctx, tx, domain.EventEntityPlan, planID, projectID, domain.EventTypePlanWaveAdded, string(payload))
	if err != nil {
		return domain.PlanWave{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.PlanWave{}, err
	}
	s.publishEvent(ctx, ev)
	return wave, nil
}

// ListPlanTasks returns every task attached to a plan, ordered by
// (wave_position, task_id) so the network-diagram renderer can stream
// columns left-to-right without an extra sort. BucketKey is resolved in
// Go via the supplied BucketResolver — the SQL stays a pure tasks-table
// read for cache friendliness and to keep migration 020's
// "no workflow_buckets join" invariant intact.
func (s *Store) ListPlanTasks(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) ([]domain.PlanTaskRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, COALESCE(t.wave_id, 0), t.title, COALESCE(t.bucket_id, 0), t.state, COALESCE(t.assigned_to, '')
FROM tasks t
LEFT JOIN plan_waves w ON w.id = t.wave_id
WHERE t.project_id = ? AND t.plan_id = ?
ORDER BY COALESCE(w.position, 1<<30) ASC, t.id ASC
`, projectID, planID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []domain.PlanTaskRow
	for rows.Next() {
		var row domain.PlanTaskRow
		var bucketID int64
		if err := rows.Scan(&row.TaskID, &row.WaveID, &row.Title, &bucketID, &row.State, &row.AssignedTo); err != nil {
			return nil, err
		}
		row.BucketKey = s.bucketKeyByID(bucketID, buckets)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListPlanWaves returns every wave for a plan ordered by position so the
// network diagram can stream columns left-to-right without an extra sort.
func (s *Store) ListPlanWaves(ctx context.Context, projectID, planID int64) ([]domain.PlanWave, error) {
	// Project scope: join plans so a caller cannot enumerate waves belonging
	// to a plan in a different project even if they guess the plan id.
	rows, err := s.db.QueryContext(ctx, `
SELECT pw.id, pw.plan_id, pw.name, pw.position
FROM plan_waves pw
JOIN plans p ON p.id = pw.plan_id
WHERE p.project_id = ? AND pw.plan_id = ?
ORDER BY pw.position ASC
`, projectID, planID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var waves []domain.PlanWave
	for rows.Next() {
		var wave domain.PlanWave
		if err := rows.Scan(&wave.ID, &wave.PlanID, &wave.Name, &wave.Position); err != nil {
			return nil, err
		}
		waves = append(waves, wave)
	}
	return waves, rows.Err()
}

// AssignTaskToPlan attaches an existing task to a (plan, wave). Both must
// live in the same project as the task — cross-project mixes fail with
// ErrPlanNotFound / ErrPlanWaveNotFound. The wave must belong to the named
// plan; mismatches fail with ErrPlanWaveNotFound.
//
// Does not emit a task.assigned event — that constant is reserved for the
// `tasks.assigned_to` free-text field handled in a later slice. Plan-link
// changes show up indirectly via the unified activity feed when a wave
// task transitions.
func (s *Store) AssignTaskToPlan(ctx context.Context, projectID, taskID, planID, waveID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Verify task exists in project.
	var taskExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID).Scan(&taskExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NewError(domain.ErrTaskNotFound, "task not found in active project",
				map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return err
	}

	// Verify plan belongs to project.
	var planProject int64
	switch err := tx.QueryRowContext(ctx, `SELECT project_id FROM plans WHERE id = ?`, planID).Scan(&planProject); {
	case errors.Is(err, sql.ErrNoRows):
		return domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	case err != nil:
		return err
	}
	if planProject != projectID {
		return domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	}

	// Verify wave belongs to plan.
	var wavePlan int64
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM plan_waves WHERE id = ?`, waveID).Scan(&wavePlan); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NewError(domain.ErrPlanWaveNotFound, "wave not found",
				map[string]any{"wave_id": waveID})
		}
		return err
	}
	if wavePlan != planID {
		return domain.NewError(domain.ErrPlanWaveNotFound, "wave does not belong to the given plan",
			map[string]any{"wave_id": waveID, "plan_id": planID})
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET plan_id = ?, wave_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE project_id = ? AND id = ?
`, planID, waveID, projectID, taskID); err != nil {
		return err
	}

	return tx.Commit()
}

// ClaimNextPlanTask atomically reserves the next claimable task in a
// plan. The transaction is opened with BEGIN IMMEDIATE on a pinned
// *sql.Conn so concurrent claims serialise behind SQLite's reserved
// write lock — each waiter re-evaluates the SELECT after the prior
// commit, so the loser sees `assigned_to` already set on the candidate
// and either picks the next free task or returns (zero, false) when
// the wave is fully claimed.
//
// The "active wave" is the lowest-position wave that still has any
// task whose bucket is not the workflow's final bucket — implicitly
// enforces wave gating (wave N+1 cannot be claimed while wave N has
// pending tasks). Wave-gate guard config wired into manual moves is
// orthogonal; this method is its own gate.
//
// Side effects (all in the same transaction):
//   - tasks.bucket_id moves from the workflow's first bucket to the
//     second (typically backlog → dev). Tasks already past the first
//     bucket are skipped — they're considered actively worked on.
//   - tasks.assigned_to is set to the agent model resolved from ctx.
//   - tasks.completed_at follows the same CASE rule as MoveTask so a
//     workflow with only one non-final bucket stays consistent.
//   - task.moved and task.assigned events emit in the same tx.
func (s *Store) ClaimNextPlanTask(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.Task, bool, error) {
	_, _, agentModel, _ := agentAttribution(ctx)
	agentModel = strings.TrimSpace(agentModel)
	if agentModel == "" {
		return domain.Task{}, false, domain.NewError(domain.ErrValidation,
			"plans.claim_next requires _agent_model in the request context",
			map[string]any{"plan_id": planID})
	}

	workflow := buckets.Workflow()
	if len(workflow.Buckets) < 2 {
		return domain.Task{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.claim_next requires a workflow with at least 2 buckets",
			map[string]any{"plan_id": planID, "bucket_count": len(workflow.Buckets)})
	}

	first := workflow.Buckets[0]
	dev := workflow.Buckets[0]
	final := workflow.Buckets[0]
	for _, b := range workflow.Buckets {
		if b.Position < first.Position {
			first = b
		}
		if b.Position > final.Position {
			final = b
		}
	}
	// dev = bucket immediately above first by position.
	devSet := false
	for _, b := range workflow.Buckets {
		if b.Position <= first.Position {
			continue
		}
		if !devSet || b.Position < dev.Position {
			dev = b
			devSet = true
		}
	}
	if !devSet {
		return domain.Task{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.claim_next could not resolve a destination bucket",
			map[string]any{"plan_id": planID})
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return domain.Task{}, false, err
	}
	connClosed := false
	closeConn := func() {
		if connClosed {
			return
		}
		connClosed = true
		_ = conn.Close()
	}
	// Pool has MaxOpenConns=4; the final taskByID lookup needs its own
	// pool slot, so the pinned conn MUST be released before that call —
	// otherwise N>4 concurrent claims deadlock all holders against the
	// post-commit reader. closeConn enforces release-once semantics so
	// the happy path and every error path agree.
	defer closeConn()

	// PRAGMA busy_timeout is per-connection in SQLite; the pool may hand
	// us a connection that bypassed Open's pragma sweep. Re-apply so
	// concurrent claims wait on SQLITE_BUSY instead of erroring out.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", kitBusyTimeoutMs())); err != nil {
		return domain.Task{}, false, fmt.Errorf("apply busy_timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return domain.Task{}, false, fmt.Errorf("begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed && !connClosed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	// 1. Locate the active wave: lowest position with any task NOT in
	//    the workflow's final bucket. NULL → nothing to claim.
	var activeWavePos sql.NullInt64
	if err := conn.QueryRowContext(ctx, `
SELECT MIN(w.position)
FROM plan_waves w
WHERE w.plan_id = ?
  AND EXISTS (
    SELECT 1 FROM tasks t
    WHERE t.wave_id = w.id
      AND t.state = 'active'
      AND COALESCE(t.bucket_id, 0) <> ?
  )
`, planID, final.ID).Scan(&activeWavePos); err != nil {
		return domain.Task{}, false, err
	}
	if !activeWavePos.Valid {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return domain.Task{}, false, err
		}
		committed = true
		closeConn()
		return domain.Task{}, false, nil
	}

	// 2. Pick the lowest-id unassigned task in that wave still sitting
	//    in the first bucket.
	var taskID int64
	err = conn.QueryRowContext(ctx, `
SELECT t.id
FROM tasks t
JOIN plan_waves w ON w.id = t.wave_id
WHERE t.plan_id = ?
  AND w.position = ?
  AND t.state = 'active'
  AND COALESCE(t.bucket_id, 0) = ?
  AND (t.assigned_to IS NULL OR t.assigned_to = '')
ORDER BY t.id ASC
LIMIT 1
`, planID, activeWavePos.Int64, first.ID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return domain.Task{}, false, err
		}
		committed = true
		closeConn()
		return domain.Task{}, false, nil
	}
	if err != nil {
		return domain.Task{}, false, err
	}

	// 3. Move + assign in one UPDATE.
	if _, err := conn.ExecContext(ctx, `
UPDATE tasks
SET bucket_id = ?, assigned_to = ?, updated_at = CURRENT_TIMESTAMP,
    completed_at = CASE WHEN ? = 1 THEN COALESCE(completed_at, CURRENT_TIMESTAMP) ELSE NULL END
WHERE project_id = ? AND id = ?
`, dev.ID, agentModel, boolToInt(dev.Key == final.Key), projectID, taskID); err != nil {
		return domain.Task{}, false, err
	}

	// 4. Emit task.moved + task.assigned in the same transaction.
	movePayload := fmt.Sprintf(`{"from":%q,"to":%q}`, first.Key, dev.Key)
	if _, err := insertTaskEvent(ctx, conn, projectID, taskID, domain.EventTypeTaskMoved, "", movePayload); err != nil {
		return domain.Task{}, false, err
	}
	assignPayload, _ := json.Marshal(map[string]any{"assignee": agentModel, "source": "plans.claim_next"})
	if _, err := insertEntityEvent(ctx, conn, domain.EventEntityTask, taskID, projectID, domain.EventTypeTaskAssigned, string(assignPayload)); err != nil {
		return domain.Task{}, false, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return domain.Task{}, false, err
	}
	committed = true
	closeConn()

	task, err := s.taskByID(ctx, projectID, taskID, buckets)
	if err != nil {
		return domain.Task{}, false, err
	}
	return task, true, nil
}

// CountPriorWavesPending returns the count of tasks in earlier waves of
// the same plan that are still pending (not in the workflow's final
// bucket and not archived). The wave_gate guard uses this to block a
// task's transition until every prior wave finishes. Returns 0 — a
// safe no-op — when the task is not attached to any wave or when no
// final bucket is resolvable.
//
// Implementation uses a CTE to capture the current task's (plan_id,
// wave_position); the outer query joins through that anchor so non-
// plan tasks (cur is empty) naturally fall through to COUNT = 0.
func (s *Store) CountPriorWavesPending(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (int, error) {
	if buckets == nil {
		return 0, nil
	}
	finalKey := buckets.Workflow().FinalBucketKey()
	finalBucket, ok := buckets.BucketByKey(finalKey)
	if !ok {
		return 0, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
WITH cur AS (
  SELECT t.plan_id AS plan_id, w.position AS position
  FROM tasks t
  JOIN plan_waves w ON w.id = t.wave_id
  WHERE t.project_id = ? AND t.id = ?
)
SELECT COUNT(1)
FROM tasks pt
JOIN plan_waves pw ON pw.id = pt.wave_id
JOIN cur ON cur.plan_id = pt.plan_id AND pw.position < cur.position
WHERE pt.state = 'active' AND COALESCE(pt.bucket_id, 0) <> ?
`, projectID, taskID, finalBucket.ID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// insertEntityEvent persists a non-task domain event in the caller's
// transaction. Mirrors insertTaskEvent for plan/wave entities so callers
// keep emission in the same atomic block as the mutation that triggered it.
func insertEntityEvent(ctx context.Context, exec dbExecutor, entityType string, entityID, projectID int64, eventType, payload string) (domain.Event, error) {
	if payload == "" {
		payload = "{}"
	}
	var event domain.Event
	if err := exec.QueryRowContext(ctx, `
INSERT INTO events(entity_type, entity_id, project_id, event_type, payload)
VALUES (?, ?, ?, ?, ?)
RETURNING id, entity_type, entity_id, project_id, event_type, COALESCE(body, ''), payload, created_at
`, entityType, entityID, projectID, eventType, payload).Scan(
		&event.ID, &event.EntityType, &event.EntityID, &event.ProjectID,
		&event.EventType, &event.Body, &event.Payload, &event.CreatedAt,
	); err != nil {
		return domain.Event{}, fmt.Errorf("record %s event: %w", eventType, err)
	}
	return event, nil
}

func scanPlan(row *sql.Row) (domain.Plan, error) {
	var plan domain.Plan
	var goalBody sql.NullString
	var completedAt sql.NullString
	if err := row.Scan(&plan.ID, &plan.ProjectID, &plan.Slug, &plan.Name,
		&goalBody, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt, &completedAt); err != nil {
		return domain.Plan{}, err
	}
	if goalBody.Valid {
		plan.GoalBody = goalBody.String
	}
	if completedAt.Valid {
		plan.CompletedAt = completedAt.String
	}
	return plan, nil
}

func scanPlanRows(rows *sql.Rows) (domain.Plan, error) {
	var plan domain.Plan
	var goalBody sql.NullString
	var completedAt sql.NullString
	if err := rows.Scan(&plan.ID, &plan.ProjectID, &plan.Slug, &plan.Name,
		&goalBody, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt, &completedAt); err != nil {
		return domain.Plan{}, err
	}
	if goalBody.Valid {
		plan.GoalBody = goalBody.String
	}
	if completedAt.Valid {
		plan.CompletedAt = completedAt.String
	}
	return plan, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite encodes UNIQUE violations in the error message as
	// "UNIQUE constraint failed:" — string match is the only portable way
	// without dragging the driver-specific error type into the storage
	// layer. errors.Is on driver-specific sentinels would couple this
	// package to the build tag.
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
