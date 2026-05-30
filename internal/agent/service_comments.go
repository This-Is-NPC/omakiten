package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// commentSinceLayout is the SQLite datetime shape the events table stamps via
// CURRENT_TIMESTAMP. The `since` window floor is formatted with this layout so
// CommentFilter.CreatedAfter compares lexicographically against created_at.
const commentSinceLayout = "2006-01-02 15:04:05"

func (s *Service) AddComment(ctx context.Context, input AddCommentInput) (CommentResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentResponse{}, err
	}
	body := input.Body
	if input.TemplateSlug != "" {
		merged, _, err := s.applyTemplateBody(input.TemplateSlug, body, TemplateKindComment)
		if err != nil {
			return CommentResponse{}, err
		}
		body = merged
	}
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = domain.CommentScopeTask
	}
	switch scope {
	case domain.CommentScopeTask:
		if input.TaskID <= 0 {
			return CommentResponse{}, domain.NewError(domain.ErrValidation, "task scope requires task_id", map[string]any{"scope": scope})
		}
	case domain.CommentScopeProject:
		if input.TaskID > 0 {
			return CommentResponse{}, domain.NewError(domain.ErrValidation, "project scope must not carry task_id", map[string]any{"scope": scope, "task_id": input.TaskID})
		}
	case domain.CommentScopeUniversal:
		if input.TaskID > 0 {
			return CommentResponse{}, domain.NewError(domain.ErrValidation, "universal scope must not carry task_id", map[string]any{"scope": scope, "task_id": input.TaskID})
		}
	default:
		return CommentResponse{}, domain.NewError(domain.ErrValidation, "unknown comment scope", map[string]any{"scope": scope})
	}

	tags := make([]domain.Tag, 0, len(input.Tags))
	for _, raw := range input.Tags {
		tags = append(tags, domain.Tag{Name: raw, Label: raw})
	}
	comment, err := s.newCommentService().AddScoped(ctx, project, domain.CommentWrite{
		Scope:      scope,
		TaskID:     input.TaskID,
		Body:       body,
		Title:      strings.TrimSpace(input.Title),
		Kind:       strings.TrimSpace(input.Kind),
		Pinned:     input.Pinned,
		AuthorType: input.AuthorType,
		Tags:       tags,
	})
	if err != nil {
		return CommentResponse{}, err
	}
	return CommentResponse{Project: projectSummary(project), Comment: commentSummary(comment)}, nil
}

func (s *Service) EditComment(ctx context.Context, input EditCommentInput) (CommentResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentResponse{}, err
	}
	workflow := s.workflow
	edit := domain.CommentEdit{
		Pinned: input.Pinned,
	}
	if input.Body != nil {
		trimmed := strings.TrimSpace(*input.Body)
		edit.Body = &trimmed
	}
	if input.Title != nil {
		trimmed := strings.TrimSpace(*input.Title)
		edit.Title = &trimmed
	}
	if input.Kind != nil {
		trimmed := strings.TrimSpace(*input.Kind)
		edit.Kind = &trimmed
	}
	comment, err := s.newCommentServiceWithWorkflow(workflow).EditScoped(ctx, project, input.CommentID, edit, input.Tags)
	if err != nil {
		return CommentResponse{}, err
	}
	return CommentResponse{Project: projectSummary(project), Comment: commentSummary(comment)}, nil
}

func (s *Service) DeleteComment(ctx context.Context, input DeleteCommentInput) (DeleteCommentResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DeleteCommentResponse{}, err
	}
	if !input.Confirmed {
		return DeleteCommentResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Deleting a comment is destructive. Confirm with confirmed=true to proceed.",
				Options: []ConfirmationOption{
					{Action: "confirm_delete", Label: "Retry comments.delete with confirmed=true to hard-delete"},
				},
			},
		}, nil
	}
	workflow := s.workflow
	event, err := s.newCommentServiceWithWorkflow(workflow).Remove(ctx, project, input.CommentID)
	if err != nil {
		return DeleteCommentResponse{}, err
	}
	snap := eventSummary(event)
	return DeleteCommentResponse{Project: projectSummary(project), Snapshot: &snap}, nil
}

// ListComments is the comments.list handler. It is project-scoped by design:
// the resolved project's id is stamped onto filter.ProjectID for every routine
// list (task/project/kind/tag/pinned/query/since), so a normal call NEVER leaks
// comments from other projects. The only project-less reads are deliberate and
// narrow: scope=universal (universal comments carry project_id NULL and only
// match when ProjectID=0). The comment_id get-by-id path still carries the
// caller's project id (the store scopes task/project rows to it and lets
// universal rows through), so it cannot read another project's comment. The
// cross-project handoff/standup digest is not a single project-less call — the
// agent enumerates each project and issues one scope=project call per project,
// each carrying that project's id (see okt-note-recap in the command table).
func (s *Service) ListComments(ctx context.Context, input ListCommentsInput) (CommentsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentsResponse{}, err
	}
	scope := strings.TrimSpace(input.Scope)
	kind := strings.TrimSpace(input.Kind)
	tag := strings.TrimSpace(input.Tag)
	query := strings.TrimSpace(input.Query)
	since := strings.TrimSpace(input.Since)

	// Pure task-scoped listing (no extra filters) keeps the original List path
	// so the default behaviour is byte-for-byte unchanged. A comment_id (the
	// get-by-id path) routes through Query so the id filter applies.
	if scope == "" && kind == "" && tag == "" && query == "" && since == "" && !input.Pinned && input.CommentID <= 0 {
		comments, err := s.newCommentService().List(ctx, project, input.TaskID)
		if err != nil {
			return CommentsResponse{}, err
		}
		return CommentsResponse{Project: projectSummary(project), Comments: commentSummaries(comments)}, nil
	}

	// Universal comments carry project_id NULL and only match when the filter's
	// ProjectID is 0; scoping the query to the active project would exclude them.
	projectID := project.ID
	if scope == domain.CommentScopeUniversal {
		projectID = 0
	}
	// A comment_id (get-by-id) names a globally unique row, but it must NOT leak
	// another project's task/project comment. Keep the caller's project id: the
	// store's id path scopes task/project rows to it while still letting universal
	// (project-less) rows fall through, so okt-note-show fetches a universal note
	// without exposing project B's private comments to project A.
	filter := domain.CommentFilter{
		CommentID:  input.CommentID,
		Scope:      scope,
		ProjectID:  projectID,
		TaskID:     input.TaskID,
		Kind:       kind,
		Tag:        tag,
		PinnedOnly: input.Pinned,
		Search:     query,
	}
	if since != "" {
		floor, err := resolveLogsSince(since, s.snapshot, s.nowFunc())
		if err != nil {
			return CommentsResponse{}, err
		}
		if !floor.IsZero() {
			filter.CreatedAfter = floor.UTC().Format(commentSinceLayout)
		}
	}
	comments, err := s.newCommentService().Query(ctx, project, filter)
	if err != nil {
		return CommentsResponse{}, err
	}
	return CommentsResponse{Project: projectSummary(project), Comments: commentSummaries(comments)}, nil
}

func (s *Service) ListTaskActivity(ctx context.Context, input ListTaskActivityInput) (ListTaskActivityResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListTaskActivityResponse{}, err
	}
	events, err := app.NewEventService(s.repo).ListTaskActivity(ctx, project, input.TaskID, input.Order)
	if err != nil {
		return ListTaskActivityResponse{}, err
	}
	resolvedOrder := strings.ToLower(strings.TrimSpace(input.Order))
	if resolvedOrder != "asc" && resolvedOrder != "desc" {
		resolvedOrder = "asc"
	}
	return ListTaskActivityResponse{Project: projectSummary(project), Events: eventSummaries(events), Order: resolvedOrder}, nil
}
