package domain

import "testing"

func TestLogsFilterCycleAndPartition(t *testing.T) {
	if got := CycleLogsFilter(LogsFilterAll, -1); got != LogsFilterSystem {
		t.Fatalf("backward all cycle = %v, want system", got)
	}
	seen := map[EventCategory]bool{}
	for _, mode := range LogsFilterModes[1:] {
		for _, category := range LogsFilterCategories(mode) {
			if seen[category] {
				t.Fatalf("category %q appears in more than one filter", category)
			}
			seen[category] = true
		}
	}
	for _, category := range KnownEventCategories {
		if !seen[category] {
			t.Fatalf("category %q is not reachable from a filter", category)
		}
	}
}

func TestComputeEventStatsClassifiesHealthAndSeedsCategories(t *testing.T) {
	rows := []EventRow{
		{EventType: EventTypeCLIToolCall, Status: "ok"},
		{EventType: EventTypeMCPToolCall, Status: "error"},
		{EventType: EventTypeHookExecuted, Status: "running"},
		{EventType: EventTypeTaskCreated, Status: "ok"},
	}
	stats := ComputeEventStats(rows, nil)
	if stats.ToolCallOK != 1 || stats.ToolCallError != 1 || stats.ToolCallRunning != 1 {
		t.Fatalf("health = %+v, want one row in each status bucket", stats)
	}
	for _, category := range KnownEventCategories {
		if _, ok := stats.Categories[category]; !ok {
			t.Fatalf("category %q was not seeded", category)
		}
	}
}

func TestFilterLogVisibleRowsPreservesUnknownRows(t *testing.T) {
	previous := EventDefByKey
	defs := make(map[string]EventDef, len(previous)+1)
	for key, definition := range previous {
		defs[key] = definition
	}
	defs["test.hidden"] = EventDef{Key: "test.hidden", LogVisible: false}
	EventDefByKey = defs
	t.Cleanup(func() { EventDefByKey = previous })

	rows := []EventRow{{ID: 1, EventType: "test.hidden"}, {ID: 2, EventType: "test.unknown"}}
	got := FilterLogVisibleRows(rows)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("visible rows = %+v, want only the unknown row", got)
	}
}
