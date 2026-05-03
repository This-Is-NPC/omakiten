package app

import (
	"context"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

func TestContextServiceDumpLevels(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	taskA, err := store.CreateTask(ctx, project.ID, "A", "Build A", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, project.ID, "B", "Build B", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}
	if _, err := NewDependencyService(store).Add(ctx, project.Context(), taskB.ID, taskA.ID); err != nil {
		t.Fatalf("Dependency Add() error = %v", err)
	}
	if _, err := NewCommentService(store).Add(ctx, project.Context(), taskA.ID, "Useful note", "human"); err != nil {
		t.Fatalf("Comment Add() error = %v", err)
	}
	service := NewContextService(store, store, store, store, store, token.ApproxCounter{})
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
	store, project := appTestStore(t, appTestBundle(1))
	defer func() { _ = store.Close() }()

	service := NewContextService(store, store, store, store, store, token.ApproxCounter{})
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

func appTestStore(t *testing.T, bundle config.Bundle) (*sqlite.Store, domain.Project) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	return store, project
}

func appTestBundle(maxTokens int) config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: maxTokens},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{Slug: "go", Name: "Go"}},
		Personas: []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}
