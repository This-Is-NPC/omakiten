package screengrid

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// A root Cell is arranged through ArrangeIn because the grid owns an explicit
// box. A host screen still owes the one leading row that screenlayout.Arrange
// spends for it; the host mount is where that row belongs, not inside Cell.
func TestRootCellMountMatchesHostArrangeIncludingLeadingRow(t *testing.T) {
	kit := screenkit.Kit{Width: 40, Height: 24}
	spec := screenlayout.Spec{
		ID: "body", MinWidth: 20, MinRows: 3,
		Weight: 1, Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}
	body := func(canvas screenlayout.Canvas) screenlayout.Block {
		return screenlayout.Block{Items: []string{
			"first row",
			"second row",
			"third row",
		}}
	}
	section := screenlayout.Func{Def: spec, Body: body}
	state := NewState().WithFocus(spec.ID)
	box := screenlayout.HostBox(kit)

	mounted := "\n" + Render(kit, state, box, Cell(spec, body)).View
	arranged := screenlayout.Arrange(kit, state.Layout(), section).View

	if mounted != arranged {
		t.Fatalf("root Cell mount differs from host Arrange:\nmounted=%q\narranged=%q", mounted, arranged)
	}
	if !strings.HasPrefix(mounted, "\n") {
		t.Fatal("host-mounted root Cell did not preserve the leading blank row")
	}
}
