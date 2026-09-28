package logs

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// TestFormatEntityComposition pins the ENTITY column rules: task / comment rows
// render `<entity_type>#<entity_id>`; system events with entity_id=0 collapse
// to the bare entity_type. The renderer relies on these strings fitting inside
// the fixed-width column, so changes here also affect alignment.
func TestFormatEntityComposition(t *testing.T) {
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
			if got := FormatEntity(tc.row); got != tc.want {
				t.Fatalf("FormatEntity() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFormatWhoColumn pins the WHO column rules: tool-call rows surface
// `source`, comment rows surface `author_type`, system events fall back to the
// em-dash. Empty rows degrade to the dash so the column is never blank.
func TestFormatWhoColumn(t *testing.T) {
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
		{"source fallback outside tool calls", domain.EventRow{EntityType: "plan", EventType: domain.EventTypeBundleSwapped, Source: "cli"}, "cli"},
		{"author fallback outside comments", domain.EventRow{EntityType: "plan", EventType: domain.EventTypeBundleSwapped, AuthorType: "human"}, "human"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatWho(tc.row); got != tc.want {
				t.Fatalf("FormatWho() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestShortTimeTrimsToWidth pins the TIME column truncation: the SQLite
// "YYYY-MM-DD HH:MM:SS" stamp shrinks to its trailing `width` characters so the
// column always carries the most recent component. Values that already fit are
// returned untouched.
func TestShortTimeTrimsToWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ts    string
		width int
		want  string
	}{
		// 19-char SQLite stamp keeps the trailing 12 chars — "-DD HH:MM:SS".
		{"2026-05-27 13:45:08", 12, "-27 13:45:08"},
		{"2026-05-27 13:45:08", 8, "13:45:08"},
		{"13:45:08", 12, "13:45:08"},
		{"2026-05-27 13:45:08", 0, ""},
		{"2026-05-27 13:45:08", -1, ""},
	}
	for _, tc := range cases {
		if got := ShortTime(tc.ts, tc.width); got != tc.want {
			t.Errorf("ShortTime(%q, %d) = %q, want %q", tc.ts, tc.width, got, tc.want)
		}
	}
}

// TestComputeEventStatsToolCallHealth pins the health subset: only
// `*.tool_call` + `hook.executed` rows contribute, and each is bucketed by
// row.Status (ok / error / running). Other event_types are silently skipped so
// the headline number cannot mix categories.
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
	stats := domain.ComputeEventStats(rows, nil)
	if stats.ToolCallOK != 2 {
		t.Errorf("ToolCallOK = %d, want 2", stats.ToolCallOK)
	}
	if stats.ToolCallError != 1 {
		t.Errorf("ToolCallError = %d, want 1", stats.ToolCallError)
	}
	if stats.ToolCallRunning != 1 {
		t.Errorf("ToolCallRunning = %d, want 1", stats.ToolCallRunning)
	}
	// Categories from a nil counts map must be seeded to zero for every known
	// category so the renderer can walk them without branching on nil.
	for _, c := range domain.KnownEventCategories {
		if _, ok := stats.Categories[c]; !ok {
			t.Errorf("Categories[%q] missing — must be seeded to 0", c)
		}
	}
}

// TestComputeEventStatsCategoryCountsPassthrough confirms the
// repository-supplied per-category counts are taken verbatim — the renderer
// never overrides the canonical totals.
func TestComputeEventStatsCategoryCountsPassthrough(t *testing.T) {
	t.Parallel()
	counts := map[domain.EventCategory]int{
		domain.EventCategoryTask:     5,
		domain.EventCategoryComment:  2,
		domain.EventCategoryToolCall: 3,
	}
	stats := domain.ComputeEventStats(nil, counts)
	for cat, want := range counts {
		if got := stats.Categories[cat]; got != want {
			t.Errorf("Categories[%q] = %d, want %d", cat, got, want)
		}
	}
}

// TestWidePanelEmitsFiveColumnLayout exercises the wide renderer end-to-end
// with a synthetic event-row buffer: the header must carry every column tag
// (TIME, TYPE, ENTITY, WHO, DETAIL) and each row must surface its
// SummarizeEvent detail string.
func TestWidePanelEmitsFiveColumnLayout(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	screen = screen.Apply(Payload{
		Rows: []domain.EventRow{
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
		},
		Stats: domain.ComputeEventStats([]domain.EventRow{
			{EventType: domain.EventTypeCLIToolCall, Status: "ok"},
			{EventType: domain.EventTypeTaskCreated},
		}, map[domain.EventCategory]int{
			domain.EventCategoryToolCall: 1,
			domain.EventCategoryTask:     1,
		}),
	})

	view := ansi.Strip(screen.View(frame))
	for _, want := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide panel missing column tag %q\n%s", want, view)
		}
	}
	for _, want := range []string{
		// The TYPE column renders EventDef.Display from the YAML registry, not
		// the raw event_type. Tests run with the omakase fixture loaded via
		// TestMain, so these labels are the canonical kit values.
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

// TestCompactPanelDropsAuxiliaryColumns confirms the narrow-terminal variant
// collapses to the marker + time + type + detail shape so it still fits the
// 32-72 cell budget — the explicit ENTITY / WHO column tags are dropped, but
// the SummarizeEvent detail still carries the per-row signal.
func TestCompactPanelDropsAuxiliaryColumns(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 80)
	screen = screen.Apply(Payload{Rows: []domain.EventRow{
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
	}})
	view := ansi.Strip(screen.View(frame))
	for _, want := range []string{
		"▸ ACTIVITY · 1",
		"CLI tool call",
		"cli/app.TaskService.Add [ok] 7ms",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("compact panel missing %q\n%s", want, view)
		}
	}
	// The compact panel must NOT print the wide-panel column tags — the width
	// budget doesn't afford them.
	for _, banned := range []string{"ENTITY", "WHO", "DETAIL"} {
		if strings.Contains(view, banned) {
			t.Errorf("compact panel unexpectedly contains wide tag %q\n%s", banned, view)
		}
	}
}

// TestRowsSanitizePersistedCellsAtTheTerminalSink proves both row shapes drop
// terminal controls only after the domain summary has composed its persisted
// fields, while harmless Unicode remains visible. The fixture deck supplies
// ESC, OSC, C0 and C1 values across comments, payload-derived titles, source,
// entity and the unknown-event summary fallback.
func TestRowsSanitizePersistedCellsAtTheTerminalSink(t *testing.T) {
	t.Parallel()
	_, frame := testScreen(t, 180)
	for _, row := range logsGoldenControlFeed() {
		assertLogRowSafe(t, frame, row)
	}
}

func assertLogRowSafe(t *testing.T, frame screenhost.Frame, row domain.EventRow) {
	t.Helper()
	for name, rendered := range map[string]string{
		"wide":    formatWideRow(frame.Kit(), " ", row, 12, 40, 40, 40, 200),
		"compact": compactRow(frame.Kit(), " ", row, 240),
	} {
		plain := ansi.Strip(rendered)
		assertNoTerminalControls(t, name, row.ID, plain)
		if !strings.Contains(plain, "日本語") || !strings.Contains(plain, "😀") {
			t.Errorf("%s row %d lost harmless Unicode in %q", name, row.ID, plain)
		}
	}
}

func assertNoTerminalControls(t *testing.T, shape string, rowID int64, plain string) {
	t.Helper()
	for _, r := range plain {
		if unicode.IsControl(r) {
			t.Errorf("%s row %d retained terminal control U+%04X in %q", shape, rowID, r, plain)
		}
	}
}

// TestSummaryTablesListEveryKnownCategory locks the Categories summary: it must
// surface every domain.KnownEventCategory so the user sees the full grouping
// vocabulary, with zero counts when the window holds no matching rows.
func TestSummaryTablesListEveryKnownCategory(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	screen = screen.Apply(Payload{Stats: domain.EventStats{Categories: map[domain.EventCategory]int{
		domain.EventCategoryTask:     3,
		domain.EventCategoryToolCall: 7,
	}}})
	view := ansi.Strip(screen.renderSummaryTables(frame.Kit(), frame.Kit().PanelContentWidth()))
	if !strings.Contains(view, "CATEGORIES") {
		t.Fatalf("summary missing the Categories table kicker\n%s", view)
	}
	// The health table kicker carries the tool_call scope hint.
	if !strings.Contains(view, "HEALTH") || !strings.Contains(view, "TOOL_CALLS") {
		t.Fatalf("summary missing the Health · tool_calls table kicker\n%s", view)
	}
	for _, cat := range domain.KnownEventCategories {
		if !strings.Contains(view, strings.ToUpper(string(cat))) {
			t.Errorf("Categories table missing %q\n%s", cat, view)
		}
	}
}

// TestEmptyStateRendersEmptyPanel confirms an empty buffer renders the
// canonical empty-state panel — not a stray table or a stale row — while
// keeping the chip strip visible so the user can cycle back off a filter.
func TestEmptyStateRendersEmptyPanel(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	view := ansi.Strip(screen.View(frame))
	for _, banned := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if strings.Contains(view, banned) {
			t.Errorf("empty Logs view unexpectedly carries wide column %q\n%s", banned, view)
		}
	}
	if !strings.Contains(view, "FILTER") {
		t.Errorf("empty Logs view must keep the chip strip visible\n%s", view)
	}
}

// TestUnavailableWithoutEventsPort proves the screen renders the placeholder
// rather than an empty panel when no repository is wired.
func TestUnavailableWithoutEventsPort(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	view := ansi.Strip(New().View(frame))
	if !strings.Contains(view, "Activity logging is not available") {
		t.Fatalf("expected the unavailable placeholder, got:\n%s", view)
	}
}

// TestWidthSplitSwitchesPanels pins the layout threshold: below it the compact
// renderer wins; at or above, the wide layout wins. Exercised through View so
// the dispatch stays covered end-to-end.
func TestWidthSplitSwitchesPanels(t *testing.T) {
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

	wide, wideFrame := testScreen(t, 180)
	wide = wide.Apply(Payload{Rows: []domain.EventRow{row}})
	if !strings.Contains(ansi.Strip(wide.View(wideFrame)), "TIME") {
		t.Errorf("width=180 must dispatch to the wide panel (carries the TIME header)")
	}

	compact, compactFrame := testScreen(t, 70)
	compact = compact.Apply(Payload{Rows: []domain.EventRow{row}})
	view := ansi.Strip(compact.View(compactFrame))
	if strings.Contains(view, "ENTITY") || strings.Contains(view, "DETAIL") {
		t.Errorf("width=70 must dispatch to the compact panel (no wide headers)\n%s", view)
	}
}

// TestRetentionNoteVariants pins the retention shapes plus the unwired
// fallback. The note explains why older tool calls may be absent, so each arm
// has to name the numbers it actually knows.
func TestRetentionNoteVariants(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)
	rows := []domain.EventRow{{ID: 1, EventType: domain.EventTypeTaskCreated, EntityType: "task", EntityID: 1, CreatedAt: "2026-05-27 13:45:08"}}

	cases := []struct {
		name      string
		retention Retention
		want      string
	}{
		{"unwired snapshot", Retention{}, "retention policy unavailable"},
		{"window only", Retention{Known: true, WindowDays: 14}, "14"},
		{"rows only", Retention{Known: true, MaxRows: 5000, WindowDays: 14}, "5000"},
		{"rows and age", Retention{Known: true, MaxAgeDays: 30, MaxRows: 5000, WindowDays: 14}, "30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen := New().Bind(Deps{
				Available: true,
				Settings:  ViewSettings{Retention: tc.retention},
			}).Apply(Payload{Rows: rows})
			view := ansi.Strip(screen.View(frame))
			if !strings.Contains(view, tc.want) {
				t.Fatalf("retention note missing %q\n%s", tc.want, view)
			}
		})
	}
}

// TestChipStripMarksTheActiveFilter proves the active preset is the bracketed
// chip and the others are not — the strip is the only on-screen cue that a
// non-default filter is narrowing the rows.
func TestChipStripMarksTheActiveFilter(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	for _, mode := range FilterModes {
		screen.filter = mode
		strip := ansi.Strip(screen.renderFilterChips(frame.Kit()))
		label := frame.Text(FilterChipKey(mode))
		if !strings.Contains(strip, "[ "+label+" ]") {
			t.Fatalf("chip strip does not bracket the active filter %q\n%s", label, strip)
		}
		for _, other := range FilterModes {
			if other == mode {
				continue
			}
			if strings.Contains(strip, "[ "+frame.Text(FilterChipKey(other))+" ]") {
				t.Fatalf("chip strip brackets an inactive filter\n%s", strip)
			}
		}
	}
}

// TestRendersDisplayLabel pins the TYPE cell: the wide and compact panels
// render EventDef.Display (from the YAML-loaded registry) and fall back to the
// raw event_type when the lookup misses.
//
// Sequential because the test mutates the package-level domain.EventDefByKey
// map; cleanup restores the prior state.
func TestRendersDisplayLabel(t *testing.T) {
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
		// The formatter emits a summary string that does NOT carry the raw
		// event_type so the "does NOT contain task.created" assertion scopes to
		// the TYPE cell rather than colliding with the DETAIL cell.
		Formatter: func(domain.EventRow) string { return "wired renderer" },
	}
	domain.EventDefByKey = cloned
	t.Cleanup(func() { domain.EventDefByKey = prev })

	created := domain.EventRow{
		ID:         1,
		EntityType: "task",
		EntityID:   42,
		EventType:  "task.created",
		CreatedAt:  "2026-05-27 13:46:00",
		Payload:    `{"title":"Wire renderer","bucket":"backlog","priority":"normal"}`,
	}

	t.Run("wide_panel_renders_display", func(t *testing.T) {
		_, frame := testScreen(t, 180)
		out := ansi.Strip(formatWideRow(frame.Kit(), " ", created, 12, 20, 16, 8, 40))
		if !strings.Contains(out, "task created") {
			t.Fatalf("wide row missing display label %q\n%s", "task created", out)
		}
		if strings.Contains(out, "task.created") {
			t.Fatalf("wide row unexpectedly carries raw event_type %q\n%s", "task.created", out)
		}
	})

	t.Run("compact_panel_renders_display", func(t *testing.T) {
		screen, frame := testScreen(t, 80)
		screen = screen.Apply(Payload{Rows: []domain.EventRow{created}})
		view := ansi.Strip(screen.View(frame))
		if !strings.Contains(view, "task created") {
			t.Fatalf("compact panel missing display label %q\n%s", "task created", view)
		}
		if strings.Contains(view, "task.created") {
			t.Fatalf("compact panel unexpectedly carries raw event_type %q\n%s", "task.created", view)
		}
	})

	t.Run("unknown_event_type_falls_back_to_raw_key", func(t *testing.T) {
		unknown := domain.EventRow{
			ID:         99,
			EntityType: "system",
			EventType:  "__test.unknown",
			CreatedAt:  "2026-05-27 13:46:00",
		}
		_, frame := testScreen(t, 180)
		wide := ansi.Strip(formatWideRow(frame.Kit(), " ", unknown, 12, 20, 16, 8, 40))
		if !strings.Contains(wide, "__test.unknown") {
			t.Fatalf("wide row missing raw fallback %q\n%s", "__test.unknown", wide)
		}

		compactScreen, compactFrame := testScreen(t, 80)
		compactScreen = compactScreen.Apply(Payload{Rows: []domain.EventRow{unknown}})
		compact := ansi.Strip(compactScreen.View(compactFrame))
		if !strings.Contains(compact, "__test.unknown") {
			t.Fatalf("compact panel missing raw fallback %q\n%s", "__test.unknown", compact)
		}
	})
}

// TestLogVisibleFilterHidesAll locks the empty-buffer short-circuit in the
// reload apply. When every fixture row maps to an EventDef with
// LogVisible == false the buffer empties, so the cursor must drop to 0 (later
// renders and marker lookups must never address a stale index) and the renderer
// must hit the canonical empty-state panel — never the wide table headers.
func TestLogVisibleFilterHidesAll(t *testing.T) {
	prev := domain.EventDefByKey
	cloned := make(map[string]domain.EventDef, len(prev))
	for k, v := range prev {
		cloned[k] = v
	}
	for _, key := range []string{"__test.hide_all_a", "__test.hide_all_b"} {
		cloned[key] = domain.EventDef{
			Key:        key,
			Category:   domain.EventCategoryDomain,
			LogVisible: false,
			Formatter:  func(domain.EventRow) string { return "" },
		}
	}
	domain.EventDefByKey = cloned
	t.Cleanup(func() { domain.EventDefByKey = prev })

	frame := screentest.FrameAt(t, 180, 40)
	screen := New().Bind(Deps{Available: true}).Apply(Payload{Rows: []domain.EventRow{
		{ID: 1, EventType: "__test.hide_all_a"},
		{ID: 2, EventType: "__test.hide_all_b"},
		{ID: 3, EventType: "__test.hide_all_a"},
	}})
	// Seed a non-zero cursor so the prepared payload is proven to reset it.
	screen.selected = 2

	screen = screen.Apply(Payload{Rows: nil})
	if len(screen.Rows()) != 0 {
		t.Fatalf("rows: got %d, want 0 (all filtered out)", len(screen.Rows()))
	}
	if screen.Selected() != 0 {
		t.Fatalf("selected: got %d, want 0 after an empty filter", screen.Selected())
	}
	view := ansi.Strip(screen.View(frame))
	for _, banned := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if strings.Contains(view, banned) {
			t.Errorf("filter-emptied Logs view unexpectedly carries wide column %q\n%s", banned, view)
		}
	}
}

// TestFilterLogVisibleRows pins the registry filter: rows whose event_type maps
// to an EventDef with LogVisible == false are dropped, while registry-miss and
// LogVisible == true rows pass through untouched and in order.
func TestFilterLogVisibleRows(t *testing.T) {
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
	got := domain.FilterLogVisibleRows(rows)
	if len(got) != 2 {
		t.Fatalf("FilterLogVisibleRows: got %d rows, want 2 (visible + unmapped)\n%+v", len(got), got)
	}
	for _, r := range got {
		if r.EventType == "__test.hidden" {
			t.Fatalf("FilterLogVisibleRows did not drop the hidden row\n%+v", got)
		}
	}
	// Sanity: the visible + unmapped rows survive in order.
	if got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("FilterLogVisibleRows reordered or lost rows: got IDs %d,%d want 2,3", got[0].ID, got[1].ID)
	}
	if got := domain.FilterLogVisibleRows(nil); got != nil {
		t.Fatalf("FilterLogVisibleRows(nil) = %v, want nil", got)
	}
}

// TestCategoryAccentMapping proves every known category resolves to its own
// accent and an unmapped one falls back to the neutral hint style, so a theme
// that pre-dates the event inspector never paints an unstyled glyph.
func TestCategoryAccentMapping(t *testing.T) {
	t.Parallel()
	styles := screentest.Styles()

	if got := styles.Accent(CategoryAccent(domain.EventCategory("__unknown"))).GetForeground(); got != styles.Hint.GetForeground() {
		t.Fatalf("unknown category foreground = %v, want the hint fallback", got)
	}
	// The categories with a dedicated theme token. `update` and `tui` are
	// deliberately absent: they share the neutral hint tone today, and this
	// extraction preserved that.
	for _, cat := range []domain.EventCategory{
		domain.EventCategoryTask,
		domain.EventCategoryTagDep,
		domain.EventCategoryComment,
		domain.EventCategoryPlan,
		domain.EventCategoryAudit,
		domain.EventCategoryDomain,
		domain.EventCategoryGuard,
		domain.EventCategoryTrick,
		domain.EventCategoryToolCall,
		domain.EventCategoryHook,
	} {
		if got := styles.Accent(CategoryAccent(cat)).GetForeground(); got == styles.Hint.GetForeground() {
			t.Fatalf("category %q fell through to the neutral hint style", cat)
		}
	}
	// Categories that intentionally share an accent must keep sharing it.
	if styles.Accent(CategoryAccent(domain.EventCategoryTask)).GetForeground() != styles.Accent(CategoryAccent(domain.EventCategoryTagDep)).GetForeground() {
		t.Fatal("task and tag-dep rows must share one accent")
	}
	if styles.Accent(CategoryAccent(domain.EventCategoryToolCall)).GetForeground() != styles.Accent(CategoryAccent(domain.EventCategoryHook)).GetForeground() {
		t.Fatal("tool-call and hook rows must share one accent")
	}
}

// --- helpers ---------------------------------------------------------

// testScreen builds a Logs screen bound to a stub port (so the renderer takes
// its "service wired" branch) plus the frame it renders against.
func testScreen(t *testing.T, width int) (Screen, screenhost.Frame) {
	t.Helper()
	return New().Bind(Deps{
		Available: true,
	}), screentest.FrameAt(t, width, 40)
}
