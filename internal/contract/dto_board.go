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

// TaskBoardResponse is a project's workflow and its active tasks.
type TaskBoardResponse struct {
	Project  ProjectSummary  `json:"project"`
	Workflow WorkflowSummary `json:"workflow"`
	Tasks    []BoardTask     `json:"tasks"`
}
