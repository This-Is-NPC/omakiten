package description

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestDescriptionOwnsLifecycleMarkdownScrollAndStates(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 16)
	screen := New().Open(domain.Task{ID: 42, Title: "Extract readers", BucketKey: "dev", Description: strings.Repeat("line\n", 80)})
	if screen.ID() != screenhost.TaskDescription {
		t.Fatalf("ID() = %q", screen.ID())
	}
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screentest.StripANSI(screen.View(frame)); !strings.Contains(got, "Extract readers") {
		t.Fatalf("view missing task title:\n%s", got)
	}
	screen = screen.Update(frame, screentest.Key("M")).Screen.(Screen)
	_ = screen.View(frame)
	if screen.rendered {
		t.Fatal("M did not switch the owned markdown mode to plain")
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.Scroll() == 0 {
		t.Fatal("j did not advance owned scroll")
	}
	screen = screen.Lifecycle(screentest.FrameAt(t, 55, 12), screenhost.LifecycleResize).Screen.(Screen)
	if screen.Width() != 55 || screen.Height() != 12 {
		t.Fatalf("resize = %dx%d", screen.Width(), screen.Height())
	}
	if got := screen.Update(frame, screentest.Key("f")).Action.Kind; got != screenhost.ActionBack {
		t.Fatalf("f action = %v", got)
	}
	if got := screen.Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionBack {
		t.Fatalf("esc action = %v", got)
	}
	if got := screen.Update(frame, screentest.Key("q")).Action.Kind; got != screenhost.ActionQuit {
		t.Fatalf("q action = %v", got)
	}

	if got := screentest.StripANSI(New().Loading().View(frame)); !strings.Contains(got, "Loading") {
		t.Fatalf("loading view = %q", got)
	}
	if got := screentest.StripANSI(New().Apply(domain.Task{}, errors.New("load failed")).View(frame)); !strings.Contains(got, "load failed") {
		t.Fatalf("error view = %q", got)
	}
}

func TestDescriptionDeclaresChromeAndHandlesEmptyLifecycle(t *testing.T) {
	// Wide enough that the detail-grid border does not wrap; wrapping would
	// clip the empty-body hint.
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Open(domain.Task{ID: 9, Title: "No body"})
	if screen.Task().ID != 9 || !screen.OwnsKey(screentest.Key("f")) || !screen.OwnsKey(screentest.Key("x")) || !screen.OwnsFooter() || !screen.BlocksHostInput() {
		t.Fatal("description did not expose its owned route capabilities")
	}
	if len(screen.Footer(frame)) == 0 || len(screen.Help(frame)) == 0 {
		t.Fatal("description footer/help declarations are empty")
	}
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = screen.Update(frame, struct{}{}).Screen.(Screen)
	if got := screentest.StripANSI(screen.View(frame)); !strings.Contains(got, "No description") {
		t.Fatalf("empty view = %q", got)
	}
	if got := screentest.StripANSI(New().View(frame)); !strings.Contains(got, "not found") {
		t.Fatalf("not-found view = %q", got)
	}
}

// C.2 deleted the private Viewport.Fit window. j/k must still move one composed
// line, not a page and not two rows — the arranger windows the items, it does
// not change what a line is.
func TestDescriptionScrollsOneLineAtATime(t *testing.T) {
	frame, screen := longDescription(t)
	if screen.Scroll() != 0 {
		t.Fatalf("enter scroll = %d", screen.Scroll())
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.Scroll() != 1 {
		t.Fatalf("j scroll = %d, want 1", screen.Scroll())
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.Scroll() != 2 {
		t.Fatalf("jj scroll = %d, want 2", screen.Scroll())
	}
	screen = screen.Update(frame, screentest.Key("k")).Screen.(Screen)
	if screen.Scroll() != 1 {
		t.Fatalf("k scroll = %d, want 1", screen.Scroll())
	}
}

// G must reach the end of the composed items: nothing left below, and j
// cannot advance the offset. A golden that shows the last lines only proves
// where the window parked, not that the key travelled the remaining distance.
func TestDescriptionGReachesTheEnd(t *testing.T) {
	frame, screen := longDescription(t)
	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if screen.Scroll() == 0 {
		t.Fatal("G left the offset at 0")
	}
	view := screentest.StripANSI(screen.View(frame))
	if strings.Contains(view, "below") {
		t.Fatalf("G left content below the window:\n%s", view)
	}
	if !strings.Contains(view, "above") {
		t.Fatalf("G did not park at a windowed bottom (no above hint):\n%s", view)
	}
	stuck := screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if stuck.Scroll() != screen.Scroll() {
		t.Fatalf("G was not at max offset: G=%d j=%d", screen.Scroll(), stuck.Scroll())
	}
}

// pgdown is the arranger's pageItems, not the Viewport half-page this screen
// used to own. The three assertions are the contract in pageItems' comment
// (#2425): a page must not skip the items the overflow hints displaced, pgup
// must undo pgdown, and a page is strictly larger than a line. None of them
// pins a width, so a later geometry change cannot make this pass by accident.
func TestDescriptionPgdnFollowsArrangerPageContract(t *testing.T) {
	frame, screen := longDescription(t)
	before, ok := screen.gridResult(frame).Placement(bodySpec.ID)
	if !ok {
		t.Fatal("body section was not placed")
	}
	origin := screen.Scroll()

	screen = screen.Update(frame, screentest.Key("pgdown")).Screen.(Screen)
	after, ok := screen.gridResult(frame).Placement(bodySpec.ID)
	if !ok {
		t.Fatal("body section was not placed after pgdown")
	}
	if after.First > before.Last {
		t.Fatalf("pgdown skipped items: last visible was %d, first visible is %d", before.Last, after.First)
	}
	if screen.Scroll() <= origin+1 {
		t.Fatalf("pgdown scroll = %d, want strictly more than one j from %d", screen.Scroll(), origin)
	}

	back := screen.Update(frame, screentest.Key("pgup")).Screen.(Screen)
	if back.Scroll() != origin {
		t.Fatalf("pgdown then pgup scroll = %d, want origin %d", back.Scroll(), origin)
	}
}

func longDescription(t *testing.T) (screenhost.Frame, Screen) {
	t.Helper()
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Open(domain.Task{
		ID: 1, Title: "Scroll probe", BucketKey: "dev",
		Description: strings.Repeat("line\n", 400),
	})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	return frame, screen
}

// TestDescriptionFailedStateRendersViaScreenstate falsifies the blank-screen
// hazard (#126545): if View's liveness guard omits err while compose skips on
// it, the screen paints nothing when Failed is live.
func TestDescriptionFailedStateRendersViaScreenstate(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 16)
	screen := New().Apply(domain.Task{ID: 42, Title: "Broken"}, errors.New("load \x1b[31mfailed\x1b[0m\nretry"))
	view := screentest.StripANSI(screen.View(frame))
	if view == "" {
		t.Fatal("failed view is blank")
	}
	if !strings.Contains(view, "[ERROR]") {
		t.Fatalf("failed view missing error badge:\n%s", view)
	}
	if !strings.Contains(view, "load failed retry") {
		t.Fatalf("failed view missing sanitized, normalized error detail:\n%s", view)
	}
}

func TestDescriptionFailedStateFinalRenderBoundsZeroWidthDetail(t *testing.T) {
	const maxFailureDetailCodePoints = 960
	const safePrefix = "failure-id=zero-width"
	const discardedSuffix = "X"
	hostile := safePrefix + strings.Repeat("\u0301", maxFailureDetailCodePoints-utf8.RuneCountInString(safePrefix)) + discardedSuffix
	frame := screentest.FrameAt(t, 80, 16)
	screen := New().Apply(domain.Task{ID: 42, Title: "Broken"}, errors.New(hostile))
	view := screentest.StripANSI(screen.View(frame))

	if !utf8.ValidString(view) {
		t.Fatal("failed view contains invalid UTF-8")
	}
	if !strings.Contains(view, safePrefix) || !strings.Contains(view, "…") {
		t.Fatalf("failed view lost its useful prefix or truncation marker:\n%s", view)
	}
	if strings.Contains(view, discardedSuffix) {
		t.Fatalf("failed view retained content beyond the secondary ceiling:\n%s", view)
	}
}
