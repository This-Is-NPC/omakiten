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
	comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, body, input.AuthorType, input.Tags)
	if err != nil {
		return CommentResponse{}, err
	}
	return CommentResponse{Project: projectSummary(project), Comment: commentSummary(comment)}, nil
}

func (s *Service) ListComments(ctx context.Context, input ListCommentsInput) (CommentsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentsResponse{}, err
	}
	comments, err := app.NewCommentService(s.repo).List(ctx, project, input.TaskID)
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
