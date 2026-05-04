package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"omakiten/internal/domain"
)

const (
	activityLogMaxRows    = 500
	activityLogMaxAgeDays = 7
)

func (s *Store) BeginActivityLog(ctx context.Context, log any) (int64, error) {
	activityLog, ok := log.(domain.ActivityLog)
	if !ok {
		return 0, fmt.Errorf("invalid activity log type: %T", log)
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO activity_logs(source, entrypoint, operation, project_id, project_slug, arguments_json, status, started_at)
VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
RETURNING id
`, activityLog.Source, activityLog.Entrypoint, activityLog.Operation, activityLog.ProjectID, activityLog.ProjectSlug, activityLog.ArgumentsJSON, activityLog.Status)

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
UPDATE activity_logs
SET status = ?, duration_ms = ?, error_message = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ?
`, status, durationMs, errorMessage, id)
	return err
}

func (s *Store) ListActivityLogs(ctx context.Context, filter domain.ActivityLogFilter) ([]domain.ActivityLog, error) {
	query := "SELECT id, source, entrypoint, operation, project_id, project_slug, arguments_json, status, duration_ms, error_message, started_at, finished_at FROM activity_logs"
	args := []any{}
	conds := []string{}

	if filter.Source != "" {
		conds = append(conds, "source = ?")
		args = append(args, string(filter.Source))
	}
	if filter.ProjectID > 0 {
		conds = append(conds, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if len(conds) > 0 {
		query += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY started_at DESC"
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
DELETE FROM activity_logs
WHERE started_at < datetime('now', '-' || ? || ' days')
`, maxAgeDays); err != nil {
			return err
		}
	}
	if maxRows > 0 {
		if _, err := s.db.ExecContext(ctx, `
DELETE FROM activity_logs
WHERE id NOT IN (
  SELECT id FROM activity_logs ORDER BY started_at DESC LIMIT ?
)
`, maxRows); err != nil {
			return err
		}
	}
	return nil
}
