package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// agentAttribution pulls source/entrypoint/agent_model/agent_session_id
// from the request context so RecordError and AddSolution can denormalize
// them on the canonical row. NULL is preserved for session_id when absent
// (the column is nullable; empty string would distort GROUP BY queries).
func agentAttribution(ctx context.Context) (source, entrypoint, agentModel string, agentSessionID any) {
	source, entrypoint, agentModel, sessionStr, _ := activity.FromContext(ctx)
	if sessionStr != "" {
		agentSessionID = sessionStr
	}
	return source, entrypoint, agentModel, agentSessionID
}

func (s *Store) RecordError(ctx context.Context, projectID int64, description, errContext string, tags []domain.Tag) (domain.ErrorRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ErrorRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var record domain.ErrorRecord
	var projectIDArg any
	if projectID > 0 {
		projectIDArg = projectID
	}
	source, entrypoint, agentModel, agentSessionID := agentAttribution(ctx)
	row := tx.QueryRowContext(ctx, `
INSERT INTO errors(description, context, project_id, source, entrypoint, agent_model, agent_session_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, description, context, COALESCE(project_id, 0), created_at, COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
`, description, errContext, projectIDArg, source, entrypoint, agentModel, agentSessionID)
	if err := row.Scan(&record.ID, &record.Description, &record.Context, &record.ProjectID, &record.CreatedAt, &record.Source, &record.Entrypoint, &record.AgentModel, &record.AgentSessionID); err != nil {
		return domain.ErrorRecord{}, err
	}

	attached, err := attachTagsTx(ctx, tx, tagPivotError, record.ID, tags)
	if err != nil {
		return domain.ErrorRecord{}, err
	}
	record.Tags = attached

	return record, tx.Commit()
}

func (s *Store) AddErrorTag(ctx context.Context, errorID, tagID int64) error {
	if err := s.ensureErrorExists(ctx, errorID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO error_tags(error_id, tag_id)
VALUES (?, ?)
ON CONFLICT(error_id, tag_id) DO NOTHING
`, errorID, tagID)
	return err
}

func (s *Store) RemoveErrorTag(ctx context.Context, errorID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM error_tags WHERE error_id = ? AND tag_id = ?`, errorID, tagID)
	return err
}

func (s *Store) ListErrorTags(ctx context.Context, errorID int64) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.name, t.label
FROM tags t
JOIN error_tags et ON et.tag_id = t.id
WHERE et.error_id = ?
ORDER BY t.name
`, errorID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []domain.Tag
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// ListTopSolutions returns the top N solutions ranked by likes globally
// (cross-project). Solutions with zero likes are still returned to fill the
// quota when fewer than N solutions have been liked. Caller (ErrorService)
// is responsible for applying config.solutions.{default_top_limit,
// max_top_limit} so the value reaching this layer is already clamped — a
// non-positive limit here is a programming error rather than a missing
// default.
func (s *Store) ListTopSolutions(ctx context.Context, limit int) ([]domain.Solution, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("ListTopSolutions: limit must be > 0 (caller forgot to apply config.solutions clamps)")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.error_id, s.description, s.steps, s.success, s.task_id, COALESCE(s.tried_at, ''), s.created_at, s.likes,
       COALESCE(e.project_id, 0), COALESCE(p.slug, ''),
       COALESCE(s.source, ''), COALESCE(s.entrypoint, ''), COALESCE(s.agent_model, ''), COALESCE(s.agent_session_id, '')
FROM solutions s
JOIN errors e ON e.id = s.error_id
LEFT JOIN projects p ON p.id = e.project_id
ORDER BY s.likes DESC, COALESCE(s.tried_at, '') DESC, s.id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Solution
	for rows.Next() {
		var solution domain.Solution
		var success sql.NullInt64
		var taskID sql.NullInt64
		if err := rows.Scan(&solution.ID, &solution.ErrorID, &solution.Description, &solution.Steps, &success, &taskID, &solution.TriedAt, &solution.CreatedAt, &solution.Likes, &solution.ProjectID, &solution.ProjectSlug, &solution.Source, &solution.Entrypoint, &solution.AgentModel, &solution.AgentSessionID); err != nil {
			return nil, err
		}
		if success.Valid {
			v := success.Int64 == 1
			solution.Success = &v
		}
		solution.TaskID = sqlutil.NullInt64Ptr(taskID)
		out = append(out, solution)
	}
	return out, rows.Err()
}

func (s *Store) AddSolution(ctx context.Context, errorID int64, description, steps string, taskID *int64) (domain.Solution, error) {
	if err := s.ensureErrorExists(ctx, errorID); err != nil {
		return domain.Solution{}, err
	}
	source, entrypoint, agentModel, agentSessionID := agentAttribution(ctx)
	row := s.db.QueryRowContext(ctx, `
INSERT INTO solutions(error_id, description, steps, task_id, source, entrypoint, agent_model, agent_session_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, error_id, description, steps, success, task_id, COALESCE(tried_at, ''), created_at, likes, COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
`, errorID, description, steps, taskID, source, entrypoint, agentModel, agentSessionID)
	return scanSolution(row)
}

// ConfirmSolution records the outcome of a solution attempt. When success is
// true the solution's like counter is incremented atomically in the same
// statement, so external callers cannot fabricate likes without a real
// confirmation.
func (s *Store) ConfirmSolution(ctx context.Context, solutionID int64, success bool) (domain.Solution, error) {
	successInt := 0
	likesIncrement := 0
	if success {
		successInt = 1
		likesIncrement = 1
	}
	row := s.db.QueryRowContext(ctx, `
UPDATE solutions
SET success = ?, tried_at = CURRENT_TIMESTAMP, likes = likes + ?
WHERE id = ?
RETURNING id, error_id, description, steps, success, task_id, COALESCE(tried_at, ''), created_at, likes, COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
`, successInt, likesIncrement, solutionID)
	solution, err := scanSolution(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Solution{}, domain.NewError(domain.ErrSolutionNotFound, "solution not found", map[string]any{"solution_id": solutionID})
		}
		return domain.Solution{}, err
	}
	return solution, nil
}

func (s *Store) ensureErrorExists(ctx context.Context, errorID int64) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM errors WHERE id = ?", errorID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return domain.NewError(domain.ErrErrorNotFound, "error not found", map[string]any{"error_id": errorID})
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSolution(row rowScanner) (domain.Solution, error) {
	var solution domain.Solution
	var success sql.NullInt64
	var taskID sql.NullInt64
	if err := row.Scan(&solution.ID, &solution.ErrorID, &solution.Description, &solution.Steps, &success, &taskID, &solution.TriedAt, &solution.CreatedAt, &solution.Likes, &solution.Source, &solution.Entrypoint, &solution.AgentModel, &solution.AgentSessionID); err != nil {
		return domain.Solution{}, err
	}
	if success.Valid {
		v := success.Int64 == 1
		solution.Success = &v
	}
	solution.TaskID = sqlutil.NullInt64Ptr(taskID)
	return solution, nil
}
