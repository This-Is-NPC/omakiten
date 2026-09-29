package graph

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestProjectKnowledgeShowsCommandAndDocumentationInOneGraph(t *testing.T) {
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
	lines := ProjectKnowledge(snapshot).Lines
	if len(lines) != 5 {
		t.Fatalf("graph lines = %+v", lines)
	}
	for i, want := range []string{"CLI", "└─ okt", "   └─ okt task", "      └─ okt task create", "         └─ documented_by → Work on a task"} {
		if !strings.Contains(lines[i].Text, want) {
			t.Errorf("line %d = %q, want %q", i, lines[i].Text, want)
		}
	}
	if lines[4].ResourceID != "okt:markdown:docs/tasks" {
		t.Fatalf("linked guide = %+v", lines[4])
	}
}

func TestProjectKnowledgeKeepsCycleVisibleWithoutLooping(t *testing.T) {
	snapshot := domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{
			{Project: "api", ID: "openapi:orders", Title: "POST /orders"},
			{Project: "api", ID: "openapi:schema:Unused", Title: "Unused schema"},
			{Project: "api", ID: "markdown:docs/orders", Title: "Orders"},
		},
		Relations: []domain.KnowledgeRelation{
			{From: "api:openapi:orders", To: "api:markdown:docs/orders", Kind: "documented_by"},
			{From: "api:markdown:docs/orders", To: "api:openapi:orders", Kind: "links_to"},
		},
	}
	lines := ProjectKnowledge(snapshot).Lines
	if len(lines) != 6 || !strings.Contains(lines[3].Text, "↗") || lines[5].ResourceID != "api:openapi:schema:Unused" {
		t.Fatalf("cycle projection = %+v", lines)
	}
}
