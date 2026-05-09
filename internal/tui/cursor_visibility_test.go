package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"omakiten/internal/config"
)

// TestTaskDescriptionCursorRendersAsPrimaryBlock locks the fix for the
// bug "no caret on the description textarea". The visible-state branch
// of bubbles' cursor.View always applies Reverse(true); without an
// explicit Cursor.Style the output is `\x1b[7m`, which lipgloss's outer
// border wrap was emitting in a way the user couldn't see against the
// dark theme. Setting Cursor.Style.Foreground(primary) makes the
// reverse pass swap to a primary-colored Background so the cursor
// renders as a visible green block — proven by the SGR sequence
// `\x1b[7;38;2;<rgb>m` in the rendered string (reverse + truecolor fg
// in one combined escape).
func TestTaskDescriptionCursorRendersAsPrimaryBlock(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := Model{styles: newStyles(config.Theme{})}
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.taskField = taskFieldDescription
	m.taskDescriptionInput.Focus()

	rendered := m.renderTaskDescriptionField(80)

	// Lipgloss combines Reverse + Foreground into one SGR sequence
	// (`\x1b[7;38;2;...m`) — that's the visible-cursor cell.
	if !strings.Contains(rendered, "\x1b[7;38;2;") {
		t.Fatalf("description textarea: expected reverse + truecolor cursor SGR — cursor would be invisible.\nrendered:\n%s", rendered)
	}
}

// TestCommentEditScreenInnerHeightStaysWithinViewport asserts the
// dedicated comment edit overlay's textarea height is bounded by the
// available task-viewport — never spilling beyond what the surrounding
// chrome can fit. Prevents the "field renders the entire screen and
// gets bigger than the terminal" regression.
func TestCommentEditScreenInnerHeightStaysWithinViewport(t *testing.T) {
	cases := []struct {
		name           string
		terminalHeight int
		wantMax        int
	}{
		{"tall terminal — capped at preferredCap", 60, 16},
		{"medium terminal — half of viewport", 30, 12},
		{"short terminal — minHeight floor", 14, 8},
		{"unknown height — minHeight fallback", 0, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{height: tc.terminalHeight}
			h := m.commentEditScreenInnerHeight()
			if h > tc.wantMax {
				t.Fatalf("commentEditScreenInnerHeight() = %d, want <= %d (terminal height %d)", h, tc.wantMax, tc.terminalHeight)
			}
			if h < 8 {
				t.Fatalf("commentEditScreenInnerHeight() = %d, want >= 8 (minHeight floor)", h)
			}
		})
	}
}

// TestCommentInputCursorRendersAsPrimaryBlock mirrors the description
// test for the comment add/edit textarea; same root cause, same fix.
func TestCommentInputCursorRendersAsPrimaryBlock(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := Model{
		styles: newStyles(config.Theme{}),
		mode:   modeComment,
		width:  120,
		height: 40,
	}
	m.commentInput = newCommentInput()
	m.commentInput.Focus()

	rendered := m.renderCommentInput()

	if !strings.Contains(rendered, "\x1b[7;38;2;") {
		t.Fatalf("comment textarea: expected reverse + truecolor cursor SGR — cursor would be invisible.\nrendered:\n%s", rendered)
	}
}
