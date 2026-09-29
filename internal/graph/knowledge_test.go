package graph

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestKnowledgeLinesShowDirectedCrossProjectEdges(t *testing.T) {
	lines := KnowledgeLines(domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{
			{Project: "client", ID: "markdown:docs/checkout", Title: "Checkout"},
			{Project: "api", ID: "openapi:createOrder", Title: "Create order"},
		},
		Relations: []domain.KnowledgeRelation{{From: "client:markdown:docs/checkout", To: "api:openapi:createOrder", Kind: "references"}},
	})
	if len(lines) != 3 || lines[2].ResourceID != "api:openapi:createOrder" || !strings.Contains(lines[2].Text, "references → api:openapi:createOrder") {
		t.Fatalf("knowledge graph lines = %+v", lines)
	}
}
