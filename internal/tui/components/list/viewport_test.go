package list

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSliceFits(t *testing.T) {
	lines := []string{"a", "b", "c"}
	visible, above, below := SliceLines(lines, 0, 5)
	if len(visible) != 3 || above != 0 || below != 0 {
		t.Errorf("Slice fits → all %d lines, above=0, below=0; got len=%d above=%d below=%d", 3, len(visible), above, below)
	}
}

func TestSliceOverflow(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	visible, above, below := SliceLines(lines, 1, 2)
	if got := strings.Join(visible, ","); got != "b,c" {
		t.Errorf("Slice overflow visible = %q, want b,c", got)
	}
	if above != 1 || below != 2 {
		t.Errorf("above=%d below=%d, want 1/2", above, below)
	}
}

func TestSliceClampsScrollPastEnd(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	visible, above, below := SliceLines(lines, 1<<20, 2)
	if strings.Join(visible, ",") != "c,d" {
		t.Errorf("scroll sentinel should clamp to end, got %v", visible)
	}
	if above != 2 || below != 0 {
		t.Errorf("above=%d below=%d, want 2/0", above, below)
	}
}

func TestUpdateNavigationKeys(t *testing.T) {
	m := NewViewport()
	tests := []struct {
		key      string
		want     int
		scrollTo int
	}{
		{"j", 1, 0},
		{"down", 2, 1},
		{"k", 1, 2},
		{"up", 0, 1},
		// G stores the sentinel; Slice clamps it on render. We assert the
		// sentinel is set (not the clamped value) so the design contract
		// "Update mutates intent, View clamps" is locked in.
		{"G", 1 << 20, 0},
		{"g", 0, 999},
	}
	for _, tt := range tests {
		m.Scroll = tt.scrollTo
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}, 2)
		if next.Scroll != tt.want {
			t.Errorf("after %q from %d, scroll = %d, want %d", tt.key, tt.scrollTo, next.Scroll, tt.want)
		}
	}
}

func TestUpdateEsc(t *testing.T) {
	m := NewViewport()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}, 5)
	if next.LastEvent() != ViewportCancel {
		t.Errorf("esc should produce ViewportCancel, got %v", next.LastEvent())
	}
}

func TestUpdatePageStepHonoursFloor(t *testing.T) {
	// viewport=2 → naive step = 1, but we floor at 4 so pgdown moves ≥ 4 rows.
	m := NewViewport()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown}, 2)
	if next.Scroll != 4 {
		t.Errorf("pgdown with tiny viewport should floor at 4, got %d", next.Scroll)
	}
}

func TestViewRendersFooterOnOverflow(t *testing.T) {
	m := Viewport{Scroll: 1}
	out := m.View([]string{"a", "b", "c", "d", "e"}, 2, lipgloss.NewStyle())
	if !strings.Contains(out, "▲ 1 above") {
		t.Errorf("View missing 'above' indicator:\n%s", out)
	}
	if !strings.Contains(out, "▼ 2 below") {
		t.Errorf("View missing 'below' indicator:\n%s", out)
	}
}

func TestViewOmitsFooterWhenFits(t *testing.T) {
	m := Viewport{Scroll: 0}
	out := m.View([]string{"a", "b"}, 5, lipgloss.NewStyle())
	if strings.Contains(out, "above") || strings.Contains(out, "below") {
		t.Errorf("View should omit footer when content fits, got:\n%s", out)
	}
}

func TestFitWindowsOverflowAndPassesThroughWhenItFits(t *testing.T) {
	m := NewViewport()
	block := strings.Repeat("line\n", 10) + "line"
	if got := m.Fit(block, 0, lipgloss.NewStyle(), nil); got != block {
		t.Errorf("Fit at height 0 must return the grid unchanged")
	}
	if got := m.Fit(block, 20, lipgloss.NewStyle(), nil); got != block {
		t.Errorf("Fit when content fits must return the grid unchanged")
	}
	out := m.Fit(block, 5, lipgloss.NewStyle(), nil)
	if !strings.Contains(out, "above") && !strings.Contains(out, "below") {
		t.Errorf("overflow Fit should render scroll footer, got:\n%s", out)
	}
}

func TestUpdateNonKeyMessageIsNoOp(t *testing.T) {
	m := Viewport{Scroll: 3}
	next, cmd := m.Update(struct{}{}, 2)
	if next.Scroll != 3 {
		t.Errorf("non-key msg mutated scroll: %d → %d", 3, next.Scroll)
	}
	if cmd != nil {
		t.Error("non-key msg should not produce a Cmd")
	}
}
