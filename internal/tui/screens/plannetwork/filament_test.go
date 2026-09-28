package plannetwork

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	networkprojection "omakiten/internal/plannetwork"
	"omakiten/internal/tui/components/screenkit"
)

func TestLanePaintsSourceArm(t *testing.T) {
	style := lipgloss.NewStyle()
	styles := screenkit.Styles{Hint: style}
	filaments := []networkprojection.Filament{{SrcRow: 0, DstRows: []int{2}, Lane: 0}}
	src := lane(styles, 0, filaments, 1)
	if src != "┌─" {
		t.Fatalf("source row = %q, want ┌─", src)
	}
	mid := lane(styles, 1, filaments, 1)
	if mid != "│ " {
		t.Fatalf("mid row = %q, want │ + space", mid)
	}
	dst := lane(styles, 2, filaments, 1)
	if dst != "└►" {
		t.Fatalf("dest row = %q, want └►", dst)
	}
}
