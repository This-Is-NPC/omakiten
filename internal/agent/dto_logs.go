package agent

import "omakiten/internal/domain"

// LogsRow is the agent-facing shape of a single Logs inspector entry.
// It mirrors domain.EventRow (every column the unified events log
// carries) and adds the rendered `summary` string derived via
// domain.SummarizeEvent so callers see human-readable text without
// re-parsing the payload JSON.
//
// Zero-valued fields are omitted from the JSON output (`omitempty`)
// because different event_types fill different subsets — e.g. comments
// populate Body + AuthorType, tool calls populate Source + Status +
// DurationMs, system events populate Payload. Summary is always set:
// SummarizeEvent never returns the empty string.
type LogsRow struct {
	ID           int64  `json:"id"`
	EntityType   string `json:"entity_type,omitempty"`
	EntityID     int64  `json:"entity_id,omitempty"`
	ProjectID    int64  `json:"project_id,omitempty"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	EventType    string `json:"event_type"`
	Body         string `json:"body,omitempty"`
	Payload      string `json:"payload,omitempty"`
	AuthorType   string `json:"author_type,omitempty"`
	Source       string `json:"source,omitempty"`
	Status       string `json:"status,omitempty"`
	DurationMs   int    `json:"duration_ms,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	AgentModel   string `json:"agent_model,omitempty"`
	// Category is the coarse bucket the Logs inspector groups
	// event_type values into (see domain.EventCategoryOf). Surfaced
	// here so the agent does not have to maintain a parallel
	// event_type → category table.
	Category string `json:"category"`
	// Summary is the human-readable detail string SummarizeEvent
	// renders for this row. Always populated; never empty.
	Summary string `json:"summary"`
}

// ListLogsInput is the MCP-side shape for `logs.list`. Every field is
// optional; the service applies defaults sourced from the active
// project's Snapshot (window) and from the SQL layer (no cap, desc).
//
//   - Categories scopes the read by event category. Empty/omitted
//     means "all categories"; a single value like "tool_call"
//     reproduces the legacy activity-log filter the predecessor
//     activity_logs.ListActivityLogs path emitted.
//   - Since is a duration expression like "24h" or "7d" understood
//     by config.parseDuration. Omitted → use Snapshot.LogsWindowDays
//     (default 30 days).
//   - Limit caps the response. 0 / omitted → no cap from MCP; the
//     SQL layer still applies its row safety ceiling.
//   - Order accepts "asc" or "desc" (case-insensitive). Anything else
//     falls back to "desc".
type ListLogsInput struct {
	ProjectSelector
	Categories []string `json:"categories,omitempty"`
	Since      string   `json:"since,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Order      string   `json:"order,omitempty"`
}

// ListLogsResponse is the wire shape `logs.list` returns. Rows is
// `nil` (encoded as `[]` by the MCP envelope) when no events match
// the filter — callers can rely on the field always being present.
type ListLogsResponse struct {
	Project ProjectSummary `json:"project"`
	Rows    []LogsRow      `json:"rows"`
	// Order echoes the effective sort direction after defaulting so
	// callers can stable-sort follow-up pages without having to
	// re-resolve the input themselves.
	Order string `json:"order"`
	// WindowSince echoes the effective time floor as an ISO-8601
	// string ("" when no floor was applied). Surfaced so callers can
	// render "logs since <date>" without re-running the duration
	// math.
	WindowSince string `json:"window_since,omitempty"`
}

func logsRow(row domain.EventRow) LogsRow {
	return LogsRow{
		ID:           row.ID,
		EntityType:   row.EntityType,
		EntityID:     row.EntityID,
		ProjectID:    row.ProjectID,
		ProjectSlug:  row.ProjectSlug,
		EventType:    row.EventType,
		Body:         row.Body,
		Payload:      row.Payload,
		AuthorType:   row.AuthorType,
		Source:       row.Source,
		Status:       row.Status,
		DurationMs:   row.DurationMs,
		ErrorMessage: row.ErrorMessage,
		CreatedAt:    row.CreatedAt,
		FinishedAt:   row.FinishedAt,
		AgentModel:   row.AgentModel,
		Category:     string(domain.EventCategoryOf(row.EventType)),
		Summary:      domain.SummarizeEvent(row),
	}
}

func logsRows(rows []domain.EventRow) []LogsRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]LogsRow, len(rows))
	for i, r := range rows {
		out[i] = logsRow(r)
	}
	return out
}
