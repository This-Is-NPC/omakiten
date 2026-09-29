package knowledge

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/graph"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestKnowledgeScreenNavigatesCompleteGraph(t *testing.T) {
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
	screen := New().Apply(snapshot, graph.ProjectKnowledge(snapshot))
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{"CLI", "okt task create", "documented_by → Work on a task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("complete graph missing %q: %s", want, view)
		}
	}
	for range 2 {
		screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	}
	screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "Create one.") || !strings.Contains(view, "Documentation: Work on a task") {
		t.Fatalf("command did not open its documentation: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if index, ok := screen.selectedResource(); !ok || snapshot.Resources[index].Title != "Work on a task" {
		t.Fatalf("cursor did not reach linked guide: index=%d ok=%t", index, ok)
	}
	screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "Create one.") {
		t.Fatalf("document body missing: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	if index, ok := screen.selectedResource(); screen.detail || !ok || snapshot.Resources[index].Title != "Work on a task" {
		t.Fatalf("return did not preserve graph cursor: index=%d ok=%t detail=%t", index, ok, screen.detail)
	}
}
