package knowledge

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestKnowledgeScreenOpensResourceAndReturnsToList(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 30)
	screen := New().Apply(domain.KnowledgeSnapshot{Resources: []domain.KnowledgeResource{{ID: "openapi:createOrder", Project: "api", Title: "Create an order", Body: "# POST /orders\n\nCreates one order."}}})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "Create an order") {
		t.Fatalf("resource missing from list: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("enter")).Screen.(Screen)
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "POST /orders") {
		t.Fatalf("resource body missing: %s", view)
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	if screen.detail {
		t.Fatal("esc did not return to resource list")
	}
}
