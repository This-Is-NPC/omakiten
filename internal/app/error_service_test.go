package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestErrorServiceRecordValidatesDescription(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store)

	_, err := service.Record(ctx, project.Context(), "  ", "", nil)
	if err == nil {
		t.Fatal("Record(empty description) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestErrorServiceRecordNormalizesTags(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store)

	rec, err := service.Record(ctx, project.Context(), "boom", "ctx", []string{"Go", "golang", "  GOLANG"})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if len(rec.Tags) != 1 {
		t.Fatalf("Record() Tags = %+v, want deduped to single canonical 'go'", rec.Tags)
	}
	if rec.Tags[0].Name != "go" {
		t.Fatalf("Record() Tag = %q, want canonical 'go'", rec.Tags[0].Name)
	}
}

func TestErrorServiceSearchByTag(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store)

	if _, err := service.Record(ctx, project.Context(), "FK error", "", []string{"sqlite", "fk"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if _, err := service.Record(ctx, project.Context(), "panic", "", []string{"go"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	results, err := service.Search(ctx, project.Context(), "", []string{"sqlite"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Description != "FK error" {
		t.Fatalf("Search(sqlite) = %+v, want only FK error", results)
	}
}

func TestErrorServiceAddSolutionValidates(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store)

	_, err := service.AddSolution(ctx, project.Context(), 0, "fix", "", nil)
	if err == nil {
		t.Fatal("AddSolution(error_id=0) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)

	rec, _ := service.Record(ctx, project.Context(), "boom", "", nil)
	_, err = service.AddSolution(ctx, project.Context(), rec.ID, "  ", "", nil)
	if err == nil {
		t.Fatal("AddSolution(empty description) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestErrorServiceConfirmSolutionRanks(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store)
	rec, _ := service.Record(ctx, project.Context(), "boom", "", []string{"boom"})

	loser, _ := service.AddSolution(ctx, project.Context(), rec.ID, "loser", "", nil)
	winner, _ := service.AddSolution(ctx, project.Context(), rec.ID, "winner", "", nil)

	if _, err := service.ConfirmSolution(ctx, project.Context(), loser.ID, false); err != nil {
		t.Fatalf("ConfirmSolution(loser) error = %v", err)
	}
	if _, err := service.ConfirmSolution(ctx, project.Context(), winner.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(winner) error = %v", err)
	}

	results, err := service.Search(ctx, project.Context(), "", []string{"boom"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() len = %d", len(results))
	}
	sols := results[0].Solutions
	if len(sols) != 2 || sols[0].ID != winner.ID {
		t.Fatalf("Solutions = %+v, want winner first", sols)
	}
}

func TestErrorServiceCrossProjectSearch(t *testing.T) {
	ctx := context.Background()
	store, projectA := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	projectB, err := store.UpsertProject(ctx, "B", "b", "/work/b")
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}

	service := NewErrorService(store)
	if _, err := service.Record(ctx, projectA.Context(), "shared issue in A", "", []string{"shared"}); err != nil {
		t.Fatalf("Record(A) error = %v", err)
	}
	if _, err := service.Record(ctx, projectB.Context(), "shared issue in B", "", []string{"shared"}); err != nil {
		t.Fatalf("Record(B) error = %v", err)
	}

	// Search from project A's context — must still see B's error
	results, err := service.Search(ctx, projectA.Context(), "", []string{"shared"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(shared) len = %d, want 2 (cross-project)", len(results))
	}
}

func TestErrorServiceTagEntityIntegration(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	tagService := NewTagService(store)
	errService := NewErrorService(store)

	rec, _ := errService.Record(ctx, project.Context(), "boom", "", nil)

	// add via TagService entity_type=error
	tag, err := tagService.Add(ctx, project.Context(), TagEntityError, rec.ID, "sqlite")
	if err != nil {
		t.Fatalf("TagService.Add(error) error = %v", err)
	}
	if tag.Name != "sqlite" {
		t.Fatalf("TagService.Add(error) tag = %q", tag.Name)
	}

	tags, err := tagService.List(ctx, project.Context(), TagEntityError, rec.ID)
	if err != nil {
		t.Fatalf("TagService.List(error) error = %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "sqlite" {
		t.Fatalf("TagService.List(error) = %+v", tags)
	}

	if err := tagService.Remove(ctx, project.Context(), TagEntityError, rec.ID, tag.ID); err != nil {
		t.Fatalf("TagService.Remove(error) error = %v", err)
	}
	tags, _ = tagService.List(ctx, project.Context(), TagEntityError, rec.ID)
	if len(tags) != 0 {
		t.Fatalf("TagService.List(error) after remove len = %d, want 0", len(tags))
	}
}

func TestErrorServiceTagEntityRequiresEntityID(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	tagService := NewTagService(store)
	_, err := tagService.Add(ctx, project.Context(), TagEntityError, 0, "x")
	if err == nil {
		t.Fatal("TagService.Add(error, 0) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}
