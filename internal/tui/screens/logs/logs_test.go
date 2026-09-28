package logs

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/testutil"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func sampleRows(n int) []domain.EventRow {
	rows := make([]domain.EventRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, domain.EventRow{
			ID:         int64(i + 1),
			EntityType: "task",
			EntityID:   int64(i + 1),
			EventType:  domain.EventTypeTaskCreated,
			CreatedAt:  "2026-05-27 13:45:08",
			Payload:    `{"title":"row"}`,
		})
	}
	return rows
}

func loadedScreen(t *testing.T, rows int) Screen {
	t.Helper()
	screen, _ := testScreen(t, 180)
	return screen.Apply(Payload{Rows: preparedRows(sampleRows(rows))})
}

// TestScreenIdentityAndChrome pins the contract surface: the stable id, the
// footer declarations (screen-owned keys only — no host nav trailer) and the
// single help group with its stable id.
func TestScreenIdentityAndChrome(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	assertLogsIdentityAndChrome(t, screen, frame)
}

func assertLogsIdentityAndChrome(t *testing.T, screen Screen, frame screenhost.Frame) {
	if screen.ID() != screenhost.StatsLogs {
		t.Fatalf("ID() = %q, want %q", screen.ID(), screenhost.StatsLogs)
	}
	if !screen.OwnsKey(screentest.Key("f")) || !screen.OwnsKey(screentest.Key("F")) || screen.OwnsKey(screentest.Key("tab")) {
		t.Fatal("logs did not claim filter keys while leaving tab to shared navigation")
	}

	assertLogsFooter(t, screen, frame)
	assertLogsHelp(t, screen, frame)
}

func assertLogsFooter(t *testing.T, screen Screen, frame screenhost.Frame) {
	footer := screen.Footer(frame)
	wantKeys := []string{"up/down", "r", "pgup/pgdn", "g/G"}
	if len(footer) != len(wantKeys) {
		t.Fatalf("footer has %d bindings, want %d", len(footer), len(wantKeys))
	}
	for i, want := range wantKeys {
		if footer[i].Key != want {
			t.Fatalf("footer[%d].Key = %q, want %q", i, footer[i].Key, want)
		}
		if footer[i].Label == "" {
			t.Fatalf("footer[%d] (%q) has no label", i, want)
		}
	}
	for _, binding := range footer {
		if binding.Key == "tab" || binding.Key == ",//" || binding.Key == "?" {
			t.Fatalf("screen advertises host-owned key %q", binding.Key)
		}
	}

}

func assertLogsHelp(t *testing.T, screen Screen, frame screenhost.Frame) {
	help := screen.Help(frame)
	if len(help) != 1 || help[0].ID != "stats_logs" {
		t.Fatalf("help groups = %+v, want a single stats_logs group", help)
	}
	if help[0].Title == "" || len(help[0].Bindings) != 7 {
		t.Fatalf("help group = %+v, want a title and 7 bindings", help[0])
	}
}

// TestUpdateMovesCursorWithinBounds pins the row-navigation vocabulary and its
// clamping: the cursor never leaves [0, len-1] regardless of how far a verb
// tries to push it.
func TestUpdateMovesCursorWithinBounds(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)

	cases := []struct {
		name  string
		start int
		keys  []string
		want  int
	}{
		{"down advances", 0, []string{"down"}, 1},
		{"j advances", 0, []string{"j"}, 1},
		{"up retreats", 3, []string{"up"}, 2},
		{"k retreats", 3, []string{"k"}, 2},
		{"up at the top is a no-op", 0, []string{"up"}, 0},
		{"down at the bottom is a no-op", 5, []string{"down"}, 5},
		{"g jumps to the first row", 4, []string{"g"}, 0},
		{"home jumps to the first row", 4, []string{"home"}, 0},
		{"G jumps to the last row", 0, []string{"G"}, 5},
		{"end jumps to the last row", 0, []string{"end"}, 5},
		// A half page is now larger than this six-row buffer, so both paging
		// verbs clamp on the first press. That is the behaviour change #2441
		// landed: the page step is PageStep(viewportRows), and viewportRows was
		// pinned at 0 for every geometry because the summary block was charged
		// as if the terminal had rows it did not — so PageStep took its floor of
		// 4 and a "half page" was four rows on a fifty-row terminal. The real
		// step is exercised over a longer buffer below.
		{"pgdown clamps at the last row", 0, []string{"pgdown"}, 5},
		{"ctrl+d clamps at the last row", 0, []string{"ctrl+d"}, 5},
		{"repeated pgdown clamps at the last row", 0, []string{"pgdown", "pgdown", "pgdown"}, 5},
		{"pgup clamps at the first row", 5, []string{"pgup"}, 0},
		{"ctrl+u clamps at the first row", 5, []string{"ctrl+u"}, 0},
		{"repeated pgup clamps at the first row", 5, []string{"pgup", "pgup"}, 0},
		{"unbound keys leave the cursor alone", 2, []string{"x"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One screen per case rather than one shared value: the screens
			// carry their composition memo behind a pointer, so two parallel
			// cases descended from one value would drive one cache from two
			// goroutines. Sharing WORK across a screen's own frames is what the
			// memo is for; sharing it across parallel tests is a data race.
			screen := loadedScreen(t, 6)
			screen.selected = tc.start
			for _, key := range tc.keys {
				outcome := screen.Update(frame, screentest.Key(key))
				if outcome.Action.Kind != screenhost.ActionNone {
					t.Fatalf("navigation emitted action %v, want none", outcome.Action.Kind)
				}
				screen = outcome.Screen.(Screen)
			}
			if screen.Selected() != tc.want {
				t.Fatalf("selected = %d, want %d", screen.Selected(), tc.want)
			}
		})
	}
}

// TestUpdateOnEmptyBufferKeepsCursorAtZero proves the navigation verbs cannot
// address a row that does not exist — the failure mode that produced a stale
// marker lookup before the cursor clamp landed.
func TestUpdateOnEmptyBufferKeepsCursorAtZero(t *testing.T) {
	t.Parallel()
	screen, frame := testScreen(t, 180)
	for _, key := range []string{"down", "up", "pgdown", "pgup", "G", "g", "end", "home"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
		if screen.Selected() != 0 {
			t.Fatalf("key %q moved the cursor to %d on an empty buffer", key, screen.Selected())
		}
	}
}

// TestUpdateIgnoresNonKeyMessages proves an unrelated bubbletea message leaves
// the screen untouched rather than falling into a key branch.
func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)
	screen := loadedScreen(t, 4)
	screen.selected = 2

	outcome := screen.Update(frame, tea.WindowSizeMsg{Width: 10, Height: 10})
	got := outcome.Screen.(Screen)
	if got.Selected() != 2 || got.Filter() != FilterAll {
		t.Fatalf("non-key message mutated the screen: selected=%d filter=%v", got.Selected(), got.Filter())
	}
}

// TestScrollFollowsTheCursorPastTheViewport proves the scroll offset advances
// once the cursor walks past the visible window, so the selected row can never
// be off-screen.
func TestScrollFollowsTheCursorPastTheViewport(t *testing.T) {
	t.Parallel()
	// The panel budget is well under 200 rows at any realistic height, so the
	// window has to move for the last row to be visible.
	frame := screentest.FrameAt(t, 180, 80)
	screen := loadedScreen(t, 200)

	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if screen.Scroll() == 0 {
		t.Fatalf("jumping to the last row must scroll the window, offset stayed at 0")
	}
	screen = screen.Update(frame, screentest.Key("g")).Screen.(Screen)
	if screen.Scroll() != 0 {
		t.Fatalf("jumping to the first row must rewind the window, offset = %d", screen.Scroll())
	}
}

// TestResizeRecomputesTheViewportBudget proves the row budget tracks the frame
// rather than any state captured at construction — a screen must never render
// against a stale geometry.
func TestResizeRecomputesTheViewportBudget(t *testing.T) {
	t.Parallel()
	screen := loadedScreen(t, 40)

	tall := screen.viewportRows(screentest.FrameAt(t, 180, 80).Kit())
	short := screen.viewportRows(screentest.FrameAt(t, 180, 60).Kit())
	tiny := screen.viewportRows(screentest.FrameAt(t, 180, 12).Kit())

	if tall <= short {
		t.Fatalf("viewport must grow with terminal height: tall=%d short=%d", tall, short)
	}
	if tiny != 0 {
		t.Fatalf("a tiny terminal must yield no budget, got %d", tiny)
	}
}

// TestLifecycleResyncsScrollWithoutMovingTheCursor pins the lifecycle contract:
// every event is idempotent and cursor-preserving.
func TestLifecycleResyncsScrollWithoutMovingTheCursor(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 80)
	screen := loadedScreen(t, 40)
	screen.selected = 20

	for _, event := range []screenhost.LifecycleEvent{
		screenhost.LifecycleEnter,
		screenhost.LifecycleLeave,
		screenhost.LifecycleFocus,
		screenhost.LifecycleBlur,
		screenhost.LifecycleResize,
	} {
		outcome := screen.Lifecycle(frame, event)
		if outcome.Action.Kind != screenhost.ActionNone {
			t.Fatalf("lifecycle %v emitted action %v, want none", event, outcome.Action.Kind)
		}
		got := outcome.Screen.(Screen)
		if got.Selected() != 20 {
			t.Fatalf("lifecycle %v moved the cursor to %d", event, got.Selected())
		}
	}
}

// TestFilterCycleReloadsAndResetsTheCursor proves `f` rotates the preset,
// re-queries with the new category set and lands the user on the first matching
// row rather than an out-of-range selection.
func TestFilterCycleReloadsAndResetsTheCursor(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)

	screen := New().Bind(Deps{Available: true}).Apply(Payload{Rows: preparedRows(sampleRows(3))})
	screen.selected = 5

	outcome := screen.Update(frame, screentest.Key("f"))
	if outcome.Action.Kind != screenhost.ActionReload {
		t.Fatalf("a filter cycle emitted action %v, want reload", outcome.Action.Kind)
	}
	got := outcome.Screen.(Screen)
	if got.Filter() != FilterToolCalls {
		t.Fatalf("f must advance to FilterToolCalls, got %v", got.Filter())
	}
	if got.Selected() != 0 {
		t.Fatalf("filter cycle must reset the cursor, got %d", got.Selected())
	}
	if len(got.Rows()) != 3 {
		t.Fatalf("filter cycle must preserve the prepared buffer, got %d rows", len(got.Rows()))
	}
}

// TestFilterCycleBackwardWrapsAround pins the reverse rotation: the first
// shift+F from `all` lands on `system`, the wraparound the help text promises.
func TestFilterCycleBackwardWrapsAround(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)
	screen, _ := testScreen(t, 180)

	got := screen.Update(frame, screentest.Key("F")).Screen.(Screen)
	if got.Filter() != FilterSystem {
		t.Fatalf("shift+F must walk FilterAll → FilterSystem, got %v", got.Filter())
	}
	got = got.Update(frame, screentest.Key("f")).Screen.(Screen)
	if got.Filter() != FilterAll {
		t.Fatalf("f must walk FilterSystem → FilterAll, got %v", got.Filter())
	}
}

// TestFilterCycleWithoutPortStillRotates keeps headless hosts usable.
func TestFilterCycleWithoutPortStillRotates(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 40)
	screen := New().Apply(Payload{Rows: preparedRows(sampleRows(2))})

	outcome := screen.Update(frame, screentest.Key("f"))
	got := outcome.Screen.(Screen)
	if got.Filter() != FilterToolCalls {
		t.Fatalf("filter must rotate without a port, got %v", got.Filter())
	}
	if outcome.Action.Kind != screenhost.ActionReload {
		t.Fatalf("a filter cycle must request host reload, got %v", outcome.Action.Kind)
	}
}

// TestApplyConsumesPreparedRows proves visibility policy is applied before the
// payload reaches the screen, including for an explicit filter.
func TestApplyHonoursTheExplicitChipOverTheRegistry(t *testing.T) {
	cloned := make(map[string]domain.EventDef)
	cloned["__test.chip_hidden"] = domain.EventDef{
		Key:        "__test.chip_hidden",
		Category:   domain.EventCategoryDomain,
		LogVisible: false,
		Formatter:  func(domain.EventRow) string { return "" },
	}
	registry := registryWith(cloned)

	rows := []domain.EventRow{registry.Prepare(domain.EventRow{ID: 1, EventType: "__test.chip_hidden"})}

	prepared := New().Apply(Payload{Rows: preparedRows(domain.FilterLogVisibleRows(rows))})
	if len(prepared.Rows()) != 0 {
		t.Fatalf("the host-prepared all payload should omit hidden rows, got %d", len(prepared.Rows()))
	}

	narrowed := New()
	narrowed.filter = FilterSystem
	if got := narrowed.Apply(Payload{Rows: preparedRows(rows)}); len(got.Rows()) != 1 {
		t.Fatalf("an explicit payload must remain untouched, got %d rows", len(got.Rows()))
	}
}

// TestApplyClampsTheCursorIntoTheNewBuffer proves a reload that shrinks the
// result cannot leave the cursor addressing a row that no longer exists.
func TestApplyClampsTheCursorIntoTheNewBuffer(t *testing.T) {
	t.Parallel()
	screen := New().Apply(Payload{Rows: preparedRows(sampleRows(10))})
	screen.selected = 9

	shrunk := screen.Apply(Payload{Rows: preparedRows(sampleRows(3))})
	if shrunk.Selected() != 2 {
		t.Fatalf("selected = %d, want the last row of the shrunken buffer", shrunk.Selected())
	}
	emptied := shrunk.Apply(Payload{Rows: preparedRows(nil)})
	if emptied.Selected() != 0 {
		t.Fatalf("selected = %d, want 0 on an emptied buffer", emptied.Selected())
	}
}

// TestApplyKeepsAnInRangeCursor proves a reload that preserves the row count
// does not disturb the user's selection — the realtime tick must not yank the
// cursor while the user is reading.
func TestApplyKeepsAnInRangeCursor(t *testing.T) {
	t.Parallel()
	screen := New().Apply(Payload{Rows: preparedRows(sampleRows(10))})
	screen.selected = 4
	if got := screen.Apply(Payload{Rows: preparedRows(sampleRows(10))}); got.Selected() != 4 {
		t.Fatalf("selected = %d, want the cursor preserved across a same-size reload", got.Selected())
	}
}

// TestResetDropsProjectBoundState pins the project-switch contract: rows and
// aggregate state cannot survive the switch while the new payload loads.
func TestResetDropsProjectBoundState(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 180, 80)
	screen := loadedScreen(t, 200)
	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if screen.Scroll() == 0 || screen.Selected() == 0 {
		t.Fatal("fixture did not move the cursor; nothing to rewind")
	}

	reset := screen.Reset()
	if reset.Selected() != 0 || reset.Scroll() != 0 {
		t.Fatalf("Reset left the cursor at %d / scroll at %d", reset.Selected(), reset.Scroll())
	}
	stats := reset.Stats()
	if len(reset.Rows()) != 0 || len(stats.Categories) != 0 || stats.ToolCallOK != 0 || stats.ToolCallError != 0 || stats.ToolCallRunning != 0 {
		t.Fatalf("Reset left project-bound data behind: rows=%d stats=%+v", len(reset.Rows()), stats)
	}
}

func registryWith(overrides map[string]domain.EventDef) *domain.EventRegistry {
	definitions := testutil.EventRegistry().Definitions()
	for i, def := range definitions {
		if override, ok := overrides[def.Key]; ok {
			definitions[i] = override
			delete(overrides, def.Key)
		}
	}
	for _, def := range overrides {
		definitions = append(definitions, def)
	}
	return domain.NewEventRegistry(definitions)
}

func preparedRows(rows []domain.EventRow) []domain.EventRow {
	for i := range rows {
		if rows[i].Category == "" {
			rows[i] = testutil.EventRegistry().Prepare(rows[i])
		}
	}
	return rows
}
