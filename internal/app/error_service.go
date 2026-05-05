package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type ErrorService struct {
	repo ErrorRepository
}

func NewErrorService(repo ErrorRepository) *ErrorService {
	return &ErrorService{repo: repo}
}

func (s *ErrorService) Record(ctx context.Context, project domain.ProjectContext, description, errContext string, rawTags []string) (record domain.ErrorRecord, err error) {
	finish := activity.Track(ctx, "app.ErrorService.Record", project, map[string]any{"tags": rawTags})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	description = strings.TrimSpace(description)
	if description == "" {
		err = domain.NewError(domain.ErrValidation, "error description is required", nil)
		return
	}

	tags := normalizeTagInputs(rawTags)
	record, err = s.repo.RecordError(ctx, project.ID, description, strings.TrimSpace(errContext), tags)
	return
}

func (s *ErrorService) Search(ctx context.Context, project domain.ProjectContext, query string, rawTags []string) (records []domain.ErrorRecord, err error) {
	finish := activity.Track(ctx, "app.ErrorService.Search", project, map[string]any{"query": query, "tags": rawTags})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	tagNames := make([]string, 0, len(rawTags))
	for _, raw := range rawTags {
		name := NormalizeTagName(raw)
		if name != "" {
			tagNames = append(tagNames, name)
		}
	}
	records, err = s.repo.SearchErrors(ctx, strings.TrimSpace(query), tagNames)
	return
}

func (s *ErrorService) AddSolution(ctx context.Context, project domain.ProjectContext, errorID int64, description, steps string, taskID *int64) (solution domain.Solution, err error) {
	finish := activity.Track(ctx, "app.ErrorService.AddSolution", project, map[string]any{"error_id": errorID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if errorID <= 0 {
		err = domain.NewError(domain.ErrValidation, "error_id must be positive", nil)
		return
	}
	description = strings.TrimSpace(description)
	if description == "" {
		err = domain.NewError(domain.ErrValidation, "solution description is required", nil)
		return
	}
	if taskID != nil && *taskID <= 0 {
		taskID = nil
	}
	solution, err = s.repo.AddSolution(ctx, errorID, description, strings.TrimSpace(steps), taskID)
	return
}

func (s *ErrorService) ConfirmSolution(ctx context.Context, project domain.ProjectContext, solutionID int64, success bool) (solution domain.Solution, err error) {
	finish := activity.Track(ctx, "app.ErrorService.ConfirmSolution", project, map[string]any{"solution_id": solutionID, "success": success})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if solutionID <= 0 {
		err = domain.NewError(domain.ErrValidation, "solution_id must be positive", nil)
		return
	}
	solution, err = s.repo.ConfirmSolution(ctx, solutionID, success)
	return
}

const defaultTopSolutionsLimit = 10

// ListTopSolutions returns the N most-liked solutions globally (cross-project).
// Limits beyond 100 are clamped to keep MCP responses bounded.
func (s *ErrorService) ListTopSolutions(ctx context.Context, project domain.ProjectContext, limit int) (solutions []domain.Solution, err error) {
	finish := activity.Track(ctx, "app.ErrorService.ListTopSolutions", project, map[string]any{"limit": limit})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if limit <= 0 {
		limit = defaultTopSolutionsLimit
	}
	if limit > 100 {
		limit = 100
	}
	solutions, err = s.repo.ListTopSolutions(ctx, limit)
	return
}

func normalizeTagInputs(rawTags []string) []domain.Tag {
	tags := make([]domain.Tag, 0, len(rawTags))
	seen := map[string]struct{}{}
	for _, raw := range rawTags {
		name := NormalizeTagName(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}
	return tags
}
