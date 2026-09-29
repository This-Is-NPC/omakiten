package graph

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestKnowledgeViewsFollowCommandTreeToDocumentation(t *testing.T) {
	snapshot := domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{
			{Project: "okt", ID: "cli:okt", Kind: "CLI Command", Title: "okt"},
			{Project: "okt", ID: "cli:task", Kind: "CLI Command", Title: "okt task"},
			{Project: "okt", ID: "cli:task.create", Kind: "CLI Command", Title: "okt task create"},
			{Project: "okt", ID: "markdown:docs/tasks", Kind: "Guide", Title: "Work on a task"},
		},
		Relations: []domain.KnowledgeRelation{
			{From: "okt:cli:okt", To: "okt:cli:task", Kind: "contains"},
			{From: "okt:cli:task", To: "okt:cli:task.create", Kind: "contains"},
			{From: "okt:cli:task.create", To: "okt:markdown:docs/tasks", Kind: "documented_by"},
		},
	}
	views := KnowledgeViews(snapshot).Views
	if len(views[KnowledgeRoot].Lines) != 1 || views[knowledgeCLI].Lines[0].FocusID != "okt:cli:okt" {
		t.Fatalf("project entry = %+v; CLI entry = %+v", views[KnowledgeRoot], views[knowledgeCLI])
	}
	if views["okt:cli:task"].Parent != "okt:cli:okt" || views["okt:cli:task"].Lines[1].FocusID != "okt:cli:task.create" {
		t.Fatalf("task command neighborhood = %+v", views["okt:cli:task"])
	}
	leaf := views["okt:cli:task.create"]
	if len(leaf.Lines) != 2 || leaf.Lines[1].ResourceID != "okt:markdown:docs/tasks" || !strings.Contains(leaf.Lines[1].Text, "documentation") {
		t.Fatalf("command documentation = %+v", leaf)
	}
}
