package screenbody

import (
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

func TestComposeStoresLinesAndSliceDoesNotRender(t *testing.T) {
	t.Parallel()
	kit, box := testKit(80, 24), screenlayout.Box{Width: 40, Rows: 12}
	calls := 0
	compose := func(box screenlayout.Box) []string {
		calls++
		return []string{"alpha-" + strconv.Itoa(box.Width), "beta"}
	}
	body := New(Spec("body"), nil).Compose(kit, box, compose)
	if body.Width() != 40 {
		t.Fatalf("width = %d, want 40", body.Width())
	}
	_ = body.View(kit, screenlayout.Box{Width: 20, Rows: 8})
	_ = body.sections()[0].Render(screenlayout.NewCanvas(20, 8, -1))
	if calls != 1 {
		t.Fatalf("slice recomposed: compose calls = %d", calls)
	}
}

func TestInvalidateDropsStoredLines(t *testing.T) {
	t.Parallel()
	kit, box := testKit(80, 24), screenlayout.Box{Width: 40, Rows: 12}
	body := New(Spec("body"), nil).Compose(kit, box, func(screenlayout.Box) []string { return []string{"keep"} })
	body = body.Invalidate()
	if body.Width() != 0 {
		t.Fatalf("invalidate left width=%d", body.Width())
	}
	block := body.sections()[0].Render(screenlayout.NewCanvas(40, 8, -1))
	if len(block.Items) != 0 {
		t.Fatalf("invalidate left items=%v", block.Items)
	}
}

func TestComposeOnIgnoresNonGeometryEvents(t *testing.T) {
	t.Parallel()
	kit, box := testKit(80, 24), screenlayout.Box{Width: 40, Rows: 12}
	calls := 0
	compose := func(screenlayout.Box) []string {
		calls++
		return []string{"x"}
	}
	body := New(Spec("body"), nil)
	for _, event := range []screenhost.LifecycleEvent{screenhost.LifecycleFocus, screenhost.LifecycleBlur, screenhost.LifecycleLeave} {
		body = body.ComposeOn(event, kit, box, compose)
	}
	if calls != 0 {
		t.Fatalf("non-geometry events composed %d times", calls)
	}
	body = body.ComposeOn(screenhost.LifecycleEnter, kit, box, compose)
	body = body.ComposeOn(screenhost.LifecycleResize, kit, box, compose)
	if calls != 2 {
		t.Fatalf("enter/resize composed %d times, want 2", calls)
	}
	if got := body.View(kit, box); got == "" {
		t.Fatal("geometry events left an empty view")
	}
}

func TestSplitKeepsHeaderOutOfTheItemWindow(t *testing.T) {
	t.Parallel()
	kit, box := testKit(80, 24), screenlayout.Box{Width: 40, Rows: 12}
	split := func(lines []string) (header, items []string) {
		return lines[:1], lines[1:]
	}
	body := New(Spec("body"), split).Compose(kit, box, func(screenlayout.Box) []string {
		return []string{"kicker", "row-a", "row-b"}
	})
	block := body.sections()[0].Render(screenlayout.NewCanvas(40, 8, -1))
	if len(block.Header) != 1 || block.Header[0] != "kicker" {
		t.Fatalf("header = %v", block.Header)
	}
	if len(block.Items) != 2 || block.Items[0] != "row-a" {
		t.Fatalf("items = %v", block.Items)
	}
}
func TestWithCursorCarriesSelectionThroughResync(t *testing.T) {
	t.Parallel()
	kit, box := testKit(80, 24), screenlayout.Box{Width: 40, Rows: 12}
	body := New(Spec("body"), nil).Compose(kit, box, func(screenlayout.Box) []string {
		return []string{"zero", "one", "two", "three", "four"}
	})
	body = body.WithCursor("body", 3).Resync(kit, box)
	placement, ok := body.Arrange(kit, box).Placement("body")
	if !ok {
		t.Fatal("body placement missing")
	}
	if placement.Cursor != 3 {
		t.Fatalf("cursor = %d, want the selected item 3", placement.Cursor)
	}
}

func TestAfterApplyIssuesResize(t *testing.T) {
	t.Parallel()
	frame := screenhost.NewFrame(screenhost.FrameOptions{Width: 80, Height: 24})
	got := AfterApply(recordScreen{}, frame).(recordScreen)
	if len(got.events) != 1 || got.events[0] != screenhost.LifecycleResize {
		t.Fatalf("events = %v, want [Resize]", got.events)
	}
}

type recordScreen struct{ events []screenhost.LifecycleEvent }

func (s recordScreen) ID() screenhost.ID { return "record" }
func (s recordScreen) Update(screenhost.Frame, tea.Msg) screenhost.Outcome {
	return screenhost.Stay(s, nil)
}
func (s recordScreen) View(screenhost.Frame) string { return "" }
func (s recordScreen) Footer(screenhost.Frame) []screenhost.FooterBinding {
	return nil
}
func (s recordScreen) Help(screenhost.Frame) []screenhost.HelpGroup { return nil }
func (s recordScreen) Lifecycle(_ screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	s.events = append(s.events, event)
	return screenhost.Stay(s, nil)
}

func testKit(width, height int) screenkit.Kit {
	return screenkit.Kit{Width: width, Height: height, ChromeRows: 3}
}
