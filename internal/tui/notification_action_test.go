package tui

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/notification"
)

func TestHandleNotificationAction_emptyCommandSkipsDispatch(t *testing.T) {
	m, _ := newPickerModel(t)
	called := false
	m.repos.DispatchCommand = func(_ context.Context, _ []string) ([]byte, error) {
		called = true
		return nil, nil
	}
	m.handleNotificationAction(notification.ActionMsg{Slug: "kit", ActionID: "skip", Command: nil})
	if called {
		t.Fatal("DispatchCommand must not run for empty Command")
	}
	if !strings.Contains(m.status, "kit") || !strings.Contains(m.status, "skip") {
		t.Fatalf("status = %q, want hint mentioning slug + action id", m.status)
	}
}

func TestHandleNotificationAction_dispatchSuccessRefreshes(t *testing.T) {
	m, _ := newPickerModel(t)
	gotArgs := []string(nil)
	m.repos.DispatchCommand = func(_ context.Context, args []string) ([]byte, error) {
		gotArgs = args
		return []byte(`{"ok":true,"data":{"message":"applied"}}` + "\n"), nil
	}
	m.handleNotificationAction(notification.ActionMsg{Slug: "kit", ActionID: "apply", Command: []string{"workflow", "show"}})
	if len(gotArgs) != 2 || gotArgs[0] != "workflow" || gotArgs[1] != "show" {
		t.Fatalf("DispatchCommand received %v, want [workflow show]", gotArgs)
	}
	if m.status != "applied" {
		t.Fatalf("status = %q, want envelope message", m.status)
	}
}

func TestHandleNotificationAction_dispatchFailureKeepsErrorInStatus(t *testing.T) {
	m, _ := newPickerModel(t)
	m.repos.DispatchCommand = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(`{"ok":false,"code":"validation_error","msg":"bad args"}` + "\n"), nil
	}
	m.handleNotificationAction(notification.ActionMsg{Slug: "kit", ActionID: "apply", Command: []string{"workflow", "show"}})
	if !strings.Contains(m.status, "validation_error") || !strings.Contains(m.status, "bad args") {
		t.Fatalf("status = %q, want envelope error", m.status)
	}
}

func TestHandleNotificationAction_recordsConfirmationGranted(t *testing.T) {
	m, _ := newPickerModel(t)
	recorder := &recordingEventRepo{inner: m.repos.Events}
	m.repos.Events = recorder
	m.repos.DispatchCommand = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(`{"ok":true,"data":{"message":"applied"}}` + "\n"), nil
	}
	m.handleNotificationAction(notification.ActionMsg{Slug: "kit", ActionID: "apply", Command: []string{"workflow", "show"}})
	got := recorder.countByType[domain.EventTypeConfirmationGranted]
	if got != 1 {
		t.Fatalf("confirmation.granted count = %d, want 1", got)
	}
}

type recordingEventRepo struct {
	inner       app.EventRepository
	countByType map[string]int
}

func (r *recordingEventRepo) RecordTaskEvent(ctx context.Context, projectID, taskID int64, eventType, body, payload string) (domain.Event, error) {
	r.tick(eventType)
	return r.inner.RecordTaskEvent(ctx, projectID, taskID, eventType, body, payload)
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

func (r *recordingEventRepo) tick(eventType string) {
	if r.countByType == nil {
		r.countByType = map[string]int{}
	}
	r.countByType[eventType]++
}
