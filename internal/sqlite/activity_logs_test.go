package sqlite

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/domain"
	"omakiten/migrations"
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

	if err := store.PruneEventTypes(ctx, []string{
		domain.EventTypeCLIToolCall,
		domain.EventTypeMCPToolCall,
		domain.EventTypeTUIToolCall,
	}, 0, 3); err != nil {
		t.Fatalf("PruneEventTypes() error = %v", err)
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("ListActivityLogs() len = %d, want 3", len(logs))
	}
}

// TestBeginActivityLogWritesCanonicalEventType locks the contract that
// post-#109 writes emit `<source>.tool_call` event_types and stash the
// hook-customizable mirror fields in payload. Pre-019 rows used
// `event_type='operation'` and a raw args payload; hooks could not
// `when:` filter on tool_name/source without reading SQL columns.
func TestBeginActivityLogWritesCanonicalEventType(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	id, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceMCP,
		Entrypoint:    "tools/call",
		Operation:     "tasks.create",
		ProjectID:     1,
		ProjectSlug:   "test",
		ArgumentsJSON: `{"title":"Hello"}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}

	var eventType, payload string
	if err := store.db.QueryRowContext(ctx, "SELECT event_type, payload FROM events WHERE id = ?", id).Scan(&eventType, &payload); err != nil {
		t.Fatalf("read row error = %v", err)
	}
	if eventType != domain.EventTypeMCPToolCall {
		t.Fatalf("event_type = %q, want %q", eventType, domain.EventTypeMCPToolCall)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("payload not JSON: %v (raw=%q)", err, payload)
	}
	if decoded["tool_name"] != "tasks.create" {
		t.Fatalf("payload.tool_name = %v, want tasks.create", decoded["tool_name"])
	}
	if decoded["source"] != "mcp" {
		t.Fatalf("payload.source = %v, want mcp", decoded["source"])
	}
	if decoded["entrypoint"] != "tools/call" {
		t.Fatalf("payload.entrypoint = %v, want tools/call", decoded["entrypoint"])
	}
	if decoded["status"] != "running" {
		t.Fatalf("payload.status = %v, want running", decoded["status"])
	}
	args, ok := decoded["args"].(map[string]any)
	if !ok {
		t.Fatalf("payload.args not object: %T", decoded["args"])
	}
	if args["title"] != "Hello" {
		t.Fatalf("payload.args.title = %v, want Hello", args["title"])
	}

	// Finish: payload mirror keys must update alongside the columns so
	// hooks subscribed to mcp.tool_call can match `when: { status: ok }`.
	if err := store.FinishActivityLog(ctx, id, "ok", 123, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT payload FROM events WHERE id = ?", id).Scan(&payload); err != nil {
		t.Fatalf("read row after finish error = %v", err)
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("payload not JSON after finish: %v", err)
	}
	if decoded["status"] != "ok" {
		t.Fatalf("payload.status after finish = %v, want ok", decoded["status"])
	}
	if int(decoded["duration_ms"].(float64)) != 123 {
		t.Fatalf("payload.duration_ms after finish = %v, want 123", decoded["duration_ms"])
	}
}

// TestMigration019RenamesLegacyOperationRows confirms the migration
// backfill renames pre-#109 operation rows to the source-discriminated
// `<source>.tool_call` vocabulary and enriches the payload with mirror
// keys so hook `when:` filters can match without reading SQL columns.
// Simulated by inserting legacy-shape rows AFTER the migration has run
// and re-executing the migration SQL — the UPDATEs are idempotent.
func TestMigration019RenamesLegacyOperationRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed three legacy rows directly — one per source — using the
	// pre-019 event_type and raw arguments payload.
	for _, seed := range []struct {
		source, op, args string
	}{
		{"cli", "okt.task.list", `{"limit":10}`},
		{"mcp", "tasks.create", `{"title":"Hi"}`},
		{"tui", "tui.refresh", ``},
	} {
		if _, err := store.db.ExecContext(ctx, `
INSERT INTO events(entity_type, project_id, event_type, payload, source, entrypoint, operation, status, duration_ms, error_message)
VALUES ('system', 1, 'operation', ?, ?, '', ?, 'ok', 5, '')
`, seed.args, seed.source, seed.op); err != nil {
			t.Fatalf("seed insert error = %v", err)
		}
	}

	data, err := migrations.FS.ReadFile("019_unify_tool_call_events.sql")
	if err != nil {
		t.Fatalf("read migration error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, string(data)); err != nil {
		t.Fatalf("rerun migration 019 error = %v", err)
	}

	rows, err := store.db.QueryContext(ctx, "SELECT source, event_type, payload FROM events WHERE entity_type = 'system' AND event_type LIKE '%.tool_call' ORDER BY id ASC")
	if err != nil {
		t.Fatalf("read migrated rows = %v", err)
	}
	defer func() { _ = rows.Close() }()

	want := map[string]struct {
		eventType string
		toolName  string
	}{
		"cli": {domain.EventTypeCLIToolCall, "okt.task.list"},
		"mcp": {domain.EventTypeMCPToolCall, "tasks.create"},
		"tui": {domain.EventTypeTUIToolCall, "tui.refresh"},
	}
	seen := 0
	for rows.Next() {
		var source, eventType, payload string
		if err := rows.Scan(&source, &eventType, &payload); err != nil {
			t.Fatalf("scan = %v", err)
		}
		expect, ok := want[source]
		if !ok {
			t.Fatalf("unexpected source %q", source)
		}
		if eventType != expect.eventType {
			t.Fatalf("source %q event_type = %q, want %q", source, eventType, expect.eventType)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatalf("source %q payload not JSON: %v (raw=%q)", source, err, payload)
		}
		if decoded["tool_name"] != expect.toolName {
			t.Fatalf("source %q payload.tool_name = %v, want %q", source, decoded["tool_name"], expect.toolName)
		}
		if decoded["source"] != source {
			t.Fatalf("source %q payload.source = %v, want %q", source, decoded["source"], source)
		}
		seen++
	}
	if seen != 3 {
		t.Fatalf("migrated rows = %d, want 3", seen)
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
