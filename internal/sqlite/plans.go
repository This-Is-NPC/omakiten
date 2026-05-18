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
