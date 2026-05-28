package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
)

// TestFormatLogsEntityComposition pins the ENTITY column rules: task /
// comment rows render `<entity_type>#<entity_id>`; system events with
// entity_id=0 collapse to the bare entity_type. The renderer relies on
// these strings to fit inside the fixed-width column, so changes here
// also affect alignment.
func TestFormatLogsEntityComposition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  domain.EventRow
		want string
	}{
		{"task with id", domain.EventRow{EntityType: "task", EntityID: 381}, "task#381"},
		{"comment with id", domain.EventRow{EntityType: "task", EntityID: 44}, "task#44"},
		{"system with no id", domain.EventRow{EntityType: "system", EntityID: 0}, "system"},
		{"bare entity_type fallback", domain.EventRow{EntityType: "plan", EntityID: 0}, "plan"},
		{"missing entity_type", domain.EventRow{EntityType: "", EntityID: 12}, "event#12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatLogsEntity(tc.row); got != tc.want {
				t.Fatalf("formatLogsEntity() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFormatLogsWhoColumn pins the WHO column rules: tool-call rows
// surface `source`, comment rows surface `author_type`, system events
// fall back to the em-dash. Empty rows degrade to the dash so the
// column is never blank.
func TestFormatLogsWhoColumn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  domain.EventRow
		want string
	}{
		{"tool_call source", domain.EventRow{EntityType: "system", EventType: domain.EventTypeCLIToolCall, Source: "cli"}, "cli"},
		{"hook source", domain.EventRow{EntityType: "system", EventType: domain.EventTypeHookExecuted, Source: "mcp"}, "mcp"},
		{"comment author", domain.EventRow{EntityType: "task", EntityID: 1, EventType: domain.EventTypeComment, AuthorType: "agent"}, "agent"},
		{"system fallback", domain.EventRow{EntityType: "system", EventType: domain.EventTypeBundleSwapped}, "—"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatLogsWho(tc.row); got != tc.want {
				t.Fatalf("formatLogsWho() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestShortTimeForLogsTrimsToWidth pins the TIME column truncation:
// the SQLite "YYYY-MM-DD HH:MM:SS" stamp shrinks to its trailing
// `width` characters so the column always carries the most recent
// component. Values that already fit are returned untouched.
func TestShortTimeForLogsTrimsToWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ts    string
		width int
		want  string
	}{
		// 19-char SQLite stamp keeps the trailing 12 chars — "-DD HH:MM:SS".
		{"2026-05-27 13:45:08", 12, "-27 13:45:08"},
		{"2026-05-27 13:45:08", 8, "13:45:08"},
		{"13:45:08", 8, "13:45:08"},
		{"45:08", 8, "45:08"},
		{"45:08", 0, ""},
	}
	for _, tc := range cases {
		if got := shortTimeForLogs(tc.ts, tc.width); got != tc.want {
			t.Fatalf("shortTimeForLogs(%q, %d) = %q, want %q", tc.ts, tc.width, got, tc.want)
		}
	}
}

// TestComputeEventStatsToolCallHealth pins the tool-call health
// folding rule: only `*.tool_call` + `hook.executed` rows contribute,
// and each is bucketed by row.Status (ok / error / running). Other
// event_types are silently skipped so the headline number cannot
// mix categories.
func TestComputeEventStatsToolCallHealth(t *testing.T) {
	t.Parallel()
	rows := []domain.EventRow{
		{EventType: domain.EventTypeCLIToolCall, Status: "ok"},
		{EventType: domain.EventTypeMCPToolCall, Status: "error"},
		{EventType: domain.EventTypeTUIToolCall, Status: "running"},
		{EventType: domain.EventTypeHookExecuted, Status: "ok"},
		// non-tool_call rows must not contribute to the health buckets.
		{EventType: domain.EventTypeTaskCreated, Status: "ok"},
		{EventType: domain.EventTypeComment, AuthorType: "human"},
	}
	stats := computeEventStats(rows, nil)
	if stats.ToolCallOK != 2 {
		t.Errorf("ToolCallOK = %d, want 2", stats.ToolCallOK)
	}
	if stats.ToolCallError != 1 {
		t.Errorf("ToolCallError = %d, want 1", stats.ToolCallError)
	}
	if stats.ToolCallRunning != 1 {
		t.Errorf("ToolCallRunning = %d, want 1", stats.ToolCallRunning)
	}
	// Categories from a nil counts map must be seeded to zero for
	// every known category so the renderer can walk them without
	// branching on nil.
	for _, c := range domain.KnownEventCategories {
		if _, ok := stats.Categories[c]; !ok {
			t.Errorf("Categories[%q] missing — must be seeded to 0", c)
		}
	}
}

// TestComputeEventStatsCategoryCountsPassthrough confirms the
// repository-supplied per-category counts are taken verbatim — the
// renderer never overrides the canonical totals.
func TestComputeEventStatsCategoryCountsPassthrough(t *testing.T) {
	t.Parallel()
	counts := map[domain.EventCategory]int{
		domain.EventCategoryTask:     5,
		domain.EventCategoryComment:  2,
		domain.EventCategoryToolCall: 3,
	}
	stats := computeEventStats(nil, counts)
	for cat, want := range counts {
		if got := stats.Categories[cat]; got != want {
			t.Errorf("Categories[%q] = %d, want %d", cat, got, want)
		}
	}
}

// TestRenderLogsWidePanelEmitsFiveColumnLayout exercises the wide
// renderer end-to-end with a synthetic event_row buffer: the header
// must carry every column tag (TIME, TYPE, ENTITY, WHO, DETAIL) and
// each row must surface its SummarizeEvent detail string.
func TestRenderLogsWidePanelEmitsFiveColumnLayout(t *testing.T) {
	t.Parallel()
	model := newRenderLogsTestModel(t, 180)
	model.events = []domain.EventRow{
		{
			ID:         1,
			EntityType: "system",
			EntityID:   0,
			EventType:  domain.EventTypeCLIToolCall,
			Source:     "cli",
			Status:     "ok",
			DurationMs: 12,
			CreatedAt:  "2026-05-27 13:45:08",
			Payload:    `{"tool_name":"app.TaskService.Add","source":"cli","status":"ok","duration_ms":12}`,
		},
		{
			ID:         2,
			EntityType: "task",
			EntityID:   42,
			EventType:  domain.EventTypeTaskCreated,
			CreatedAt:  "2026-05-27 13:46:00",
			Payload:    `{"title":"Wire renderer","bucket":"backlog","priority":"normal"}`,
		},
	}
	model.eventStats = computeEventStats(model.events, map[domain.EventCategory]int{
		domain.EventCategoryToolCall: 1,
		domain.EventCategoryTask:     1,
	})

	view := ansi.Strip(model.renderLogsWidePanel())
	for _, want := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide panel missing column tag %q\n%s", want, view)
		}
	}
	for _, want := range []string{
		// Phase 3 (#355): TYPE column renders EventDef.Display from the
		// YAML registry, not the raw event_type. Tests run with the
		// omakase fixture loaded via TestMain, so these labels are the
		// canonical kit values.
		"CLI tool call",
		"task created",
		"system",
		"task#42",
		"cli",
		"cli/app.TaskService.Add [ok] 12ms",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("wide panel missing %q\n%s", want, view)
		}
	}
}

// TestRenderLogsCompactPanelDropsAuxiliaryColumns confirms the
// narrow-terminal variant collapses to the marker + time + type +
// detail shape so it still fits the 32-72 cell budget — the explicit
// ENTITY / WHO column tags are dropped, but the SummarizeEvent
// detail still carries the per-row signal.
func TestRenderLogsCompactPanelDropsAuxiliaryColumns(t *testing.T) {
	t.Parallel()
	model := newRenderLogsTestModel(t, 80)
	model.events = []domain.EventRow{
		{
			ID:         1,
			EntityType: "system",
			EntityID:   0,
			EventType:  domain.EventTypeCLIToolCall,
			Source:     "cli",
			Status:     "ok",
			DurationMs: 7,
			CreatedAt:  "2026-05-27 13:45:08",
			Payload:    `{"tool_name":"app.TaskService.Add","source":"cli","status":"ok","duration_ms":7}`,
		},
	}
	view := ansi.Strip(model.renderLogsCompactPanel())
	for _, want := range []string{
		"// ACTIVITY · 1",
		// Phase 3 (#355): TYPE column now uses EventDef.Display from the
		// YAML registry.
		"CLI tool call",
		"cli/app.TaskService.Add [ok] 7ms",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("compact panel missing %q\n%s", want, view)
		}
	}
	// Compact panel must NOT print the wide-panel column tags — the
	// width budget doesn't afford them.
	for _, banned := range []string{"ENTITY", "WHO", "DETAIL"} {
		if strings.Contains(view, banned) {
			t.Errorf("compact panel unexpectedly contains wide tag %q\n%s", banned, view)
		}
	}
}

// TestRenderLogsSummaryTablesListsEveryKnownCategory locks AC#4: the
// Categories summary must surface every domain.KnownEventCategory so
// the user sees the full grouping vocabulary, with zero counts when
// the window holds no matching rows.
func TestRenderLogsSummaryTablesListsEveryKnownCategory(t *testing.T) {
	t.Parallel()
	model := newRenderLogsTestModel(t, 180)
	model.eventStats = computeEventStats(nil, map[domain.EventCategory]int{
		domain.EventCategoryTask:     3,
		domain.EventCategoryToolCall: 7,
	})
	view := ansi.Strip(model.renderLogsSummaryTables())
	if !strings.Contains(view, "CATEGORIES") {
		t.Fatalf("summary missing Categories table kicker\n%s", view)
	}
	// Health table kicker carries the tool_call scope hint.
	if !strings.Contains(view, "HEALTH") || !strings.Contains(view, "TOOL_CALLS") {
		t.Fatalf("summary missing Health · tool_calls table kicker\n%s", view)
	}
	for _, cat := range domain.KnownEventCategories {
		if !strings.Contains(view, strings.ToUpper(string(cat))) {
			t.Errorf("Categories table missing %q\n%s", cat, view)
		}
	}
}

// TestRenderLogsEmptyStateUsesPanelHelper confirms AC#7: an
// empty events buffer renders the canonical empty-state panel —
// not a stray table or a stale row. The empty-state string comes
// from the existing tui.empty.logs catalog key so the message
// stays localised.
func TestRenderLogsEmptyStateRendersEmptyPanel(t *testing.T) {
	t.Parallel()
	model := newRenderLogsTestModel(t, 180)
	model.repos.Events = stubEmptyEventRepo{}
	view := ansi.Strip(model.renderLogs())
	// the panel should NOT carry the wide-table headers when empty.
	for _, banned := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if strings.Contains(view, banned) {
			t.Errorf("empty Logs view unexpectedly carries wide column %q\n%s", banned, view)
		}
	}
}

// TestRenderLogsWidthSplitSwitchesPanels pins AC#6 — the 92-cell
// threshold drives which renderer the model dispatches to. Below
// the threshold compact wins; at or above, the wide layout wins.
// We exercise the toggle through renderLogs so the dispatch logic
// stays exercised end-to-end.
func TestRenderLogsWidthSplitSwitchesPanels(t *testing.T) {
	t.Parallel()
	row := domain.EventRow{
		ID:         1,
		EntityType: "system",
		EventType:  domain.EventTypeCLIToolCall,
		Source:     "cli",
		Status:     "ok",
		DurationMs: 3,
		CreatedAt:  "2026-05-27 13:45:08",
		Payload:    `{"tool_name":"app.TaskService.Add","source":"cli","status":"ok","duration_ms":3}`,
	}

	wide := newRenderLogsTestModel(t, 180)
	wide.events = []domain.EventRow{row}
	wide.eventStats = computeEventStats(wide.events, nil)
	if !strings.Contains(ansi.Strip(wide.renderLogs()), "TIME") {
		t.Errorf("width=180 must dispatch to wide panel (carries TIME header)")
	}

	compact := newRenderLogsTestModel(t, 70)
	compact.events = []domain.EventRow{row}
	compact.eventStats = computeEventStats(compact.events, nil)
	view := ansi.Strip(compact.renderLogs())
	if strings.Contains(view, "ENTITY") || strings.Contains(view, "DETAIL") {
		t.Errorf("width=70 must dispatch to compact panel (no wide headers)\n%s", view)
	}
}

// --- helpers ---------------------------------------------------------

// newRenderLogsTestModel builds the minimum Model surface the
// renderer needs: a styles bundle from the production theme + a
// width override + the Events port wired to a non-nil stub so the
// "Activity logging is not available" branch doesn't short-circuit.
func newRenderLogsTestModel(t *testing.T, width int) Model {
	t.Helper()
	theme := tuiTestTheme()
	model := Model{
		styles: newStyles(theme),
		theme:  theme,
		width:  width,
		height: 40,
	}
	model.repos.Events = stubEmptyEventRepo{}
	return model
}

// stubEmptyEventRepo satisfies app.EventRepository for the renderer
// branches that just need a non-nil port. The renderer never calls
// any method on it directly in these tests — refresh wiring is
// exercised by model_test.go's TestModelLoadsActivityLogsWhenOpeningLogsView.
type stubEmptyEventRepo struct{}

func (stubEmptyEventRepo) RecordTaskEvent(context.Context, int64, int64, string, string, string) (domain.Event, error) {
	return domain.Event{}, nil
}
func (stubEmptyEventRepo) RecordEntityEvent(context.Context, string, int64, int64, string, string) error {
	return nil
}
func (stubEmptyEventRepo) ListTaskActivity(context.Context, int64, int64, string) ([]domain.Event, error) {
	return nil, nil
}
func (stubEmptyEventRepo) ListEvents(context.Context, domain.EventFilter) ([]domain.EventRow, error) {
	return nil, nil
}
func (stubEmptyEventRepo) EventCategoryCounts(context.Context, int64, time.Time) (map[domain.EventCategory]int, error) {
	return nil, nil
}

// TestActivityLogRendersDisplayLabel pins AC#1 + AC#2 of Phase 3:
// the wide and compact panels render EventDef.Display (from the
// YAML-loaded registry) for the TYPE cell, and fall back to the
// raw event_type when the lookup misses.
//
// Sequential because the test mutates the package-level
// domain.EventDefByKey map; cleanup restores the prior state so
// other tests that run after this one are unaffected.
func TestActivityLogRendersDisplayLabel(t *testing.T) {
	// Snapshot + restore the registry so parallel tests in the same
	// package see the original (empty / production-loaded) map.
	prev := domain.EventDefByKey
	cloned := make(map[string]domain.EventDef, len(prev))
	for k, v := range prev {
		cloned[k] = v
	}
	cloned["task.created"] = domain.EventDef{
		Key:        "task.created",
		Category:   domain.EventCategoryTask,
		Display:    "task created",
		LogVisible: true,
		// Formatter emits a summary string that does NOT carry the raw
		// event_type so the "does NOT contain task.created" assertion
		// scopes to the TYPE cell rather than colliding with the DETAIL
		// cell's payload echo.
		Formatter: func(domain.EventRow) string { return "wired renderer" },
	}
	domain.EventDefByKey = cloned
	t.Cleanup(func() { domain.EventDefByKey = prev })

	t.Run("wide_panel_renders_display", func(t *testing.T) {
		model := newRenderLogsTestModel(t, 180)
		row := domain.EventRow{
			ID:         1,
			EntityType: "task",
			EntityID:   42,
			EventType:  "task.created",
			CreatedAt:  "2026-05-27 13:46:00",
			Payload:    `{"title":"Wire renderer","bucket":"backlog","priority":"normal"}`,
		}
		out := ansi.Strip(formatLogsWideRow(model, " ", row, 12, 20, 16, 8, 40))
		if !strings.Contains(out, "task created") {
			t.Fatalf("wide row missing display label %q\n%s", "task created", out)
		}
		if strings.Contains(out, "task.created") {
			t.Fatalf("wide row unexpectedly carries raw event_type %q\n%s", "task.created", out)
		}
	})

	t.Run("compact_panel_renders_display", func(t *testing.T) {
		model := newRenderLogsTestModel(t, 80)
		model.events = []domain.EventRow{{
			ID:         1,
			EntityType: "task",
			EntityID:   42,
			EventType:  "task.created",
			CreatedAt:  "2026-05-27 13:46:00",
			Payload:    `{"title":"Wire renderer","bucket":"backlog","priority":"normal"}`,
		}}
		view := ansi.Strip(model.renderLogsCompactPanel())
		if !strings.Contains(view, "task created") {
			t.Fatalf("compact panel missing display label %q\n%s", "task created", view)
		}
		if strings.Contains(view, "task.created") {
			t.Fatalf("compact panel unexpectedly carries raw event_type %q\n%s", "task.created", view)
		}
	})

	t.Run("unknown_event_type_falls_back_to_raw_key", func(t *testing.T) {
		model := newRenderLogsTestModel(t, 180)
		row := domain.EventRow{
			ID:         99,
			EntityType: "system",
			EventType:  "__test.unknown",
			CreatedAt:  "2026-05-27 13:46:00",
		}
		wide := ansi.Strip(formatLogsWideRow(model, " ", row, 12, 20, 16, 8, 40))
		if !strings.Contains(wide, "__test.unknown") {
			t.Fatalf("wide row missing raw fallback %q\n%s", "__test.unknown", wide)
		}

		compactModel := newRenderLogsTestModel(t, 80)
		compactModel.events = []domain.EventRow{row}
		compact := ansi.Strip(compactModel.renderLogsCompactPanel())
		if !strings.Contains(compact, "__test.unknown") {
			t.Fatalf("compact panel missing raw fallback %q\n%s", "__test.unknown", compact)
		}
	})
}

// TestActivityLogLogVisibleFilterHides pins AC#3: filterLogVisibleRows
// drops rows whose event_type maps to an EventDef with
// LogVisible == false, while leaving registry-miss + LogVisible == true
// rows untouched.
func TestActivityLogLogVisibleFilterHides(t *testing.T) {
	prev := domain.EventDefByKey
	cloned := make(map[string]domain.EventDef, len(prev))
	for k, v := range prev {
		cloned[k] = v
	}
	cloned["__test.hidden"] = domain.EventDef{
		Key:        "__test.hidden",
		Category:   domain.EventCategoryDomain,
		LogVisible: false,
		Formatter:  func(domain.EventRow) string { return "" },
	}
	cloned["__test.visible"] = domain.EventDef{
		Key:        "__test.visible",
		Category:   domain.EventCategoryDomain,
		LogVisible: true,
		Formatter:  func(domain.EventRow) string { return "" },
	}
	domain.EventDefByKey = cloned
	t.Cleanup(func() { domain.EventDefByKey = prev })

	rows := []domain.EventRow{
		{ID: 1, EventType: "__test.hidden"},
		{ID: 2, EventType: "__test.visible"},
		{ID: 3, EventType: "__test.unmapped"}, // registry miss → passes
	}
	got := filterLogVisibleRows(rows)
	if len(got) != 2 {
		t.Fatalf("filterLogVisibleRows: got %d rows, want 2 (visible + unmapped)\n%+v", len(got), got)
	}
	for _, r := range got {
		if r.EventType == "__test.hidden" {
			t.Fatalf("filterLogVisibleRows did not drop hidden row\n%+v", got)
		}
	}
	// Sanity: the visible + unmapped rows survive in order.
	if got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("filterLogVisibleRows reordered or lost rows: got IDs %d,%d want 2,3", got[0].ID, got[1].ID)
	}
}
