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

// TestNoteServiceEditTagLabelMatchesCreate locks the applyTagLabel
// extraction: Create and Edit normalise the same raw input into the
// same (Name, Label) pair. Before the fix Edit passed normalised names
// straight through and the sqlite layer set Label = Name, dropping the
// original casing the user typed.
func TestNoteServiceEditTagLabelMatchesCreate(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewNoteService(store, store.Snapshot())

	created, err := svc.Create(ctx, CreateNoteInput{
		Project: project.Context(),
		Kind:    "free",
		Title:   "T",
		Body:    "B",
		Tags:    []string{"Architecture"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created.Tags) != 1 || created.Tags[0].Name != "architecture" || created.Tags[0].Label != "Architecture" {
		t.Fatalf("Create tag = %+v, want {architecture, Architecture}", created.Tags)
	}

	replacement := []string{"Architecture"}
	edited, err := svc.Edit(ctx, EditNoteInput{NoteID: created.ID, Tags: &replacement})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if len(edited.Tags) != 1 {
		t.Fatalf("Edit tags len = %d, want 1: %+v", len(edited.Tags), edited.Tags)
	}
	if edited.Tags[0].Name != created.Tags[0].Name || edited.Tags[0].Label != created.Tags[0].Label {
		t.Fatalf("Edit tag drift: Create=%+v Edit=%+v", created.Tags[0], edited.Tags[0])
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
