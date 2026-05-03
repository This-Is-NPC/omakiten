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

func (s *TaskService) Edit(ctx context.Context, project domain.ProjectContext, taskID int64, update domain.TaskUpdate) (domain.Task, error) {
	if taskID <= 0 {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "task id must be positive", nil)
	}

	changed := false
	if update.Title != nil {
		title := strings.TrimSpace(*update.Title)
		if title == "" {
			return domain.Task{}, domain.NewError(domain.ErrValidation, "task title is required", nil)
		}
		update.Title = &title
		changed = true
	}
	if update.Description != nil {
		description := strings.TrimSpace(*update.Description)
		update.Description = &description
		changed = true
	}
	if update.Priority != nil {
		priority := domain.Priority(strings.TrimSpace(string(*update.Priority)))
		switch priority {
		case domain.PriorityLow, domain.PriorityNormal, domain.PriorityHigh:
			update.Priority = &priority
		default:
			return domain.Task{}, domain.NewError(domain.ErrValidation, "priority must be low, normal, or high", map[string]any{"priority": *update.Priority})
		}
		changed = true
	}

	update.BucketKey = strings.TrimSpace(update.BucketKey)
	if update.BucketKey != "" {
		changed = true
	}
	if !changed {
		return domain.Task{}, domain.NewError(domain.ErrValidation, "at least one task update is required", nil)
	}

	var task domain.Task
	var err error
	if update.BucketKey != "" {
		task, err = s.repo.MoveTask(ctx, project.ID, taskID, update.BucketKey)
		if err != nil {
			return domain.Task{}, err
		}
	}
	if update.Title != nil || update.Description != nil || update.Priority != nil {
		task, err = s.repo.UpdateTask(ctx, project.ID, taskID, update)
		if err != nil {
			return domain.Task{}, err
		}
	}

	return task, nil
}
