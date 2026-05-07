package picker

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	m := New(Single)
	next, _ := m.Update(keyMsg("enter"), 5, 4)
	if next.LastEvent() != EventSelect {
		t.Errorf("Single enter → EventSelect, got %v", next.LastEvent())
	}
}

func TestMultiEnterIsNoOp(t *testing.T) {
	// In multi mode enter must NOT confirm — ctrl+s is the explicit save key
	// and enter is reserved for special rows like "+ create new" handled by
	// the parent. The picker just signals "no event" so parent fall-through
	// can apply the special-row logic.
	m := New(Multi)
	next, _ := m.Update(keyMsg("enter"), 5, 4)
	if next.LastEvent() != EventNone {
		t.Errorf("Multi enter must be EventNone (parent fall-through), got %v", next.LastEvent())
	}
}

func TestMultiSpaceFiresToggle(t *testing.T) {
	m := New(Multi)
	next, _ := m.Update(keyMsg("space"), 5, 4)
	if next.LastEvent() != EventToggle {
		t.Errorf("Multi space → EventToggle, got %v", next.LastEvent())
	}
}

func TestSingleSpaceIsNoOp(t *testing.T) {
	// Space must not toggle in single-select; it would silently mutate state
	// the parent isn't tracking.
	m := New(Single)
	next, _ := m.Update(keyMsg("space"), 5, 4)
	if next.LastEvent() != EventNone {
		t.Errorf("Single space must be EventNone, got %v", next.LastEvent())
	}
}

func TestMultiCtrlSFiresSelect(t *testing.T) {
	m := New(Multi)
	next, _ := m.Update(keyMsg("ctrl+s"), 5, 4)
	if next.LastEvent() != EventSelect {
		t.Errorf("Multi ctrl+s → EventSelect, got %v", next.LastEvent())
	}
}

func TestEscFiresCancelInBothModes(t *testing.T) {
	for _, mode := range []Mode{Single, Multi} {
		m := New(mode)
		next, _ := m.Update(keyMsg("esc"), 5, 4)
		if next.LastEvent() != EventCancel {
			t.Errorf("mode=%v esc → EventCancel, got %v", mode, next.LastEvent())
		}
	}
}

func TestNavigationDoesNotFireEvents(t *testing.T) {
	m := New(Single)
	for _, key := range []string{"down", "j", "up", "k", "g", "G", "home", "end", "pgup", "pgdown"} {
		m.lastEvent = EventSelect // poison the field — Update must clear it
		next, _ := m.Update(keyMsg(key), 5, 4)
		if next.LastEvent() != EventNone {
			t.Errorf("nav key %q produced %v, want EventNone", key, next.LastEvent())
		}
	}
}

func TestCursorAdvancesAndScrollFollows(t *testing.T) {
	m := New(Single)
	// rowCount=10 viewport=3 → after 4 downs cursor=4, scroll follows to 2
	for i := 0; i < 4; i++ {
		m, _ = m.Update(keyMsg("down"), 10, 3)
	}
	if m.Cursor != 4 {
		t.Errorf("cursor = %d, want 4", m.Cursor)
	}
	if m.Scroll != 2 {
		t.Errorf("scroll = %d, want 2 (cursor 4 in 3-row window)", m.Scroll)
	}
}

func TestEndKeyJumpsAndClampsScroll(t *testing.T) {
	m := New(Single)
	m, _ = m.Update(keyMsg("G"), 10, 3)
	if m.Cursor != 9 {
		t.Errorf("cursor = %d, want 9", m.Cursor)
	}
	if m.Scroll != 7 {
		t.Errorf("scroll = %d, want 7 (10-3)", m.Scroll)
	}
}

func TestEmptyRowCountIsSafe(t *testing.T) {
	m := New(Single)
	next, _ := m.Update(keyMsg("down"), 0, 4)
	if next.Cursor != 0 || next.Scroll != 0 {
		t.Errorf("empty picker cursor/scroll mutated: cursor=%d scroll=%d", next.Cursor, next.Scroll)
	}
	if next.LastEvent() != EventNone {
		t.Errorf("empty picker must not signal events, got %v", next.LastEvent())
	}
}

func TestNonKeyMessageIsNoOp(t *testing.T) {
	m := Model{Cursor: 3, Scroll: 1}
	next, cmd := m.Update(struct{}{}, 10, 4)
	if next.Cursor != 3 || next.Scroll != 1 {
		t.Errorf("non-key msg mutated state: cursor=%d scroll=%d", next.Cursor, next.Scroll)
	}
	if cmd != nil {
		t.Error("non-key msg should not produce a Cmd")
	}
}
