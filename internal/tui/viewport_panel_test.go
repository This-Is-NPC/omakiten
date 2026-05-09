package tui

import (
	"strings"
	"testing"
)

// TestPanelViewportRowsLeavesRoomForChrome verifies that the shared
// panelViewportRows budget plus the surrounding screen chrome and the
// requested panel chrome never exceeds the terminal height. This locks
// the contract for table, board, and entity grid: with a known terminal
// size H, calling panelViewportRows(c) returns at most H - screenChrome
// - statusLine - 1 (leading blank) - 2 (footer) - c rows of data.
//
// Without this contract, the prior hard-coded chromes silently
// undercounted and let columns / tables overflow below the footer —
// the user reported "rows lost in the invisible footer" and "columns
// bigger than the terminal".
func TestPanelViewportRowsLeavesRoomForChrome(t *testing.T) {
	for _, tc := range []struct {
		name           string
		terminalHeight int
		panelChrome    int
		statusMsg      string
	}{
		{"50-row terminal, table panel chrome 5", 50, 5, ""},
		{"50-row terminal, board panel chrome 4", 50, 4, ""},
		{"40-row terminal, entity panel chrome 4", 40, 4, ""},
		{"50-row terminal with status badge", 50, 5, "Editing task"},
		{"tiny 12-row terminal", 12, 5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{
				height: tc.terminalHeight,
				status: tc.statusMsg,
				styles: styles{},
			}
			rows := m.panelViewportRows(tc.panelChrome)
			if rows == 0 {
				return // tiny-terminal escape — caller falls back to clamp
			}
			screenHeader := strings.Count(m.renderHeader(), "\n") + 1
			statusLine := 0
			if tc.statusMsg != "" {
				statusLine = 2
			}
			chrome := screenHeader + statusLine + 1 /*leading blank*/ + tc.panelChrome + 2 /*footer*/
			total := chrome + rows
			if total > tc.terminalHeight {
				t.Fatalf("panelViewportRows(%d) on H=%d returned %d rows; total chrome+data=%d exceeds terminal",
					tc.panelChrome, tc.terminalHeight, rows, total)
			}
		})
	}
}
