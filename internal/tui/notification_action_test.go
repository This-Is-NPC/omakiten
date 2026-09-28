package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func TestHandleNotificationAction_emptyCommandSkipsDispatch(t *testing.T) {
	m, _ := newPickerModel(t)
	called := false
	m.repos.DispatchAction = func(_ context.Context, _ contract.ActionRequest) (contract.ActionResult, error) {
		called = true
		return contract.ActionResult{}, nil
	}
	m.handleNotificationAction(ActionMsg{Slug: "kit", ActionID: "skip", Operation: ""})
	if called {
		t.Fatal("DispatchCommand must not run for empty Command")
	}
	if !strings.Contains(m.status, "kit") || !strings.Contains(m.status, "skip") {
		t.Fatalf("status = %q, want hint mentioning slug + action id", m.status)
	}
}

func TestHandleNotificationAction_dispatchSuccessRefreshes(t *testing.T) {
	m, _ := newPickerModel(t)
	var gotRequest contract.ActionRequest
	m.repos.DispatchAction = func(_ context.Context, request contract.ActionRequest) (contract.ActionResult, error) {
		gotRequest = request
		return contract.ActionResult{Message: "applied"}, nil
	}
	m.handleNotificationAction(ActionMsg{Slug: "kit", ActionID: "apply", Operation: "workflow.show"})
	if gotRequest.Operation != "workflow.show" {
		t.Fatalf("action = %+v", gotRequest)
	}
	if m.status != "applied" {
		t.Fatalf("status = %q, want envelope message", m.status)
	}
}

func TestHandleNotificationAction_dispatchFailureKeepsErrorInStatus(t *testing.T) {
	m, _ := newPickerModel(t)
	m.repos.DispatchAction = func(_ context.Context, _ contract.ActionRequest) (contract.ActionResult, error) {
		return contract.ActionResult{}, domain.NewError(domain.ErrValidation, "bad args", nil)
	}
	m.handleNotificationAction(ActionMsg{Slug: "kit", ActionID: "apply", Operation: "workflow.show"})
	if !strings.Contains(m.status, "validation_error") || !strings.Contains(m.status, "bad args") {
		t.Fatalf("status = %q, want envelope error", m.status)
	}
}

func TestHandleNotificationAction_recordsConfirmationGranted(t *testing.T) {
	m, _ := newPickerModel(t)
	recorder := &recordingEventRepo{inner: m.repos.Events}
	m.repos.Events = recorder
	m.repos.DispatchAction = func(_ context.Context, _ contract.ActionRequest) (contract.ActionResult, error) {
		return contract.ActionResult{Message: "applied"}, nil
	}
	m.handleNotificationAction(ActionMsg{Slug: "kit", ActionID: "apply", Operation: "workflow.show"})
	got := recorder.countByType[domain.EventTypeConfirmationGranted]
	if got != 1 {
		t.Fatalf("confirmation.granted count = %d, want 1", got)
	}
}

// recordingEventRepo wraps the real app.EventRepository and bumps a
// per-eventType counter on every recorded write. Read paths delegate to
// inner so consumers (e.g. plan finalization, activity feed) still see
// real data. The embedded eventrepo.NoOp is a forward-compat safety net:
// when new methods land on app.EventRepository, the recorder inherits
// no-op defaults instead of failing to compile in lock-step.
type recordingEventRepo struct {
	inner       EventStore
	countByType map[string]int
}

func (r *recordingEventRepo) RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error {
	r.tick(eventType)
	return r.inner.RecordEntityEvent(ctx, entityType, entityID, projectID, eventType, payload)
}

func (r *recordingEventRepo) ListTaskActivity(ctx context.Context, projectID, taskID int64, order string) ([]domain.Event, error) {
	return r.inner.ListTaskActivity(ctx, projectID, taskID, order)
}

func (r *recordingEventRepo) ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.EventRow, error) {
	return r.inner.ListEvents(ctx, filter)
}

func (r *recordingEventRepo) EventCategoryCounts(ctx context.Context, projectID int64, since time.Time) (map[domain.EventCategory]int, error) {
	return r.inner.EventCategoryCounts(ctx, projectID, since)
}

func (r *recordingEventRepo) tick(eventType string) {
	if r.countByType == nil {
		r.countByType = map[string]int{}
	}
	r.countByType[eventType]++
}
