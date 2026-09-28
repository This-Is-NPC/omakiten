package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screens/commentdetail"
)

// TestCommentEditScreenInnerHeightStaysWithinViewport asserts the comment
// edit textarea height is the HostBox remainder after header chrome (#2444) —
// never taller than the body budget the arranger assigned.
func TestCommentEditScreenInnerHeightStaysWithinViewport(t *testing.T) {
	cases := []struct {
		name           string
		terminalHeight int
	}{
		{"tall terminal", 60},
		{"medium terminal", 30},
		{"short terminal", 14},
		{"unknown height", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{height: tc.terminalHeight, styles: newStyles(config.Theme{})}
			frame := m.screenFrame()
			screen := commentdetail.New().Open(commentdetail.Payload{Comment: domain.Comment{ID: 1}})
			h := screen.EditHeight(frame)
			hostRows := screenlayout.HostBox(frame.Kit()).Rows
			header := 3 // kicker + hint + blank; Open leaves err nil
			want := hostRows - header
			if want < 1 {
				want = 1
			}
			if h != want {
				t.Fatalf("EditHeight() = %d, want %d (HostBox=%d minus header=%d at height %d)", h, want, hostRows, header, tc.terminalHeight)
			}
			if h > hostRows {
				t.Fatalf("EditHeight() = %d exceeds HostBox %d", h, hostRows)
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
