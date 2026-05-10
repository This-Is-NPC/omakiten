package keyfooter

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderFormatsTokens(t *testing.T) {
	styles := Styles{Primary: lipgloss.NewStyle(), Secondary: lipgloss.NewStyle()}
	got := Render([]Token{{Key: "tab", Label: "details", Primary: true}, {Key: "esc", Label: "close"}}, styles)
	if got != "tab details  esc close" {
		t.Fatalf("Render() = %q", got)
	}
}

func TestRenderWrappedKeepsRowsWithinWidth(t *testing.T) {
	styles := Styles{Primary: lipgloss.NewStyle(), Secondary: lipgloss.NewStyle()}
	got := RenderWrapped([]Token{{Key: "tab", Label: "details", Primary: true}, {Key: "esc/q/enter/space", Label: "close"}}, styles, 23)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected wrapped footer, got %q", got)
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 23 {
			t.Fatalf("line width = %d, want <= 23: %q", w, line)
		}
	}
}

func TestRenderWrappedAlignsEachLine(t *testing.T) {
	styles := Styles{Primary: lipgloss.NewStyle(), Secondary: lipgloss.NewStyle(), Align: AlignRight}
	got := RenderWrapped([]Token{{Key: "tab", Label: "details"}, {Key: "esc", Label: "close"}}, styles, 14)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected wrapped footer, got %q", got)
	}
	if ansi.Strip(lines[0]) != "   tab details" {
		t.Fatalf("first line = %q, want right-aligned tab details", ansi.Strip(lines[0]))
	}
	if ansi.Strip(lines[1]) != "     esc close" {
		t.Fatalf("second line = %q, want right-aligned esc close", ansi.Strip(lines[1]))
	}
}

func TestRenderWrappedCenterAlignsSingleToken(t *testing.T) {
	styles := Styles{Primary: lipgloss.NewStyle(), Secondary: lipgloss.NewStyle(), Align: AlignCenter}
	got := RenderWrapped([]Token{{Key: "esc", Label: "close"}}, styles, 13)
	if ansi.Strip(got) != "  esc close  " {
		t.Fatalf("got %q, want centered token", ansi.Strip(got))
	}
}
