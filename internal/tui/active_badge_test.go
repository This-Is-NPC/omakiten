package tui

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestActiveBadgeMarksWiredEntities pins the Settings catalog contract: the
// column lists every persona (full catalog) but only the wired subset carries
// the ACTIVE badge.
func TestActiveBadgeMarksWiredEntities(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.personas = []domain.Persona{
		{Key: "wired", Name: "Wired One", Description: "active persona", Active: true},
		{Key: "dormant", Name: "Dormant One", Description: "inactive persona", Active: false},
	}
	model.entityKind = entityKindPersona
	model.width = 200
	model.height = 40

	cell := model.renderEntityCell(entityKindPersona)

	// Both catalog entries must be listed regardless of wiring.
	for _, want := range []string{"wired", "dormant"} {
		if !strings.Contains(cell, want) {
			t.Fatalf("renderEntityCell missing catalog entry %q\n%s", want, cell)
		}
	}
	// Exactly one ACTIVE badge — the wired persona.
	if got := strings.Count(cell, "ACTIVE"); got != 1 {
		t.Fatalf("ACTIVE badge count = %d, want 1 (only the wired persona)\n%s", got, cell)
	}
}
