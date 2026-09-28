package taskdetail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screens/screentest"
)

func TestSubtasksAndActivitySectionsPaintClosedFramesAtCharacterizedWidths(t *testing.T) {
	cases := map[string]int{
		"80 columns":  80,
		"120 columns": 120,
		"200 columns": 200,
	}
	for name, terminalWidth := range cases {
		t.Run(name, func(t *testing.T) {
			frame := testFrame(terminalWidth, 40)
			screen := New().Open(taskDetailBenchPayload(8, 16), frame)
			width := frame.Kit().PanelContentWidth()
			for sectionName, section := range map[string]screenlayout.Section{
				"SUB-TASKS": screen.subtasksSection(frame),
				"ACTIVITY":  screen.activitySection(frame),
			} {
				t.Run(sectionName, func(t *testing.T) {
					block := section.Render(screenlayout.NewCanvas(width, 12, 0))
					assertSectionPaintsClosedFrame(t, terminalWidth, block)
				})
			}
		})
	}
}

func assertSectionPaintsClosedFrame(t *testing.T, terminalWidth int, block screenlayout.Block) {
	t.Helper()
	if len(block.Header) == 0 || !strings.Contains(block.Header[0], "┌") || !strings.Contains(block.Header[0], "┐") {
		t.Fatalf("top frame missing at %d columns: header=%q", terminalWidth, block.Header)
	}
	if len(block.Footer) == 0 || !strings.Contains(block.Footer[len(block.Footer)-1], "└") || !strings.Contains(block.Footer[len(block.Footer)-1], "┘") {
		t.Fatalf("bottom frame missing at %d columns: footer=%q", terminalWidth, block.Footer)
	}
}

func TestSubtaskBoardClosesItsLanesWithinAShortSectionBudget(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Open(taskDetailBenchPayload(8, 16), frame)
	const rows = 8
	block := screen.subtasksSection(frame).Render(screenlayout.NewCanvas(frame.Kit().PanelContentWidth(), rows, 0))

	painted := append(append([]string{}, block.Header...), block.Items...)
	painted = append(painted, block.Footer...)
	view := strings.Join(painted, "\n")
	if got := lipgloss.Height(view); got > rows {
		t.Fatalf("SUB-TASKS painted %d rows into a %d-row allocation:\n%s", got, rows, view)
	}
	if !strings.Contains(strings.Join(block.Items, "\n"), "└") {
		t.Fatalf("subtask lanes have no closing bottom border within the %d-row allocation:\n%s", rows, view)
	}
}
