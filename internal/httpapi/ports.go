package httpapi

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// Operations is the operation facade the adapter drives, pinned to the
// HTTP surface by the composition root.
type Operations interface {
	ListProjects(ctx context.Context) ([]domain.Project, error)
	Overview(ctx context.Context, input contract.OverviewInput) (contract.OverviewResponse, error)
	ResumeProject(ctx context.Context, input contract.ResumeProjectInput) (contract.ResumeProjectResponse, error)
	ShowWorkflow(ctx context.Context, input contract.WorkflowInput) (contract.WorkflowResponse, error)
	TaskBoard(ctx context.Context, input contract.TaskBoardInput) (contract.TaskBoardResponse, error)
	ListTasks(ctx context.Context, input contract.ListTasksInput) (contract.ListTasksResponse, error)
	ShowTask(ctx context.Context, input contract.ShowTaskInput) (contract.ShowTaskResponse, error)
	CreateTaskIntent(ctx context.Context, input contract.CreateTaskInput) (contract.CreateTaskResponse, error)
	EditTask(ctx context.Context, input contract.EditTaskInput) (contract.EditTaskResponse, error)
	MoveTask(ctx context.Context, input contract.MoveTaskInput) (contract.MoveTaskResponse, error)
	AssignTask(ctx context.Context, input contract.AssignTaskInput) (contract.AssignTaskResponse, error)
	ListComments(ctx context.Context, input contract.ListCommentsInput) (contract.CommentsResponse, error)
	AddComment(ctx context.Context, input contract.AddCommentInput) (contract.CommentResponse, error)
	ListTaskActivity(ctx context.Context, input contract.ListTaskActivityInput) (contract.ListTaskActivityResponse, error)
	ListDependencies(ctx context.Context, input contract.ListDependenciesInput) (contract.DependenciesResponse, error)
	ListPlans(ctx context.Context, input contract.ListPlansInput) (contract.ListPlansResponse, error)
	ShowPlan(ctx context.Context, input contract.ShowPlanInput) (contract.ShowPlanResponse, error)
	Search(ctx context.Context, input contract.SearchInput) (contract.SearchResponse, error)
	ListLogs(ctx context.Context, input contract.ListLogsInput) (contract.ListLogsResponse, error)
	InsightsSummary(ctx context.Context, input contract.InsightsSummaryInput) (contract.InsightsSummaryResponse, error)
	MetricsSummary(ctx context.Context, input contract.MetricsSummaryInput) (contract.MetricsSummaryResponse, error)
}

// Runtimes resolves the operation facade for each request.
type Runtimes interface {
	// Project returns the facade and selector of the project with slug.
	Project(ctx context.Context, slug string) (Operations, contract.ProjectSelector, error)
	// Global returns the facade for project-independent operations.
	Global(ctx context.Context) (Operations, error)
	// Catalog returns the catalog for the GUI language preference.
	Catalog() *config.Catalog
}

// EventLog replays committed events for SSE resumption.
type EventLog interface {
	// EventsAfter returns up to limit visible rows with id > afterID,
	// oldest first.
	EventsAfter(ctx context.Context, afterID int64, limit int) ([]contract.LogsRow, error)
}
