package agent

import "omakiten/internal/domain"

type TaskSummary struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	BucketKey   string          `json:"bucket_key,omitempty"`
	Priority    domain.Priority `json:"priority,omitempty"`
	State       string          `json:"state,omitempty"`
}

type ContinueTaskInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id"`
	// IncludeWorkflow overrides config.mcp.include_workflow_in_continue for
	// this single call. nil → use the configured default; *true → force
	// inclusion; *false → skip the workflow block. Use *false on subsequent
	// continues in a session where `okt` already loaded the workflow shape.
	IncludeWorkflow *bool `json:"include_workflow,omitempty"`
}

type ContinueTaskResponse struct {
	Project        ProjectSummary      `json:"project"`
	Task           TaskSummary         `json:"task"`
	Workflow       WorkflowSummary     `json:"workflow"`
	Dependencies   []DependencySummary `json:"dependencies,omitempty"`
	Comments       []CommentSummary    `json:"comments,omitempty"`
	RecentContext  []ContextSnippet    `json:"recent_context,omitempty"`
	NextStepPrompt string              `json:"next_step_prompt"`
}

type ListTasksInput struct {
	ProjectSelector
	BucketKey string `json:"bucket_key,omitempty"`
}

type ListTasksResponse struct {
	Project ProjectSummary `json:"project"`
	Tasks   []TaskSummary  `json:"tasks"`
}

type CreateTaskInput struct {
	ProjectSelector
	Title               string `json:"title,omitempty"`
	Description         string `json:"description"`
	Priority            string `json:"priority,omitempty"`
	BucketKey           string `json:"bucket_key,omitempty"`
	Confirmed           bool   `json:"confirmed,omitempty"`
	SkipSimilarityCheck bool   `json:"skip_similarity_check,omitempty"`
	TemplateSlug        string `json:"template_slug,omitempty"`
}

type CreateTaskResponse struct {
	Project      ProjectSummary       `json:"project"`
	Confirmation Confirmation         `json:"confirmation,omitempty"`
	SimilarTasks []TaskSummary        `json:"similar_tasks,omitempty"`
	Task         *TaskSummary         `json:"task,omitempty"`
	Template     *TaskTemplateSummary `json:"template,omitempty"`
}

// TaskTemplateSummary is the scaffold returned alongside CreateTask responses.
// Body is the raw markdown the agent should use when authoring the task body.
type TaskTemplateSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body"`
}

type MoveTaskInput struct {
	ProjectSelector
	TaskID    int64  `json:"task_id"`
	BucketKey string `json:"bucket_key"`
}

type MoveTaskResponse struct {
	Project ProjectSummary `json:"project"`
	Task    TaskSummary    `json:"task"`
}

type DeleteTaskInput struct {
	ProjectSelector
	TaskID    int64 `json:"task_id"`
	Confirmed bool  `json:"confirmed,omitempty"`
}

type DeleteTaskResponse struct {
	Project   ProjectSummary `json:"project"`
	Confirmation Confirmation `json:"confirmation,omitempty"`
	Snapshot  *EventSummary  `json:"snapshot,omitempty"`
}

type ArchiveTaskInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id"`
}

type ArchiveTaskResponse struct {
	Project ProjectSummary `json:"project"`
	Task    TaskSummary    `json:"task"`
}

func taskSummary(task domain.Task) TaskSummary {
	s := TaskSummary{ID: task.ID, Title: task.Title, Description: task.Description, BucketKey: task.BucketKey, Priority: task.Priority}
	if task.State != "" && task.State != domain.TaskStateActive {
		s.State = string(task.State)
	}
	return s
}
