package app

import (
	"context"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

func TestContextServiceDumpLevels(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskA, err := store.CreateTask(ctx, project.ID, "A", "Build A", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, project.ID, "B", "Build B", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}
	if _, err := NewDependencyService(store).Add(ctx, project.Context(), taskB.ID, taskA.ID); err != nil {
		t.Fatalf("Dependency Add() error = %v", err)
	}
	if _, err := NewCommentService(store, store.Snapshot()).Add(ctx, project.Context(), taskA.ID, "Useful note", "human", nil); err != nil {
		t.Fatalf("Comment Add() error = %v", err)
	}
	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	if _, err := service.Add(ctx, project.Context(), "Handoff context"); err != nil {
		t.Fatalf("Context Add() error = %v", err)
	}

	level1, err := service.Dump(ctx, project.Context(), 1)
	if err != nil {
		t.Fatalf("Dump(level 1) error = %v", err)
	}
	if len(level1.ContextEntries) != 1 || len(level1.Tasks) != 0 || len(level1.Laws) != 0 {
		t.Fatalf("Dump(level 1) = %#v, want entries only", level1)
	}

	level2, err := service.Dump(ctx, project.Context(), 2)
	if err != nil {
		t.Fatalf("Dump(level 2) error = %v", err)
	}
	if len(level2.Tasks) != 2 || len(level2.Dependencies) != 1 || level2.Workflow.Key != "default" {
		t.Fatalf("Dump(level 2) = %#v, want tasks dependencies and workflow", level2)
	}
	if len(level2.Comments) != 0 || len(level2.Laws) != 0 {
		t.Fatalf("Dump(level 2) included level 3 fields: %#v", level2)
	}

	level3, err := service.Dump(ctx, project.Context(), 3)
	if err != nil {
		t.Fatalf("Dump(level 3) error = %v", err)
	}
	if len(level3.Comments) != 1 || len(level3.Laws) != 1 {
		t.Fatalf("Dump(level 3) = %#v, want comments and laws", level3)
	}
	if level3.TokenMetrics.EstimatedTotal == 0 {
		t.Fatalf("Dump(level 3) token estimate = 0, want positive")
	}
}

func TestContextServiceDumpRespectsTokenBudget(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1))
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	if _, err := service.Add(ctx, project.Context(), "too many words"); err != nil {
		t.Fatalf("Context Add() error = %v", err)
	}
	dump, err := service.Dump(ctx, project.Context(), 1)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}
	if !dump.TokenMetrics.Truncated {
		t.Fatalf("Dump().TokenMetrics.Truncated = false, want true")
	}
	if len(dump.ContextEntries) != 0 {
		t.Fatalf("Dump().ContextEntries len = %d, want 0 due budget", len(dump.ContextEntries))
	}
}

func TestContextServiceAddValidates(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	_, err := service.Add(ctx, project.Context(), "")
	if err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Add(ctx, project.Context(), "   ")
	if err == nil {
		t.Fatal("Add() whitespace error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestContextServiceDumpInvalidLevel(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	_, err := service.Dump(ctx, project.Context(), 0)
	if err == nil {
		t.Fatal("Dump(level 0) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Dump(ctx, project.Context(), 4)
	if err == nil {
		t.Fatal("Dump(level 4) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestContextServiceDumpUnlimitedBudget(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 0))
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store.Snapshot(), token.ApproxCounter{}, testfixtures.CanonicalRegistry())
	if _, err := service.Add(ctx, project.Context(), "some context entry"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	dump, err := service.Dump(ctx, project.Context(), 1)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}
	if dump.TokenMetrics.Truncated {
		t.Fatal("Dump().Truncated = true, want false")
	}
	if len(dump.ContextEntries) != 1 {
		t.Fatalf("Dump().ContextEntries len = %d, want 1", len(dump.ContextEntries))
	}
}

func TestContextBudgetAdd(t *testing.T) {
	b := contextBudget{maxTokens: 10}
	if !b.add(5) {
		t.Fatal("add(5) = false, want true")
	}
	if b.total != 5 {
		t.Fatalf("total = %d, want 5", b.total)
	}
	if !b.add(5) {
		t.Fatal("add(5) = false, want true")
	}
	if b.total != 10 {
		t.Fatalf("total = %d, want 10", b.total)
	}
	if b.add(1) {
		t.Fatal("add(1) = true, want false")
	}
	if !b.truncated {
		t.Fatal("truncated = false, want true")
	}

	// Negative estimate
	b2 := contextBudget{maxTokens: 10}
	if !b2.add(-5) {
		t.Fatal("add(-5) = false, want true")
	}
	if b2.total != 0 {
		t.Fatalf("total = %d, want 0", b2.total)
	}
	if b2.truncated {
		t.Fatal("truncated = true, want false")
	}

	// Unlimited budget (maxTokens == 0)
	b3 := contextBudget{maxTokens: 0}
	if !b3.add(1000) {
		t.Fatal("add(1000) = false, want true")
	}
	if b3.total != 1000 {
		t.Fatalf("total = %d, want 1000", b3.total)
	}
	if b3.truncated {
		t.Fatal("truncated = true, want false")
	}
}

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
