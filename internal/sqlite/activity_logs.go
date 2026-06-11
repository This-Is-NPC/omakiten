package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// toolCallEventTypeList is the SQL-friendly form of the canonical
// tool-call event_type vocabulary. Readers filter via `event_type IN
// (...)` so legacy rows that the migration left under 'operation' (a
// source value outside the cli/mcp/tui set) are explicitly excluded —
// those are stale fixtures, not real activity, and would distort the
// logs view.
const toolCallEventTypeList = "'cli.tool_call', 'mcp.tool_call', 'tui.tool_call'"

// toolCallPayload builds the canonical payload JSON written alongside
// every BeginActivityLog row. Keys mirror the discrete columns so hooks
// can filter via `when: { tool_name: ..., source: mcp }` without
// needing to read SQL columns; `args` carries the raw tool argument
// blob the caller passed in.
func toolCallPayload(log domain.ActivityLog) (string, error) {
	out := map[string]any{
		"tool_name":     log.Operation,
		"source":        string(log.Source),
		"entrypoint":    log.Entrypoint,
		"status":        log.Status,
		"duration_ms":   log.DurationMs,
		"error_message": log.ErrorMessage,
	}
	args := normalizeArgsPayload(log.ArgumentsJSON)
	out["args"] = args
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// normalizeArgsPayload returns a JSON-encodable value to nest under
// `args` in the tool-call payload. Empty/whitespace-only blobs collapse
// to {}; valid JSON is passed through verbatim as a RawMessage so the
// caller's key order is preserved (json.Marshal on map[string]any
// would sort alphabetically and break renderers that do substring
// matches against the original blob). Malformed blobs land under
// {"raw": "<original>"} so the data is not silently lost.
func normalizeArgsPayload(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	return map[string]any{"raw": raw}
}

// BeginActivityLog inserts an in-flight `<source>.tool_call` event and
// returns its id. FinishActivityLog later updates that same row with
// status/duration. The two-step API is preserved so callers don't need
// to know about the unified events table — the migration to a global
// log is invisible from the outside.
func (s *Store) BeginActivityLog(ctx context.Context, log any) (int64, error) {
	activityLog, ok := log.(domain.ActivityLog)
	if !ok {
		return 0, fmt.Errorf("invalid activity log type: %T", log)
	}
	eventType := domain.ToolCallEventTypeForSource(activityLog.Source)
	if eventType == "" {
		return 0, fmt.Errorf("unknown activity source: %q", activityLog.Source)
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
	payload, err := toolCallPayload(activityLog)
	if err != nil {
		return 0, fmt.Errorf("encode activity log payload: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO events(entity_type, project_id, project_slug, event_type, payload, source, entrypoint, operation, status, agent_model, agent_session_id, created_at)
VALUES ('system', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
RETURNING id
`, projectID, projectSlug, eventType, payload, string(activityLog.Source), activityLog.Entrypoint, activityLog.Operation, activityLog.Status, activityLog.AgentModel, sessionID)

	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("begin activity log: %w", err)
	}
	// Prune synchronously after insert; cleanup is fast on local SQLite.
	// Errors during pruning must not break the original insert. Retention
	// limits come from config.events.retention resolved through
	// Store.SetEventsPolicy; when unset (tests that skip ApplyConfig)
	// prune is skipped.
	s.pruneRetentionForEventType(ctx, eventType)
	return id, nil
}

func (s *Store) FinishActivityLog(ctx context.Context, id int64, status string, durationMs int, errorMessage string) error {
	// Patch both the discrete columns (read by ActivityLogStats /
	// list view for indexable speed) and the payload mirror keys
	// (read by hook `when:` filters). json_set keeps args + tool_name
	// + source intact so subscribers can match on those after the row
	// is finalised.
	if _, err := s.db.ExecContext(ctx, `
UPDATE events
SET status = ?, duration_ms = ?, error_message = ?,
    payload = json_set(
        COALESCE(NULLIF(payload, ''), '{}'),
        '$.status', ?,
        '$.duration_ms', ?,
        '$.error_message', ?
    ),
    finished_at = CURRENT_TIMESTAMP
WHERE id = ? AND event_type IN (`+toolCallEventTypeList+`)
`, status, durationMs, errorMessage, status, durationMs, errorMessage, id); err != nil {
		return err
	}

	// Fan-out the finished row so the hooks engine can dispatch on
	// `mcp.tool_call` / `cli.tool_call` / `tui.tool_call`. Hooks fire
	// only after Finish so the payload carries the final status,
	// duration, and error_message — pre-call dispatch would race the
	// call itself. publishEvent is a no-op when no bus is wired.
	var ev domain.Event
	var projectID, durationMsCol sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
SELECT id, entity_type, COALESCE(entity_id, 0), project_id, event_type, COALESCE(payload, ''), COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(operation, ''), COALESCE(status, ''), duration_ms, COALESCE(error_message, ''), created_at, COALESCE(finished_at, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
FROM events WHERE id = ?
`, id).Scan(&ev.ID, &ev.EntityType, &ev.EntityID, &projectID, &ev.EventType, &ev.Payload, &ev.Source, &ev.Entrypoint, &ev.Operation, &ev.Status, &durationMsCol, &ev.ErrorMessage, &ev.CreatedAt, &ev.FinishedAt, &ev.AgentModel, &ev.AgentSessionID); err != nil {
		// Telemetry path — swallow read errors so a missing-row race
		// can't break the caller's business logic.
		return nil
	}
	if projectID.Valid {
		ev.ProjectID = projectID.Int64
	}
	if durationMsCol.Valid {
		ev.DurationMs = int(durationMsCol.Int64)
	}
	s.publishEvent(ctx, ev)
	return nil
}

func (s *Store) ListActivityLogs(ctx context.Context, filter domain.ActivityLogFilter) ([]domain.ActivityLog, error) {
	// `arguments_json` is preserved as a top-level field on the
	// ActivityLog DTO, so we extract just the `$.args` slice of the
	// enriched payload rather than returning the whole envelope.
	// json_extract on a missing key returns NULL, which COALESCE folds
	// to '{}' for callers that expect a non-empty string.
	query := "SELECT id, source, entrypoint, operation, COALESCE(project_id, 0), COALESCE(project_slug, ''), COALESCE(json_extract(payload, '$.args'), '{}'), COALESCE(status, ''), COALESCE(duration_ms, 0), COALESCE(error_message, ''), created_at, finished_at, COALESCE(agent_model, ''), COALESCE(agent_session_id, '') FROM events"
	args := []any{}
	conds := []string{"event_type IN (" + toolCallEventTypeList + ")"}

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
		// LIMIT bound via parameter instead of fmt.Sprintf so the
		// query string stays immutable across calls (cleaner audit,
		// and rules out a future caller plumbing an attacker-
		// controlled int through filter.Limit).
		query += " LIMIT ?"
		args = append(args, filter.Limit)
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
		log.FinishedAt = sqlutil.NullStringOr(finishedAt, "")
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// ActivityLogStats returns the unbounded aggregate over the activity
// log scope. Honours `filter.Source` / `filter.Sources` / `filter.ProjectID`
// for narrowing; ignores `filter.Limit` and `filter.Order` because the
// summary is meant to reflect the full project history regardless of
// how the panel beneath it is paged. Implemented as a single scan with
// per-status / per-source SUM(CASE …) aggregates so the round-trip
// cost stays O(1) regardless of project size.
func (s *Store) ActivityLogStats(ctx context.Context, filter domain.ActivityLogFilter) (domain.ActivityLogStats, error) {
	args := []any{}
	conds := []string{"event_type IN (" + toolCallEventTypeList + ")"}
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

	query := `SELECT
COUNT(*),
SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END),
SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),
SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END),
SUM(CASE WHEN source = 'cli' THEN 1 ELSE 0 END),
SUM(CASE WHEN source = 'mcp' THEN 1 ELSE 0 END),
SUM(CASE WHEN source = 'tui' THEN 1 ELSE 0 END),
COALESCE(MIN(created_at), ''),
COALESCE(MAX(created_at), '')
FROM events WHERE ` + strings.Join(conds, " AND ")

	row := s.db.QueryRowContext(ctx, query, args...)
	var stats domain.ActivityLogStats
	var ok, errCount, running, cli, mcp, tui sql.NullInt64
	if err := row.Scan(&stats.Total, &ok, &errCount, &running, &cli, &mcp, &tui, &stats.OldestAt, &stats.NewestAt); err != nil {
		return domain.ActivityLogStats{}, err
	}
	stats.Ok = int(ok.Int64)
	stats.Error = int(errCount.Int64)
	stats.Running = int(running.Int64)
	stats.CLI = int(cli.Int64)
	stats.MCP = int(mcp.Int64)
	stats.TUI = int(tui.Int64)
	return stats, nil
}

