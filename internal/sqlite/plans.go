package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
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

	return txMutateAndEmit(ctx, s, TxMutation[domain.Plan]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanCreated,
		ProjectID:  projectID,
		EntityID:   func(p domain.Plan) int64 { return p.ID },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.Plan, error) {
			row := tx.QueryRowContext(ctx, `
INSERT INTO plans(project_id, slug, name, goal_body)
VALUES (?, ?, ?, ?)
RETURNING id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
`, projectID, slug, name, goalBody)
			plan, err := sqlutil.ScanRow(row, decodePlan)
			if err != nil {
				if isUniqueViolation(err) {
					return domain.Plan{}, domain.NewError(domain.ErrPlanSlugConflict,
						"plan slug already exists for this project",
						map[string]any{"project_id": projectID, "slug": slug})
				}
				return domain.Plan{}, err
			}
			return plan, nil
		},
		Payload: func(plan domain.Plan) (string, error) {
			b, err := json.Marshal(map[string]any{
				"slug":       plan.Slug,
				"name":       plan.Name,
				"project_id": plan.ProjectID,
			})
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	})
}

// GetPlanBySlug resolves a plan by its (project_id, slug) pair. Errors with
// ErrPlanNotFound when no row matches.
func (s *Store) GetPlanBySlug(ctx context.Context, projectID int64, slug string) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at
FROM plans WHERE project_id = ? AND slug = ?
`, projectID, slug)
	plan, err := sqlutil.ScanRow(row, decodePlan)
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
	plan, err := sqlutil.ScanRow(row, decodePlan)
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

	return sqlutil.ScanAll(rows, decodePlan)
}

// UpdatePlanGoalBody rewrites the plan's goal_body column and bumps
// updated_at. Returns ErrPlanNotFound when the plan id belongs to a
// different project or does not exist; emits plan.goal_edited so
// metrics.summary and the hooks engine see the edit.
func (s *Store) UpdatePlanGoalBody(ctx context.Context, projectID, planID int64, goalBody string) (domain.Plan, error) {
	return txMutateAndEmit(ctx, s, TxMutation[domain.Plan]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanGoalEdited,
		ProjectID:  projectID,
		EntityID:   func(p domain.Plan) int64 { return p.ID },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.Plan, error) {
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
			return sqlutil.ScanRow(row, decodePlan)
		},
		Payload: func(_ domain.Plan) (string, error) {
			b, err := json.Marshal(map[string]any{"length": len(goalBody)})
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	})
}

// ListPlanTaskDependencies returns dependency edges where both
// endpoints (task_id and depends_on_task_id) belong to the same plan.
// Cross-plan or out-of-plan edges are filtered out so the network
// diagram only draws in-plan arrows. Ordered by (task_id,
// depends_on_task_id) for stable rendering across refreshes.
func (s *Store) ListPlanTaskDependencies(ctx context.Context, projectID, planID int64) ([]domain.TaskDependency, error) {
	var ownerProjectID int64
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM plans WHERE id = ?`, planID).Scan(&ownerProjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"plan_id": planID})
		}
		return nil, err
	}
	if ownerProjectID != projectID {
		return nil, domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT d.project_id, d.task_id, d.depends_on_task_id
FROM task_dependencies d
JOIN tasks t  ON t.id  = d.task_id           AND t.plan_id  = ?
JOIN tasks tb ON tb.id = d.depends_on_task_id AND tb.plan_id = ?
WHERE d.project_id = ?
ORDER BY d.task_id, d.depends_on_task_id
`, planID, planID, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deps []domain.TaskDependency
	for rows.Next() {
		var dep domain.TaskDependency
		if err := rows.Scan(&dep.ProjectID, &dep.TaskID, &dep.DependsOnTaskID); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

// PeekNextClaimable returns the next task plans.claim_next would
// reserve, without mutating anything. Powers plans.continue so an
// agent picking up work can preview the candidate before committing
// the claim. Resolves the active wave by the same rule ClaimNext uses
// (lowest-position wave with any pending task) and picks the
// lowest-id unassigned task still sitting in the workflow's first
// bucket. Returns (zero, false, nil) when nothing is claimable.
func (s *Store) PeekNextClaimable(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.PlanTaskRow, bool, error) {
	workflow := buckets.Workflow()
	if len(workflow.Buckets) < 2 {
		return domain.PlanTaskRow{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.peek_next_claimable requires a workflow with at least 2 buckets",
			map[string]any{"plan_id": planID, "bucket_count": len(workflow.Buckets)})
	}
	first := workflow.Buckets[0]
	final := workflow.Buckets[0]
	for _, b := range workflow.Buckets {
		if b.Position < first.Position {
			first = b
		}
		if b.Position > final.Position {
			final = b
		}
	}

	var ownerProjectID int64
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM plans WHERE id = ?`, planID).Scan(&ownerProjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.PlanTaskRow{}, false, domain.NewError(domain.ErrPlanNotFound, "plan not found",
				map[string]any{"plan_id": planID})
		}
		return domain.PlanTaskRow{}, false, err
	}
	if ownerProjectID != projectID {
		return domain.PlanTaskRow{}, false, domain.NewError(domain.ErrPlanNotFound, "plan not found in active project",
			map[string]any{"plan_id": planID, "project_id": projectID})
	}

	var activeWavePos sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
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
		return domain.PlanTaskRow{}, false, err
	}
	if !activeWavePos.Valid {
		return domain.PlanTaskRow{}, false, nil
	}

	var row domain.PlanTaskRow
	var bucketID int64
	err := s.db.QueryRowContext(ctx, `
SELECT t.id, COALESCE(t.wave_id, 0), t.title, COALESCE(t.bucket_id, 0), t.state, COALESCE(t.assigned_to, '')
FROM tasks t
JOIN plan_waves w ON w.id = t.wave_id
WHERE t.plan_id = ?
  AND w.position = ?
  AND t.state = 'active'
  AND COALESCE(t.bucket_id, 0) = ?
  AND (t.assigned_to IS NULL OR t.assigned_to = '')
ORDER BY t.id ASC
LIMIT 1
`, planID, activeWavePos.Int64, first.ID).Scan(&row.TaskID, &row.WaveID, &row.Title, &bucketID, &row.State, &row.AssignedTo)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PlanTaskRow{}, false, nil
	}
	if err != nil {
		return domain.PlanTaskRow{}, false, err
	}
	row.BucketKey = s.bucketKeyByID(bucketID, buckets)
	return row, true, nil
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

	return txMutateAndEmit(ctx, s, TxMutation[domain.PlanWave]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanWaveAdded,
		ProjectID:  projectID,
		// plan.wave_added is keyed by plan id (the wave id rides the
		// payload) so dashboards can group additions by plan via
		// entity_id without needing a follow-up join.
		EntityID: func(_ domain.PlanWave) int64 { return planID },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.PlanWave, error) {
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
			return wave, nil
		},
		Payload: func(wave domain.PlanWave) (string, error) {
			b, err := json.Marshal(map[string]any{
				"wave_id":  wave.ID,
				"name":     wave.Name,
				"position": wave.Position,
			})
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	})
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

// ClaimNextPlanTask atomically claims ownership of the next unassigned
// task in a plan's active wave by setting tasks.assigned_to to the
// caller's _agent_model. The transaction is opened with BEGIN IMMEDIATE
// on a pinned *sql.Conn so concurrent claims serialise behind SQLite's
// reserved write lock — each waiter re-evaluates the SELECT after the
// prior commit, so the loser sees `assigned_to` already set on the
// candidate and either picks the next free task or returns (zero, false)
// when the wave is fully claimed.
//
// The "active wave" is the lowest-position wave that still has any task
// whose bucket is not the workflow's final bucket — implicitly enforces
// wave gating (wave N+1 cannot be claimed while wave N has pending
// tasks). Wave-gate guard config wired into manual moves is orthogonal;
// this method is its own gate.
//
// Side effects (all in the same transaction):
//   - tasks.assigned_to is set to the agent model resolved from ctx.
//   - task.assigned event emits.
//
// Bucket movement is intentionally NOT part of the claim: each preset
// owns its bucket-transition rules (omakase, for instance, requires a
// self-branch comment before backlog → dev), and ClaimNext used to
// bypass those guards by stamping the destination bucket inline. The
// caller now performs the move via WorkflowService.MoveTask once the
// preset-defined preconditions are met.
func (s *Store) ClaimNextPlanTask(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.Task, bool, error) {
	_, _, agentModel, _ := agentAttribution(ctx)
	agentModel = strings.TrimSpace(agentModel)
	if agentModel == "" {
		return domain.Task{}, false, domain.NewError(domain.ErrValidation,
			"plans.claim_next requires _agent_model in the request context",
			map[string]any{"plan_id": planID})
	}

	workflow := buckets.Workflow()
	if len(workflow.Buckets) == 0 {
		return domain.Task{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.claim_next requires a workflow with at least 1 bucket",
			map[string]any{"plan_id": planID, "bucket_count": len(workflow.Buckets)})
	}
	first := workflow.Buckets[0]
	final := workflow.Buckets[0]
	for _, b := range workflow.Buckets {
		if b.Position < first.Position {
			first = b
		}
		if b.Position > final.Position {
			final = b
		}
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
	busyTimeoutMs := s.busyTimeoutMs
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = kitBusyTimeoutMs()
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMs)); err != nil {
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
	//    in the first bucket (claim only the entry point; tasks already
	//    in flight on another bucket are not "next").
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

	// 3. Assign only — no bucket transition. Preset guards on the
	//    bucket move remain authoritative; the caller is responsible
	//    for the subsequent WorkflowService.MoveTask once those guards
	//    are satisfied.
	if _, err := conn.ExecContext(ctx, `
UPDATE tasks
SET assigned_to = ?, updated_at = CURRENT_TIMESTAMP
WHERE project_id = ? AND id = ?
`, agentModel, projectID, taskID); err != nil {
		return domain.Task{}, false, err
	}

	assignPayload, _ := json.Marshal(map[string]any{"assignee": agentModel, "source": "plans.claim_next"})
	assignEvent, err := insertEntityEvent(ctx, conn, domain.EventEntityTask, taskID, projectID, domain.EventTypeTaskAssigned, string(assignPayload))
	if err != nil {
		return domain.Task{}, false, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return domain.Task{}, false, err
	}
	committed = true
	closeConn()

	// Publish post-commit. Pre-fix the row landed on disk but the
	// bus stayed silent — TUI live views and hooks consumers never
	// observed the claim until the next refresh. ClaimNextPlanTask
	// keeps its hand-rolled IMMEDIATE-tx shape (pinned sql.Conn,
	// per-conn busy_timeout reapply) because txMutateAndEmit uses
	// BeginTx which would lose the reserved-lock serialisation under
	// concurrent claims; the publish step is the one piece the
	// inline path always forgot.
	s.publishEvent(ctx, assignEvent)

	task, err := s.taskByID(ctx, projectID, taskID, buckets)
	if err != nil {
		return domain.Task{}, false, err
	}
	return task, true, nil
}

// AssignTask sets tasks.assigned_to and emits task.assigned (non-empty
// new assignee) or task.unassigned (empty new assignee) in the same
// transaction. When the new value matches the existing one the call
// is a no-op (zero Event returned, no event emitted) so repeated
// idempotent writes do not spam the activity feed.
//
// source labels the call site ("cli.assign", "plans.claim_next",
// "task.moved") so downstream consumers can attribute the change.
func (s *Store) AssignTask(ctx context.Context, projectID, taskID int64, assignee, source string, buckets domain.BucketResolver) (domain.Task, domain.Event, error) {
	assignee = strings.TrimSpace(assignee)

	// The no-op path (new == previous) bypasses the lifecycle helper:
	// it neither mutates rows nor emits an event, so threading it
	// through txMutateAndEmit would force a synthetic "no publish"
	// branch the helper does not (and should not) model. Handled
	// inline with its own small tx instead.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, domain.Event{}, err
	}
	var prev sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID).Scan(&prev); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, domain.Event{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project",
				map[string]any{"task_id": taskID, "project_id": projectID})
		}
		return domain.Task{}, domain.Event{}, err
	}
	prevStr := sqlutil.NullStringOr(prev, "")
	if prevStr == assignee {
		task, terr := s.taskByIDTx(ctx, tx, projectID, taskID, buckets)
		if terr != nil {
			_ = tx.Rollback()
			return domain.Task{}, domain.Event{}, terr
		}
		if err := tx.Commit(); err != nil {
			return domain.Task{}, domain.Event{}, err
		}
		return task, domain.Event{}, nil
	}
	// Discard the probe tx; the helper opens a fresh one for the
	// mutate-and-emit cycle. The probe never wrote anything, so
	// rollback is cost-free.
	_ = tx.Rollback()

	type assignResult struct {
		task  domain.Task
		event domain.Event
	}
	eventType := domain.EventTypeTaskAssigned
	if assignee == "" {
		eventType = domain.EventTypeTaskUnassigned
	}
	out, err := txMutateAndEmit(ctx, s, TxMutation[assignResult]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityTask,
		EventType:  eventType,
		ProjectID:  projectID,
		EntityID:   func(_ assignResult) int64 { return taskID },
		Mutate: func(ctx context.Context, tx *sql.Tx) (assignResult, error) {
			var newVal any
			if assignee != "" {
				newVal = assignee
			}
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET assigned_to = ?, updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?`, newVal, projectID, taskID); err != nil {
				return assignResult{}, err
			}
			task, err := s.taskByIDTx(ctx, tx, projectID, taskID, buckets)
			if err != nil {
				return assignResult{}, err
			}
			return assignResult{task: task}, nil
		},
		Payload: func(_ assignResult) (string, error) {
			var payload []byte
			var err error
			if assignee == "" {
				payload, err = json.Marshal(map[string]any{"former_assignee": prevStr, "source": source})
			} else {
				payload, err = json.Marshal(map[string]any{"assignee": assignee, "source": source})
			}
			if err != nil {
				return "", err
			}
			return string(payload), nil
		},
	})
	if err != nil {
		return domain.Task{}, domain.Event{}, err
	}
	// The helper publishes the event itself; callers also expect the
	// event back. Reconstruct it from the same shape the helper
	// emitted (entity_type='task', entity_id=taskID, event_type as
	// computed) — the persisted row carries server-stamped id/created_at
	// that the existing callers don't read, so the envelope returned
	// here matches the prior contract.
	out.event = domain.Event{
		EntityType: domain.EventEntityTask,
		EntityID:   taskID,
		ProjectID:  projectID,
		EventType:  eventType,
	}
	return out.task, out.event, nil
}

// MaybeFinalizePlanForTask transitions the task's owning plan to
// status='done' (with completed_at stamped) when every other task in
// the same plan already sits in the workflow's final bucket. Returns
// (true, nil) when the plan was finalised on this call; (false, nil)
// when the task has no plan, the plan is already done/abandoned, the
// plan has no active tasks (degenerate empty plan), or pending tasks
// remain in non-final buckets. Emits plan.done on a successful
// transition.
//
// Called from WorkflowService.MoveTask after a task lands in the
// final bucket. Recompute-on-write keeps the invariant cheap and
// recoverable: a crash between MoveTask's commit and this call simply
// defers the finalisation to the next task that closes — the count
// query is naturally idempotent and the UPDATE is gated by
// `status = 'active'` so concurrent finalisers cannot double-emit.
func (s *Store) MaybeFinalizePlanForTask(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (bool, error) {
	if buckets == nil {
		return false, nil
	}
	finalKey := buckets.Workflow().FinalBucketKey()
	finalBucket, ok := buckets.BucketByKey(finalKey)
	if !ok {
		return false, nil
	}

	var planID sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT plan_id FROM tasks WHERE project_id = ? AND id = ?`, projectID, taskID).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !planID.Valid {
		return false, nil
	}

	// errSkipFinalize is a sentinel translated to (false, nil) below —
	// signals 'gating check inside Mutate rejected the transition, roll
	// the tx back without emitting an event'. The helper's contract is
	// always-emit-on-success; a skip path needs an out-of-band signal.
	type finalizeResult struct {
		// active is the active-task count snapshot used in the
		// plan.done payload. Captured pre-transition so the payload
		// reflects what the finaliser saw.
		active int
	}
	errSkipFinalize := errors.New("skip finalize")
	_, err := txMutateAndEmit(ctx, s, TxMutation[finalizeResult]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanDone,
		ProjectID:  projectID,
		EntityID:   func(_ finalizeResult) int64 { return planID.Int64 },
		Mutate: func(ctx context.Context, tx *sql.Tx) (finalizeResult, error) {
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM plans WHERE id = ? AND project_id = ?`, planID.Int64, projectID).Scan(&status); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return finalizeResult{}, errSkipFinalize
				}
				return finalizeResult{}, err
			}
			if status != "active" {
				return finalizeResult{}, errSkipFinalize
			}

			var active, pending int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE plan_id = ? AND project_id = ? AND state = 'active'`, planID.Int64, projectID).Scan(&active); err != nil {
				return finalizeResult{}, err
			}
			if active == 0 {
				return finalizeResult{}, errSkipFinalize
			}
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE plan_id = ? AND project_id = ? AND state = 'active' AND COALESCE(bucket_id, 0) <> ?`, planID.Int64, projectID, finalBucket.ID).Scan(&pending); err != nil {
				return finalizeResult{}, err
			}
			if pending > 0 {
				return finalizeResult{}, errSkipFinalize
			}

			res, err := tx.ExecContext(ctx, `UPDATE plans SET status = 'done', completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'active'`, planID.Int64)
			if err != nil {
				return finalizeResult{}, err
			}
			changed, err := res.RowsAffected()
			if err != nil {
				return finalizeResult{}, err
			}
			if changed == 0 {
				// Lost a race against another finaliser — already transitioned.
				return finalizeResult{}, errSkipFinalize
			}
			return finalizeResult{active: active}, nil
		},
		Payload: func(r finalizeResult) (string, error) {
			b, err := json.Marshal(map[string]any{"total": r.active})
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	})
	if err != nil {
		if errors.Is(err, errSkipFinalize) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

// decodePlan reads the canonical `plans` SELECT column list into a
// domain.Plan. The closure shape lets the QueryRow path
// (sqlutil.ScanRow) and the QueryContext path (sqlutil.ScanAll) share
// the same column list, so the historic scanPlan / scanPlanRows pair
// can no longer drift apart silently. Column order MUST match every
// `SELECT id, project_id, slug, name, goal_body, status, created_at,
// updated_at, completed_at FROM plans ...` in this file.
func decodePlan(scan func(...any) error) (domain.Plan, error) {
	var plan domain.Plan
	var goalBody sql.NullString
	var completedAt sql.NullString
	if err := scan(&plan.ID, &plan.ProjectID, &plan.Slug, &plan.Name,
		&goalBody, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt, &completedAt); err != nil {
		return domain.Plan{}, err
	}
	plan.GoalBody = sqlutil.NullStringOr(goalBody, "")
	plan.CompletedAt = sqlutil.NullStringOr(completedAt, "")
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
