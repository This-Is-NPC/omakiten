package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type CommentService struct {
	repo CommentRepository
}

func NewCommentService(repo CommentRepository) *CommentService {
	return &CommentService{repo: repo}
}

func (s *CommentService) Add(ctx context.Context, project domain.ProjectContext, taskID int64, body, authorType string, rawTags []string) (comment domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.Add", project, map[string]any{"task_id": taskID, "author": authorType})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task id must be positive", nil)
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		err = domain.NewError(domain.ErrValidation, "comment body is required", nil)
		return
	}
	authorType = strings.TrimSpace(authorType)
	if authorType == "" {
		authorType = "human"
	}
	if authorType != "human" && authorType != "agent" {
		err = domain.NewError(domain.ErrValidation, "author type must be human or agent", map[string]any{"author_type": authorType})
		return
	}

	tags := make([]domain.Tag, 0, len(rawTags))
	for _, raw := range rawTags {
		name := NormalizeTagName(raw)
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}

	comment, err = s.repo.AddComment(ctx, project.ID, taskID, body, authorType, tags)
	return
}

func (s *CommentService) List(ctx context.Context, project domain.ProjectContext, taskID int64) (comments []domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.List", project, map[string]any{"task_id": taskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID < 0 {
		err = domain.NewError(domain.ErrValidation, "task id cannot be negative", nil)
		return
	}
	comments, err = s.repo.ListComments(ctx, project.ID, taskID)
	return
}
