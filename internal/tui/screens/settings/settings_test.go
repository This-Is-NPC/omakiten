package settings

import (
	"errors"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestScreenContractAndIndependentScroll(t *testing.T) {
	// 16 rows no longer leaves a scrollable body at this width. The general
	// hint is 102 cells and now wraps to two rows instead of painting 102 cells
	// into a 100-column terminal and letting it cut the tail, so the body
	// budget is one row smaller — and 16 rows was exactly on the boundary where
	// ViewportRows stops handing out a window at all. The extra rows give the
	// scroll something to scroll, the same way #2420 gave the Studio scroll
	// test a terminal with a body rather than softening its assertion.
	frame := screentest.FrameAt(t, 100, 24)
	general := NewGeneral().Bind(testPayload(), nil)
	guards := NewGuards().Bind(testPayload(), nil)
	if general.ID() != screenhost.SettingsGeneral || guards.ID() != screenhost.SettingsGuards {
		t.Fatalf("ids = %q, %q", general.ID(), guards.ID())
	}
	if !general.OwnsFooter() || len(general.Footer(frame)) != 12 || len(general.Help(frame)) != 1 {
		t.Fatalf("general chrome contract incomplete: footer=%+v help=%+v", general.Footer(frame), general.Help(frame))
	}
	for _, key := range []string{"j", "pgdown", "g", "G", "r", "t", "c", "s", "e"} {
		if !general.OwnsKey(screentest.Key(key)) {
			t.Fatalf("screen does not own %q", key)
		}
	}
	general = general.Update(frame, screentest.Key("G")).Screen.(Screen)
	if general.Scroll() == 0 {
		t.Fatal("general did not scroll overflowing body")
	}
	if guards.Scroll() != 0 {
		t.Fatalf("guards inherited general scroll = %d", guards.Scroll())
	}
}

func TestScreenOutcomesAndLifecycle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 15)
	screen := NewGeneral().Bind(testPayload(), nil)
	wants := map[string]screenhost.ActionKind{
		"r": screenhost.ActionReload, "t": screenhost.ActionOpenThemePicker,
		"c": screenhost.ActionOpenConfigPicker, "s": screenhost.ActionOpenSubtaskKitPicker,
		"e": screenhost.ActionOpenConfigEditor,
	}
	for key, want := range wants {
		if got := screen.Update(frame, screentest.Key(key)).Action.Kind; got != want {
			t.Errorf("%s action = %v, want %v", key, got, want)
		}
	}
	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	wide := screentest.FrameAt(t, 160, 80)
	screen = screen.Lifecycle(wide, screenhost.LifecycleResize).Screen.(Screen)
	if screen.Scroll() != 0 {
		t.Fatalf("resize must clamp stale scroll, got %d", screen.Scroll())
	}
	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if screen.Scroll() != 0 {
		t.Fatalf("enter must reset scroll, got %d", screen.Scroll())
	}
}

func TestReloadProjectionEmptyAndError(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 80)
	screen := NewGeneral().Bind(testPayload(), nil)
	if got := screentest.StripANSI(screen.View(frame)); !strings.Contains(got, "v-test") || !strings.Contains(got, "omakase") {
		t.Fatalf("initial projection missing:\n%s", got)
	}
	payload := testPayload()
	payload.Runtime.Version = "v-reloaded"
	payload.ThemeKey = "reloaded-theme"
	screen = screen.Bind(payload, nil)
	if got := screentest.StripANSI(screen.View(frame)); !strings.Contains(got, "v-reloaded") || !strings.Contains(got, "reloaded-theme") {
		t.Fatalf("reloaded projection missing:\n%s", got)
	}
	if got := screentest.StripANSI(NewGeneral().View(frame)); !strings.Contains(got, "—") {
		t.Fatalf("empty projection must render placeholders:\n%s", got)
	}
	if got := screentest.StripANSI(screen.Bind(payload, errors.New("bundle failed")).View(frame)); !strings.Contains(got, "bundle failed") {
		t.Fatalf("error state missing:\n%s", got)
	}
}

func TestGeneralOrderingAndGuardModes(t *testing.T) {
	frame := screentest.FrameAt(t, 140, 80)
	payload := testPayload()
	general := screentest.StripANSI(NewGeneral().Bind(payload, nil).View(frame))
	if runtime, project, theme := strings.Index(general, "RUNTIME"), strings.Index(general, "PROJECT"), strings.Index(general, "THEME"); runtime >= project || project >= theme {
		t.Fatalf("effective config ordering drifted: runtime=%d project=%d theme=%d\n%s", runtime, project, theme, general)
	}
	guards := screentest.StripANSI(NewGuards().Bind(payload, nil).View(frame))
	if !strings.Contains(guards, "comments_tagged") || strings.Contains(guards, "SUBTASK KIT") {
		t.Fatalf("single guard matrix drifted:\n%s", guards)
	}
	sub := testBundle([]config.TransitionGuard{{Type: "comments_min", Count: 2}})
	root := testBundle([]config.TransitionGuard{{Type: "comments_tagged"}})
	root.SubtaskBundle = &sub
	payload.Snapshot = config.BuildSnapshot(root)
	payload.Workflow = payload.Snapshot.Workflow()
	guards = screentest.StripANSI(NewGuards().Bind(payload, nil).View(frame))
	if !strings.Contains(guards, "ROOT") || !strings.Contains(guards, "SUBTASK KIT") || !strings.Contains(guards, "comments_min") {
		t.Fatalf("dual guard matrix drifted:\n%s", guards)
	}
}
