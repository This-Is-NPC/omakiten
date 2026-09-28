package list

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/scrollwindow"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestSingleEnterFiresSelect(t *testing.T) {
	m := NewPicker(Single)
	next, _ := m.Update(keyMsg("enter"), 5, 4)
	if next.LastEvent() != PickerSelect {
		t.Errorf("Single enter → PickerSelect, got %v", next.LastEvent())
	}
}

func TestMultiEnterIsNoOp(t *testing.T) {
	// In multi mode enter must NOT confirm — ctrl+s is the explicit save key
	// and enter is reserved for special rows like "+ create new" handled by
	// the parent. The picker just signals "no event" so parent fall-through
	// can apply the special-row logic.
	m := NewPicker(Multi)
	next, _ := m.Update(keyMsg("enter"), 5, 4)
	if next.LastEvent() != PickerNone {
		t.Errorf("Multi enter must be PickerNone (parent fall-through), got %v", next.LastEvent())
	}
}

func TestMultiSpaceFiresToggle(t *testing.T) {
	m := NewPicker(Multi)
	next, _ := m.Update(keyMsg("space"), 5, 4)
	if next.LastEvent() != PickerToggle {
		t.Errorf("Multi space → PickerToggle, got %v", next.LastEvent())
	}
}

func TestSingleSpaceIsNoOp(t *testing.T) {
	// Space must not toggle in single-select; it would silently mutate state
	// the parent isn't tracking.
	m := NewPicker(Single)
	next, _ := m.Update(keyMsg("space"), 5, 4)
	if next.LastEvent() != PickerNone {
		t.Errorf("Single space must be PickerNone, got %v", next.LastEvent())
	}
}

func TestMultiCtrlSFiresSelect(t *testing.T) {
	m := NewPicker(Multi)
	next, _ := m.Update(keyMsg("ctrl+s"), 5, 4)
	if next.LastEvent() != PickerSelect {
		t.Errorf("Multi ctrl+s → PickerSelect, got %v", next.LastEvent())
	}
}

func TestEscFiresCancelInBothModes(t *testing.T) {
	for _, mode := range []PickerMode{Single, Multi} {
		m := NewPicker(mode)
		next, _ := m.Update(keyMsg("esc"), 5, 4)
		if next.LastEvent() != PickerCancel {
			t.Errorf("mode=%v esc → PickerCancel, got %v", mode, next.LastEvent())
		}
	}
}

func TestNavigationDoesNotFireEvents(t *testing.T) {
	m := NewPicker(Single)
	for _, key := range []string{"down", "j", "up", "k", "g", "G", "home", "end", "pgup", "pgdown"} {
		m.lastEvent = PickerSelect // poison the field — Update must clear it
		next, _ := m.Update(keyMsg(key), 5, 4)
		if next.LastEvent() != PickerNone {
			t.Errorf("nav key %q produced %v, want PickerNone", key, next.LastEvent())
		}
	}
}

func TestCursorAdvancesAndScrollFollows(t *testing.T) {
	m := NewPicker(Single)
	// rowCount=10 viewport=3: mid-list both hints fire, so the renderer
	// spends two of the three rows on ▲/▼ and paints a single data row —
	// scroll therefore tracks the cursor exactly. The old expectation of 2
	// assumed a full three-row data window that the renderer never paints,
	// which is the defect this suite now pins.
	for i := 0; i < 4; i++ {
		m, _ = m.Update(keyMsg("down"), 10, 3)
	}
	if m.Cursor != 4 {
		t.Errorf("cursor = %d, want 4", m.Cursor)
	}
	if m.Scroll != 4 {
		t.Errorf("scroll = %d, want 4 (one data row between the ▲/▼ hints)", m.Scroll)
	}
}

func TestEndKeyJumpsAndClampsScroll(t *testing.T) {
	m := NewPicker(Single)
	m, _ = m.Update(keyMsg("G"), 10, 3)
	if m.Cursor != 9 {
		t.Errorf("cursor = %d, want 9", m.Cursor)
	}
	// At the bottom only the ▲ hint fires, so two data rows fit: the
	// ceiling is 10-3+1, not the bare 10-3 that stranded row 9.
	if m.Scroll != 8 {
		t.Errorf("scroll = %d, want 8 (10-3+AboveHintRows)", m.Scroll)
	}
}

// renderedWindow is the oracle: the exact [first, last] row range the
// HintsSplit renderer draws for a scroll offset over a unit-height list.
// The picker's follow math is only correct if it agrees with this.
func renderedWindow(scroll, viewport, total int) (first, last int) {
	heights := make([]int, total)
	for i := range heights {
		heights[i] = 1
	}
	end := scrollwindow.Slice(scroll, heights, viewport, scrollwindow.HintsSplit)
	return scroll, end - 1
}

// TestCursorStaysInsideTheRenderedWindow is the regression test for the
// stranded-tail defect the reachability property caught: followCursor
// clamped to a bare total-viewport, as if the renderer painted a full
// viewport of rows, while the renderer spends one or two of those rows
// on the ▲/▼ hints. The last options then never entered the window, not
// even on end/G.
//
// The assertion is renderer-truthful rather than a hard-coded offset:
// whatever scroll the picker settles on, the row the cursor sits on must
// be one the renderer actually paints.
func TestCursorStaysInsideTheRenderedWindow(t *testing.T) {
	for _, tc := range []struct{ total, viewport int }{
		{10, 3}, {10, 4}, {36, 7}, {36, 12}, {24, 5}, {8, 8}, {9, 8}, {5, 12},
	} {
		t.Run(fmt.Sprintf("total%d_viewport%d", tc.total, tc.viewport), func(t *testing.T) {
			assertCursorKeys(t, tc.total, tc.viewport)
		})
	}
}

func assertCursorKeys(t *testing.T, total, viewport int) {
	for _, key := range []string{"G", "end", "pgdown", "down"} {
		m := NewPicker(Single)
		presses := 1
		if key == "down" || key == "pgdown" {
			presses = total
		}
		for i := 0; i < presses; i++ {
			m, _ = m.Update(keyMsg(key), total, viewport)
			first, last := renderedWindow(m.Scroll, viewport, total)
			if m.Cursor < first || m.Cursor > last {
				t.Fatalf("key %q press %d: cursor=%d scroll=%d but the renderer paints rows %d..%d — cursor stranded outside the window", key, i+1, m.Cursor, m.Scroll, first, last)
			}
		}
	}
}

// TestScrollCeilingMatchesTheHintReservation pins the shared bound: the
// deepest offset the picker will ever settle on is the one that renders
// the final row, i.e. scrollwindow.MaxOffset for the HintsSplit contract
// — the same bound linelist.clampScroll uses.
func TestScrollCeilingMatchesTheHintReservation(t *testing.T) {
	const total, viewport = 36, 7
	m := NewPicker(Single)
	m, _ = m.Update(keyMsg("G"), total, viewport)
	want := scrollwindow.MaxOffset(total, viewport, scrollwindow.HintsSplit)
	if m.Scroll != want {
		t.Errorf("scroll after G = %d, want %d (scrollwindow.MaxOffset for HintsSplit)", m.Scroll, want)
	}
}

func TestEmptyRowCountIsSafe(t *testing.T) {
	m := NewPicker(Single)
	next, _ := m.Update(keyMsg("down"), 0, 4)
	if next.Cursor != 0 || next.Scroll != 0 {
		t.Errorf("empty picker cursor/scroll mutated: cursor=%d scroll=%d", next.Cursor, next.Scroll)
	}
	if next.LastEvent() != PickerNone {
		t.Errorf("empty picker must not signal events, got %v", next.LastEvent())
	}
}

func TestNonKeyMessageIsNoOp(t *testing.T) {
	m := Picker{Cursor: 3, Scroll: 1}
	next, cmd := m.Update(struct{}{}, 10, 4)
	if next.Cursor != 3 || next.Scroll != 1 {
		t.Errorf("non-key msg mutated state: cursor=%d scroll=%d", next.Cursor, next.Scroll)
	}
	if cmd != nil {
		t.Error("non-key msg should not produce a Cmd")
	}
}
