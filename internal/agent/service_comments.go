package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
)

func (s *Service) AddComment(ctx context.Context, input AddCommentInput) (CommentResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentResponse{}, err
	}
	body := input.Body
	if input.TemplateSlug != "" {
		merged, _, err := s.applyTemplateBody(input.TemplateSlug, body, "comment")
		if err != nil {
			return CommentResponse{}, err
		}
		body = merged
	}
	comment, err := s.newCommentService().Add(ctx, project, input.TaskID, body, input.AuthorType, input.Tags)
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
	workflow := app.NewWorkflowServiceFromStore(s.repo, s.registry, s.snapshot)
	comment, err := s.newCommentServiceWithWorkflow(workflow).Edit(ctx, project, input.CommentID, input.Body, input.Tags)
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
	workflow := app.NewWorkflowServiceFromStore(s.repo, s.registry, s.snapshot)
	event, err := s.newCommentServiceWithWorkflow(workflow).Remove(ctx, project, input.CommentID)
	if err != nil {
		return DeleteCommentResponse{}, err
	}
	snap := eventSummary(event)
	return DeleteCommentResponse{Project: projectSummary(project), Snapshot: &snap}, nil
}

func (s *Service) ListComments(ctx context.Context, input ListCommentsInput) (CommentsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentsResponse{}, err
	}
	comments, err := s.newCommentService().List(ctx, project, input.TaskID)
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
