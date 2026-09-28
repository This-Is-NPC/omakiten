package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/commentdetail"
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

func TestCtrlKBlockedOnHomeWithoutActiveProject(t *testing.T) {
	model, _ := newPickerModel(t)
	model.project = domain.ProjectContext{}
	model.navigation = firstSub(screenhost.TopHome)
	next, _ := model.Update(ctrlK())
	if got := next.(Model); got.paletteOpen {
		t.Fatal("ctrl+k opened palette on Home without an active project")
	}
}

func TestCtrlKBlockedWhileCommentScreenActive(t *testing.T) {
	model, _ := newPickerModel(t)
	model.commentDetailScreen = commentdetail.New().Open(commentdetail.Payload{Comment: domain.Comment{ID: 1}})
	model.screenStack = []screenhost.ID{screenhost.CommentDetail}
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite active comment route")
	}
}

func TestCtrlKBlockedWhileMoveModeActive(t *testing.T) {
	model, _ := newPickerModel(t)
	model.tasks = []domain.Task{{ID: 1, BucketKey: "backlog"}}
	model.workflow = domain.Workflow{Buckets: []domain.Bucket{{Key: "backlog"}}}
	model.boardScreen = board.New()
	model.boardScreen = model.boundBoardScreen()
	model = pressRune(t, model, 'm')
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite moveMode=true")
	}
}

func TestCtrlKBlockedWhileTaskScreenOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.screenStack = []screenhost.ID{screenhost.TaskDetail}
	next, _ := model.Update(ctrlK())
	got := next.(Model)
	if got.paletteOpen {
		t.Fatalf("ctrl+k opened palette despite hosted Task Detail")
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
