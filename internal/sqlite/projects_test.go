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

	want := "a fresh project description"
	updated, err := store.UpdateProject(ctx, project.ID, domain.ProjectPatch{Description: &want})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
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

	_, err := store.UpdateProject(ctx, 4242, domain.ProjectPatch{})
	if err == nil {
		t.Fatal("UpdateProject on unknown id: expected error, got nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok {
		t.Fatalf("error type = %T, want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("coded.Code = %q, want %q", coded.Code, domain.ErrProjectNotFound)
	}
}

func TestNewProjectDefaultsToLocalScopeWithUID(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")

	project, err := store.UpsertProject(ctx, "Project", "p", "/work/p")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if project.SyncScope != domain.SyncScopeLocal {
		t.Fatalf("SyncScope = %q, want local", project.SyncScope)
	}
	var uid string
	if err := store.query(ctx).QueryRowContext(ctx, "SELECT uid FROM projects WHERE id = ?", project.ID).Scan(&uid); err != nil {
		t.Fatalf("read uid: %v", err)
	}
	if _, err := domain.ParseUID(uid); err != nil {
		t.Fatalf("uid %q invalid: %v", uid, err)
	}
}

func TestUpdateProjectSyncScopeKeepsDescription(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	project, err := store.UpsertProject(ctx, "Project", "p", "/work/p")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	description, scope := "kept", domain.SyncScopeSynced
	if _, err := store.UpdateProject(ctx, project.ID, domain.ProjectPatch{Description: &description}); err != nil {
		t.Fatalf("UpdateProject description: %v", err)
	}
	updated, err := store.UpdateProject(ctx, project.ID, domain.ProjectPatch{SyncScope: &scope})
	if err != nil {
		t.Fatalf("UpdateProject scope: %v", err)
	}
	if updated.SyncScope != domain.SyncScopeSynced || updated.Description != description {
		t.Fatalf("updated = %+v, want synced scope and kept description", updated)
	}
}
