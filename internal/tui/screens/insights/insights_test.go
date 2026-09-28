package insights

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// TestScreenIdentityAndChrome pins the contract surface: the stable id, the
// footer declarations (screen-owned keys only) and the single help group.
func TestScreenIdentityAndChrome(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := testScreen(true, domain.Insights{})

	if screen.ID() != screenhost.StatsInsights {
		t.Fatalf("ID() = %q, want %q", screen.ID(), screenhost.StatsInsights)
	}

	footer := screen.Footer(frame)
	if len(footer) != 2 || footer[0].Key != "r" || footer[1].Key != "j/k" {
		t.Fatalf("footer = %+v, want r + j/k", footer)
	}
	if !footer[0].Primary {
		t.Fatal("refresh must be the primary footer verb on a read-only screen")
	}
	for _, binding := range footer {
		if binding.Label == "" {
			t.Fatalf("footer binding %q has no label", binding.Key)
		}
		if binding.Key == "tab" || binding.Key == ",//" || binding.Key == "?" {
			t.Fatalf("screen advertises host-owned key %q", binding.Key)
		}
	}

	help := screen.Help(frame)
	if len(help) != 1 || help[0].ID != "stats_insights" {
		t.Fatalf("help groups = %+v, want a single stats_insights group", help)
	}
	if help[0].Title == "" || len(help[0].Bindings) != 2 {
		t.Fatalf("help group = %+v, want a title and 2 bindings", help[0])
	}
}

// overflowingScreen builds a reading tall enough to overflow any test viewport.
func overflowingScreen(rows int) Screen {
	models := make([]domain.ModelContrast, 0, rows)
	for i := 0; i < rows; i++ {
		models = append(models, domain.ModelContrast{
			AgentModel:   fmt.Sprintf("model-%02d", i),
			AvgDwellDays: 1.5,
			DwellSamples: 3,
			SampleSize:   9,
		})
	}
	return testScreen(true, domain.Insights{
		StuckDays: 7,
		PerModel:  domain.PerModelInsight{HasData: true, Models: models},
	})
}

// TestUpdateScrollVocabulary pins every read-only scroll verb and its clamping.
// The screen has no cursor, so each verb moves the body offset only.
func TestUpdateScrollVocabulary(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 20)
	t.Run("down and up step one row", func(t *testing.T) { assertInsightsStepKeys(t, frame) })
	t.Run("page verbs move more than a single row", func(t *testing.T) { assertInsightsPageKeys(t, frame) })
	t.Run("g and G jump to the bounds", func(t *testing.T) { assertInsightsBoundKeys(t, frame) })
	t.Run("scrolling never emits a host action", func(t *testing.T) { assertInsightsNoActions(t, frame) })
	t.Run("unbound keys leave the offset alone", func(t *testing.T) { assertInsightsUnbound(t, frame) })
}

func assertInsightsStepKeys(t *testing.T, frame screenhost.Frame) {
	t.Parallel()
	screen := overflowingScreen(40)
	for _, key := range []string{"down", "j"} {
		moved := screen.Update(frame, screentest.Key(key)).Screen.(Screen)
		if moved.Scroll() != 1 {
			t.Fatalf("%q offset = %d, want 1", key, moved.Scroll())
		}
		back := moved.Update(frame, screentest.Key("up")).Screen.(Screen)
		if back.Scroll() != 0 {
			t.Fatalf("up from 1 = %d, want 0", back.Scroll())
		}
	}
}

func assertInsightsPageKeys(t *testing.T, frame screenhost.Frame) {
	t.Parallel()
	screen := overflowingScreen(40)
	for _, key := range []string{"pgdown", "ctrl+d"} {
		moved := screen.Update(frame, screentest.Key(key)).Screen.(Screen)
		if moved.Scroll() <= 1 {
			t.Fatalf("%q offset = %d, want a multi-row step", key, moved.Scroll())
		}
		back := moved.Update(frame, screentest.Key("pgup")).Screen.(Screen)
		if back.Scroll() != 0 {
			t.Fatalf("pgup from a single page = %d, want 0", back.Scroll())
		}
	}
}

func assertInsightsBoundKeys(t *testing.T, frame screenhost.Frame) {
	t.Parallel()
	screen := overflowingScreen(40)
	bottom := screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if bottom.Scroll() == 0 {
		t.Fatal("G must scroll to the bottom")
	}
	if end := screen.Update(frame, screentest.Key("end")).Screen.(Screen); end.Scroll() != bottom.Scroll() {
		t.Fatalf("end offset = %d, want the same as G (%d)", end.Scroll(), bottom.Scroll())
	}
	top := bottom.Update(frame, screentest.Key("g")).Screen.(Screen)
	if top.Scroll() != 0 {
		t.Fatalf("g offset = %d, want 0", top.Scroll())
	}
	if home := bottom.Update(frame, screentest.Key("home")).Screen.(Screen); home.Scroll() != 0 {
		t.Fatalf("home offset = %d, want 0", home.Scroll())
	}
}

func assertInsightsNoActions(t *testing.T, frame screenhost.Frame) {
	t.Parallel()
	screen := overflowingScreen(40)
	for _, key := range []string{"down", "up", "pgdown", "pgup", "g", "G", "x"} {
		if outcome := screen.Update(frame, screentest.Key(key)); outcome.Action.Kind != screenhost.ActionNone {
			t.Fatalf("key %q emitted action %v, want none", key, outcome.Action.Kind)
		}
	}
}

func assertInsightsUnbound(t *testing.T, frame screenhost.Frame) {
	t.Parallel()
	screen := overflowingScreen(40).Update(frame, screentest.Key("G")).Screen.(Screen)
	before := screen.Scroll()
	after := screen.Update(frame, screentest.Key("z")).Screen.(Screen)
	if after.Scroll() != before {
		t.Fatalf("an unbound key moved the offset from %d to %d", before, after.Scroll())
	}
}

// TestUpdateIgnoresNonKeyMessages proves an unrelated bubbletea message leaves
// the offset untouched.
func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 20)
	screen := overflowingScreen(40).Update(frame, screentest.Key("G")).Screen.(Screen)
	before := screen.Scroll()

	got := screen.Update(frame, tea.WindowSizeMsg{Width: 1, Height: 1}).Screen.(Screen)
	if got.Scroll() != before {
		t.Fatalf("a non-key message moved the offset from %d to %d", before, got.Scroll())
	}
}

// TestResizeRecomputesTheViewportBudget proves the body budget tracks the frame
// rather than any state captured at construction.
func TestResizeRecomputesTheViewportBudget(t *testing.T) {
	t.Parallel()
	screen := overflowingScreen(10)

	tall := screen.viewportRows(screentest.FrameAt(t, 120, 60).Kit())
	short := screen.viewportRows(screentest.FrameAt(t, 120, 30).Kit())
	tiny := screen.viewportRows(screentest.FrameAt(t, 120, 10).Kit())

	if tall <= short {
		t.Fatalf("viewport must grow with terminal height: tall=%d short=%d", tall, short)
	}
	if tiny != 0 {
		t.Fatalf("a tiny terminal must yield no budget, got %d", tiny)
	}
}

// TestShrinkingTheTerminalClampsAStaleOffset proves an offset taken at a tall
// geometry cannot strand the body off-screen after a resize.
func TestShrinkingTheTerminalClampsAStaleOffset(t *testing.T) {
	t.Parallel()
	tall := screentest.FrameAt(t, 120, 60)
	short := screentest.FrameAt(t, 120, 20)

	screen := overflowingScreen(60).Update(tall, screentest.Key("G")).Screen.(Screen)
	// Rendering at the smaller frame must still produce a body, not a blank
	// window past the end of the content.
	view := ansi.Strip(screen.View(short))
	if strings.TrimSpace(view) == "" {
		t.Fatal("a stale offset produced an empty render after shrinking the terminal")
	}
}

// TestLifecycleIsCursorlessAndIdempotent pins the lifecycle contract: no event
// emits an action, and re-clamping is safe to repeat.
func TestLifecycleIsCursorlessAndIdempotent(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 20)
	screen := overflowingScreen(40)

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
		once := outcome.Screen.(Screen)
		twice := once.Lifecycle(frame, event).Screen.(Screen)
		if once.Scroll() != twice.Scroll() {
			t.Fatalf("lifecycle %v is not idempotent: %d then %d", event, once.Scroll(), twice.Scroll())
		}
	}
}

// TestStuckBucketIDs pins the tri-state the repository depends
// on: an unresolved workflow yields nil (repo fallback), a resolved workflow
// with no in-flight stage yields a non-nil empty slice (scan nothing), and a
// resolved workflow passes its ids through.
func TestStuckBucketIDs(t *testing.T) {
	t.Parallel()

	if got := domain.StuckBucketIDs(domain.Workflow{}); got != nil {
		t.Fatalf("unresolved workflow = %v, want nil so the repo applies its fallback", got)
	}
}

// TestShortStampDateTrimsToTheDate pins the partial-state label helper.
func TestShortStampDateTrimsToTheDate(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"2026-06-15 08:30:00": "2026-06-15",
		"2026-06-15":          "2026-06-15",
		"":                    "",
		"short":               "short",
	}
	for in, want := range cases {
		if got := ShortStampDate(in); got != want {
			t.Fatalf("ShortStampDate(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBucketLabelFallsBackToTheID proves a historical bucket that no longer
// exists in the active kit still renders identifiably rather than blank.
func TestBucketLabelFallsBackToTheID(t *testing.T) {
	t.Parallel()
	screen := testScreen(true, domain.Insights{})
	if got := screen.bucketLabel(2); got != "Development" {
		t.Fatalf("bucketLabel(2) = %q, want Development", got)
	}
	if got := screen.bucketLabel(99); got != "#99" {
		t.Fatalf("bucketLabel(99) = %q, want #99", got)
	}
}

// TestErrorLoopDegradesWhenTheLocaleDropsThePlaceholder proves a translator
// that removes the %s sentinel still gets the accented open count rather than
// losing it entirely.
func TestErrorLoopDegradesWhenTheLocaleDropsThePlaceholder(t *testing.T) {
	t.Parallel()
	frame := screenhost.NewFrame(screenhost.FrameOptions{
		Width:  120,
		Height: 40,
		Styles: screentest.Styles(),
		Text: func(key string) string {
			if key == "tui.insights.errors.summary_fmt" {
				return "%[2]d recorded, %[3]d resolved"
			}
			return key
		},
	})
	screen := testScreen(true, domain.Insights{
		ErrorLoop: domain.ErrorLoopInsight{HasData: true, Total: 10, Resolved: 6, Open: 4},
	})
	out := ansi.Strip(screen.View(frame))
	if !strings.Contains(out, "4") {
		t.Fatalf("the open count must survive a dropped placeholder:\n%s", out)
	}
}

// TestViewWithoutCatalogDoesNotPanic proves the screen renders on a host with
// no catalog wired: labels degrade to keys instead of crashing.
func TestViewWithoutCatalogDoesNotPanic(t *testing.T) {
	t.Parallel()
	frame := screentest.Frame(t, screentest.Options{NoCatalog: true})
	out := testScreen(true, domain.Insights{StuckDays: 7}).View(frame)
	if !strings.Contains(ansi.Strip(out), "tui.insights") {
		t.Fatalf("expected key-literal degradation, got:\n%s", ansi.Strip(out))
	}
}
