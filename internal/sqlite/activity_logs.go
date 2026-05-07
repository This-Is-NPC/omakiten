package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

const (
	activityLogMaxRows    = 500
	activityLogMaxAgeDays = 7
)

// BeginActivityLog inserts an in-flight `operation` event and returns its id.
// FinishActivityLog later updates that same row with status/duration. We keep
// the legacy two-step API so callers don't need to know about the unified
// events table — the migration to a global log is invisible from the outside.
func (s *Store) BeginActivityLog(ctx context.Context, log any) (int64, error) {
	activityLog, ok := log.(domain.ActivityLog)
	if !ok {
		return 0, fmt.Errorf("invalid activity log type: %T", log)
	}
	var projectID any
	if activityLog.ProjectID > 0 {
		projectID = activityLog.ProjectID
	}
	var projectSlug any
	if activityLog.ProjectSlug != "" {
		projectSlug = activityLog.ProjectSlug
	}
	var sessionID any
	if activityLog.AgentSessionID != "" {
		sessionID = activityLog.AgentSessionID
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO events(entity_type, project_id, project_slug, event_type, payload, source, entrypoint, operation, status, agent_model, agent_session_id, created_at)
VALUES ('system', ?, ?, 'operation', ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
RETURNING id
`, projectID, projectSlug, activityLog.ArgumentsJSON, string(activityLog.Source), activityLog.Entrypoint, activityLog.Operation, activityLog.Status, activityLog.AgentModel, sessionID)

	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("begin activity log: %w", err)
	}
	// Prune synchronously after insert; cleanup is fast on local SQLite.
	// Errors during pruning must not break the original insert.
	_ = s.PruneActivityLogs(ctx, activityLogMaxRows, activityLogMaxAgeDays)
	return id, nil
}

func (s *Store) FinishActivityLog(ctx context.Context, id int64, status string, durationMs int, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE events
SET status = ?, duration_ms = ?, error_message = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ? AND event_type = 'operation'
`, status, durationMs, errorMessage, id)
	return err
}

func (s *Store) ListActivityLogs(ctx context.Context, filter domain.ActivityLogFilter) ([]domain.ActivityLog, error) {
	query := "SELECT id, source, entrypoint, operation, COALESCE(project_id, 0), COALESCE(project_slug, ''), COALESCE(payload, ''), COALESCE(status, ''), COALESCE(duration_ms, 0), COALESCE(error_message, ''), created_at, finished_at, COALESCE(agent_model, ''), COALESCE(agent_session_id, '') FROM events"
	args := []any{}
	conds := []string{"event_type = 'operation'"}

	if filter.Source != "" {
		conds = append(conds, "source = ?")
		args = append(args, string(filter.Source))
	}
	if len(filter.Sources) > 0 {
		ph := make([]string, len(filter.Sources))
		for i, src := range filter.Sources {
			ph[i] = "?"
			args = append(args, string(src))
		}
		conds = append(conds, "source IN ("+strings.Join(ph, ",")+")")
	}
	if filter.ProjectID > 0 {
		conds = append(conds, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	query += " WHERE " + strings.Join(conds, " AND ")
	direction := "DESC"
	if filter.Order == "asc" {
		direction = "ASC"
	}
	query += " ORDER BY created_at " + direction
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []domain.ActivityLog
	for rows.Next() {
		var log domain.ActivityLog
		var finishedAt sql.NullString
		if err := rows.Scan(
			&log.ID, &log.Source, &log.Entrypoint, &log.Operation, &log.ProjectID, &log.ProjectSlug,
			&log.ArgumentsJSON, &log.Status, &log.DurationMs, &log.ErrorMessage, &log.StartedAt, &finishedAt,
			&log.AgentModel, &log.AgentSessionID,
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			log.FinishedAt = finishedAt.String
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (s *Store) PruneActivityLogs(ctx context.Context, maxRows int, maxAgeDays int) error {
	if maxAgeDays > 0 {
		if _, err := s.db.ExecContext(ctx, `
DELETE FROM events
WHERE event_type = 'operation' AND created_at < datetime('now', '-' || ? || ' days')
`, maxAgeDays); err != nil {
			return err
		}
	}
	if maxRows > 0 {
		if _, err := s.db.ExecContext(ctx, `
DELETE FROM events
WHERE event_type = 'operation' AND id NOT IN (
  SELECT id FROM events WHERE event_type = 'operation' ORDER BY created_at DESC LIMIT ?
)
`, maxRows); err != nil {
			return err
		}
	}
	return nil
}
