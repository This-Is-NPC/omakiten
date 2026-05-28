package app

import (
	"context"
	"strings"
	"time"

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

// ListEventsParams is the per-call shape EventService.ListEvents accepts.
// Defaults are applied here so MCP / CLI / TUI surfaces hand the
// service raw parsed inputs without re-doing the resolution math:
//
//   - Empty Categories  → no category filter (every event_type).
//   - Since zero-value  → caller is expected to substitute the
//     project's configured window before invoking; the service
//     does not reach for *config.Snapshot to keep the dependency
//     surface narrow.
//   - Limit <= 0        → no row cap. Adapters/SQL layer may impose
//     their own ceiling for safety.
//   - Order ""          → adapter default ("desc"). Accepts
//     case-insensitive "asc" / "desc"; anything else collapses to
//     "desc" so consumers never receive a surprising ordering.
type ListEventsParams struct {
	Categories []domain.EventCategory
	Since      time.Time
	Limit      int
	Order      string
}

// ListEvents returns rows from the unified events log for the given
// project. Backs the generic Logs inspector surface so MCP / CLI / TUI
// all read the same shape; SummarizeEvent renders the per-row detail
// string at projection time.
//
// The service trims the order field, normalises it to "asc" / "desc"
// (anything else falls back to "desc"), and forwards everything else
// verbatim to the repository — the SQL layer owns category expansion
// and time-floor formatting.
func (s *EventService) ListEvents(ctx context.Context, project domain.ProjectContext, params ListEventsParams) (rows []domain.EventRow, err error) {
	finish := activity.Track(ctx, "app.EventService.ListEvents", project, map[string]any{
		"categories": params.Categories,
		"since":      params.Since,
		"limit":      params.Limit,
		"order":      params.Order,
	})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	order := strings.ToLower(strings.TrimSpace(params.Order))
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	rows, err = s.repo.ListEvents(ctx, domain.EventFilter{
		ProjectID:  project.ID,
		Categories: params.Categories,
		Since:      params.Since,
		Limit:      params.Limit,
		Order:      order,
	})
	return
}
