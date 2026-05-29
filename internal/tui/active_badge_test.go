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

// TestInactiveLawHasNoScopeBadge pins F1: the scope badge asserts a wiring
// scope (GLOBAL/PROJECT/PERSONA), which is meaningless for a catalog law that
// is not wired into the active bundle. Inactive laws must render no scope
// badge — otherwise an unwired law reads as GLOBAL yet carries no ACTIVE mark.
func TestInactiveLawHasNoScopeBadge(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.laws = []domain.Law{
		{Key: "wired-law", Body: "active law body", Scope: domain.LawScopeGlobal, Active: true},
		{Key: "dormant-law", Body: "inactive law body", Active: false},
	}
	model.entityKind = entityKindLaw
	model.width = 200
	model.height = 40

	cell := model.renderEntityCell(entityKindLaw)

	for _, want := range []string{"wired-law", "dormant-law"} {
		if !strings.Contains(cell, want) {
			t.Fatalf("renderEntityCell missing catalog law %q\n%s", want, cell)
		}
	}
	// One GLOBAL scope badge — the wired law only; the inactive law carries
	// no scope claim. One ACTIVE badge — the wired law.
	if got := strings.Count(cell, "GLOBAL"); got != 1 {
		t.Fatalf("GLOBAL scope badge count = %d, want 1 (only the wired law)\n%s", got, cell)
	}
	if got := strings.Count(cell, "ACTIVE"); got != 1 {
		t.Fatalf("ACTIVE badge count = %d, want 1 (only the wired law)\n%s", got, cell)
	}
}
