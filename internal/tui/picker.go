package tui

import tea "github.com/charmbracelet/bubbletea"

// pickerNavKey routes the navigation keys shared by every list-picker —
// up/down/k/j/pgup/pgdn/ctrl+u/ctrl+d/home/g/end/G — into a single new
// cursor value. Returns (newCursor, true) when the key is a recognised
// navigation key, or (cursor, false) so callers can fall through to their
// picker-specific keys (space toggle, enter confirm, ctrl+s save, esc).
//
// rowCount is the picker's current row count; viewport is the number of
// rows visible at once and feeds the half-page step. Both can be 0 — the
// helper still clamps the cursor into [0, rowCount-1] so empty pickers
// never produce out-of-range indices.
func pickerNavKey(key tea.KeyMsg, cursor, rowCount, viewport int) (int, bool) {
	if rowCount <= 0 {
		return 0, false
	}
	switch key.String() {
	case "up", "k":
		if cursor > 0 {
			cursor--
		}
	case "down", "j":
		if cursor < rowCount-1 {
			cursor++
		}
	case "pgup", "ctrl+u":
		cursor -= taskViewPageStep(viewport)
	case "pgdown", "ctrl+d":
		cursor += taskViewPageStep(viewport)
	case "home", "g":
		cursor = 0
	case "end", "G":
		cursor = rowCount - 1
	default:
		return cursor, false
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > rowCount-1 {
		cursor = rowCount - 1
	}
	return cursor, true
}
