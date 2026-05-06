package agent

import (
	"fmt"

	"omakiten/internal/domain"
)

type ProjectSelector struct {
	ProjectID int64  `json:"project_id,omitempty"`
	Project   string `json:"project,omitempty"`
	CWD       string `json:"cwd,omitempty"`
}

type ProjectSummary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	RootPath string `json:"root_path"`
}

type TaskSummary struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	BucketKey   string          `json:"bucket_key,omitempty"`
	Priority    domain.Priority `json:"priority,omitempty"`
}

type WorkflowSummary struct {
	Key         string              `json:"key"`
	Name        string              `json:"name"`
	Buckets     []BucketSummary     `json:"buckets,omitempty"`
	Transitions []TransitionSummary `json:"transitions,omitempty"`
}

type BucketSummary struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type TransitionSummary struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type BucketCount struct {
	BucketKey string `json:"bucket_key"`
	Name      string `json:"name,omitempty"`
	Count     int    `json:"count"`
}

type DependencySummary struct {
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

type CommentSummary struct {
	ID         int64        `json:"id"`
	TaskID     int64        `json:"task_id"`
	Body       string       `json:"body"`
	AuthorType string       `json:"author_type"`
	CreatedAt  string       `json:"created_at,omitempty"`
	Tags       []TagSummary `json:"tags,omitempty"`
}

type ContextSnippet struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at,omitempty"`
}

type Guidance struct {
	Message string   `json:"message"`
	Actions []string `json:"actions,omitempty"`
}

type Failure struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	Guidance Guidance       `json:"guidance"`
}

type Confirmation struct {
	RequiresConfirmation bool                 `json:"requires_confirmation"`
	Reason               string               `json:"reason,omitempty"`
	Options              []ConfirmationOption `json:"options,omitempty"`
}

type ConfirmationOption struct {
	Action string `json:"action"`
	Label  string `json:"label"`
}

type OverviewInput struct {
	ProjectSelector
}

type OverviewResponse struct {
	Project        ProjectSummary   `json:"project"`
	Workflow       WorkflowSummary  `json:"workflow"`
	PendingCount   int              `json:"pending_count"`
	TaskBuckets    []BucketCount    `json:"task_buckets,omitempty"`
	RecentContext  []ContextSnippet `json:"recent_context,omitempty"`
	NextStepPrompt string           `json:"next_step_prompt"`
}

type ResumeProjectInput struct {
	ProjectSelector
}

type ResumeProjectResponse struct {
	Project        ProjectSummary      `json:"project"`
	Workflow       WorkflowSummary     `json:"workflow"`
	TaskBuckets    []BucketCount       `json:"task_buckets,omitempty"`
	LikelyNextWork []TaskSummary       `json:"likely_next_work,omitempty"`
	BlockedWork    []TaskSummary       `json:"blocked_work,omitempty"`
	Dependencies   []DependencySummary `json:"dependencies,omitempty"`
	RecentContext  []ContextSnippet    `json:"recent_context,omitempty"`
	NextStepPrompt string              `json:"next_step_prompt"`
}

type ContinueTaskInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id"`
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

type AddCommentInput struct {
	ProjectSelector
	TaskID     int64    `json:"task_id"`
	Body       string   `json:"body"`
	AuthorType string   `json:"author_type,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type ListCommentsInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id"`
}

type CommentsResponse struct {
	Project  ProjectSummary   `json:"project"`
	Comments []CommentSummary `json:"comments"`
}

type CommentResponse struct {
	Project ProjectSummary `json:"project"`
	Comment CommentSummary `json:"comment"`
}

type AddDependencyInput struct {
	ProjectSelector
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

type RemoveDependencyInput struct {
	ProjectSelector
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
	Confirmed       bool  `json:"confirmed,omitempty"`
}

type ListDependenciesInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id,omitempty"`
}

type DependencyResponse struct {
	Project    ProjectSummary    `json:"project"`
	Dependency DependencySummary `json:"dependency"`
}

type RemoveDependencyResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Removed      bool           `json:"removed"`
}

type DependenciesResponse struct {
	Project      ProjectSummary      `json:"project"`
	Dependencies []DependencySummary `json:"dependencies"`
}

type AddContextInput struct {
	ProjectSelector
	Body string `json:"body"`
}

type ContextResponse struct {
	Project ProjectSummary `json:"project"`
	Entry   ContextSnippet `json:"context_entry"`
}

type DumpContextInput struct {
	ProjectSelector
	Level int `json:"level,omitempty"`
}

type DumpContextResponse struct {
	Project      ProjectSummary      `json:"project"`
	Level        int                 `json:"level"`
	TaskCount    int64               `json:"task_count"`
	TokenMetrics domain.TokenMetrics `json:"token_metrics"`
	Context      []ContextSnippet    `json:"context_entries,omitempty"`
	Workflow     WorkflowSummary     `json:"workflow,omitempty"`
	Tasks        []TaskSummary       `json:"tasks,omitempty"`
	Dependencies []DependencySummary `json:"dependencies,omitempty"`
	Comments     []CommentSummary    `json:"comments,omitempty"`
}

type WorkflowInput struct {
	ProjectSelector
}

type WorkflowResponse struct {
	Project  ProjectSummary  `json:"project"`
	Workflow WorkflowSummary `json:"workflow"`
}

type RecordProgressInput struct {
	ProjectSelector
	TaskID       int64            `json:"task_id,omitempty"`
	Title        *string          `json:"title,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Priority     *domain.Priority `json:"priority,omitempty"`
	MoveToBucket string           `json:"move_to_bucket,omitempty"`
	Comment      string           `json:"comment,omitempty"`
	Context      string           `json:"context,omitempty"`
	AuthorType   string           `json:"author_type,omitempty"`
}

type RecordProgressResponse struct {
	Project      ProjectSummary  `json:"project"`
	Task         *TaskSummary    `json:"task,omitempty"`
	Comment      *CommentSummary `json:"comment,omitempty"`
	ContextEntry *ContextSnippet `json:"context_entry,omitempty"`
}

func projectSummary(project domain.ProjectContext) ProjectSummary {
	return ProjectSummary{ID: project.ID, Name: project.Name, Slug: project.Slug, RootPath: project.RootPath}
}

func taskSummary(task domain.Task) TaskSummary {
	return TaskSummary{ID: task.ID, Title: task.Title, Description: task.Description, BucketKey: task.BucketKey, Priority: task.Priority}
}

func workflowSummary(workflow domain.Workflow) WorkflowSummary {
	out := WorkflowSummary{Key: workflow.Key, Name: workflow.Name}
	for _, bucket := range workflow.Buckets {
		out.Buckets = append(out.Buckets, BucketSummary{Key: bucket.Key, Name: bucket.Name, Position: bucket.Position})
	}
	for _, transition := range workflow.Transitions {
		out.Transitions = append(out.Transitions, TransitionSummary{From: transition.FromBucketKey, To: transition.ToBucketKey})
	}
	return out
}

func dependencySummary(dependency domain.TaskDependency) DependencySummary {
	return DependencySummary{TaskID: dependency.TaskID, DependsOnTaskID: dependency.DependsOnTaskID}
}

func commentSummary(comment domain.Comment) CommentSummary {
	s := CommentSummary{ID: comment.ID, TaskID: comment.TaskID, Body: comment.Body, AuthorType: comment.AuthorType, CreatedAt: comment.CreatedAt}
	if len(comment.Tags) > 0 {
		s.Tags = tagSummaries(comment.Tags)
	}
	return s
}

func contextSnippet(entry domain.ContextEntry) ContextSnippet {
	return ContextSnippet{ID: entry.ID, Body: entry.Body, CreatedAt: entry.CreatedAt}
}

// --- Tags ---

type TagSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	UsageCount int    `json:"usage_count,omitempty"`
}

type AddTagInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
	TagName    string `json:"tag_name"`
}

type TagResponse struct {
	Project ProjectSummary `json:"project"`
	Tag     TagSummary     `json:"tag"`
}

type RemoveTagInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
	TagID      int64  `json:"tag_id"`
	Confirmed  bool   `json:"confirmed,omitempty"`
}

type RemoveTagResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Removed      bool           `json:"removed"`
}

type ListTagsInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
}

type TagListResponse struct {
	Project ProjectSummary `json:"project"`
	Tags    []TagSummary   `json:"tags"`
}

type AllTagsResponse struct {
	Tags []TagSummary `json:"tags"`
}

type MergeTagsInput struct {
	SourceTagID int64 `json:"source_tag_id"`
	TargetTagID int64 `json:"target_tag_id"`
}

func tagSummary(tag domain.Tag) TagSummary {
	return TagSummary{ID: tag.ID, Name: tag.Name, Label: tag.Label, UsageCount: tag.UsageCount}
}

func tagSummaries(tags []domain.Tag) []TagSummary {
	out := make([]TagSummary, len(tags))
	for i, t := range tags {
		out[i] = tagSummary(t)
	}
	return out
}

// --- Errors & Solutions ---

type RecordErrorInput struct {
	ProjectSelector
	Description string   `json:"description"`
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type SearchErrorsInput struct {
	ProjectSelector
	Query string   `json:"query,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

type AddSolutionInput struct {
	ProjectSelector
	ErrorID     int64  `json:"error_id"`
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	TaskID      int64  `json:"task_id,omitempty"`
}

type ConfirmSolutionInput struct {
	ProjectSelector
	SolutionID int64 `json:"solution_id"`
	Success    bool  `json:"success"`
}

type ListTopSolutionsInput struct {
	ProjectSelector
	Limit int `json:"limit,omitempty"`
}

// ListTemplatesInput drives the templates.list MCP endpoint. All fields are
// optional; an unfiltered call returns every loaded template (without body
// to keep payloads compact).
type ListTemplatesInput struct {
	ProjectSelector
	Kind        string `json:"kind,omitempty"`
	Project     string `json:"project,omitempty"`
	IncludeBody bool   `json:"include_body,omitempty"`
}

type ListTemplatesResponse struct {
	Templates []TemplateSummary `json:"templates"`
}

type ShowTemplateInput struct {
	ProjectSelector
	Slug string `json:"slug"`
}

type ShowTemplateResponse struct {
	Template TemplateSummary `json:"template"`
}

// TemplateSummary is the agent-facing view of a loaded template. Body is
// included on show and (optionally) on list; default/project surface the
// active binding so the agent can tell which template is the canonical
// scaffold for a kind without having to compute it.
type TemplateSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Entity      string `json:"entity,omitempty"`
	Default     string `json:"default,omitempty"`
	Project     string `json:"project,omitempty"`
	IsCustom    bool   `json:"is_custom,omitempty"`
	Body        string `json:"body,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
}

type ErrorSummary struct {
	ID          int64             `json:"id"`
	Description string            `json:"description"`
	Context     string            `json:"context,omitempty"`
	ProjectID   int64             `json:"project_id,omitempty"`
	ProjectSlug string            `json:"project_slug,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	Tags        []TagSummary      `json:"tags,omitempty"`
	Solutions   []SolutionSummary `json:"solutions,omitempty"`
}

type SolutionSummary struct {
	ID          int64  `json:"id"`
	ErrorID     int64  `json:"error_id"`
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	Success     *bool  `json:"success,omitempty"`
	TaskID      *int64 `json:"task_id,omitempty"`
	TriedAt     string `json:"tried_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Likes       int    `json:"likes"`
	LikesBadge  string `json:"likes_badge,omitempty"`
	ProjectID   int64  `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
}

type ErrorRecordResponse struct {
	Project ProjectSummary `json:"project"`
	Error   ErrorSummary   `json:"error"`
}

type SearchErrorsResponse struct {
	Project ProjectSummary `json:"project"`
	Errors  []ErrorSummary `json:"errors"`
}

type SolutionResponse struct {
	Project  ProjectSummary  `json:"project"`
	Solution SolutionSummary `json:"solution"`
}

type TopSolutionsResponse struct {
	Project   ProjectSummary    `json:"project"`
	Solutions []SolutionSummary `json:"solutions"`
}

func errorSummary(record domain.ErrorRecord) ErrorSummary {
	out := ErrorSummary{
		ID:          record.ID,
		Description: record.Description,
		Context:     record.Context,
		ProjectID:   record.ProjectID,
		ProjectSlug: record.ProjectSlug,
		CreatedAt:   record.CreatedAt,
	}
	if len(record.Tags) > 0 {
		out.Tags = tagSummaries(record.Tags)
	}
	if len(record.Solutions) > 0 {
		out.Solutions = make([]SolutionSummary, len(record.Solutions))
		for i, sol := range record.Solutions {
			out.Solutions[i] = solutionSummary(sol)
		}
	}
	return out
}

func solutionSummary(s domain.Solution) SolutionSummary {
	out := SolutionSummary{
		ID:          s.ID,
		ErrorID:     s.ErrorID,
		Description: s.Description,
		Steps:       s.Steps,
		Success:     s.Success,
		TaskID:      s.TaskID,
		TriedAt:     s.TriedAt,
		CreatedAt:   s.CreatedAt,
		Likes:       s.Likes,
		ProjectID:   s.ProjectID,
		ProjectSlug: s.ProjectSlug,
	}
	if s.Likes > 0 {
		out.LikesBadge = solutionLikesBadge(s.Likes)
	}
	return out
}

func solutionLikesBadge(likes int) string {
	if likes <= 0 {
		return ""
	}
	return fmt.Sprintf("[★ %d]", likes)
}
