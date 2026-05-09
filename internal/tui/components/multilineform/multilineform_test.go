package multilineform

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// testTheme returns a minimal Theme with explicit truecolor so the
// rendered SGR sequences are deterministic across environments.
func testTheme() Theme {
	return Theme{
		Border:       lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#444444")).Padding(0, 2),
		BorderActive: lipgloss.Color("#00FF88"),
		Cursor:       lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF88")),
	}
}

// TestRenderPreservesContentAfterResize asserts the leaf component's
// core contract: a textarea sized via Resize at one width keeps its
// content visible after Render at the same width. Locks the regression
// behind the package's existence — the prior inline render path on
// task description forgot the Resize step and the field went blank
// after the first Update.
func TestRenderPreservesContentAfterResize(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	theme := testTheme()
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	const (
		outerWidth = 40
		height     = 4
	)
	Resize(&input, outerWidth, height, theme)
	input.SetValue("hello world")
	input.Focus()

	rendered := Render(input, outerWidth, height, true, theme)
	if !strings.Contains(rendered, "hello world") {
		t.Fatalf("Render() output missing content; got:\n%s", rendered)
	}
}

// TestRenderFocusSwapsBorderColor proves the focused branch swaps
// BorderForeground to theme.BorderActive while the unfocused branch
// keeps the base style's border color. This is the only visual cue
// distinguishing the active textarea from sibling form fields.
func TestRenderFocusSwapsBorderColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	theme := testTheme()
	input := textarea.New()
	input.Prompt = ""
	input.SetValue("x")
	Resize(&input, 30, 3, theme)

	focused := Render(input, 30, 3, true, theme)
	blurred := Render(input, 30, 3, false, theme)

	const activeRGB = "\x1b[38;2;0;255;136m"
	const baseRGB = "\x1b[38;2;68;68;68m"
	if !strings.Contains(focused, activeRGB) {
		t.Fatalf("focused Render() missing active border SGR %q; got:\n%s", activeRGB, focused)
	}
	if !strings.Contains(blurred, baseRGB) {
		t.Fatalf("blurred Render() missing base border SGR %q; got:\n%s", baseRGB, blurred)
	}
}

// TestResizeSyncsPersistentModel verifies Resize mutates the
// caller's textarea (pointer receiver) so subsequent Update(msg)
// calls operate on the new wrap width. This is the bug fix from the
// task description: previously only the render-time copy got SetWidth.
//
// Width()/Height() in bubbles textarea v1.0.0 return the inner content
// dimensions after subtracting the prompt width and (when enabled) the
// line-number gutter. The test mirrors the production constructor by
// blanking the prompt and disabling line numbers so the geometry math
// reduces to outer-minus-padding without any extra reserved chars.
func TestResizeSyncsPersistentModel(t *testing.T) {
	theme := testTheme()
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	const (
		outerWidth = 50
		height     = 5
	)
	Resize(&input, outerWidth, height, theme)

	wantInner := outerWidth - theme.Border.GetHorizontalPadding()
	if got := input.Width(); got != wantInner {
		t.Fatalf("input.Width() = %d, want %d (outer %d minus padding %d)", got, wantInner, outerWidth, theme.Border.GetHorizontalPadding())
	}
	if got := input.Height(); got != height {
		t.Fatalf("input.Height() = %d, want %d", got, height)
	}
}

// TestInnerWidthFloor guards against tiny terminals collapsing the
// content column past the usable minimum. Floor matches the prior
// inline `if innerWidth < 8` guard scattered across the render sites.
func TestInnerWidthFloor(t *testing.T) {
	theme := testTheme()
	if got := innerWidth(0, theme); got != minInnerWidth {
		t.Fatalf("innerWidth(0) = %d, want %d (floor)", got, minInnerWidth)
	}
	if got := innerWidth(2, theme); got != minInnerWidth {
		t.Fatalf("innerWidth(2) = %d, want %d (floor)", got, minInnerWidth)
	}
}
