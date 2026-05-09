package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
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

	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(name, label) VALUES (?, ?)`, tag.Name, tag.Label); err != nil {
			return domain.ErrorRecord{}, err
		}
		var tagID int64
		var label string
		if err := tx.QueryRowContext(ctx, `SELECT id, label FROM tags WHERE name = ?`, tag.Name).Scan(&tagID, &label); err != nil {
			return domain.ErrorRecord{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO error_tags(error_id, tag_id) VALUES (?, ?)`, record.ID, tagID); err != nil {
			return domain.ErrorRecord{}, err
		}
		record.Tags = append(record.Tags, domain.Tag{ID: tagID, Name: tag.Name, Label: label})
	}

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

// SearchErrors returns errors matching the supplied tag names (intersection: any
// match) and/or description text. Results are cross-project. Solutions are
// nested ranked by success DESC, then tried_at DESC.
func (s *Store) SearchErrors(ctx context.Context, query string, tagNames []string) ([]domain.ErrorRecord, error) {
	query = strings.TrimSpace(query)

	cleanedTags := make([]string, 0, len(tagNames))
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name != "" {
			cleanedTags = append(cleanedTags, name)
		}
	}

	sqlQuery := `
SELECT DISTINCT e.id, e.description, e.context, COALESCE(e.project_id, 0), COALESCE(p.slug, ''), e.created_at, COALESCE(e.source, ''), COALESCE(e.entrypoint, ''), COALESCE(e.agent_model, ''), COALESCE(e.agent_session_id, '')
FROM errors e
LEFT JOIN projects p ON p.id = e.project_id
`
	conditions := []string{}
	args := []any{}

	if len(cleanedTags) > 0 {
		placeholders := make([]string, len(cleanedTags))
		for i, name := range cleanedTags {
			placeholders[i] = "?"
			args = append(args, name)
		}
		sqlQuery += `JOIN error_tags et ON et.error_id = e.id
JOIN tags t ON t.id = et.tag_id
`
		conditions = append(conditions, "t.name IN ("+strings.Join(placeholders, ",")+")")
	}

	if query != "" {
		conditions = append(conditions, "(e.description LIKE ? OR e.context LIKE ?)")
		like := "%" + query + "%"
		args = append(args, like, like)
	}

	if len(conditions) > 0 {
		sqlQuery += "WHERE " + strings.Join(conditions, " AND ") + "\n"
	}
	sqlQuery += "ORDER BY e.created_at DESC, e.id DESC"

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []domain.ErrorRecord
	for rows.Next() {
		var record domain.ErrorRecord
		if err := rows.Scan(&record.ID, &record.Description, &record.Context, &record.ProjectID, &record.ProjectSlug, &record.CreatedAt, &record.Source, &record.Entrypoint, &record.AgentModel, &record.AgentSessionID); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return records, nil
	}

	ids := make([]int64, len(records))
	for i, r := range records {
		ids[i] = r.ID
	}

	tagsByError, err := s.errorTagsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	solutionsByError, err := s.solutionsByErrorIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Tags = tagsByError[records[i].ID]
		records[i].Solutions = solutionsByError[records[i].ID]
	}

	return records, nil
}

func (s *Store) errorTagsByIDs(ctx context.Context, errorIDs []int64) (map[int64][]domain.Tag, error) {
	if len(errorIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(errorIDs))
	args := make([]any, len(errorIDs))
	for i, id := range errorIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT et.error_id, t.id, t.name, t.label FROM error_tags et JOIN tags t ON t.id = et.tag_id WHERE et.error_id IN ("+strings.Join(placeholders, ",")+") ORDER BY et.error_id, t.name",
		args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Tag{}
	for rows.Next() {
		var errorID int64
		var tag domain.Tag
		if err := rows.Scan(&errorID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		result[errorID] = append(result[errorID], tag)
	}
	return result, rows.Err()
}

func (s *Store) solutionsByErrorIDs(ctx context.Context, errorIDs []int64) (map[int64][]domain.Solution, error) {
	if len(errorIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(errorIDs))
	args := make([]any, len(errorIDs))
	for i, id := range errorIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, error_id, description, steps, success, task_id, COALESCE(tried_at, ''), created_at, likes, COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
FROM solutions
WHERE error_id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY error_id,
  CASE WHEN success = 1 THEN 0 WHEN success IS NULL THEN 1 ELSE 2 END,
  likes DESC,
  COALESCE(tried_at, '') DESC,
  id DESC
`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Solution{}
	for rows.Next() {
		solution, err := scanSolution(rows)
		if err != nil {
			return nil, err
		}
		result[solution.ErrorID] = append(result[solution.ErrorID], solution)
	}
	return result, rows.Err()
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
		if taskID.Valid {
			v := taskID.Int64
			solution.TaskID = &v
		}
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
	if taskID.Valid {
		v := taskID.Int64
		solution.TaskID = &v
	}
	return solution, nil
}
