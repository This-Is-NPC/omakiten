package sqlite

import (
	"context"
	"encoding/json"
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
		Source:        domain.ActivitySourceTUI,
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
	if log.Source != domain.ActivitySourceTUI {
		t.Fatalf("log.Source = %q, want tui", log.Source)
	}
}

func TestActivityLogListFilterBySource(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, source := range []domain.ActivitySource{domain.ActivitySourceCLI, domain.ActivitySourceTUI} {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{Source: source, Operation: "test", Status: "running"})
		if err != nil {
			t.Fatalf("BeginActivityLog(%s) error = %v", source, err)
		}
		if err := store.FinishActivityLog(ctx, id, "ok", 1, ""); err != nil {
			t.Fatalf("FinishActivityLog() error = %v", err)
		}
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{Source: domain.ActivitySourceTUI, Limit: 10})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListActivityLogs() len = %d, want 1", len(logs))
	}
	if logs[0].Source != domain.ActivitySourceTUI {
		t.Fatalf("log.Source = %q, want tui", logs[0].Source)
	}
}

// TestActivityLogStatsAggregatesFullScope covers the aggregate query
// the Stats › Logs summary tables read from. The fixture spans two
// projects + every source/status combination so the test guarantees:
//   - the project filter actually narrows the count;
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
	for i := 0; i < 3; i++ {
		insert(domain.ActivitySourceCLI, 1, "ok")
	}
	for i := 0; i < 2; i++ {
		insert(domain.ActivitySourceTUI, 1, "ok")
	}
	for i := 0; i < 4; i++ {
		insert(domain.ActivitySourceTUI, 1, "ok")
	}
	insert(domain.ActivitySourceTUI, 1, "error")
	insert(domain.ActivitySourceTUI, 1, "") // remains running

	// Project 2 noise — must not show up under project_id = 1.
	for i := 0; i < 5; i++ {
		insert(domain.ActivitySourceCLI, 2, "ok")
	}

	stats, err := store.ActivityLogStats(ctx, domain.ActivityLogFilter{ProjectID: 1})
	if err != nil {
		t.Fatalf("ActivityLogStats() error = %v", err)
	}
	assertFullActivityStats(t, stats)

	// Empty scope: a project without logs returns a zeroed stats with
	// empty timestamp markers.
	empty, err := store.ActivityLogStats(ctx, domain.ActivityLogFilter{ProjectID: 99})
	if err != nil {
		t.Fatalf("ActivityLogStats(empty scope) error = %v", err)
	}
	assertEmptyActivityStats(t, empty)
}

func assertFullActivityStats(t *testing.T, stats domain.ActivityLogStats) {
	t.Helper()
	want := map[string]int{"Total": 11, "Ok": 9, "Error": 1, "Running": 1, "CLI": 3, "TUI": 8}
	got := map[string]int{"Total": stats.Total, "Ok": stats.Ok, "Error": stats.Error, "Running": stats.Running, "CLI": stats.CLI, "TUI": stats.TUI}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("%s = %d, want %d", key, got[key], wantValue)
		}
	}
	if stats.OldestAt == "" || stats.NewestAt == "" {
		t.Errorf("expected non-empty Oldest/NewestAt timestamps, got %q / %q", stats.OldestAt, stats.NewestAt)
	}
}

func assertEmptyActivityStats(t *testing.T, stats domain.ActivityLogStats) {
	t.Helper()
	if stats.Total != 0 || stats.OldestAt != "" || stats.NewestAt != "" {
		t.Errorf("empty scope = %+v, want zero values", stats)
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
		domain.EventTypeTUIToolCall,
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
		Source:        domain.ActivitySourceCLI,
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
	assertToolCallStart(t, eventType, payload)

	// Finish: payload mirror keys must update alongside the columns so
	// hooks subscribed to cli.tool_call can match `when: { status: ok }`.
	if err := store.FinishActivityLog(ctx, id, "ok", 123, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT payload FROM events WHERE id = ?", id).Scan(&payload); err != nil {
		t.Fatalf("read row after finish error = %v", err)
	}
	assertToolCallFinish(t, payload)
}

func assertToolCallStart(t *testing.T, eventType, payload string) {
	t.Helper()
	if eventType != domain.EventTypeCLIToolCall {
		t.Fatalf("event_type = %q, want %q", eventType, domain.EventTypeCLIToolCall)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("payload not JSON: %v (raw=%q)", err, payload)
	}
	for key, want := range map[string]any{"tool_name": "tasks.create", "source": "cli", "entrypoint": "tools/call", "status": "running"} {
		if decoded[key] != want {
			t.Errorf("payload.%s = %v, want %v", key, decoded[key], want)
		}
	}
	args, ok := decoded["args"].(map[string]any)
	if !ok || args["title"] != "Hello" {
		t.Errorf("payload.args.title = %v, want Hello", decoded["args"])
	}
}

func assertToolCallFinish(t *testing.T, payload string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("payload not JSON after finish: %v", err)
	}
	if decoded["status"] != "ok" || int(decoded["duration_ms"].(float64)) != 123 {
		t.Errorf("finished payload = %v, want status=ok duration_ms=123", decoded)
	}
}

func TestCurrentSchemaContainsEventsLog(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	// Sanity: events table must exist (activity_logs was folded into events
	// in the current schema — the legacy table is gone).
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='events'").Scan(&count); err != nil {
		t.Fatalf("table check error = %v", err)
	}
	if count != 1 {
		t.Fatalf("events table missing from current schema")
	}
}
