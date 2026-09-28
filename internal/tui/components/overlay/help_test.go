package overlay_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/overlay"
)

func testStyles() overlay.Styles {
	return overlay.Styles{
		Info:      lipgloss.NewStyle().Bold(true),
		Hint:      lipgloss.NewStyle().Faint(true),
		Key:       lipgloss.NewStyle().Bold(true),
		Footer:    lipgloss.NewStyle().Faint(true),
		Separator: lipgloss.NewStyle(),
	}
}

func TestViewportRowsChargesMeasuredChrome(t *testing.T) {
	t.Parallel()
	// header 2 + footer 1 + leading blank 1 = 4, matching the old constant
	// on an overlay header — but now the inputs are measured.
	if got := overlay.ViewportRows(40, 2, 1); got != 36 {
		t.Fatalf("ViewportRows(40,2,1) = %d, want 36", got)
	}
	if got := overlay.ViewportRows(10, 2, 1); got != 0 {
		t.Fatalf("tiny terminal must refuse a viewport, got %d", got)
	}
	if got := overlay.ViewportRows(0, 2, 1); got != 0 {
		t.Fatalf("unmeasured height must refuse a viewport, got %d", got)
	}
}

func TestFooterHeightMatchesFooter(t *testing.T) {
	t.Parallel()
	styles := testStyles()
	text := "press ? to close"
	if got, want := overlay.FooterHeight(styles, text), lipgloss.Height(overlay.Footer(styles, text)); got != want {
		t.Fatalf("FooterHeight = %d, Footer paints %d", got, want)
	}
}

func TestRenderScrollsAndKeepsHint(t *testing.T) {
	t.Parallel()
	styles := testStyles()
	groups := []overlay.Group{{
		Title: "global",
		Bindings: []overlay.Binding{
			{Key: "?", Desc: "close"},
			{Key: "a", Desc: "toggle"},
			{Key: "q", Desc: "quit"},
			{Key: "r", Desc: "refresh"},
			{Key: "0", Desc: "home"},
		},
	}}
	opts := overlay.Options{
		Title: "help", ScopeHint: "a toggles scope", Groups: groups,
		Scroll: 0, Viewport: 6,
		FormatScrollHint: func(_, _ int) string {
			return lipgloss.NewStyle().Render("hint")
		},
	}
	out := overlay.Render(styles, opts)
	if !strings.Contains(out, "hint") {
		t.Fatalf("scrolled render must keep the hint row:\n%s", out)
	}
	full := overlay.Render(styles, overlay.Options{
		Title: "help", ScopeHint: "a toggles scope", Groups: groups,
	})
	if lipgloss.Height(out) >= lipgloss.Height(full) {
		t.Fatalf("scrolled body height %d should be shorter than full body %d",
			lipgloss.Height(out), lipgloss.Height(full))
	}
}
