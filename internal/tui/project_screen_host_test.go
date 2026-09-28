package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/screenhost"
)

func ctrlP() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlP} }
func fKey() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}} }

func TestProjectRoutesUseScreenStack(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	prior := model.navigation

	next, _ := model.Update(ctrlP())
	got := next.(Model)
	if len(got.screenStack) != 1 || got.screenStack[0] != screenhost.Project {
		t.Fatalf("ctrl+p stack = %v, want [project]", got.screenStack)
	}
	if got.navigation != prior {
		t.Fatalf("project route changed legacy nav from %+v to %+v", prior, got.navigation)
	}
	if len(got.projectScreen.Activity()) != 3 {
		t.Fatalf("project activity = %d, want 3", len(got.projectScreen.Activity()))
	}

	next, _ = got.Update(fKey())
	got = next.(Model)
	if len(got.screenStack) != 2 || got.screenStack[1] != screenhost.ProjectForm {
		t.Fatalf("f stack = %v, want project form reader", got.screenStack)
	}
	if !got.activeScreenBlocksHostInput() || got.canOpenPalette() {
		t.Fatalf("project form reader must block host input and palette")
	}

	next, _ = got.Update(escKey())
	got = next.(Model)
	if len(got.screenStack) != 1 || got.screenStack[0] != screenhost.Project {
		t.Fatalf("reader esc stack = %v, want project", got.screenStack)
	}
	next, _ = got.Update(escKey())
	got = next.(Model)
	if len(got.screenStack) != 0 || got.navigation != prior {
		t.Fatalf("project esc did not restore base route: stack=%v nav=%+v", got.screenStack, got.navigation)
	}
}

func TestProjectCommentOutcomeOpensRootReader(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	model.openProjectView()
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	wantCursor := model.projectScreen.ActivityCursor()
	events := model.projectScreen.Activity()
	if len(events) == 0 {
		t.Fatal("setup: project activity is empty")
	}
	model.applyScreenOutcome(screenhost.OpenProjectComment(model.projectScreen, events[0].ID, nil))
	if len(model.screenStack) < 2 || model.screenStack[len(model.screenStack)-1] != screenhost.CommentDetail {
		t.Fatalf("comment reader stack = %v", model.screenStack)
	}
	payload := model.commentDetailScreen.Payload()
	if payload.Comment.ID != events[0].ID || payload.Editable || payload.Deletable {
		t.Fatalf("project comment payload = %+v", payload)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.projectScreen.ActivityCursor() != wantCursor {
		t.Fatalf("project activity cursor after comment round-trip = %d, want %d", model.projectScreen.ActivityCursor(), wantCursor)
	}
}
