package tui

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screens/logs"
)

func logsReloadMessage(projectID int64, generation uint64, body string) realtimeReloadMsg {
	return realtimeReloadMsg{
		kind:              realtimeReloadLogs,
		projectID:         projectID,
		projectGeneration: generation,
		gen:               1,
		events:            []domain.EventRow{{ID: 1, ProjectID: projectID, Body: body}},
		logsValid:         true,
	}
}

func TestLogsProjectSwitchDropsRowsBeforeRender(t *testing.T) {
	t.Parallel()

	m := Model{
		project:              domain.ProjectContext{ID: 1},
		logsScreen:           logs.New().Apply(logs.Payload{Rows: []domain.EventRow{{ID: 1, ProjectID: 1, Body: "project one"}}}),
		dataVersionBaselines: map[realtimeReloadKind]int64{realtimeReloadLogs: 7},
		lastAppliedReloadGen: map[realtimeReloadKind]uint64{realtimeReloadLogs: 3},
	}
	if err := m.selectHomeProject(domain.Project{ID: 2, Name: "Project 2", Slug: "project-2"}); err != nil {
		t.Fatalf("selectHomeProject() = %v", err)
	}

	if got := m.logsScreen.Rows(); len(got) != 0 {
		t.Fatalf("rows after project switch = %d, want 0", len(got))
	}
	if _, ok := m.dataVersionBaselines[realtimeReloadLogs]; ok {
		t.Fatal("project switch retained the previous Logs watermark baseline")
	}
	if _, ok := m.lastAppliedReloadGen[realtimeReloadLogs]; ok {
		t.Fatal("project switch retained the previous Logs reload generation")
	}
}

func TestLogsReloadAppliesCurrentProjectResult(t *testing.T) {
	t.Parallel()

	m := Model{project: domain.ProjectContext{ID: 2}, projectGeneration: 1, logsScreen: logs.New()}
	m.applyRealtimeReload(logsReloadMessage(2, 1, "project two"))

	rows := m.logsScreen.Rows()
	if len(rows) != 1 || rows[0].Body != "project two" {
		t.Fatalf("current-project rows = %+v, want project two", rows)
	}
}

func TestLogsReloadCapturesProjectGenerationBeforeWorkerRuns(t *testing.T) {
	t.Parallel()

	m := Model{
		ctx:               context.Background(),
		project:           domain.ProjectContext{ID: 2},
		projectGeneration: 4,
		repos:             Repositories{Events: &stubHookEvents{}},
	}
	cmd := m.realtimeRefreshCmd(realtimeReloadLogs, 0, false)
	if cmd == nil {
		t.Fatal("realtime Logs reload command = nil")
	}
	m.projectGeneration = 5
	result := cmd()
	msg, ok := result.(realtimeReloadMsg)
	if !ok {
		t.Fatalf("realtime Logs reload command returned %T, want realtimeReloadMsg", result)
	}
	if msg.projectGeneration != 4 {
		t.Fatalf("captured project generation = %d, want 4", msg.projectGeneration)
	}
}

func TestLogsFailedReloadLeavesClearedRowsAndBaselineForRetry(t *testing.T) {
	t.Parallel()

	m := Model{
		project:              domain.ProjectContext{ID: 2},
		projectGeneration:    1,
		logsScreen:           logs.New(),
		dataVersionBaselines: map[realtimeReloadKind]int64{realtimeReloadLogs: 7},
	}
	failed := logsReloadMessage(2, 1, "unavailable")
	failed.err = errors.New("logs unavailable")
	m.applyRealtimeReload(failed)

	if len(m.logsScreen.Rows()) != 0 {
		t.Fatalf("failed reload repopulated rows = %d, want 0", len(m.logsScreen.Rows()))
	}
	if got := m.dataVersionBaselines[realtimeReloadLogs]; got != 7 {
		t.Fatalf("failed reload advanced baseline to %d, want 7", got)
	}
	if m.status != "logs unavailable" {
		t.Fatalf("failed reload status = %q, want logs unavailable", m.status)
	}
}

func TestLogsRapidProjectSwitchDropsDelayedResultByGeneration(t *testing.T) {
	t.Parallel()

	// The delayed result belongs to the first visit to project 1. Returning to
	// project 1 must not make that old result current again.
	m := Model{project: domain.ProjectContext{ID: 1}, logsScreen: logs.New()}
	delayed := logsReloadMessage(1, 0, "stale project one")
	m.project = domain.ProjectContext{ID: 2}
	m.projectGeneration = 1
	m.logsScreen = m.logsScreen.Reset()
	m.project = domain.ProjectContext{ID: 1}
	m.projectGeneration = 2
	m.logsScreen = m.logsScreen.Reset()

	m.applyRealtimeReload(delayed)
	if len(m.logsScreen.Rows()) != 0 {
		t.Fatalf("delayed result applied after A -> B -> A switch: rows=%+v", m.logsScreen.Rows())
	}

	m.applyRealtimeReload(logsReloadMessage(1, 2, "current project one"))
	if rows := m.logsScreen.Rows(); len(rows) != 1 || rows[0].Body != "current project one" {
		t.Fatalf("current result after rapid switch = %+v, want current project one", rows)
	}
}
