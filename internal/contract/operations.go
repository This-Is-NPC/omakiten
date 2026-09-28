package contract

import (
	"context"

	"omakiten/internal/domain"
)

type Operations interface {
	AddComment(ctx context.Context, input AddCommentInput) (CommentResponse, error)
	EditComment(ctx context.Context, input EditCommentInput) (CommentResponse, error)
	DeleteComment(ctx context.Context, input DeleteCommentInput) (DeleteCommentResponse, error)
	AddSkill(ctx context.Context, input domain.SkillInput) (domain.Skill, error)
	RemoveSkill(ctx context.Context, ref string) (string, error)
	AddLaw(ctx context.Context, input domain.LawInput) (domain.Law, error)
	RemoveLaw(ctx context.Context, ref string) (string, error)
	AddPersona(ctx context.Context, input domain.PersonaInput) (domain.Persona, error)
	EditPersona(ctx context.Context, ref string, update domain.PersonaUpdate) (domain.Persona, error)
	RemovePersona(ctx context.Context, ref string) (string, error)
	MetricsSummary(ctx context.Context, input MetricsSummaryInput) (MetricsSummaryResponse, error)
	MigrateOrphans(ctx context.Context, input MigrateOrphansInput) (MigrateOrphansResponse, error)
	EditPlan(ctx context.Context, input EditPlanInput) (EditPlanResponse, error)
	ResumeProject(ctx context.Context, input ResumeProjectInput) (ResumeProjectResponse, error)
	Search(ctx context.Context, input SearchInput) (SearchResponse, error)
	AddTag(ctx context.Context, input AddTagInput) (TagResponse, error)
	RemoveTag(ctx context.Context, input RemoveTagInput) (RemoveTagResponse, error)
	MergeTags(ctx context.Context, input MergeTagsInput) (TagResponse, error)
	CreateTask(ctx context.Context, input CreateTaskInput) (CreateTaskResponse, error)
	EditTask(ctx context.Context, input EditTaskInput) (EditTaskResponse, error)
	MoveTask(ctx context.Context, input MoveTaskInput) (MoveTaskResponse, error)
	DeleteTask(ctx context.Context, input DeleteTaskInput) (DeleteTaskResponse, error)
	ArchiveTask(ctx context.Context, input ArchiveTaskInput) (ArchiveTaskResponse, error)
	UnarchiveTask(ctx context.Context, input ArchiveTaskInput) (ArchiveTaskResponse, error)
	AssignTask(ctx context.Context, input AssignTaskInput) (AssignTaskResponse, error)
	BoardSnapshot(ctx context.Context, input BoardSnapshotInput) (BoardSnapshot, error)
	ListPlanRollups(ctx context.Context, input ProjectSelector) ([]domain.PlanRollup, error)
	ShowPlanView(ctx context.Context, input ShowPlanInput) (domain.PlanShow, error)
	SyncBlockers(ctx context.Context, input SyncBlockersInput) error
	QueryComments(ctx context.Context, project domain.ProjectContext, filter domain.CommentFilter) ([]domain.Comment, error)
	InsightsToday(ctx context.Context, project domain.ProjectContext, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error)
	ResolveBucketPermissions(ctx context.Context, project domain.ProjectContext, taskID int64, entity, op string) (bool, string, error)
	EmitGuardViolated(ctx context.Context, projectID int64, entityType string, entityID int64, operation, rule, hint string, target map[string]any)
	SetTemplateDefault(ctx context.Context, slug, kind, projectSlug string) error
}
