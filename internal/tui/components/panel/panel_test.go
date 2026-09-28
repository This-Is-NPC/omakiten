package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFixedBoxHeightMatchesWhatFixedBoxDraws(t *testing.T) {
	border := lipgloss.NewStyle()
	for _, n := range []int{0, 1, 5, 20} {
		lines := make([]string, n)
		got := strings.Count(FixedBox(lines, 30, border), "\n") + 1
		if want := FixedBoxHeight(n); got != want {
			t.Fatalf("FixedBox(%d lines) is %d rows, FixedBoxHeight says %d", n, got, want)
		}
	}
}

func TestHRuleGuardsNonPositiveWidth(t *testing.T) {
	sep := lipgloss.NewStyle()
	if got := HRule(sep, 0); got != "" {
		t.Fatalf("HRule(0) = %q, want empty", got)
	}
	if got := HRule(sep, -3); got != "" {
		t.Fatalf("HRule(-3) = %q, want empty", got)
	}
	if got := lipgloss.Width(HRule(sep, 17)); got != 17 {
		t.Fatalf("HRule(17) width = %d, want 17", got)
	}
}

func TestChevronOnlyWhenSelected(t *testing.T) {
	accent := lipgloss.NewStyle()
	if got := Chevron(accent, false); got != "" {
		t.Fatalf("unselected = %q, want empty", got)
	}
	if got := Chevron(accent, true); got != "› " {
		t.Fatalf("selected = %q, want › + space", got)
	}
}

func TestJoinMeetsTheSides(t *testing.T) {
	border := lipgloss.NewStyle()
	got := Join(border, 8)
	if !strings.HasPrefix(got, "├") || !strings.HasSuffix(got, "┤") {
		t.Fatalf("Join was not ├─┤: %q", got)
	}
	if strings.Contains(got, "│") {
		t.Fatalf("Join was wrapped as │────│: %q", got)
	}
	if lipgloss.Width(got) != 10 {
		t.Fatalf("Join width = %d, want 10", lipgloss.Width(got))
	}
}

func TestWrapLineKeepsSGR(t *testing.T) {
	border := lipgloss.NewStyle()
	kicker := "\x1b[38;2;57;255;20m▸ BACKLOG\x1b[0m"
	got := WrapLine(border, 16)(kicker)
	if !strings.Contains(got, "\x1b[38;2;57;255;20m") {
		t.Fatalf("WrapLine sanitized focused kicker SGR: %q", got)
	}
	if !strings.HasPrefix(got, "│") || !strings.HasSuffix(got, "│") {
		t.Fatalf("WrapLine dropped the sides: %q", got)
	}
}

func TestFramePinsKickerBetweenTopAndJoin(t *testing.T) {
	header, footer, wrap := Frame(lipgloss.NewStyle(), 12, "▸ TASKS")
	if len(header) != 3 {
		t.Fatalf("header = %#v, want top + kicker + join", header)
	}
	if !strings.HasPrefix(header[0], "┌") || !strings.HasSuffix(header[0], "┐") {
		t.Fatalf("top = %q", header[0])
	}
	if !strings.Contains(header[1], "TASKS") {
		t.Fatalf("kicker missing: %#v", header)
	}
	if !strings.HasPrefix(header[2], "├") || !strings.HasSuffix(header[2], "┤") {
		t.Fatalf("join = %q", header[2])
	}
	if len(footer) != 1 || !strings.HasPrefix(footer[0], "└") {
		t.Fatalf("footer = %#v", footer)
	}
	row := wrap("▼ 4 below")
	if !strings.Contains(row, "▼ 4 below") || !strings.HasPrefix(row, "│") {
		t.Fatalf("wrap = %q", row)
	}
}
