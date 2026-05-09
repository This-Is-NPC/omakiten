package app

import (
	"context"
	"encoding/json"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type ErrorService struct {
	repo     ErrorRepository
	defaults SolutionsDefaults
}

// SolutionsDefaults mirrors config.SolutionsSettings without forcing the
// app layer to import internal/config (avoids a cycle through BundleStore).
// Composition root resolves the values from the loaded bundle and writes
// them via SetSolutionsDefaults; tests that exercise ListTopSolutions wire
// their own values explicitly. Zero values mean "settings not yet wired"
// and ListTopSolutions errors loudly so the gap surfaces.
type SolutionsDefaults struct {
	// TopLimitDefault is the limit applied when the caller passes <=0.
	// Validator-required > 0 in the bundle.
	TopLimitDefault int
	// TopLimitMax caps caller-supplied limits. Validator-required >=
	// TopLimitDefault in the bundle.
	TopLimitMax int
}

func NewErrorService(repo ErrorRepository) *ErrorService {
	return &ErrorService{repo: repo}
}

// SetSolutionsDefaults installs the limits used by ListTopSolutions.
// Composition root calls this once at startup with values resolved from
// config.solutions.
func (s *ErrorService) SetSolutionsDefaults(defaults SolutionsDefaults) {
	s.defaults = defaults
}

// emitDomainEvent serializes payload to JSON and writes a domain event row.
// Telemetry must not break business logic: emission errors are swallowed by
// design, mirroring activity.Track's contract.
func (s *ErrorService) emitDomainEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType string, payload map[string]any) {
	body := "{}"
	if len(payload) > 0 {
		if b, err := json.Marshal(payload); err == nil {
			body = string(b)
		}
	}
	_ = s.repo.RecordEntityEvent(ctx, entityType, entityID, projectID, eventType, body)
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
	if err != nil {
		return
	}
	tagNames := make([]string, len(record.Tags))
	for i, t := range record.Tags {
		tagNames[i] = t.Name
	}
	s.emitDomainEvent(ctx, "error", record.ID, record.ProjectID, domain.EventTypeErrorRecorded, map[string]any{
		"tags":        tagNames,
		"has_context": record.Context != "",
	})
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
	cleanQuery := strings.TrimSpace(query)
	records, err = s.repo.SearchErrors(ctx, cleanQuery, tagNames)
	if err != nil {
		return
	}
	s.emitDomainEvent(ctx, "error", 0, project.ID, domain.EventTypeErrorSearched, map[string]any{
		"query":        cleanQuery,
		"tags":         tagNames,
		"result_count": len(records),
	})
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
	if err != nil {
		return
	}
	s.emitDomainEvent(ctx, "solution", solution.ID, project.ID, domain.EventTypeSolutionAdded, map[string]any{
		"error_id": solution.ErrorID,
	})
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
	if err != nil {
		return
	}
	s.emitDomainEvent(ctx, "solution", solution.ID, project.ID, domain.EventTypeSolutionConfirmed, map[string]any{
		"error_id": solution.ErrorID,
		"success":  success,
		"likes":    solution.Likes,
	})
	eventType := domain.EventTypeSolutionFailed
	if success {
		eventType = domain.EventTypeSolutionLiked
	}
	s.emitDomainEvent(ctx, "solution", solution.ID, project.ID, eventType, map[string]any{
		"error_id": solution.ErrorID,
		"likes":    solution.Likes,
	})
	return
}

// ListTopSolutions returns the N most-liked solutions globally (cross-project).
// Limits beyond config.solutions.max_top_limit are clamped so MCP responses
// stay bounded. Caller-omitted limit (<=0) inherits
// config.solutions.default_top_limit. Validator guarantees both knobs are
// present and positive when the bundle reaches runtime.
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

	if s.defaults.TopLimitDefault <= 0 || s.defaults.TopLimitMax <= 0 {
		err = domain.NewError(domain.ErrValidation, "ErrorService.SolutionsDefaults not configured", map[string]any{
			"hint": "composition root must call SetSolutionsDefaults from config.solutions before serving requests",
		})
		return
	}
	if limit <= 0 {
		limit = s.defaults.TopLimitDefault
	}
	if limit > s.defaults.TopLimitMax {
		limit = s.defaults.TopLimitMax
	}
	solutions, err = s.repo.ListTopSolutions(ctx, limit)
	if err != nil {
		return
	}
	s.emitDomainEvent(ctx, "solution", 0, project.ID, domain.EventTypeSolutionViewedTop, map[string]any{
		"limit":          limit,
		"returned_count": len(solutions),
	})
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
