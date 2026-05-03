package app

import (
	"context"
	"strings"

	"omakiten/internal/domain"
)

type CommentService struct {
	repo CommentRepository
}

func NewCommentService(repo CommentRepository) *CommentService {
	return &CommentService{repo: repo}
}

func (s *CommentService) Add(ctx context.Context, project domain.ProjectContext, taskID int64, body, authorType string) (domain.Comment, error) {
	if taskID <= 0 {
		return domain.Comment{}, domain.NewError(domain.ErrValidation, "task id must be positive", nil)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.Comment{}, domain.NewError(domain.ErrValidation, "comment body is required", nil)
	}
	authorType = strings.TrimSpace(authorType)
	if authorType == "" {
		authorType = "human"
	}
	if authorType != "human" && authorType != "agent" {
		return domain.Comment{}, domain.NewError(domain.ErrValidation, "author type must be human or agent", map[string]any{"author_type": authorType})
	}

	return s.repo.AddComment(ctx, project.ID, taskID, body, authorType)
}

func (s *CommentService) List(ctx context.Context, project domain.ProjectContext, taskID int64) ([]domain.Comment, error) {
	if taskID < 0 {
		return nil, domain.NewError(domain.ErrValidation, "task id cannot be negative", nil)
	}
	return s.repo.ListComments(ctx, project.ID, taskID)
}
