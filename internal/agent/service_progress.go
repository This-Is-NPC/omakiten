package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func (s *Service) RecordProgress(ctx context.Context, input RecordProgressInput) (RecordProgressResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RecordProgressResponse{}, err
	}

	if input.TaskID <= 0 && (input.Title != nil || input.Description != nil || input.Priority != nil || strings.TrimSpace(input.MoveToBucket) != "" || strings.TrimSpace(input.Comment) != "") {
		return RecordProgressResponse{}, domain.NewError(domain.ErrValidation, "task_id is required for task edits, comments, and workflow moves", nil)
	}
	if input.TaskID <= 0 && strings.TrimSpace(input.Context) == "" {
		return RecordProgressResponse{}, domain.NewError(domain.ErrValidation, "at least one progress update is required", nil)
	}

	response := RecordProgressResponse{Project: projectSummary(project)}
	if input.TaskID > 0 && (input.Title != nil || input.Description != nil || input.Priority != nil || strings.TrimSpace(input.MoveToBucket) != "") {
		update := domain.TaskUpdate{
			Title:       input.Title,
			Description: input.Description,
			BucketKey:   input.MoveToBucket,
		}
		if input.Priority != nil {
			label := strings.TrimSpace(*input.Priority)
			if label == "" {
				return RecordProgressResponse{}, domain.NewError(domain.ErrValidation,
					"priority must be a non-empty label when provided; omit the field to leave it unchanged",
					map[string]any{"priority": *input.Priority})
			}
			p, ok := s.registry.PriorityFromLabel(label)
			if !ok {
				return RecordProgressResponse{}, domain.NewError(domain.ErrValidation,
					"unknown priority label; must match a value in config.priorities",
					map[string]any{"priority": label})
			}
			update.Priority = &p
		}
		task, err := app.NewTaskServiceFromStore(s.repo, s.registry).Edit(ctx, project, input.TaskID, update)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := taskSummary(task, s.registry)
		response.Task = &summary
	}
	if strings.TrimSpace(input.Comment) != "" {
		comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, input.Comment, input.AuthorType, nil)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := commentSummary(comment)
		response.Comment = &summary
	}
	if strings.TrimSpace(input.Context) != "" {
		entry, err := app.NewContextService(s.repo, s.repo, s.repo, s.repo, s.repo, s.counter, s.registry).Add(ctx, project, input.Context)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := contextSnippet(entry)
		response.ContextEntry = &summary
	}

	return response, nil
}
