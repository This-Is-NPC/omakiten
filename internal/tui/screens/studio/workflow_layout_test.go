package studio

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func workflowScreen(tb testing.TB, width, height int, state State) (Screen, screenhost.Frame) {
	tb.Helper()
	deps, err := StudioWorkflowDeps(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	frame := screentest.FrameAt(tb, width, height)
	screen := New().Bind(screenhost.StudioWorkflow, deps).WithState(state)
	return screen.withFrame(frame), frame
}

func TestStudioWorkflowColsStacksAt80AndSitsBesideAt120(t *testing.T) {
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
			screen, frame := workflowScreen(t, tc.width, tc.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			res := screen.withFrame(frame).workflowGridResult()
			assertStudioColsBreakpoint(t, res, sectionWorkflow, sectionWorkflowList, workflowZones, tc.width, tc.height, tc.want)
			if tc.want != screenlayout.Stacked {
				return
			}
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioPaintedLeaf(t, screen.withFrame(frame).workflowGridResult(), sectionWorkflowFields, tc.width, tc.height)
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioPaintedLeaf(t, screen.withFrame(frame).workflowGridResult(), sectionWorkflowList, tc.width, tc.height)
		})
	}
}

func TestStudioWorkflowTabWalksListThenInspector(t *testing.T) {
	t.Parallel()

	screen, frame := workflowScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.grid.Focus(); got != sectionWorkflowList {
		t.Fatalf("entry focus = %q, want %q", got, sectionWorkflowList)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionWorkflowFields {
		t.Fatalf("after tab focus = %q, want %q", got, sectionWorkflowFields)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionWorkflowList {
		t.Fatalf("after second tab focus = %q, want %q", got, sectionWorkflowList)
	}
}

func TestStudioWorkflowCtrlSOpensApplyFromCols(t *testing.T) {
	t.Parallel()

	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioWorkflow, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionWorkflowFields {
		t.Fatalf("tab did not land on inspector: %q", screen.grid.Focus())
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

func TestStudioWorkflowRowsMatchOmakaseSVG(t *testing.T) {
	t.Parallel()

	rows := studioWorkflowRows(studioWorkflowGoldenBundle().Workflows[0], nil, nil)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		line := strings.TrimSpace(row.Left)
		if row.Right != "" {
			line += "  " + row.Right
		}
		got = append(got, line)
	}

	wantContains := []string{
		"01 // BACKLOG · 0",
		"#self-branch",
		"blockers_in",
		"wave_gate",
		"02 // DEVELOPMENT · 0",
		"#resume",
		"#tests-passing",
		"subtasks_complete",
		"03 // REVIEW · 0",
		"#documentation",
		"open",
		"04 // DONE · 0 · final",
		"+ transition",
		"05 // archive",
	}
	joined := strings.Join(got, "\n")
	for _, want := range wantContains {
		if !strings.Contains(joined, want) {
			t.Fatalf("omakase list missing %q\n%s", want, joined)
		}
	}

	var tagged, opens, archives int
	for _, row := range rows {
		tagged, opens, archives = assertWorkflowRowShape(t, row, tagged, opens, archives)
	}
	if tagged < 4 {
		t.Fatalf("comments_tagged rows = %d, want at least self-branch/resume/tests-passing/documentation", tagged)
	}
	if opens == 0 {
		t.Fatal("no open transition rows")
	}
	if archives != 1 {
		t.Fatalf("archive rows = %d, want 1", archives)
	}
}

func assertWorkflowRowShape(t *testing.T, row workflowRow, tagged, opens, archives int) (int, int, int) {
	t.Helper()
	if row.Kind == workflowRowGuard && row.Guard.Type == "comments_tagged" {
		tagged++
		if row.Guard.Tag == "" {
			t.Fatal("comments_tagged row has empty tag")
		}
		if strings.Count(row.Left, "#") != 1 {
			t.Fatalf("comments_tagged row should be one tag, got %q", row.Left)
		}
	}
	if row.Kind == workflowRowOpen {
		opens++
	}
	if row.Kind == workflowRowOp && row.OpKind == StudioGuardSetArchive {
		archives++
	}
	if row.Kind == workflowRowAdd && !strings.Contains(row.Left, "+ transition") {
		t.Fatalf("add home label = %q", row.Left)
	}
	return tagged, opens, archives
}

func TestStudioWorkflowListMatchesOmakaseShape(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		size := size
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()
			screen, frame := workflowScreen(t, size.width, size.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			assertWorkflowListShape(t, screen.View(frame), size.width, size.height)
		})
	}
}

func assertWorkflowListShape(t *testing.T, rawView string, width, height int) {
	t.Helper()
	view := screentest.StripANSI(rawView)
	for _, want := range []string{"WORKFLOW", "▸ WORKFLOW", "4 buckets", "7 guards", "01 // BACKLOG"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workflow at %dx%d missing %q\n%s", width, height, want, view)
		}
	}
	if width < 120 {
		return
	}
	for _, want := range []string{"02 // DEVELOPMENT", "#resume", "blockers_in", "+ transition", "archive"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workflow at %dx%d missing %q\n%s", width, height, want, view)
		}
	}
}

func TestWorkflowTransitionGuardCountSkipsOperations(t *testing.T) {
	t.Parallel()
	bundle := studioWorkflowGoldenBundle()
	if len(bundle.Workflows) == 0 {
		t.Fatal("omakase overlay has no workflow")
	}
	workflow := bundle.Workflows[0]
	if got := workflowTransitionGuardCount(workflow); got != 7 {
		t.Fatalf("transition guards = %d, want 7 (archive operation guards stay off the header)", got)
	}
	if n := len(workflow.Operations.Archive.Guards); n == 0 {
		t.Fatal("fixture lost the archive operation guard this assertion is about")
	}
}

func TestStudioWorkflowInspectorFoci(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pred func(workflowRow) bool
		want []string
	}{
		{"bucket", func(row workflowRow) bool { return row.Kind == workflowRowBucket && row.Bucket.Key == "dev" }, []string{"02 // DEVELOPMENT", "key", "dev", "task edit"}},
		{"resume", func(row workflowRow) bool { return row.Kind == workflowRowGuard && row.Guard.Tag == "resume" }, []string{"#resume", "comments_tagged", "comment-resume", "**Before**"}},
		{"blockers", func(row workflowRow) bool { return row.Guard.Type == "blockers_in" }, []string{"blockers_in", "pending", "done", "Tiny"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen, frame := workflowScreen(t, 120, 40, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			for _, key := range workflowFocusKeys(tc.pred) {
				screen = studioDrive(t, screen, frame, key)
			}
			view := screentest.StripANSI(screen.View(frame))
			for _, want := range tc.want {
				if !strings.Contains(view, want) {
					t.Fatalf("focus %s missing %q\n%s", tc.name, want, view)
				}
			}
		})
	}
}

func TestStudioWorkflowTransitionHomeAndBucketEditsUseDraft(t *testing.T) {
	t.Parallel()

	screen, frame := workflowScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)

	// Named home: + transition is after the buckets. Walk until the add row.
	for i := 0; i < 40; i++ {
		rows := studioWorkflowRows(screen.studioWorkflowOnly(), screen.activeTaskCountsByBucket(), screen.t)
		if i >= len(rows) {
			t.Fatal("walked off the workflow list without finding + transition")
		}
		if rows[screen.studioWorkflowIndex].Kind == workflowRowAdd {
			break
		}
		screen = studioDrive(t, screen, frame, "down")
	}
	if got := studioWorkflowRows(screen.studioWorkflowOnly(), screen.activeTaskCountsByBucket(), screen.t)[screen.studioWorkflowIndex]; got.Kind != workflowRowAdd {
		t.Fatalf("named home kind = %d, want add", got.Kind)
	}
	screen = studioDrive(t, screen, frame, "a")
	if !screen.studioDraft.Report().Dirty {
		t.Fatal("a on + transition left the candidate clean")
	}
	if !strings.Contains(screen.studioWorkflowMsg, "transition added") {
		t.Fatalf("add message = %q", screen.studioWorkflowMsg)
	}

	screen, frame = workflowScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	before := screen.studioWorkflowOnly().Buckets[0].Key
	screen = studioDrive(t, screen, frame, "K")
	if screen.studioWorkflowOnly().Buckets[0].Key == before {
		t.Fatal("K did not change the bucket key through StudioDraft")
	}
	screen = studioDrive(t, screen, frame, "]")
	if !screen.studioDraft.Report().Dirty {
		t.Fatal("bucket key/order edits left the candidate clean")
	}
	screen = studioDrive(t, screen, frame, "e")
	if screen.studioWorkflowMsg == "" {
		t.Fatal("permission toggle left no message")
	}
}

func TestPaintWorkflowListRowUsesThemeTokens(t *testing.T) {
	t.Parallel()
	styles := screenkit.Styles{
		Warning: lipgloss.NewStyle().Bold(true),
		Success: lipgloss.NewStyle().Underline(true),
		Hint:    lipgloss.NewStyle().Italic(true),
	}
	tag := paintWorkflowListRow(styles, workflowRow{
		Kind:  workflowRowGuard,
		Left:  "#resume",
		Right: "-> review",
		Guard: config.TransitionGuard{Type: "comments_tagged", Tag: "resume"},
	})
	if got, want := tag.Left, styles.Warning.Render("#resume"); got != want {
		t.Fatalf("tagged left = %q, want Warning token %q", got, want)
	}
	if got, want := tag.Right, styles.Warning.Render("-> review"); got != want {
		t.Fatalf("tagged right = %q, want Warning token %q", got, want)
	}
	open := paintWorkflowListRow(styles, workflowRow{
		Kind: workflowRowOpen, Left: "open", Right: "-> dev",
	})
	if got, want := open.Left, styles.Success.Render("open"); got != want {
		t.Fatalf("open left = %q, want Success token %q", got, want)
	}
	muted := paintWorkflowListRow(styles, workflowRow{
		Kind:  workflowRowGuard,
		Left:  "wave_gate",
		Right: "-> dev",
		Guard: config.TransitionGuard{Type: "wave_gate"},
	})
	if got, want := muted.Left, styles.Hint.Render("wave_gate"); got != want {
		t.Fatalf("wave_gate left = %q, want Hint token %q", got, want)
	}
}
