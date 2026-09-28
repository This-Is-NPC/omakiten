package lane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/screenkit"
)

func lanePlain() screenkit.Styles {
	return screenkit.Styles{
		Separator: lipgloss.NewStyle(),
		Hint:      lipgloss.NewStyle(),
		Empty:     lipgloss.NewStyle().Width(20).Align(lipgloss.Center),
	}
}

func TestEmptyLineUsesEmptyNotHint(t *testing.T) {
	styles := lanePlain()
	styles.Empty = lipgloss.NewStyle().Width(20).Align(lipgloss.Center).Foreground(lipgloss.Color("#111111"))
	styles.Hint = lipgloss.NewStyle().Foreground(lipgloss.Color("#222222"))
	kit := screenkit.Kit{Styles: styles}
	got := Render(kit, Spec{Header: "// BACKLOG · 0", Inner: 20, EmptyText: "empty", Cards: list.NewCards()})
	if !strings.Contains(got, "empty") {
		t.Fatalf("Render dropped the empty text: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if !strings.Contains(line, "empty") {
			continue
		}
		// Hint would left-align inside the box (`│empty`). Empty centres.
		if strings.Contains(line, "│empty") {
			t.Fatalf("empty line painted flush-left; Styles.Empty should centre: %q", line)
		}
		return
	}
}

func TestRenderDrawsHeaderRuleAndEmptyBody(t *testing.T) {
	kit := screenkit.Kit{Styles: lanePlain()}
	out := Render(kit, Spec{Header: "// BACKLOG · 0", Inner: 20, EmptyText: "(empty)", Cards: list.NewCards()})
	if !strings.Contains(out, "BACKLOG") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "(empty)") {
		t.Fatalf("missing empty line: %q", out)
	}
	if lipgloss.Height(out) < 3 {
		t.Fatalf("column shorter than header+rule+empty: height %d", lipgloss.Height(out))
	}
}
