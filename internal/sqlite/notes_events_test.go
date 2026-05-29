package sqlite

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/domain"
)

// TestCreateNoteEmitsNoteCreated asserts CreateNote inserts a single
// `note.created` row into the events table in the same transaction
// that persisted the note. Payload carries the standard
// {title, kind, scope, tags} contract every note formatter reads.
func TestCreateNoteEmitsNoteCreated(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "decision", "Adopt SQLite", "body", true, "claude",
		[]domain.Tag{{Name: "arch", Label: "Arch"}})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	rows, err := store.ListRecentEvents(ctx, domain.EventTypeNoteCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("note.created rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.EntityType != domain.EventEntityNote || got.EntityID != note.ID {
		t.Fatalf("note.created targets %s/%d, want note/%d", got.EntityType, got.EntityID, note.ID)
	}
	if got.ProjectID != project.ID {
		t.Fatalf("note.created project_id = %d, want %d", got.ProjectID, project.ID)
	}
	payload := decodeJSON(t, got.Payload)
	if payload["title"] != "Adopt SQLite" {
		t.Fatalf("payload.title = %v", payload["title"])
	}
	if payload["kind"] != "decision" {
		t.Fatalf("payload.kind = %v", payload["kind"])
	}
	if payload["scope"] != "project" {
		t.Fatalf("payload.scope = %v", payload["scope"])
	}
	tags, _ := payload["tags"].([]any)
	if len(tags) != 1 || tags[0] != "arch" {
		t.Fatalf("payload.tags = %v, want [arch]", tags)
	}
}

// TestCreateGlobalNoteEmitsScopeGlobal locks the project_id IS NULL
// branch — global notes are created with projectID=0 and the emitted
// event must report scope="global" so the activity feed groups it
// under the global lane instead of falling through to "project".
func TestCreateGlobalNoteEmitsScopeGlobal(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	if _, err := store.CreateNote(ctx, 0, "glossary", "Terms", "g", false, "", nil); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	rows, err := store.ListRecentEvents(ctx, domain.EventTypeNoteCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("note.created rows = %d, want 1", len(rows))
	}
	if rows[0].ProjectID != 0 {
		t.Fatalf("global note event project_id = %d, want 0", rows[0].ProjectID)
	}
	payload := decodeJSON(t, rows[0].Payload)
	if payload["scope"] != "global" {
		t.Fatalf("payload.scope = %v, want global", payload["scope"])
	}
}

// TestUpdateNoteEmitsEdited covers the field-edit path: a non-pinned
// patch emits exactly one `note.edited` event and no `note.pinned`.
// Payload mirrors the post-mutation row state so consumers do not need
// a follow-up SELECT.
func TestUpdateNoteEmitsEdited(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "handoff", "T1", "body", false, "", nil)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	title := "T2"
	if _, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Title: &title}); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}

	edited, err := store.ListRecentEvents(ctx, domain.EventTypeNoteEdited, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents edited: %v", err)
	}
	if len(edited) != 1 {
		t.Fatalf("note.edited rows = %d, want 1", len(edited))
	}
	payload := decodeJSON(t, edited[0].Payload)
	if payload["title"] != "T2" {
		t.Fatalf("note.edited payload.title = %v, want T2", payload["title"])
	}

	pinned, err := store.ListRecentEvents(ctx, domain.EventTypeNotePinned, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents pinned: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("note.pinned rows = %d, want 0 (pinned flag did not change)", len(pinned))
	}
}

// TestUpdateNoteNoOpDoesNotEmitEdited locks the no-op suppression
// contract: a patch that touches no field — neither explicitly (empty
// NoteUpdate{}) nor effectively (Title pointer set to the same value;
// Tags pointer set to the same set) — emits zero note.edited rows and
// leaves updated_at untouched. The previous implementation always
// stamped updated_at + fired note.edited; the activity feed surfaced
// phantom edits whenever a caller "rewrote" a field with its current
// value.
func TestUpdateNoteNoOpDoesNotEmitEdited(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		mutate  func(prev domain.Note) domain.NoteUpdate
		seedTag []domain.Tag
	}{
		{
			name:   "empty patch",
			mutate: func(domain.Note) domain.NoteUpdate { return domain.NoteUpdate{} },
		},
		{
			name: "title pointer matches previous",
			mutate: func(prev domain.Note) domain.NoteUpdate {
				title := prev.Title
				return domain.NoteUpdate{Title: &title}
			},
		},
		{
			name:    "tags pointer matches current set",
			seedTag: []domain.Tag{{Name: "arch", Label: "Arch"}},
			mutate: func(prev domain.Note) domain.NoteUpdate {
				// Re-pass the same tags so attachNoteTagsTx writes
				// the same rows it already wrote on Create.
				same := append([]domain.Tag(nil), prev.Tags...)
				return domain.NoteUpdate{Tags: &same}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := openTestStore(t)
			project := mustUpsertProject(t, store, "P", "p", "/work/p")
			note, err := store.CreateNote(ctx, project.ID, "handoff", "T1", "body", false, "", tc.seedTag)
			if err != nil {
				t.Fatalf("CreateNote: %v", err)
			}

			refreshed, err := store.UpdateNote(ctx, note.ID, tc.mutate(note))
			if err != nil {
				t.Fatalf("UpdateNote: %v", err)
			}

			edited, err := store.ListRecentEvents(ctx, domain.EventTypeNoteEdited, 10)
			if err != nil {
				t.Fatalf("ListRecentEvents: %v", err)
			}
			if len(edited) != 0 {
				t.Fatalf("note.edited rows = %d, want 0 (no-op patch)", len(edited))
			}
			if refreshed.UpdatedAt != note.UpdatedAt {
				t.Fatalf("updated_at = %q, want unchanged %q", refreshed.UpdatedAt, note.UpdatedAt)
			}
		})
	}
}

// TestUpdateNotePinToggleCoEmitsPinned locks the dual-emit contract:
// a patch that flips `pinned` produces BOTH a note.edited row (any
// field change is an edit) and a note.pinned row (the toggle itself).
// The pinned payload carries the post-mutation `pinned` boolean so the
// formatter can distinguish pin from unpin without re-reading.
func TestUpdateNotePinToggleCoEmitsPinned(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "decision", "T1", "body", false, "", nil)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	on := true
	if _, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Pinned: &on}); err != nil {
		t.Fatalf("UpdateNote pin: %v", err)
	}

	edited, _ := store.ListRecentEvents(ctx, domain.EventTypeNoteEdited, 10)
	if len(edited) != 1 {
		t.Fatalf("note.edited rows = %d, want 1", len(edited))
	}
	pinned, _ := store.ListRecentEvents(ctx, domain.EventTypeNotePinned, 10)
	if len(pinned) != 1 {
		t.Fatalf("note.pinned rows = %d, want 1", len(pinned))
	}
	payload := decodeJSON(t, pinned[0].Payload)
	if payload["pinned"] != true {
		t.Fatalf("note.pinned payload.pinned = %v, want true", payload["pinned"])
	}

	// Toggling back to false must emit pinned=false so the formatter
	// can render "note unpinned" rather than inferring from absence.
	off := false
	if _, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Pinned: &off}); err != nil {
		t.Fatalf("UpdateNote unpin: %v", err)
	}
	pinned, _ = store.ListRecentEvents(ctx, domain.EventTypeNotePinned, 10)
	if len(pinned) != 2 {
		t.Fatalf("note.pinned rows after unpin = %d, want 2", len(pinned))
	}
	// ListRecentEvents orders newest-first.
	payload = decodeJSON(t, pinned[0].Payload)
	if payload["pinned"] != false {
		t.Fatalf("most recent note.pinned payload.pinned = %v, want false", payload["pinned"])
	}
}

// TestUpdateNoteSamePinnedDoesNotEmitPinned guards against a regression
// where passing the current pinned value (a no-op for that field) would
// still emit note.pinned. The edit event still fires because the
// UpdateNote call always stamps updated_at, but the pin lane stays
// quiet so the activity feed does not show a phantom toggle.
func TestUpdateNoteSamePinnedDoesNotEmitPinned(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "decision", "T1", "body", true, "", nil)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	on := true
	if _, err := store.UpdateNote(ctx, note.ID, domain.NoteUpdate{Pinned: &on}); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	pinned, _ := store.ListRecentEvents(ctx, domain.EventTypeNotePinned, 10)
	if len(pinned) != 0 {
		t.Fatalf("note.pinned rows = %d, want 0 (pinned value did not flip)", len(pinned))
	}
}

// TestDeleteNoteEmitsNoteRemovedWithSnapshot locks the snapshot-before-
// delete contract: note.removed fires INSIDE the same tx as the
// DELETE, and its payload carries the title that was on the row so
// activity-feed consumers still have context after the row is gone.
func TestDeleteNoteEmitsNoteRemovedWithSnapshot(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	note, err := store.CreateNote(ctx, project.ID, "decision", "DropMe", "body", false, "",
		[]domain.Tag{{Name: "arch", Label: "Arch"}})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := store.DeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	rows, err := store.ListRecentEvents(ctx, domain.EventTypeNoteRemoved, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("note.removed rows = %d, want 1", len(rows))
	}
	if rows[0].EntityID != note.ID {
		t.Fatalf("note.removed entity_id = %d, want %d", rows[0].EntityID, note.ID)
	}
	payload := decodeJSON(t, rows[0].Payload)
	if payload["title"] != "DropMe" {
		t.Fatalf("note.removed payload.title = %v, want DropMe", payload["title"])
	}
	if payload["kind"] != "decision" {
		t.Fatalf("note.removed payload.kind = %v, want decision", payload["kind"])
	}
	tags, _ := payload["tags"].([]any)
	if len(tags) != 1 || tags[0] != "arch" {
		t.Fatalf("note.removed payload.tags = %v, want [arch]", tags)
	}
}

func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode payload %q: %v", raw, err)
	}
	return out
}
