package app

import (
	"context"
	"strings"

	"omakiten/internal/domain"
)

type TaskService struct {
	repo TaskRepository
}

func NewTaskService(repo TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

func (s *TaskService) Add(ctx context.Context, project domain.ProjectContext, title, description, bucketKey string) (domain.Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "task title is required", nil)
	}

	return s.repo.CreateTask(ctx, project.ID, title, strings.TrimSpace(description), strings.TrimSpace(bucketKey))
}

func (s *TaskService) List(ctx context.Context, project domain.ProjectContext, filter domain.TaskFilter) ([]domain.Task, error) {
	return s.repo.ListTasks(ctx, project.ID, filter)
}

func (s *TaskService) Move(ctx context.Context, project domain.ProjectContext, taskID int64, targetBucketKey string) (domain.Task, error) {
	if taskID <= 0 {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "task id must be positive", nil)
	}
	if strings.TrimSpace(targetBucketKey) == "" {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "target bucket is required", nil)
	}

	return s.repo.MoveTask(ctx, project.ID, taskID, targetBucketKey)
}
