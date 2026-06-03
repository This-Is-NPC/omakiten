package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestUpdateProjectDescriptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")

	project, err := store.UpsertProject(ctx, "Project", "p", "/work/p")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	const want = "a fresh project description"
	updated, err := store.UpdateProjectDescription(ctx, project.ID, want)
	if err != nil {
		t.Fatalf("UpdateProjectDescription: %v", err)
	}
	if updated.Description != want {
		t.Fatalf("returned Description = %q, want %q", updated.Description, want)
	}

	reread, err := store.FindProjectByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("FindProjectByID: %v", err)
	}
	if reread.Description != want {
		t.Fatalf("re-read Description = %q, want %q", reread.Description, want)
	}
}

func TestUpdateProjectDescriptionUnknownID(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")

	_, err := store.UpdateProjectDescription(ctx, 4242, "nobody home")
	if err == nil {
		t.Fatal("UpdateProjectDescription on unknown id: expected error, got nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok {
		t.Fatalf("error type = %T, want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("coded.Code = %q, want %q", coded.Code, domain.ErrProjectNotFound)
	}
}
