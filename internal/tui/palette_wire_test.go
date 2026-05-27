package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlK() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyCtrlK} }
func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

func TestCtrlKOpensPaletteFromBoard(t *testing.T) {
	model, _ := newPickerModel(t)
	if model.paletteOpen {
		t.Fatalf("setup: palette should start closed")
	}
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if !got.paletteOpen {
		t.Fatalf("ctrl+k from board did not open palette")
	}
}

func TestCtrlKBlockedWhileCommentScreenActive(t *testing.T) {
	model, _ := newPickerModel(t)
	model.commentScreenOpen = true
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite commentScreenOpen=true")
	}
}

func TestCtrlKBlockedWhileMoveModeActive(t *testing.T) {
	model, _ := newPickerModel(t)
	model.moveMode = true
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite moveMode=true")
	}
}

func TestCtrlKBlockedWhileTaskScreenOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.taskScreen = taskScreenView
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite taskScreen=view")
	}
}

func TestCtrlKBlockedWhileHelpOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.helpOpen = true
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite helpOpen=true")
	}
}

func TestEscFromPaletteClosesOverlay(t *testing.T) {
	model, _ := newPickerModel(t)
	opened, _ := model.Update(ctrlK())
	openedM := opened.(Model)
	if !openedM.paletteOpen {
		t.Fatalf("setup: palette should be open after ctrl+k")
	}
	next, cmd := openedM.Update(escKey())
	got := next.(Model)
	if cmd == nil {
		t.Fatalf("esc cmd was nil, want palette.DismissMsg producer")
	}
	// Run the cmd to surface DismissMsg, then feed it back through
	// Update so the root closes the overlay.
	msg := cmd()
	next, _ = got.Update(msg)
	got = next.(Model)
	if got.paletteOpen {
		t.Fatalf("palette still open after esc → DismissMsg round-trip")
	}
}

func TestPaletteKeystrokesRouteToOverlay(t *testing.T) {
	model, _ := newPickerModel(t)
	opened, _ := model.Update(ctrlK())
	got := opened.(Model)
	if !got.paletteOpen {
		t.Fatalf("setup: palette should be open")
	}
	// Type "nav:11" into the palette via Update routing.
	for _, r := range "nav:11" {
		next, _ := got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		got = next.(Model)
	}
	if got.palette.Tricks() != "nav:11" {
		t.Fatalf("palette tricks input = %q, want nav:11", got.palette.Tricks())
	}
}
