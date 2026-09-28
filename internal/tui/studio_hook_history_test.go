package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/studioprojection"
)

type stubHookEvents struct {
	rows    []domain.EventRow
	err     error
	filter  domain.EventFilter
	wait    <-chan struct{}
	started chan<- struct{}
}

func (s *stubHookEvents) ListEvents(_ context.Context, filter domain.EventFilter) ([]domain.EventRow, error) {
	s.filter = filter
	if s.started != nil {
		close(s.started)
		s.started = nil
	}
	if s.wait != nil {
		<-s.wait
	}
	if s.err != nil {
		return nil, s.err
	}
	rows := make([]domain.EventRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.ProjectID == 0 || row.ProjectID == filter.ProjectID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (*stubHookEvents) ListTaskActivity(context.Context, int64, int64, string) ([]domain.Event, error) {
	return nil, nil
}

func (*stubHookEvents) RecordEntityEvent(context.Context, string, int64, int64, string, string) error {
	return nil
}

func (*stubHookEvents) EventCategoryCounts(context.Context, int64, time.Time) (map[domain.EventCategory]int, error) {
	return nil, nil
}

func TestStudioHookHistoryAdapterFiltersByHookIndex(t *testing.T) {
	t.Parallel()

	events := &stubHookEvents{rows: []domain.EventRow{
		{EventType: domain.EventTypeHookExecuted, CreatedAt: "2026-08-13 14:02:11", Payload: `{"hook_index":4,"success":true,"duration_ms":4,"event_type":"guard.violated","target_event_id":2457}`},
		{EventType: domain.EventTypeHookExecuted, CreatedAt: "2026-08-13 14:33:01", Payload: `{"hook_index":30,"success":false,"duration_ms":3001,"event_type":"task.created","error":"exec bash timed out after 3s"}`},
		{EventType: "task.created", CreatedAt: "2026-08-13 14:00:00", Payload: `{"hook_index":4}`},
	}}
	history, err := studioprojection.LoadHookHistory(context.Background(), events, 42, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if events.filter.ProjectID != 42 {
		t.Fatalf("history filter project id = %d, want 42", events.filter.ProjectID)
	}
	got := history[4]
	if len(got) != 1 {
		t.Fatalf("index 4 rows = %d, want 1", len(got))
	}
	if !got[0].Success || got[0].EventType != "guard.violated" || got[0].TargetEventID != 2457 {
		t.Fatalf("index 4 row = %+v", got[0])
	}
	fail := history[30]
	if len(fail) != 1 || fail[0].Success || !strings.Contains(fail[0].Error, "timed out") {
		t.Fatalf("index 30 row = %+v", fail)
	}
	empty := history[0]
	if len(empty) != 0 {
		t.Fatalf("index 0 rows = %d, want 0", len(empty))
	}
}

func TestStudioHookHistoryAdapterNilEvents(t *testing.T) {
	t.Parallel()
	got, err := studioprojection.LoadHookHistory(context.Background(), nil, 42, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("nil events returned %d rows", len(got))
	}
}

func TestStudioHookHistoryAdapterKeepsProjectsSeparate(t *testing.T) {
	t.Parallel()

	events := &stubHookEvents{rows: []domain.EventRow{
		{ProjectID: 1, EventType: domain.EventTypeHookExecuted, Payload: `{"hook_index":1}`},
		{ProjectID: 2, EventType: domain.EventTypeHookExecuted, Payload: `{"hook_index":2}`},
	}}
	// The fake models the repository's project predicate. If the projection
	// omits ProjectID, both rows are returned and this assertion fails.
	rows, err := studioprojection.LoadHookHistory(context.Background(), events, 2, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if events.filter.ProjectID != 2 {
		t.Fatalf("history filter project id = %d, want 2", events.filter.ProjectID)
	}
	if len(rows[1]) != 0 || len(rows[2]) != 1 {
		t.Fatalf("project 2 history = %+v, want only hook index 2", rows)
	}
}

func TestStudioHookHistoryCacheLoadsForSwitchedProject(t *testing.T) {
	t.Parallel()

	events := &stubHookEvents{rows: []domain.EventRow{{ProjectID: 2, EventType: domain.EventTypeHookExecuted, Payload: `{"hook_index":2}`}}}
	m := Model{
		ctx:     context.Background(),
		project: domain.ProjectContext{ID: 1},
		repos:   Repositories{Events: events},
		sub:     subStudioHooks,
	}
	if cmd := m.prepareStudioHookHistory(); cmd == nil {
		t.Fatal("initial history load returned no command")
	} else {
		m.applyStudioHookHistory(cmd().(studioHookHistoryResultMsg))
	}
	if !m.studioHookHistoryReady || len(m.studioHookHistory) != 0 {
		t.Fatalf("initial project history = ready %t, rows %v", m.studioHookHistoryReady, m.studioHookHistory)
	}

	if err := m.selectHomeProject(domain.Project{ID: 2, Name: "Project 2", Slug: "project-2"}); err != nil {
		t.Fatalf("selectHomeProject() = %v", err)
	}
	cmd := m.prepareStudioHookHistory()
	if cmd == nil {
		t.Fatal("switched project history returned no command")
	}
	m.applyStudioHookHistory(cmd().(studioHookHistoryResultMsg))
	if !m.studioHookHistoryReady || m.studioHookHistoryProjectID != 2 || len(m.studioHookHistory[2]) != 1 {
		t.Fatalf("switched project history = ready %t, project %d, rows %v", m.studioHookHistoryReady, m.studioHookHistoryProjectID, m.studioHookHistory)
	}
}

func TestStudioHookHistoryFailureDoesNotBecomeReady(t *testing.T) {
	t.Parallel()

	events := &stubHookEvents{err: errors.New("history unavailable")}
	m := Model{
		ctx:     context.Background(),
		project: domain.ProjectContext{ID: 7},
		repos:   Repositories{Events: events},
		sub:     subStudioHooks,
	}
	cmd := m.prepareStudioHookHistory()
	if cmd == nil {
		t.Fatal("history failure returned no command")
	}
	m.applyStudioHookHistory(cmd().(studioHookHistoryResultMsg))
	if m.studioHookHistoryReady || m.studioHookHistory != nil || m.studioHookHistoryLoading {
		t.Fatalf("failed history cache = ready %t, rows %v, loading %t", m.studioHookHistoryReady, m.studioHookHistory, m.studioHookHistoryLoading)
	}
	if m.status != "history unavailable" {
		t.Fatalf("failure status = %q", m.status)
	}
}

func TestStudioHookHistoryDropsDelayedResultAfterProjectSwitch(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})
	events := &stubHookEvents{
		rows:    []domain.EventRow{{ProjectID: 1, EventType: domain.EventTypeHookExecuted, Payload: `{"hook_index":1}`}},
		wait:    release,
		started: started,
	}
	m := Model{
		ctx:     context.Background(),
		project: domain.ProjectContext{ID: 1},
		repos:   Repositories{Events: events},
		sub:     subStudioHooks,
	}
	cmd := m.prepareStudioHookHistory()
	if cmd == nil {
		t.Fatal("delayed history returned no command")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	<-started

	m.project.ID = 2
	m.invalidateStudioHookHistory()
	close(release)
	m.applyStudioHookHistory((<-result).(studioHookHistoryResultMsg))
	if m.studioHookHistoryReady || m.studioHookHistory != nil || m.studioHookHistoryProjectID != 0 {
		t.Fatalf("stale history applied after switch = ready %t, project %d, rows %v", m.studioHookHistoryReady, m.studioHookHistoryProjectID, m.studioHookHistory)
	}
}
