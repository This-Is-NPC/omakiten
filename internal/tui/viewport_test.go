package tui

import (
	"strings"
	"testing"
)

func TestSliceViewportEmpty(t *testing.T) {
	visible, above, below := sliceViewport(nil, 0, 10)
	if len(visible) != 0 {
		t.Errorf("visible = %v, want empty", visible)
	}
	if above != 0 || below != 0 {
		t.Errorf("above=%d below=%d, want 0/0", above, below)
	}
}

func TestSliceViewportNonPositiveHeight(t *testing.T) {
	lines := []string{"a", "b", "c"}
	visible, above, below := sliceViewport(lines, 1, 0)
	if len(visible) != 3 {
		t.Errorf("non-positive height should return all lines, got %d", len(visible))
	}
	if above != 0 || below != 0 {
		t.Errorf("above=%d below=%d, want 0/0", above, below)
	}
}

func TestSliceViewportFits(t *testing.T) {
	lines := []string{"a", "b", "c"}
	visible, above, below := sliceViewport(lines, 0, 5)
	if len(visible) != 3 {
		t.Errorf("len(visible) = %d, want 3 (everything fits)", len(visible))
	}
	if above != 0 || below != 0 {
		t.Errorf("above=%d below=%d, want 0/0 when content fits", above, below)
	}
}

func TestSliceViewportOverflow(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	visible, above, below := sliceViewport(lines, 1, 2)
	if got := strings.Join(visible, ","); got != "b,c" {
		t.Errorf("visible = %q, want b,c", got)
	}
	if above != 1 {
		t.Errorf("above = %d, want 1", above)
	}
	if below != 2 {
		t.Errorf("below = %d, want 2", below)
	}
}

func TestSliceViewportClampsScrollPastEnd(t *testing.T) {
	// "Jump to end" callers store a sentinel like 1<<20 — the helper must
	// clamp without producing an out-of-range slice.
	lines := []string{"a", "b", "c", "d"}
	visible, above, below := sliceViewport(lines, 1<<20, 2)
	if got := strings.Join(visible, ","); got != "c,d" {
		t.Errorf("visible = %q, want c,d (clamped to end)", got)
	}
	if above != 2 || below != 0 {
		t.Errorf("above=%d below=%d, want 2/0 at end", above, below)
	}
}

func TestSliceViewportNegativeScroll(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	visible, above, below := sliceViewport(lines, -5, 2)
	if got := strings.Join(visible, ","); got != "a,b" {
		t.Errorf("visible = %q, want a,b (negative scroll → 0)", got)
	}
	if above != 0 {
		t.Errorf("above = %d, want 0", above)
	}
	if below != 2 {
		t.Errorf("below = %d, want 2", below)
	}
}
