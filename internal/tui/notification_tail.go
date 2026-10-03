package tui

import (
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	hookactions "omakiten/internal/hooks/actions"
)

// noticeBatch caps the hook rows one tail read takes.
const noticeBatch = 200

// noticeTickMsg asks the notification tail to read; one read ends in the
// next tick, so reads never overlap.
type noticeTickMsg struct{}

func scheduleNoticeTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return noticeTickMsg{} })
}

// noticeRowsMsg carries the hook rows recorded after the cursor; started
// marks the reads after the first, which only places the cursor.
type noticeRowsMsg struct {
	rows    []domain.EventRow
	started bool
	err     error
}

// noticeTailCmd reads the hook rows other processes recorded since the last
// read, so the TUI shows the notifications they raised with no screen: an
// agent's CLI call, the daemon. The first read places the cursor at the
// newest row; what came before the TUI opened is not replayed. It reads
// only while a notification could be shown, and otherwise waits a tick.
func (m Model) noticeTailCmd() tea.Cmd {
	if m.repos.Events == nil || m.notification != nil || m.helpOpen || m.paletteOpen || m.mode != modeNormal {
		return scheduleNoticeTick()
	}
	events, ctx, started := m.repos.Events, m.ctx, m.noticeStarted
	filter := domain.EventFilter{ProjectID: m.project.ID, Categories: []domain.EventCategory{domain.EventCategoryHook}, AfterID: m.noticeCursor, Limit: noticeBatch}
	if !started {
		filter = domain.EventFilter{ProjectID: m.project.ID, Categories: []domain.EventCategory{domain.EventCategoryHook}, Limit: 1, Order: "desc"}
	}
	return func() tea.Msg {
		rows, err := events.ListEvents(ctx, filter)
		return noticeRowsMsg{rows: rows, started: started, err: err}
	}
}

// applyNoticeRows advances the cursor, shows the newest notification the
// rows hold, and schedules the next read.
func (m Model) applyNoticeRows(msg noticeRowsMsg) (tea.Model, tea.Cmd) {
	// A failed read leaves the cursor where it was for the next one.
	if msg.err != nil {
		return m, scheduleNoticeTick()
	}
	m.noticeStarted = true
	var newest *domain.EventRow
	for i := range msg.rows {
		row := &msg.rows[i]
		m.noticeCursor = max(m.noticeCursor, row.ID)
		if msg.started && row.EventType == domain.EventTypeNotificationShown {
			newest = row
		}
	}
	if newest == nil {
		return m, scheduleNoticeTick()
	}
	var payload struct {
		Notification string `json:"notification"`
		Text         string `json:"text"`
		Detail       string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(newest.Payload), &payload); err != nil {
		return m, scheduleNoticeTick()
	}
	card, ok := m.notifications[payload.Notification]
	if !ok {
		return m, scheduleNoticeTick()
	}
	next, cmd, _ := m.dispatchNotificationShow(hookactions.NotificationShowMsg{Notification: card, Text: payload.Text, DetailText: payload.Detail})
	return next, tea.Batch(cmd, scheduleNoticeTick())
}
