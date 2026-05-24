package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/layout"
)

// TestKanbanColumnSizedTotalRowsMatchBudget pins the contract the
// W14 cascade relies on: when renderSubtasksPanel asks for a box of
// N total rows, lipgloss must produce exactly N rows on screen
// (borders included). lipgloss `Style.Height(n)` treats n as the
// INNER content rows — borders stack outside — so the caller has
// to subtract layout.PanelBorders before handing the budget off.
//
// The +2 drift this test guards against is the bug that took down
// W13: callers passed total rows directly to `.Height(n)` and every
// panel rendered 2 rows taller than the budget, overflowing the
// outer slice.
func TestKanbanColumnSizedTotalRowsMatchBudget(t *testing.T) {
	cases := []int{8, 12, 20, 35}
	for _, totalRows := range cases {
		inner := totalRows - layout.PanelBorders
		style := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			Width(30).
			Height(inner)
		// Body deliberately shorter than the budget so padding
		// kicks in — the bug we are pinning is that lipgloss extends
		// to (inner + borders) total, never more.
		body := "hello"
		rendered := style.Render(body)
		got := lipgloss.Height(rendered)
		if got != totalRows {
			t.Fatalf("kanbanColumnSized.Height(%d) total rows = %d, want %d (caller asked for %d total)", inner, got, totalRows, totalRows)
		}
	}
}

// TestRenderFixedBoxTotalRowsMatchInput pins the same contract for
// the manually-bordered fixed box (used by the activity rail).
// renderFixedBox emits one top border row, one row per content
// line, and one bottom border row → total = len(lines) + 2.
func TestRenderFixedBoxTotalRowsMatchInput(t *testing.T) {
	cases := []int{0, 1, 5, 20}
	for _, bodyLines := range cases {
		lines := make([]string, bodyLines)
		for i := range lines {
			lines[i] = ""
		}
		border := lipgloss.NewStyle()
		rendered := renderFixedBox(lines, 30, border)
		got := strings.Count(rendered, "\n") + 1
		want := bodyLines + 2
		if got != want {
			t.Fatalf("renderFixedBox(%d body lines) total rows = %d, want %d", bodyLines, got, want)
		}
	}
}
