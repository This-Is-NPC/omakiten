package tui

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
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

// TestEntityCellViewportNeverExceedsBudget locks the fix for the bug
// "tags grid extends past the terminal at the last item." When the user
// scrolled past the first row the renderer reserved a row for the
// "▼ below" hint but not for the "▲ above" hint that the same scroll
// triggers, so total rendered height was viewport+1 — one card row
// beyond the budget. The fix accounts for both hints; this test runs
// the renderer with a worst-case fixture (>1 grid row, scrolled
// mid-list and at last-row) and asserts the rendered grid never
// exceeds the viewport.
func TestEntityCellViewportNeverExceedsBudget(t *testing.T) {
	const viewport = 12

	// 30 tags = 10 rows of 3 cards each; each card renders 3 lines.
	tags := make([]domain.Tag, 30)
	for i := range tags {
		tags[i] = domain.Tag{ID: int64(i + 1), Name: fmt.Sprintf("tag-%02d", i), Label: fmt.Sprintf("tag-%02d", i), UsageCount: 1}
	}
	for _, scroll := range []int{0, 6, 18, 24, 27} {
		t.Run(fmt.Sprintf("scroll=%d", scroll), func(t *testing.T) {
			m := Model{
				styles:        newStyles(config.Theme{}),
				width:         200,
				height:        80,
				tags:          tags,
				entityKind:    entityKindTag,
				entityCursors: map[entityKind]int{entityKindTag: scroll},
			}
			// Drive the cardlist-owned scroll state via the same sync
			// routine production calls; the per-kind list lands with
			// a row-index scroll aligned to the supplied cursor.
			m.syncFocusedEntityScroll()
			rendered := m.renderEntityCellWithViewport(entityKindTag, viewport, 200)
			lines := strings.Count(rendered, "\n") + 1
			// kicker + separator add 2 lines on top of the viewport budget.
			const kickerSeparator = 2
			maxLines := viewport + kickerSeparator
			if lines > maxLines {
				t.Fatalf("entity cell rendered %d lines; viewport=%d max=%d (scroll=%d)\noutput:\n%s",
					lines, viewport, maxLines, scroll, rendered)
			}
		})
	}
}
