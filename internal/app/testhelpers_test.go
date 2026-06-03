package app

import (
	"context"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

func appTestStore(t *testing.T, bundle config.Bundle) (*snapstore.Store, domain.Project) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	return store, project
}

// appTestBundle loads testdata/default.yaml for the workflow/policy/kit
// shape, then layers minimal inline entities (skills/personas/laws) on
// top. The entity arrays carry `yaml:"-"` in production — they are
// populated by LoadBundle from per-entity folders, not from the YAML —
// so tests that need them must wire them in Go. maxTokens varies per
// test and is overlaid last.
func appTestBundle(t *testing.T, maxTokens int) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Config.Context.MaxTokens = maxTokens
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
}
