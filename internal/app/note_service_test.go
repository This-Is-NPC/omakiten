package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestNoteServiceCreateRejectsEmptyFields(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())

	_, err := svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "free", Title: "", Body: "x"})
	if err == nil {
		t.Fatal("Create empty title: error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "free", Title: "T", Body: ""})
	if err == nil {
		t.Fatal("Create empty body: error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "", Title: "T", Body: "B"})
	if err == nil {
		t.Fatal("Create empty kind: error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestNoteServiceCreateBodySoftLimit(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())
	big := strings.Repeat("x", NoteBodySoftLimit+1)
	_, err := svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "free", Title: "T", Body: big})
	if err == nil {
		t.Fatal("Create big body: error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestNoteServiceScopeResolution(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())

	// Default scope with resolved project => project-scoped row.
	projectNote, err := svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "handoff", Title: "P", Body: "scoped"})
	if err != nil {
		t.Fatalf("Create scoped: %v", err)
	}
	if projectNote.ProjectID != project.ID {
		t.Fatalf("default scope project_id = %d, want %d", projectNote.ProjectID, project.ID)
	}

	// Explicit global ignores the resolved project.
	globalNote, err := svc.Create(ctx, CreateNoteInput{Scope: "global", Project: project.Context(), Kind: "glossary", Title: "G", Body: "everywhere"})
	if err != nil {
		t.Fatalf("Create global: %v", err)
	}
	if globalNote.ProjectID != 0 {
		t.Fatalf("global note project_id = %d, want 0", globalNote.ProjectID)
	}

	// scope=project without a resolved project rejects.
	_, err = svc.Create(ctx, CreateNoteInput{Scope: "project", Kind: "handoff", Title: "X", Body: "Y"})
	if err == nil {
		t.Fatal("Create project-scoped sans project: error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestNoteServiceDeleteRequiresConfirm(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())
	note, err := svc.Create(ctx, CreateNoteInput{Project: project.Context(), Kind: "free", Title: "T", Body: "B"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, note.ID, false); err == nil {
		t.Fatal("Delete sans confirm: error = nil")
	} else {
		assertCodedError(t, err, domain.ErrValidation)
	}

	if err := svc.Delete(ctx, note.ID, true); err != nil {
		t.Fatalf("Delete confirmed: %v", err)
	}
}

func TestNoteServiceListRejectsInvalidScope(t *testing.T) {
	ctx := context.Background()
	store, _ := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())
	_, err := svc.List(ctx, ListNotesInput{Scope: "weird"})
	if err == nil {
		t.Fatal("List invalid scope: error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}
