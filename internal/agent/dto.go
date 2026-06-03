package agent

import (
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

type BucketCount struct {
	BucketKey string `json:"bucket_key"`
	Name      string `json:"name,omitempty"`
	Count     int    `json:"count"`
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
	Project        ProjectSummary  `json:"project"`
	Workflow       WorkflowSummary `json:"workflow"`
	PendingCount   int             `json:"pending_count"`
	TaskBuckets    []BucketCount   `json:"task_buckets,omitempty"`
	NextStepPrompt string          `json:"next_step_prompt"`
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
	NextStepPrompt string              `json:"next_step_prompt"`
}

func projectSummary(project domain.ProjectContext) ProjectSummary {
	return ProjectSummary{ID: project.ID, Name: project.Name, Slug: project.Slug, RootPath: project.RootPath}
}

// EditProjectInput carries the project selector plus the new
// description. Only the description is mutable today — the write path
// for the projects.description column (schema-only since migration 002)
// is restored here.
type EditProjectInput struct {
	ProjectSelector
	Description string `json:"description"`
}

// EditProjectResponse shapes the EditProject result: the refreshed
// project DTO plus the next-step prompt, mirroring the other project
// service responses.
type EditProjectResponse struct {
	Project        ProjectSummary `json:"project"`
	Description    string         `json:"description"`
	NextStepPrompt string         `json:"next_step_prompt"`
}
