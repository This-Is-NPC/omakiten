package domain

type ActivitySource string

const (
	ActivitySourceCLI ActivitySource = "cli"
	ActivitySourceTUI ActivitySource = "tui"
	ActivitySourceMCP ActivitySource = "mcp"
)

type ActivityLog struct {
	ID             int64          `json:"id"`
	Source         ActivitySource `json:"source"`
	Entrypoint     string         `json:"entrypoint"`
	Operation      string         `json:"operation"`
	ProjectID      int64          `json:"project_id"`
	ProjectSlug    string         `json:"project_slug"`
	ArgumentsJSON  string         `json:"arguments_json"`
	Status         string         `json:"status"`
	DurationMs     int            `json:"duration_ms"`
	ErrorMessage   string         `json:"error_message"`
	StartedAt      string         `json:"started_at"`
	FinishedAt     string         `json:"finished_at,omitempty"`
	AgentModel     string         `json:"agent_model,omitempty"`
	AgentSessionID string         `json:"agent_session_id,omitempty"`
}

type ActivityLogFilter struct {
	Source    ActivitySource
	Sources   []ActivitySource
	ProjectID int64
	Limit     int
	Order     string
}

// ActivityLogStats is an aggregate over an unbounded slice of the
// activity log — used by the Stats › Logs summary tables so they
// reflect the entire project's history rather than only the limit-N
// rows materialised for the panel below. Counts are exclusive: every
// row that lands in the scope is counted exactly once on the status
// axis and once on the source axis.
type ActivityLogStats struct {
	Total    int
	Ok       int
	Error    int
	Running  int
	CLI      int
	MCP      int
	TUI      int
	OldestAt string // created_at of the earliest entry in scope; empty when scope is empty
	NewestAt string // created_at of the latest entry in scope; empty when scope is empty
}
