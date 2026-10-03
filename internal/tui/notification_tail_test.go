package tui

import (
	"errors"
	"testing"

	"omakiten/internal/domain"
)

func TestNoticeTailShowsNotificationsRecordedElsewhere(t *testing.T) {
	model := newNotificationTestModel(t)

	// The first read places the cursor; what came before the TUI opened stays shown nowhere.
	next, _ := model.applyNoticeRows(noticeRowsMsg{rows: []domain.EventRow{
		{ID: 7, EventType: domain.EventTypeNotificationShown, Payload: `{"notification":"guard-violation","text":"old"}`},
	}})
	model = next.(Model)
	if model.notification != nil || model.noticeCursor != 7 || !model.noticeStarted {
		t.Fatalf("first read showed %v at cursor %d, want nothing shown and the cursor at 7", model.notification != nil, model.noticeCursor)
	}

	next, _ = model.applyNoticeRows(noticeRowsMsg{started: true, rows: []domain.EventRow{
		{ID: 8, EventType: domain.EventTypeNotificationShown, Payload: `{"notification":"guard-violation","text":"first"}`},
		{ID: 9, EventType: domain.EventTypeHookExecuted, Payload: `{}`},
		{ID: 10, EventType: domain.EventTypeNotificationShown, Payload: `{"notification":"guard-violation","text":"newest","detail":"tag the branch"}`},
	}})
	model = next.(Model)
	if model.noticeCursor != 10 {
		t.Fatalf("cursor = %d, want 10", model.noticeCursor)
	}
	if model.notification == nil {
		t.Fatal("no notification shown for the recorded rows")
	}
	if got := model.notification.typed(); got != "newest" {
		t.Fatalf("shown text = %q, want the newest notification", got)
	}
}

func TestNoticeTailSkipsCardsTheKitDoesNotLoad(t *testing.T) {
	model := newNotificationTestModel(t)
	model.noticeStarted = true
	next, _ := model.applyNoticeRows(noticeRowsMsg{started: true, rows: []domain.EventRow{
		{ID: 3, EventType: domain.EventTypeNotificationShown, Payload: `{"notification":"ghost","text":"x"}`},
	}})
	if next.(Model).notification != nil {
		t.Fatal("a notification with no loaded card was shown")
	}
}

func TestNoticeTailKeepsTheCursorOnAFailedRead(t *testing.T) {
	model := newNotificationTestModel(t)
	next, _ := model.applyNoticeRows(noticeRowsMsg{err: errors.New("database is locked")})
	if got := next.(Model); got.noticeStarted || got.noticeCursor != 0 {
		t.Fatalf("a failed first read started the tail at %d", got.noticeCursor)
	}
}
