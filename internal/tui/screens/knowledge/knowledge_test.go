package knowledge

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/graph"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestKnowledgeScreenNavigatesFromCommandToItsDocumentation(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 30)
	snapshot := domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{
			{ID: "cli:okt", Project: "okt", Kind: "CLI Command", Title: "okt"},
			{ID: "cli:task", Project: "okt", Kind: "CLI Command", Title: "okt task"},
			{ID: "cli:task.create", Project: "okt", Kind: "CLI Command", Title: "okt task create"},
			{ID: "markdown:docs/task", Project: "okt", Kind: "Guide", Title: "Work on a task", Body: "# Work on a task\n\nCreate one."},
		},
		Relations: []domain.KnowledgeRelation{
			{From: "okt:cli:okt", To: "okt:cli:task", Kind: "contains"},
			{From: "okt:cli:task", To: "okt:cli:task.create", Kind: "contains"},
			{From: "okt:cli:task.create", To: "okt:markdown:docs/task", Kind: "documented_by"},
		},
	}
	screen := New().Apply(snapshot, graph.KnowledgeViews(snapshot))
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "CLI → commands") || strings.Contains(view, "Work on a task") {
		t.Fatalf("entry is not interface-first: %s", view)
	}
	for _, focus := range []string{"category:cli", "okt:cli:okt", "okt:cli:task", "okt:cli:task.create"} {
		screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
		if screen.focus != focus {
			t.Fatalf("focus = %q, want %q", screen.focus, focus)
		}
		if focus == "okt:cli:okt" || focus == "okt:cli:task" {
			screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
		}
	}
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "documentation → Work on a task") {
		t.Fatalf("command documentation missing: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "Create one.") {
		t.Fatalf("document body missing: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	if screen.detail || screen.focus != "okt:cli:task.create" {
		t.Fatalf("return from document = detail %t, focus %q", screen.detail, screen.focus)
	}
}
