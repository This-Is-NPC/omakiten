package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestActivityLogCRUD(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	id, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceMCP,
		Entrypoint:    "tasks.create_intent",
		Operation:     "app.TaskService.Add",
		ProjectID:     1,
		ProjectSlug:   "test",
		ArgumentsJSON: `{"title":"Test"}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}
	if id <= 0 {
		t.Fatalf("BeginActivityLog() id = %d, want > 0", id)
	}

	if err := store.FinishActivityLog(ctx, id, "ok", 42, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListActivityLogs() len = %d, want 1", len(logs))
	}
	log := logs[0]
	if log.Status != "ok" {
		t.Fatalf("log.Status = %q, want ok", log.Status)
	}
	if log.DurationMs != 42 {
		t.Fatalf("log.DurationMs = %d, want 42", log.DurationMs)
	}
	if log.Source != domain.ActivitySourceMCP {
		t.Fatalf("log.Source = %q, want mcp", log.Source)
	}
}

func TestActivityLogListFilterBySource(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, source := range []domain.ActivitySource{domain.ActivitySourceCLI, domain.ActivitySourceMCP} {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{Source: source, Operation: "test", Status: "running"})
		if err != nil {
			t.Fatalf("BeginActivityLog(%s) error = %v", source, err)
		}
		if err := store.FinishActivityLog(ctx, id, "ok", 1, ""); err != nil {
			t.Fatalf("FinishActivityLog() error = %v", err)
		}
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{Source: domain.ActivitySourceMCP, Limit: 10})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListActivityLogs() len = %d, want 1", len(logs))
	}
	if logs[0].Source != domain.ActivitySourceMCP {
		t.Fatalf("log.Source = %q, want mcp", logs[0].Source)
	}
}

// TestActivityLogStatsAggregatesFullScope covers the aggregate query
// the Stats › Logs summary tables read from. The fixture spans two
// projects + every source/status combination so the test guarantees:
//   - the project filter actually narrows the count;
//   - sources outside cli/mcp/tui aren't double-counted by `Total`;
//   - status counts include `running` (rows that never reached Finish).
func TestActivityLogStatsAggregatesFullScope(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	insert := func(source domain.ActivitySource, projectID int64, finishStatus string) {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{
			Source:    source,
			ProjectID: projectID,
			Operation: "test",
			Status:    "running",
		})
		if err != nil {
			t.Fatalf("BeginActivityLog(%s) error = %v", source, err)
		}
		if finishStatus == "" {
			return
		}
		if err := store.FinishActivityLog(ctx, id, finishStatus, 1, ""); err != nil {
			t.Fatalf("FinishActivityLog(%s) error = %v", source, err)
		}
	}

	// Project 1: 3 cli/ok, 2 mcp/ok, 4 tui/ok, 1 mcp/error, 1 tui/running.
	for i := 0; i < 3; i++ {
		insert(domain.ActivitySourceCLI, 1, "ok")
	}
	for i := 0; i < 2; i++ {
		insert(domain.ActivitySourceMCP, 1, "ok")
	}
	for i := 0; i < 4; i++ {
		insert(domain.ActivitySourceTUI, 1, "ok")
	}
	insert(domain.ActivitySourceMCP, 1, "error")
	insert(domain.ActivitySourceTUI, 1, "") // remains running

	// Project 2 noise — must not show up under project_id = 1.
	for i := 0; i < 5; i++ {
		insert(domain.ActivitySourceCLI, 2, "ok")
	}

	stats, err := store.ActivityLogStats(ctx, domain.ActivityLogFilter{ProjectID: 1})
	if err != nil {
		t.Fatalf("ActivityLogStats() error = %v", err)
	}
	if stats.Total != 11 {
		t.Fatalf("Total = %d, want 11", stats.Total)
	}
	if stats.Ok != 9 {
		t.Fatalf("Ok = %d, want 9", stats.Ok)
	}
	if stats.Error != 1 {
		t.Fatalf("Error = %d, want 1", stats.Error)
	}
	if stats.Running != 1 {
		t.Fatalf("Running = %d, want 1", stats.Running)
	}
	if stats.CLI != 3 {
		t.Fatalf("CLI = %d, want 3", stats.CLI)
	}
	if stats.MCP != 3 {
		t.Fatalf("MCP = %d, want 3", stats.MCP)
	}
	if stats.TUI != 5 {
		t.Fatalf("TUI = %d, want 5", stats.TUI)
	}
	if stats.OldestAt == "" || stats.NewestAt == "" {
		t.Fatalf("expected non-empty Oldest/NewestAt timestamps, got %q / %q", stats.OldestAt, stats.NewestAt)
	}

	// Empty scope: a project without logs returns a zeroed stats with
	// empty timestamp markers.
	empty, err := store.ActivityLogStats(ctx, domain.ActivityLogFilter{ProjectID: 99})
	if err != nil {
		t.Fatalf("ActivityLogStats(empty scope) error = %v", err)
	}
	if empty.Total != 0 || empty.OldestAt != "" || empty.NewestAt != "" {
		t.Fatalf("empty scope = %+v, want zero values", empty)
	}
}

func TestActivityLogPruneKeepsNewest(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	for i := 0; i < 5; i++ {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{Source: domain.ActivitySourceCLI, Operation: "test", Status: "running"})
		if err != nil {
			t.Fatalf("BeginActivityLog() error = %v", err)
		}
		if err := store.FinishActivityLog(ctx, id, "ok", 1, ""); err != nil {
			t.Fatalf("FinishActivityLog() error = %v", err)
		}
	}

	if err := store.PruneActivityLogs(ctx, 3, 0); err != nil {
		t.Fatalf("PruneActivityLogs() error = %v", err)
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("ListActivityLogs() len = %d, want 3", len(logs))
	}
}

func TestActivityLogMigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() #1 error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() #1 error = %v", err)
	}

	store2, err := Open(ctx, t.TempDir()+"/omakiten2.db")
	if err != nil {
		t.Fatalf("Open() #2 error = %v", err)
	}
	defer func() { _ = store2.Close() }()

	// Sanity: events table must exist (activity_logs was folded into events
	// in migration 009 — the legacy table is gone).
	var count int
	if err := store2.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='events'").Scan(&count); err != nil {
		t.Fatalf("table check error = %v", err)
	}
	if count != 1 {
		t.Fatalf("events table missing after migration")
	}
}
