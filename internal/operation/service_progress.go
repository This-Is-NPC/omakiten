package operation

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func (s *Service) RecordProgress(ctx context.Context, input RecordProgressInput) (RecordProgressResponse, error) {
	if err := s.allow("progress.record"); err != nil {
		return RecordProgressResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RecordProgressResponse{}, err
	}

	if input.TaskID <= 0 {
		return RecordProgressResponse{}, domain.NewError(domain.ErrValidation, "task_id is required for task edits, comments, and workflow moves", nil)
	}

	response := RecordProgressResponse{Project: projectSummary(project)}
	if progressHasTaskEdit(input) {
		task, err := s.updateProgressTask(ctx, project, input)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := taskSummary(task, s.registry)
		response.Task = &summary
	}
	if strings.TrimSpace(input.Comment) != "" {
		comment, err := s.newCommentService().Add(ctx, project, input.TaskID, input.Comment, input.AuthorType, nil)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := commentSummary(comment)
		response.Comment = &summary
	}

	return response, nil
}

func progressHasTaskEdit(input RecordProgressInput) bool {
	return input.Title != nil || input.Description != nil || input.Priority != nil || strings.TrimSpace(input.MoveToBucket) != ""
}

func (s *Service) updateProgressTask(ctx context.Context, project domain.ProjectContext, input RecordProgressInput) (domain.Task, error) {
	update := domain.TaskUpdate{
		Title:       input.Title,
		Description: input.Description,
		BucketKey:   input.MoveToBucket,
	}
	if input.Priority != nil {
		label := strings.TrimSpace(*input.Priority)
		if label == "" {
			return domain.Task{}, domain.NewError(domain.ErrValidation,
				"priority must be a non-empty label when provided; omit the field to leave it unchanged",
				map[string]any{"priority": *input.Priority})
		}
		p, ok := s.registry.PriorityFromLabel(label)
		if !ok {
			return domain.Task{}, domain.NewError(domain.ErrValidation,
				"unknown priority label; must match a value in config.priorities",
				map[string]any{"priority": label})
		}
		update.Priority = &p
	}
	return app.NewTaskServiceFromStore(s.repo, s.registry, s.snapshot).Edit(ctx, project, input.TaskID, update)
}
