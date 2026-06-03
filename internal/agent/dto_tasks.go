package agent

import (
	"encoding/json"

	"omakiten/internal/domain"
)

// OptionalInt64 is a tri-state JSON input value for an integer field.
//
// Stock encoding/json collapses absent and explicit-null inputs when the
// target type is `**int64` (both produce a nil outer pointer), which makes
// JSON-RPC inputs unable to distinguish "leave parent_id alone" from "clear
// parent_id to nil". OptionalInt64 preserves the three cases through a
// custom UnmarshalJSON:
//
//	field absent    → OptionalInt64{Set:false, Value:nil}
//	field is null   → OptionalInt64{Set:true,  Value:nil}
//	field is an int → OptionalInt64{Set:true,  Value:&v}
//
// Consumers check Set to decide whether the caller supplied the field and
// then read Value to learn what was supplied.
type OptionalInt64 struct {
	Set   bool
	Value *int64
}

// UnmarshalJSON marks the field as present and reads either nil (for the
// JSON null literal) or a concrete int64 value. Any non-null, non-integer
// JSON token surfaces as a decode error so callers see a self-explanatory
// rejection rather than a silently dropped value.
func (o *OptionalInt64) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type TaskSummary struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	BucketKey   string `json:"bucket_key,omitempty"`
	// Priority is the configured priority label resolved via the
	// bundle-scoped EnumRegistry at projection time. Empty string when
	// the task's priority id is no longer in the active table.
	Priority string `json:"priority,omitempty"`
	State    string `json:"state,omitempty"`
	// ParentID names the task this row is a sub-task of. nil for root
	// tasks; consumers serialize null in JSON.
	ParentID *int64 `json:"parent_id,omitempty"`
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
	NextStepPrompt string              `json:"next_step_prompt"`
	// AgentOutputLanguage carries config.languages.agent_output verbatim
	// so the agent can introspect the directive without re-reading the
	// composed MCP prompt. Empty when unset — consumers treat empty as
	// "no directive in effect".
	AgentOutputLanguage string `json:"agent_output_language,omitempty"`
}

type ListTasksInput struct {
	ProjectSelector
	BucketKey string `json:"bucket_key,omitempty"`
	// ParentID scopes the list by tasks.parent_id with three states:
	//   ParentID.Set == false           → no filter (every task surfaces).
	//   ParentID.Set, Value == nil      → roots only (parent_id IS NULL).
	//   ParentID.Set, Value != nil      → direct children of that id.
	// Encoded through OptionalInt64 so JSON-RPC inputs can distinguish
	// "absent" from "null" — stock encoding/json collapses both cases
	// when targeting **int64.
	ParentID OptionalInt64 `json:"parent_id,omitempty"`
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
	// ParentID attaches the new task as a sub-task of the given id. nil
	// (the default) creates a root task. The handler validates that the
	// parent exists in the same project before writing.
	ParentID *int64 `json:"parent_id,omitempty"`
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

// EditTaskInput is the MCP-side shape for tasks.edit. Title, Description,
// and Priority are pointers so the caller can opt into partial updates: a
// nil pointer means "leave this field alone", while a non-nil pointer
// (even pointing at "") signals an explicit edit. At least one of the
// three must be non-nil, otherwise the service returns ErrValidation.
// BucketKey is intentionally omitted from this surface — bucket moves go
// through tasks.move so the activity log distinguishes the two intents.
type EditTaskInput struct {
	ProjectSelector
	TaskID      int64   `json:"task_id"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Priority    *string `json:"priority,omitempty"`
	// ParentID re-parents the task. Tri-state encoding through OptionalInt64:
	//   ParentID.Set == false           → leave parent_id untouched.
	//   ParentID.Set, Value == nil      → clear parent_id (the task becomes a root).
	//   ParentID.Set, Value != nil      → set parent_id to that id; anti-cycle is enforced.
	ParentID OptionalInt64 `json:"parent_id,omitempty"`
}

type EditTaskResponse struct {
	Project ProjectSummary `json:"project"`
	Task    TaskSummary    `json:"task"`
}

// taskSummary projects a domain.Task into the MCP wire shape, resolving the
// priority label via the supplied registry. registry is nil-safe — Priority
// is left empty when no registry is available, so consumers can still parse
// the rest of the shape.
func taskSummary(task domain.Task, registry *domain.EnumRegistry) TaskSummary {
	s := TaskSummary{
		ID:          task.ID,
		Title:       task.Title,
		Description: task.Description,
		BucketKey:   task.BucketKey,
		Priority:    registry.PriorityLabel(task.Priority),
		ParentID:    task.ParentID,
	}
	if task.State != "" && task.State != domain.TaskStateActive {
		s.State = string(task.State)
	}
	return s
}
