package studio

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestStudioCommandsColsStacksAt80AndSitsBesideAt120(t *testing.T) {
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
			screen, frame := commandsScreen(t, tc.width, tc.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			res := screen.withFrame(frame).commandsGridResult()
			assertStudioColsBreakpoint(t, res, sectionCommands, sectionCommandsList, commandsZones, tc.width, tc.height, tc.want)
			if tc.want != screenlayout.Stacked {
				return
			}
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioListIsOffScreen(t, screen.withFrame(frame).commandsGridResult(), sectionCommandsList, sectionCommandsFields, tc.width, tc.height)
			screen = studioDrive(t, screen, frame, "tab")
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioPaintedLeaf(t, screen.withFrame(frame).commandsGridResult(), sectionCommandsList, tc.width, tc.height)
		})
	}
}

// At 80×24 the body cannot pay the list AND the inspector, so whichever side
// holds the focus is the side on screen. Inside the inspector both zones fit —
// they declare five rows each and the body has thirteen — so tabbing into it
// brings both, which is the same rule one level down and not an exception to it.
func TestStudioCommandsTabAt80SwapsListForInspector(t *testing.T) {
	t.Parallel()

	screen, frame := commandsScreen(t, 80, 24, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	assertStudioPaintedLeaf(t, screen.withFrame(frame).commandsGridResult(), sectionCommandsList, 80, 24)
	view := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "▸ COMMANDS") {
		t.Fatalf("list fullscreen missing COMMANDS kicker\n%s", view)
	}
	if strings.Contains(view, "PREVIEW") {
		t.Fatalf("list fullscreen still paints PREVIEW\n%s", view)
	}

	screen = studioDrive(t, screen, frame, "tab")
	assertStudioListIsOffScreen(t, screen.withFrame(frame).commandsGridResult(), sectionCommandsList, sectionCommandsFields, 80, 24)
	view = screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "persona") {
		t.Fatalf("tab did not bring the inspector on screen\n%s", view)
	}
	if strings.Contains(view, "▸ COMMANDS") {
		t.Fatalf("the list is off screen but kept its focus kicker\n%s", view)
	}

	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	assertStudioPaintedLeaf(t, screen.withFrame(frame).commandsGridResult(), sectionCommandsList, 80, 24)
}

func TestStudioCommandsTabWalksListThenPreview(t *testing.T) {
	t.Parallel()

	screen, frame := commandsScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.grid.Focus(); got != sectionCommandsList {
		t.Fatalf("entry focus = %q, want %q", got, sectionCommandsList)
	}
	// One tab per painted zone, in reading order: metadata, then PREVIEW. The
	// metadata table used to be skipped because the inspector was one leaf.
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsFields {
		t.Fatalf("after tab focus = %q, want the metadata zone %q", got, sectionCommandsFields)
	}
	view := screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "▸ OKT") {
		t.Fatalf("the metadata zone holds the focus and does not say so\n%s", view)
	}
	if strings.Contains(view, "▸ PREVIEW") {
		t.Fatalf("the focus is on the metadata zone but PREVIEW is accented\n%s", view)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsPreview {
		t.Fatalf("after the second tab focus = %q, want preview leaf %q", got, sectionCommandsPreview)
	}
	view = screentest.StripANSI(screen.View(frame))
	if !strings.Contains(view, "▸ PREVIEW") {
		t.Fatalf("tab did not land on PREVIEW\n%s", view)
	}
	assertNoCommandFieldCursor(t, view)
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsList {
		t.Fatalf("after wrapping tab focus = %q, want %q", got, sectionCommandsList)
	}
	// Three zones, so three tabs come back round.
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsList {
		t.Fatalf("after wrapping tab focus = %q, want %q", got, sectionCommandsList)
	}
}

func TestStudioCommandsCtrlSOpensApplyFromCols(t *testing.T) {
	t.Parallel()

	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioCommands, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionCommandsFields {
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

func TestStudioCommandsGoldenStatePinsMetadataAndPreview(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		size := size
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()
			screen, frame := commandsScreen(t, size.width, size.height, State{CommandIndex: 4})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			screen = studioDrive(t, screen, frame, "j")
			screen = studioDrive(t, screen, frame, "k")
			view := screentest.StripANSI(screen.View(frame))
			want := []string{
				"▸ COMMANDS",
				"› 05 // okt-sample-04",
				"okt-sample-04",
			}
			if size.width >= 120 {
				want = append(want, "persona", "skills", "laws", "// PREVIEW")
			}
			if strings.Contains(view, "STUDIO COMMANDS") || strings.Contains(view, "PROMPT PREVIEW:") || strings.Contains(view, "COMMAND BINDINGS") {
				t.Fatalf("Cols at %dx%d still paints the old dump chrome\n%s", size.width, size.height, view)
			}
			for _, token := range want {
				if !strings.Contains(view, token) {
					t.Fatalf("Cols at %dx%d missing %q\n%s", size.width, size.height, token, view)
				}
			}
			assertNoCommandFieldCursor(t, view)
		})
	}
}
