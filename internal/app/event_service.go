package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type EventService struct {
	repo EventRepository
}

func NewEventService(repo EventRepository) *EventService {
	return &EventService{repo: repo}
}

// ListTaskActivity returns the unified feed for a task: comments and
// system events ordered by created_at. order is "asc" (default — chronological)
// or "desc". The repository decides how to read it; this layer only validates
// inputs and tracks the call.
func (s *EventService) ListTaskActivity(ctx context.Context, project domain.ProjectContext, taskID int64, order string) (events []domain.Event, err error) {
	finish := activity.Track(ctx, "app.EventService.ListTaskActivity", project, map[string]any{"task_id": taskID, "order": order})
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
	order = strings.ToLower(strings.TrimSpace(order))
	if order != "asc" && order != "desc" {
		order = "asc"
	}
	events, err = s.repo.ListTaskActivity(ctx, project.ID, taskID, order)
	return
}
