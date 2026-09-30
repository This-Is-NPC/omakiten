package contract

// TaskBoardInput selects the project whose board is read.
type TaskBoardInput struct {
	ProjectSelector
}

// BoardTask is one active task with the relation counts a board card shows.
type BoardTask struct {
	TaskSummary
	// PriorityColor is the workflow's colour token for the priority:
	// error, warning, success, or info.
	PriorityColor string `json:"priority_color,omitempty"`
	Blockers      int    `json:"blockers"`
	Comments      int    `json:"comments"`
	Subtasks      int    `json:"subtasks"`
	// Tags are the task's tag labels, as the TUI shows them.
	Tags []string `json:"tags,omitempty"`
}

// BoardPriority is one priority a task can take, in the configured order.
type BoardPriority struct {
	Value string `json:"value"`
	// Color is the colour token of the priority: error, warning, success,
	// or info.
	Color   string `json:"color,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// TaskBoardResponse is a project's workflow, its active tasks, and the
// priorities they can take.
type TaskBoardResponse struct {
	Project    ProjectSummary  `json:"project"`
	Workflow   WorkflowSummary `json:"workflow"`
	Tasks      []BoardTask     `json:"tasks"`
	Priorities []BoardPriority `json:"priorities,omitempty"`
}
