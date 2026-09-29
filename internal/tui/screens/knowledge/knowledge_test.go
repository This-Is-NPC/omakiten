package knowledge

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/graph"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestKnowledgeScreenShowsGraphAndOpensLinkedResource(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 30)
	snapshot := domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{
			{ID: "openapi:createOrder", Project: "api", Title: "Create an order", Body: "# POST /orders\n\nCreates one order."},
			{ID: "markdown:docs/checkout", Project: "client", Title: "Checkout", Body: "# Checkout"},
		},
		Relations: []domain.KnowledgeRelation{{From: "client:markdown:docs/checkout", To: "api:openapi:createOrder", Kind: "references"}},
	}
	screen := New().Apply(snapshot, graph.KnowledgeLines(snapshot))
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "references → api:openapi:createOrder") {
		t.Fatalf("relation missing from graph: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "POST /orders") {
		t.Fatalf("linked resource body missing: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	if screen.detail {
		t.Fatal("esc did not return to knowledge graph")
	}
	screen = screen.Update(frame, screentest.Key("l")).Screen.(Screen)
	if screen.graph {
		t.Fatal("l did not switch to resource list")
	}
}
