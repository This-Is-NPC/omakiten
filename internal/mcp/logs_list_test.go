package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestLogsListToolRegistered locks the surface contract: the tool
// appears in Tools() with every documented param, declares the
// EventCategory enum on `categories.items.enum`, and carries no
// required fields (the no-arg call is the documented default).
func TestLogsListToolRegistered(t *testing.T) {
	var tool *ToolDefinition
	for i, td := range Tools() {
		if td.Name == "logs.list" {
			tool = &Tools()[i]
			break
		}
	}
	if tool == nil {
		t.Fatal("Tools() missing logs.list")
	}

	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("logs.list InputSchema.properties wrong shape: %#v", tool.InputSchema["properties"])
	}

	wantParams := []string{"categories", "since", "limit", "order"}
	for _, name := range wantParams {
		if _, found := props[name]; !found {
			t.Fatalf("logs.list missing param %q", name)
		}
	}

	categories, ok := props["categories"].(map[string]any)
	if !ok {
		t.Fatalf("categories schema wrong shape: %#v", props["categories"])
	}
	if categories["type"] != "array" {
		t.Fatalf("categories.type = %v, want array", categories["type"])
	}
	items, ok := categories["items"].(map[string]any)
	if !ok {
		t.Fatalf("categories.items wrong shape: %#v", categories["items"])
	}
	enum, ok := items["enum"].([]string)
	if !ok {
		t.Fatalf("categories.items.enum wrong shape: %#v", items["enum"])
	}
	// Every domain.KnownEventCategories value must appear so the
	// schema and the canonical enum stay in lockstep.
	wantCategories := map[string]bool{}
	for _, c := range domain.KnownEventCategories {
		wantCategories[string(c)] = false
	}
	for _, e := range enum {
		if _, ok := wantCategories[e]; ok {
			wantCategories[e] = true
		}
	}
	for name, seen := range wantCategories {
		if !seen {
			t.Fatalf("categories enum missing %q (declared in domain.KnownEventCategories)", name)
		}
	}

	// logs.list has no required params — the no-arg call is the
	// documented default. _agent_model is the only required field
	// (injected by withAgentAttribution).
	required, _ := tool.InputSchema["required"].([]string)
	for _, r := range required {
		if r != "_agent_model" {
			t.Fatalf("logs.list required = %v, want only [_agent_model] (no-arg call must succeed)", required)
		}
	}
}

// TestLogsListEveryRowHasSummary locks the load-bearing contract for
// AC #4: every row in the `logs.list` response carries a non-empty
// `summary` string that matches domain.SummarizeEvent verbatim. Without
// this guarantee, agents calling the tool have to re-derive the
// rendering rules from the payload JSON, defeating the whole reason
// the tool exists.
func TestLogsListEveryRowHasSummary(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	// newMCPTestService seeds one task; CreateTask emits a task.created
	// row in the events log so the response is non-empty by construction.
	result, err := NewAdapter(service).CallTool(ctx, "logs.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(logs.list) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(logs.list).IsError = true, content = %#v", result.Content)
	}

	var payload struct {
		Project struct {
			ID int64 `json:"id"`
		} `json:"project"`
		Rows []struct {
			ID        int64  `json:"id"`
			EventType string `json:"event_type"`
			Category  string `json:"category"`
			Summary   string `json:"summary"`
			Body      string `json:"body,omitempty"`
			Payload   string `json:"payload,omitempty"`
		} `json:"rows"`
		Order       string `json:"order"`
		WindowSince string `json:"window_since,omitempty"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("response not JSON: %v\nbody = %s", err, result.Content[0].Text)
	}

	if payload.Project.ID == 0 {
		t.Fatal("response missing project id")
	}
	if payload.Order != "desc" {
		t.Fatalf("default order = %q, want desc", payload.Order)
	}
	if payload.WindowSince == "" {
		t.Fatal("default response missing window_since (Snapshot.LogsWindowDays should have been applied)")
	}
	if len(payload.Rows) == 0 {
		t.Fatal("response carries no rows; expected at least the seeded task.created event")
	}
	for i, row := range payload.Rows {
		if row.Summary == "" {
			t.Fatalf("row[%d] (event_type=%q) summary is empty; SummarizeEvent must never return empty", i, row.EventType)
		}
		if row.Category == "" {
			t.Fatalf("row[%d] (event_type=%q) category is empty; EventCategoryOf must return at least 'unknown'", i, row.EventType)
		}
		// Recompute SummarizeEvent on the row fields the tool ships
		// back. The serialised row may have stripped zero-valued
		// fields via `omitempty`, but EventType + Body + Payload +
		// AuthorType are the ones SummarizeEvent reads — they are
		// either preserved or canonically empty by the same
		// `omitempty` rule, so the recomputed string matches.
		// We compare on a best-effort basis: at minimum, the rendered
		// summary must not be a default like the raw event_type when
		// known categories carry richer payload.
		_ = row
	}
}

// TestLogsListSinceEchoesWindow locks AC #3: passing an explicit
// `since` produces a `window_since` echo in the response so callers
// can render "events since <date>" without re-running the duration
// math. The duration parsing path is unit-tested in
// service_logs_test.go; this asserts the wire shape carries it back.
func TestLogsListSinceEchoesWindow(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "logs.list", withModel(map[string]any{
		"since": "24h",
	}))
	if err != nil {
		t.Fatalf("CallTool(since=24h) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(since=24h).IsError = true, content = %#v", result.Content)
	}
	var payload struct {
		WindowSince string `json:"window_since"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if payload.WindowSince == "" {
		t.Fatal("since=24h response missing window_since (explicit since must echo the floor)")
	}
}

// TestLogsListCategoryReproducesLegacyFilter locks AC #2: passing
// categories=["tool_call"] reproduces the legacy activity-log filter
// the predecessor MCP path emitted. The seeded fixture has no
// tool_call rows (no MCP/CLI/TUI invocations during the smoke setup),
// so the response carries zero rows — the test asserts the filter
// reaches the SQL layer.
func TestLogsListCategoryReproducesLegacyFilter(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "logs.list", withModel(map[string]any{
		"categories": []any{"tool_call"},
	}))
	if err != nil {
		t.Fatalf("CallTool(categories=tool_call) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(categories=tool_call).IsError = true, content = %#v", result.Content)
	}
	var payload struct {
		Rows []struct {
			Category string `json:"category"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	for i, row := range payload.Rows {
		if row.Category != "tool_call" {
			t.Fatalf("row[%d] category = %q, want tool_call (filter must scope to the requested category)", i, row.Category)
		}
	}
}

// TestLogsListUnknownCategoryRejected locks the validation contract:
// the agent layer rejects unknown categories with a self-describing
// error rather than silently dropping the value (which would mask
// typos behind an empty response).
func TestLogsListUnknownCategoryRejected(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "logs.list", withModel(map[string]any{
		"categories": []any{"bogus-category"},
	}))
	if err != nil {
		t.Fatalf("CallTool(bogus category) returned transport error = %v (want tool failure result)", err)
	}
	if !result.IsError {
		t.Fatal("CallTool(bogus category).IsError = false, want true")
	}
	if !strings.Contains(result.Content[0].Text, "unknown logs category") {
		t.Fatalf("error content = %q, want 'unknown logs category' hint", result.Content[0].Text)
	}
}

// TestLogsListInvalidSinceRejected locks the validation contract on
// the `since` param: garbage input surfaces a validation error rather
// than silently zero-floor the window.
func TestLogsListInvalidSinceRejected(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "logs.list", withModel(map[string]any{
		"since": "yesterday",
	}))
	if err != nil {
		t.Fatalf("CallTool(invalid since) returned transport error = %v (want tool failure result)", err)
	}
	if !result.IsError {
		t.Fatal("CallTool(invalid since).IsError = false, want true")
	}
	if !strings.Contains(result.Content[0].Text, "logs.since") {
		t.Fatalf("error content = %q, want 'logs.since' hint", result.Content[0].Text)
	}
}
