package studio

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func hooksScreen(tb testing.TB, width, height int, state State) (Screen, screenhost.Frame) {
	tb.Helper()
	deps, err := StudioHooksDeps(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	frame := screentest.FrameAt(tb, width, height)
	screen := New().Bind(screenhost.StudioHooks, deps).WithState(state)
	return screen.withFrame(frame), frame
}

func TestStudioHooksColsStacksAt80AndSitsBesideAt120(t *testing.T) {
	t.Parallel()

	cases := []struct {
		width, height int
		want          screenlayout.Arrangement
	}{
		{80, 24, screenlayout.Stacked},
		{96, 24, screenlayout.SideBySide},
		{120, 40, screenlayout.SideBySide},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			t.Parallel()
			screen, frame := hooksScreen(t, tc.width, tc.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			res := screen.withFrame(frame).hooksGridResult()
			assertStudioColsBreakpoint(t, res, sectionHooks, sectionHooksList, hooksZones, tc.width, tc.height, tc.want)
			if tc.want != screenlayout.Stacked {
				return
			}
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioListIsOffScreen(t, screen.withFrame(frame).hooksGridResult(), sectionHooksList, sectionHooksFields, tc.width, tc.height)
			screen = studioDrive(t, screen, frame, "tab")
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioPaintedLeaf(t, screen.withFrame(frame).hooksGridResult(), sectionHooksList, tc.width, tc.height)
		})
	}
}

func TestStudioHooksTabWalksListThenInspector(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.grid.Focus(); got != sectionHooksList {
		t.Fatalf("entry focus = %q, want %q", got, sectionHooksList)
	}
	// One stop per painted zone: the hook's fields, then its HISTORY, then back
	// to the list. The fields used to be skipped — the inspector was one leaf and
	// its focus was spent on the HISTORY kicker.
	for i, want := range []screenlayout.ID{sectionHooksFields, sectionHooksHistory, sectionHooksList} {
		screen = studioDrive(t, screen, frame, "tab")
		if got := screen.grid.Focus(); got != want {
			t.Fatalf("after tab %d focus = %q, want %q", i+1, got, want)
		}
	}
}

func TestStudioHooksHistoryCursorMovesAloneAndResets(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range hooksFocusKeys(studioHookIsGuardTaskDelete) {
		screen = studioDrive(t, screen, frame, "down")
	}
	hookIndex := screen.studioHookIndex
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionHooksHistory {
		t.Fatalf("focus = %q, want %q", got, sectionHooksHistory)
	}

	screen = studioDrive(t, screen, frame, "j")
	screen = studioDrive(t, screen, frame, "j")
	if got := screen.grid.Layout().Cursor(sectionHooksHistory); got != 2 {
		t.Fatalf("history cursor = %d, want 2", got)
	}
	if got := screen.studioHookIndex; got != hookIndex {
		t.Fatalf("history movement changed hook index to %d, want %d", got, hookIndex)
	}
	state := screen.State()
	if got := state.HookHistoryIndex; got != 2 {
		t.Fatalf("state history cursor = %d, want 2", got)
	}
	restored, restoredFrame := hooksScreen(t, 120, 40, state)
	restored = restored.Lifecycle(restoredFrame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := restored.State().HookHistoryIndex; got != 2 {
		t.Fatalf("restored state history cursor = %d, want 2", got)
	}
	if got := restored.grid.Layout().Cursor(sectionHooksHistory); got != 2 {
		t.Fatalf("restored grid history cursor = %d, want 2", got)
	}

	// Return to the hook list and select a different hook. Its HISTORY starts at
	// the first row rather than inheriting the previous hook's third-row cursor;
	// an empty target exposes that reset as no selection.
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "down")
	wantCursor := 0
	if len(screen.hookHistoryFor(screen.studioHookIndex)) == 0 {
		wantCursor = -1
	}
	if got := screen.grid.Layout().Cursor(sectionHooksHistory); got != wantCursor {
		t.Fatalf("history cursor after hook change = %d, want %d", got, wantCursor)
	}
	if got := screen.studioHookHistoryIndex; got != 0 {
		t.Fatalf("persisted history cursor after hook change = %d, want reset 0", got)
	}
}

func TestStudioHooksHistoryCursorPersistsAndClamps(t *testing.T) {
	t.Parallel()

	hookIndex := HookIndexFor(studioHooksGoldenBundle().Config.Hooks, func(spec config.HookSpec) bool { return spec.Do == "exec" })
	screen, frame := hooksScreen(t, 120, 40, State{HookIndex: hookIndex, HookHistoryIndex: 99})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.studioHookHistoryIndex; got != 1 {
		t.Fatalf("clamped history cursor = %d, want last row 1", got)
	}
	if got := screen.grid.Layout().Cursor(sectionHooksHistory); got != 1 {
		t.Fatalf("grid history cursor = %d, want last row 1", got)
	}
	if got := screen.State().HookHistoryIndex; got != 1 {
		t.Fatalf("persisted history cursor = %d, want 1", got)
	}
}

func TestStudioHooksHistoryEnterIsExactNoOp(t *testing.T) {
	t.Parallel()

	hookIndex := HookIndexFor(studioHooksGoldenBundle().Config.Hooks, studioHookIsGuardTaskDelete)
	screen, frame := hooksScreen(t, 120, 40, State{HookIndex: hookIndex, HookHistoryIndex: 1})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionHooksHistory {
		t.Fatalf("focus = %q, want %q", got, sectionHooksHistory)
	}

	beforeHook := screen.grid.Layout().Cursor(sectionHooksList)
	beforeHistory := screen.grid.Layout().Cursor(sectionHooksHistory)
	beforeFocus := screen.grid.Focus()
	beforeView := screen.View(frame)
	outcome := screen.Update(frame, screentest.Key("enter"))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		t.Fatalf("enter carried %T", outcome.Screen)
	}
	if outcome.Action.Kind != screenhost.ActionNone {
		t.Fatalf("enter action = %v, want ActionNone", outcome.Action.Kind)
	}
	if outcome.Command != nil {
		t.Fatal("enter returned a command, want nil")
	}
	if got := next.grid.Layout().Cursor(sectionHooksList); got != beforeHook {
		t.Fatalf("enter moved hook cursor from %d to %d", beforeHook, got)
	}
	if got := next.grid.Layout().Cursor(sectionHooksHistory); got != beforeHistory {
		t.Fatalf("enter moved history cursor from %d to %d", beforeHistory, got)
	}
	if got := next.studioHookIndex; got != screen.studioHookIndex {
		t.Fatalf("enter moved hook index from %d to %d", screen.studioHookIndex, got)
	}
	if got := next.studioHookHistoryIndex; got != screen.studioHookHistoryIndex {
		t.Fatalf("enter moved persisted history cursor from %d to %d", screen.studioHookHistoryIndex, got)
	}
	if got := next.grid.Focus(); got != beforeFocus {
		t.Fatalf("enter moved focus from %q to %q", beforeFocus, got)
	}
	if got := next.View(frame); got != beforeView {
		t.Fatalf("enter changed rendered view\n--- before ---\n%s\n--- after ---\n%s", beforeView, got)
	}
}

func TestStudioHooksEmptyHistoryHasNoSelection(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionHooksHistory {
		t.Fatalf("focus = %q, want %q", got, sectionHooksHistory)
	}
	if got := screen.grid.Layout().Cursor(sectionHooksHistory); got != -1 {
		t.Fatalf("empty history cursor = %d, want no selection", got)
	}

	hookIndex := screen.studioHookIndex
	screen = studioDrive(t, screen, frame, "j")
	if got := screen.grid.Layout().Cursor(sectionHooksHistory); got != -1 {
		t.Fatalf("empty history cursor after j = %d, want no selection", got)
	}
	if got := screen.studioHookIndex; got != hookIndex {
		t.Fatalf("empty history movement changed hook index to %d, want %d", got, hookIndex)
	}
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "No hook.executed rows for this index.") {
		t.Fatalf("empty HISTORY copy disappeared\n%s", view)
	}
}

func TestStudioHooksCtrlSOpensApplyFromCols(t *testing.T) {
	t.Parallel()

	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioHooks, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); !studioZoneIsInspector(got, sectionHooksList) {
		t.Fatalf("tab did not land on an inspector zone: %q", got)
	}
	outcome := screen.Update(frame, screentest.Key("ctrl+s"))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		t.Fatalf("Update carried %T", outcome.Screen)
	}
	if outcome.Action.Kind == screenhost.ActionNavigate {
		t.Fatalf("inspector ctrl+s navigated to %s; want overlay Stay", outcome.Action.Target)
	}
	if !next.ApplyOverlayOpen() {
		t.Fatal("inspector ctrl+s did not open the apply overlay")
	}
}

func TestStudioHooksListContainsNotifyAndExec(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{
		"HOOKS",
		"▸ HOOKS ·",
		"notify",
		"exec",
		"guard.violated",
		"task.delete",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("hooks list missing %q\n%s", want, view)
		}
	}
}

func TestStudioHooksInspectorNotifyFocus(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range hooksFocusKeys(studioHookIsGuardTaskDelete) {
		screen = studioDrive(t, screen, frame, "down")
	}
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{
		"guard.violated",
		"kitten_destructive",
		"hint",
		"[ok]",
		"// HISTORY · hook.executed",
		"TIME",
		"TYPE",
		"ENTITY",
		"#2457",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("notify inspector missing %q\n%s", want, view)
		}
	}
}

func TestStudioHooksInspectorExecFocus(t *testing.T) {
	t.Parallel()

	screen, frame := hooksScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range hooksFocusKeys(func(spec config.HookSpec) bool { return spec.Do == "exec" }) {
		screen = studioDrive(t, screen, frame, "down")
	}
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{
		"log-created.sh",
		"3000",
		"[fail]",
		"timed out",
		"exec",
		"TIME",
		"DETAIL",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("exec inspector missing %q\n%s", want, view)
		}
	}
}

func TestStudioHooksHistoryTablePinnedAt80x24(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pred func(config.HookSpec) bool
		want []string
	}{
		{
			name: "notify",
			pred: studioHookIsGuardTaskDelete,
			want: []string{"HISTORY", "TIME", "TYPE", "ENTITY", "[ok]", "#2457", "4ms"},
		},
		{
			name: "exec",
			pred: func(spec config.HookSpec) bool { return spec.Do == "exec" },
			want: []string{"HISTORY", "TIME", "DETAIL", "[fail]", "3001ms", "timed out"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen, frame := hooksScreen(t, 80, 24, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			for range hooksFocusKeys(tc.pred) {
				screen = studioDrive(t, screen, frame, "down")
			}
			// HISTORY is the SECOND inspector stop: the hook's fields come first.
			screen = studioDrive(t, screen, frame, "tab")
			screen = studioDrive(t, screen, frame, "tab")
			view := screentest.StripANSI(screen.View(frame))
			if strings.Contains(view, "2026-08-13 14:") && strings.Contains(view, "  [fail]  ") {
				t.Fatalf("%s 80×24 still paints hookHistoryLine prose\n%s", tc.name, view)
			}
			for _, want := range tc.want {
				if !strings.Contains(view, want) {
					t.Fatalf("%s 80×24 HISTORY missing %q\n%s", tc.name, want, view)
				}
			}
		})
	}
}

func TestStudioHooksHistoryHeadingStaysPinnedWhileRowsScroll(t *testing.T) {
	t.Parallel()

	hookIndex := HookIndexFor(studioHooksGoldenBundle().Config.Hooks, studioHookIsGuardTaskDelete)
	history := make([]HookExecuted, 20)
	for i := range history {
		history[i] = HookExecuted{
			CreatedAt:     fmt.Sprintf("2026-08-13 14:%02d:00", i),
			Success:       true,
			DurationMs:    int64(i + 1),
			EventType:     "hook.executed",
			TargetEventID: int64(9000 + i),
		}
	}
	screen, frame := hooksScreen(t, 120, 24, State{HookIndex: hookIndex})
	screen.hookHistory = map[int][]HookExecuted{hookIndex: history}
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	initial := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(initial, "#9000") {
		t.Fatalf("first HISTORY row is not initially visible\n%s", initial)
	}
	for range 12 {
		screen = studioDrive(t, screen, frame, "j")
	}
	view := screentest.StripANSI(screen.View(frame))
	if strings.Contains(view, "#9000") {
		t.Fatalf("first HISTORY row remained visible after scrolling\n%s", view)
	}
	for _, want := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if !strings.Contains(view, want) {
			t.Fatalf("scrolled HISTORY lost pinned heading %q\n%s", want, view)
		}
	}
}

func TestStudioHooksEmptyHistoryPinned(t *testing.T) {
	t.Parallel()

	for _, geo := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		geo := geo
		t.Run(fmt.Sprintf("%dx%d", geo.w, geo.h), func(t *testing.T) {
			t.Parallel()
			screen, frame := hooksScreen(t, geo.w, geo.h, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			if geo.w < 120 {
				screen = studioDrive(t, screen, frame, "tab")
				screen = studioDrive(t, screen, frame, "tab")
			}
			view := screentest.StripANSI(screen.View(frame))
			if !strings.Contains(view, "No hook.executed rows for this index.") {
				t.Fatalf("index 0 inspector missing empty HISTORY copy at %dx%d\n%s", geo.w, geo.h, view)
			}
		})
	}
}

func TestStudioHooksNilPortPaintsEmptyHistory(t *testing.T) {
	t.Parallel()

	deps, err := StudioHooksDeps(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps.HookHistory = nil
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(screenhost.StudioHooks, deps)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range hooksFocusKeys(studioHookIsGuardTaskDelete) {
		screen = studioDrive(t, screen, frame, "down")
	}
	view := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "No hook.executed rows for this index.") {
		t.Fatalf("nil port did not pin empty HISTORY\n%s", view)
	}
}

func TestStudioHooksDoesNotImportAppOrSQLite(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("hooks.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, banned := range []string{"internal/sqlite", "internal/app", "internal/operation", "internal/tui/screens/logs"} {
		if strings.Contains(text, banned) {
			t.Fatalf("hooks.go contains banned import %q", banned)
		}
	}
}

func TestStudioHooksHistoryTableWideAndCompact(t *testing.T) {
	t.Parallel()

	m := Screen{hookHistory: map[int][]HookExecuted{
		0: {
			{CreatedAt: "2026-08-13 14:33:01", Success: false, DurationMs: 3001, EventType: "task.created", Error: "exec bash timed out after 3s"},
			{CreatedAt: "2026-08-13 14:12:08", Success: true, DurationMs: 12, EventType: "task.created", TargetEventID: 99},
		},
	}}
	m.studioDraft = nil
	kicker, columns, wide := m.hooksHistoryContent(0, 80)
	wide = append([]string{columns}, wide...)
	if !strings.Contains(kicker, "// HISTORY · hook.executed") {
		t.Fatalf("wide kicker = %q", kicker)
	}
	wideBody := strings.Join(wide, "\n")
	for _, want := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL", "[fail]", "[ok]", "task.created", "#99", "3001ms"} {
		if !strings.Contains(wideBody, want) {
			t.Fatalf("wide HISTORY missing %q\n%s", want, wideBody)
		}
	}
	if strings.Contains(wideBody, "2026-08-13 14:33:01  [fail]  3001ms") {
		t.Fatalf("wide HISTORY still paints hookHistoryLine prose\n%s", wideBody)
	}
	if !strings.Contains(wide[0], "TIME") {
		t.Fatalf("wide HISTORY first body line is not the column header: %q", wide[0])
	}

	_, _, compact := m.hooksHistoryContent(0, 40)
	compactBody := strings.Join(compact, "\n")
	if strings.Contains(compactBody, "TIME") && strings.Contains(compactBody, "TYPE") && strings.Contains(compactBody, "ENTITY") {
		t.Fatalf("compact HISTORY painted the wide column header\n%s", compactBody)
	}
	for _, want := range []string{"14:33:01", "task.created", "[fail]", "3001ms"} {
		if !strings.Contains(compactBody, want) {
			t.Fatalf("compact HISTORY missing %q\n%s", want, compactBody)
		}
	}
	if strings.Contains(compactBody, "2026-08-13 14:33:01  [fail]  3001ms") {
		t.Fatalf("compact HISTORY still paints hookHistoryLine prose\n%s", compactBody)
	}
}

// A long HISTORY must not cost the field table its rows. The box below it is
// windowed by the arranger, so the reservation is its MINIMUM — reserving the
// height of everything it holds is what sliced the field table on the hook with
// thirty-four executions.
func TestALongHistoryDoesNotEvictTheFieldTable(t *testing.T) {
	t.Parallel()

	screen, _ := hooksScreen(t, 80, 55, State{})
	hooks := screen.studioHookSpecs()
	if len(hooks) == 0 {
		t.Fatal("fixture carries no hooks")
	}
	fields := hookInspectorFields(screen, hooks[0], nil)
	const rows = 28 // what the inspector gets stacked on an 80x55 terminal

	full := screen.studioFieldTable(76, 0, hooksFieldOpts, screen.studioFieldRows("01 // task.created", false, fields...))
	got := screen.studioFieldTable(76, rows, hooksFieldOpts, screen.studioFieldRows("01 // task.created", false, fields...))
	if len(got) != len(full) {
		t.Fatalf("field table = %d of %d lines; the history below it is windowed, not fixed", len(got), len(full))
	}
}
