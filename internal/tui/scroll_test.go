package tui

import "testing"

func TestFollowCursorEmptyAndZeroViewport(t *testing.T) {
	if got := followCursor(5, 3, 0, 10); got != 0 {
		t.Errorf("zero viewport should return 0, got %d", got)
	}
	if got := followCursor(5, 3, 4, 0); got != 0 {
		t.Errorf("zero total should return 0, got %d", got)
	}
}

func TestFollowCursorContentFits(t *testing.T) {
	if got := followCursor(0, 2, 10, 5); got != 0 {
		t.Errorf("content fits → scroll should be 0, got %d", got)
	}
}

func TestFollowCursorCursorInsideWindow(t *testing.T) {
	// scroll=2, viewport=4 → window covers rows 2..5. Cursor at 4 stays.
	if got := followCursor(2, 4, 4, 10); got != 2 {
		t.Errorf("cursor inside window should keep scroll=2, got %d", got)
	}
}

func TestFollowCursorCursorBelowWindow(t *testing.T) {
	// scroll=0, viewport=3, total=10. Cursor at 5 → scroll = 5 - 3 + 1 = 3.
	if got := followCursor(0, 5, 3, 10); got != 3 {
		t.Errorf("cursor below window → scroll = cursor-viewport+1, got %d, want 3", got)
	}
}

func TestFollowCursorCursorAboveWindow(t *testing.T) {
	// scroll=5, viewport=3, total=10. Cursor at 1 → scroll = 1.
	if got := followCursor(5, 1, 3, 10); got != 1 {
		t.Errorf("cursor above window → scroll = cursor, got %d, want 1", got)
	}
}

func TestFollowCursorClampsToTotalMinusViewport(t *testing.T) {
	// Defensive: a stale scroll past the end gets pulled back so the last
	// row remains the bottom of the visible window.
	if got := followCursor(20, 9, 3, 10); got != 7 {
		t.Errorf("scroll past end should clamp to 10-3=7, got %d", got)
	}
}

func TestFollowCursorNegativeScrollNormalised(t *testing.T) {
	if got := followCursor(-5, 0, 3, 10); got != 0 {
		t.Errorf("negative scroll should clamp to 0, got %d", got)
	}
}
