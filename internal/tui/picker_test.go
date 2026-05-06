package tui

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
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	default:
		// Single-rune keys (j, k, g, G, etc.) come through as KeyRunes.
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPickerNavKeyEmptyRowCount(t *testing.T) {
	cursor, handled := pickerNavKey(keyMsg("down"), 0, 0, 5)
	if handled {
		t.Errorf("empty picker should never claim to handle keys (would return 0/false), got handled=true")
	}
	if cursor != 0 {
		t.Errorf("cursor on empty picker should stay 0, got %d", cursor)
	}
}

func TestPickerNavKeyDown(t *testing.T) {
	cursor, handled := pickerNavKey(keyMsg("down"), 1, 5, 4)
	if !handled {
		t.Error("down should be handled")
	}
	if cursor != 2 {
		t.Errorf("cursor = %d, want 2", cursor)
	}
}

func TestPickerNavKeyJ(t *testing.T) {
	cursor, handled := pickerNavKey(keyMsg("j"), 1, 5, 4)
	if !handled || cursor != 2 {
		t.Errorf("j should advance cursor like down, got cursor=%d handled=%v", cursor, handled)
	}
}

func TestPickerNavKeyDownClampsAtLastRow(t *testing.T) {
	cursor, _ := pickerNavKey(keyMsg("down"), 4, 5, 4)
	if cursor != 4 {
		t.Errorf("down at last row should stay, got %d", cursor)
	}
}

func TestPickerNavKeyUpClampsAtZero(t *testing.T) {
	cursor, _ := pickerNavKey(keyMsg("up"), 0, 5, 4)
	if cursor != 0 {
		t.Errorf("up at first row should stay, got %d", cursor)
	}
}

func TestPickerNavKeyHomeEnd(t *testing.T) {
	if cursor, _ := pickerNavKey(keyMsg("g"), 3, 10, 4); cursor != 0 {
		t.Errorf("g should jump to 0, got %d", cursor)
	}
	if cursor, _ := pickerNavKey(keyMsg("G"), 3, 10, 4); cursor != 9 {
		t.Errorf("G should jump to last (9), got %d", cursor)
	}
	if cursor, _ := pickerNavKey(keyMsg("home"), 3, 10, 4); cursor != 0 {
		t.Errorf("home should jump to 0, got %d", cursor)
	}
	if cursor, _ := pickerNavKey(keyMsg("end"), 3, 10, 4); cursor != 9 {
		t.Errorf("end should jump to 9, got %d", cursor)
	}
}

func TestPickerNavKeyPageStep(t *testing.T) {
	// viewport=10 → step = 10/2 = 5 (above the 4-row floor)
	cursor, handled := pickerNavKey(keyMsg("pgdown"), 0, 100, 10)
	if !handled || cursor != 5 {
		t.Errorf("pgdown should advance by half-page (5), got cursor=%d handled=%v", cursor, handled)
	}
	cursor, _ = pickerNavKey(keyMsg("pgup"), 50, 100, 10)
	if cursor != 45 {
		t.Errorf("pgup from 50 by 5 → 45, got %d", cursor)
	}
}

func TestPickerNavKeyPageStepFloor(t *testing.T) {
	// Viewport too small → step floors at 4. From 0, pgdown should land at 4.
	cursor, _ := pickerNavKey(keyMsg("pgdown"), 0, 100, 2)
	if cursor != 4 {
		t.Errorf("pgdown with tiny viewport should use floor step (4), got %d", cursor)
	}
}

func TestPickerNavKeyUnknownReturnsFalse(t *testing.T) {
	cursor, handled := pickerNavKey(keyMsg("space"), 3, 10, 4)
	if handled {
		t.Error("space is not a navigation key — should return handled=false")
	}
	if cursor != 3 {
		t.Errorf("unknown key should return cursor unchanged, got %d", cursor)
	}
}
