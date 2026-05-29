package sqlite

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestCreateNoteRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "decision", "Adopt SQLite", "We adopt SQLite for local persistence.", true, "claude-opus-4-7",
		[]domain.Tag{{Name: "arch", Label: "Arch"}, {Name: "datastore", Label: "Datastore"}})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if note.ID == 0 || note.Kind != "decision" || note.Title != "Adopt SQLite" || !note.Pinned {
		t.Fatalf("CreateNote returned unexpected row: %+v", note)
	}
	if note.ProjectID != project.ID {
		t.Fatalf("CreateNote project_id = %d, want %d", note.ProjectID, project.ID)
	}
	if len(note.Tags) != 2 {
		t.Fatalf("CreateNote tags len = %d, want 2", len(note.Tags))
	}
	if note.AuthorModel != "claude-opus-4-7" {
		t.Fatalf("author_model = %q, want claude-opus-4-7", note.AuthorModel)
	}

	loaded, err := store.NoteByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("NoteByID: %v", err)
	}
	if loaded.Body != note.Body {
		t.Fatalf("NoteByID body = %q, want %q", loaded.Body, note.Body)
	}
	if len(loaded.Tags) != 2 {
		t.Fatalf("NoteByID tags len = %d, want 2", len(loaded.Tags))
	}
}

func TestCreateGlobalNote(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	note, err := store.CreateNote(ctx, 0, "glossary", "Terms", "Global glossary.", false, "", nil)
	if err != nil {
		t.Fatalf("CreateNote global: %v", err)
	}
	if note.ProjectID != 0 {
		t.Fatalf("global note project_id = %d, want 0", note.ProjectID)
	}
}

func TestUpdateNotePatchSemantics(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "handoff", "T1", "Body v1", false, "", []domain.Tag{{Name: "a", Label: "A"}})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	title := "T2"
	pinned := true
	tags := []domain.Tag{{Name: "b", Label: "B"}, {Name: "c", Label: "C"}}
	updated, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Title: &title, Pinned: &pinned, Tags: &tags})
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if updated.Title != "T2" || !updated.Pinned {
		t.Fatalf("UpdateNote returned %+v", updated)
	}
	if updated.Body != "Body v1" {
		t.Fatalf("UpdateNote body changed unexpectedly: %q", updated.Body)
	}
	names := tagNamesByName(updated.Tags)
	if !names["b"] || !names["c"] || names["a"] {
		t.Fatalf("UpdateNote tags did not replace cleanly: %+v", updated.Tags)
	}

	// Empty tag replacement clears every tag.
	empty := []domain.Tag{}
	cleared, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Tags: &empty})
	if err != nil {
		t.Fatalf("UpdateNote clear tags: %v", err)
	}
	if len(cleared.Tags) != 0 {
		t.Fatalf("UpdateNote tags not cleared: %+v", cleared.Tags)
	}
}

func TestListNotesScopeFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	projectA := mustUpsertProject(t, store, "A", "a", "/work/a")
	projectB := mustUpsertProject(t, store, "B", "b", "/work/b")

	if _, err := store.CreateNote(ctx, projectA.ID, "handoff", "A-handoff", "ax", false, "", nil); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := store.CreateNote(ctx, projectB.ID, "handoff", "B-handoff", "bx", false, "", nil); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	if _, err := store.CreateNote(ctx, 0, "glossary", "Global", "g", false, "", nil); err != nil {
		t.Fatalf("seed G: %v", err)
	}

	global, err := store.ListNotes(ctx, domain.NoteFilter{Scope: domain.NoteScopeGlobal})
	if err != nil {
		t.Fatalf("ListNotes global: %v", err)
	}
	if len(global) != 1 || global[0].ProjectID != 0 {
		t.Fatalf("global notes = %+v", global)
	}

	scopedA, err := store.ListNotes(ctx, domain.NoteFilter{Scope: domain.NoteScopeProject, ProjectID: projectA.ID})
	if err != nil {
		t.Fatalf("ListNotes A: %v", err)
	}
	if len(scopedA) != 1 || scopedA[0].ProjectID != projectA.ID {
		t.Fatalf("project A notes = %+v", scopedA)
	}

	// scope=any + project_id constrains to that project plus none of global —
	// matches "show me what's in project A only" semantics callers use.
	anyA, err := store.ListNotes(ctx, domain.NoteFilter{Scope: domain.NoteScopeAny, ProjectID: projectA.ID})
	if err != nil {
		t.Fatalf("ListNotes any+A: %v", err)
	}
	if len(anyA) != 1 || anyA[0].ProjectID != projectA.ID {
		t.Fatalf("any+A notes = %+v", anyA)
	}

	all, err := store.ListNotes(ctx, domain.NoteFilter{Scope: domain.NoteScopeAny})
	if err != nil {
		t.Fatalf("ListNotes all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all notes len = %d, want 3", len(all))
	}
}

func TestListNotesTagIntersection(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	if _, err := store.CreateNote(ctx, project.ID, "free", "N1", "x", false, "", []domain.Tag{{Name: "alpha", Label: "Alpha"}, {Name: "beta", Label: "Beta"}}); err != nil {
		t.Fatalf("seed N1: %v", err)
	}
	if _, err := store.CreateNote(ctx, project.ID, "free", "N2", "y", false, "", []domain.Tag{{Name: "alpha", Label: "Alpha"}}); err != nil {
		t.Fatalf("seed N2: %v", err)
	}

	hits, err := store.ListNotes(ctx, domain.NoteFilter{Scope: domain.NoteScopeAny, Tags: []string{"alpha", "beta"}})
	if err != nil {
		t.Fatalf("ListNotes tags: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "N1" {
		t.Fatalf("tag intersection returned %+v, want only N1", hits)
	}
}

func TestProjectDeleteCascadesNotes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	scoped, err := store.CreateNote(ctx, project.ID, "handoff", "scoped", "x", false, "", nil)
	if err != nil {
		t.Fatalf("seed scoped: %v", err)
	}
	global, err := store.CreateNote(ctx, 0, "glossary", "global", "g", false, "", nil)
	if err != nil {
		t.Fatalf("seed global: %v", err)
	}

	if err := store.DeleteProject(ctx, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if _, err := store.NoteByID(ctx, scoped.ID); err == nil {
		t.Fatalf("NoteByID scoped after cascade: expected not found")
	}
	if _, err := store.NoteByID(ctx, global.ID); err != nil {
		t.Fatalf("NoteByID global after cascade: %v (global note should survive)", err)
	}
}

func TestDeleteNoteRemovesRow(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "free", "x", "y", false, "", nil)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := store.DeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if _, err := store.NoteByID(ctx, note.ID); err == nil {
		t.Fatalf("expected not found after delete")
	}
	// Second delete must surface validation_error, not a silent success.
	if err := store.DeleteNote(ctx, note.ID); err == nil {
		t.Fatalf("second delete should error")
	}
}

func TestNotesFTSReturnsHit(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	if _, err := store.CreateNote(ctx, project.ID, "runbook", "Postgres failover", "Use pg_ctl promote when the primary loses quorum.", false, "", nil); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	// Per-entity FTS via notes_fts MATCH.
	var rowid int64
	var title string
	if err := store.db.QueryRow(`SELECT rowid, title FROM notes_fts WHERE notes_fts MATCH 'failover' LIMIT 1`).Scan(&rowid, &title); err != nil {
		t.Fatalf("notes_fts MATCH: %v", err)
	}
	if !strings.Contains(title, "Postgres") {
		t.Fatalf("notes_fts MATCH returned %q", title)
	}

	// Unified search_index surfaces notes under entity_type='note'.
	hits, err := store.Search(ctx, "failover", 0, []domain.SearchEntityType{domain.SearchEntityNote}, 50)
	if err != nil {
		t.Fatalf("Search note: %v", err)
	}
	if len(hits) == 0 || hits[0].EntityType != domain.SearchEntityNote {
		t.Fatalf("Search note returned %+v", hits)
	}
}

func TestNotesUpdateRefreshesFTS(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "free", "alpha", "original keyword apricot", false, "", nil)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	newBody := "fresh keyword banana"
	if _, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Body: &newBody}); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}

	hits, err := store.Search(ctx, "banana", 0, []domain.SearchEntityType{domain.SearchEntityNote}, 50)
	if err != nil {
		t.Fatalf("Search after update: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("Search(banana) empty after update")
	}

	stale, err := store.Search(ctx, "apricot", 0, []domain.SearchEntityType{domain.SearchEntityNote}, 50)
	if err != nil {
		t.Fatalf("Search stale: %v", err)
	}
	for _, h := range stale {
		if h.ID == note.ID {
			t.Fatalf("stale apricot hit survives update: %+v", h)
		}
	}
}

func tagNamesByName(tags []domain.Tag) map[string]bool {
	out := map[string]bool{}
	for _, t := range tags {
		out[t.Name] = true
	}
	return out
}
