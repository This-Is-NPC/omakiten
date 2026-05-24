package tui

import (
	"testing"
)

// TestPlanNetworkScrollIsCardIndexNotLineOffset pins the W11-B-3
// migration: the planNetwork cardlist.Model owns the scroll field
// internally so callers cannot write the wrong unit. Builds a
// synthetic row list whose total height exceeds the viewport, walks
// the cursor through every position, and asserts the cardlist's
// Scroll() stays in card-index range and the cursor never sits
// above the scroll.
func TestPlanNetworkScrollIsCardIndexNotLineOffset(t *testing.T) {
	rows := make([]planNetworkRow, 25)
	for i := range rows {
		// Alternate wave-header and task-card kinds so
		// planNetworkRowHeights injects separators every other row;
		// the height variance is what the cardlist's
		// scrollwindow.Resync must respect.
		if i%4 == 0 {
			rows[i] = planNetworkRow{Kind: planRowWaveHeader, WaveID: int64(i / 4)}
		} else {
			rows[i] = planNetworkRow{Kind: planRowTaskCard}
		}
	}

	var m Model
	m.height = 24
	m.width = 100
	m.planNetworkCursor = m.planNetworkCursor.WithItemCount(len(rows))
	for step := 0; step < len(rows); step++ {
		m.planNetworkCursor = m.planNetworkCursor.SetCursor(step)
		m.syncPlanNetworkScroll(rows)
		scroll := m.planNetwork.Scroll()
		cursor := m.planNetwork.Cursor()
		if scroll < 0 || scroll >= len(rows) {
			t.Fatalf("step %d: planNetwork.Scroll=%d out of row-index range [0,%d)", step, scroll, len(rows))
		}
		if cursor != step {
			t.Fatalf("step %d: planNetwork.Cursor=%d, want %d (cursor mirror broken)", step, cursor, step)
		}
		if cursor < scroll {
			t.Fatalf("step %d: cursor=%d above scroll=%d (cursor scrolled off the top)", step, cursor, scroll)
		}
	}
}

// TestPlanNetworkScrollResetsOnEmptyRows pins the early-return:
// passing an empty row list drains the cardlist's items so a
// subsequent render sees Scroll()=0 instead of a stale offset from
// a prior plan with rows.
func TestPlanNetworkScrollResetsOnEmptyRows(t *testing.T) {
	rows := make([]planNetworkRow, 40)
	for i := range rows {
		rows[i] = planNetworkRow{Kind: planRowTaskCard}
	}

	var m Model
	m.height = 20 // panelViewportRows yields a budget smaller than 40 rows
	m.width = 100
	m.planNetworkCursor = m.planNetworkCursor.WithItemCount(len(rows)).SetCursor(39)
	m.syncPlanNetworkScroll(rows)
	if m.planNetwork.Scroll() == 0 {
		t.Fatalf("setup: expected non-zero scroll after walking to cursor=39 in a 40-row list bigger than viewport")
	}
	m.syncPlanNetworkScroll(nil)
	if got := m.planNetwork.Scroll(); got != 0 {
		t.Fatalf("empty rows did not reset scroll: got %d, want 0", got)
	}
}
