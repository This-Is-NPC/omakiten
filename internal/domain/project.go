package domain

type Project struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	RootPath string `json:"root_path"`
}

// ProjectDeleteCounters is the per-table row-count snapshot the
// service layer resolves before a destructive ProjectService.Delete.
// Exposed to the CLI / TUI so the user sees what is about to be
// removed; carried in the project.removed event payload so downstream
// hooks can attribute blast radius to a deletion. Tags counts the
// project_tags bridge entries (project-scoped tag attachments), not
// the tags table itself which stays global. ActivityLogEntries counts
// rows in events with event_type='operation' / cli.tool_call /
// mcp.tool_call / tui.tool_call — the per-call activity log
// associated with the project.
type ProjectDeleteCounters struct {
	Tasks              int `json:"tasks"`
	Comments           int `json:"comments"`
	Plans              int `json:"plans"`
	Tags               int `json:"tags"`
	ActivityLogEntries int `json:"activity_log_entries"`
}

type ProjectContext struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	RootPath string `json:"root_path"`
}

func (p Project) Context() ProjectContext {
	return ProjectContext(p)
}
