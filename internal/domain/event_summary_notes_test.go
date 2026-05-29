package domain

import "testing"

// TestSummarizeNoteEvents pins the per-event golden output for the
// note.* formatter family. The cases mirror event_summary_test.go
// rendering tables so the activity feed has one assertion site per
// emitted event type. Each row sets EventType + Payload only — the
// formatter is pure (no clock, no entity_id resolution) so the rest
// of EventRow is irrelevant.
func TestSummarizeNoteEvents(t *testing.T) {
	cases := map[string]struct {
		row  EventRow
		want string
	}{
		"note.created project handoff": {
			row:  EventRow{EventType: EventTypeNoteCreated, Payload: `{"title":"Adopt SQLite","kind":"decision","scope":"project","tags":["arch","datastore"]}`},
			want: `note created "Adopt SQLite" (decision · project) #arch #datastore`,
		},
		"note.created global without tags": {
			row:  EventRow{EventType: EventTypeNoteCreated, Payload: `{"title":"Glossary","kind":"glossary","scope":"global"}`},
			want: `note created "Glossary" (glossary · global)`,
		},
		"note.edited with title and tags": {
			row:  EventRow{EventType: EventTypeNoteEdited, Payload: `{"title":"Adopt SQLite","kind":"decision","scope":"project","tags":["arch"]}`},
			want: `note edited "Adopt SQLite" (decision · project) #arch`,
		},
		"note.edited title only": {
			row:  EventRow{EventType: EventTypeNoteEdited, Payload: `{"title":"Adopt SQLite"}`},
			want: `note edited "Adopt SQLite"`,
		},
		"note.pinned true": {
			row:  EventRow{EventType: EventTypeNotePinned, Payload: `{"title":"Adopt SQLite","kind":"decision","scope":"project","pinned":true}`},
			want: `note pinned "Adopt SQLite" (decision · project)`,
		},
		"note.pinned false": {
			row:  EventRow{EventType: EventTypeNotePinned, Payload: `{"title":"Adopt SQLite","kind":"decision","scope":"project","pinned":false}`},
			want: `note unpinned "Adopt SQLite" (decision · project)`,
		},
		"note.removed with title": {
			row:  EventRow{EventType: EventTypeNoteRemoved, Payload: `{"title":"Adopt SQLite","kind":"decision","scope":"project"}`},
			want: `note removed "Adopt SQLite" (decision · project)`,
		},
		"note.removed without title": {
			row:  EventRow{EventType: EventTypeNoteRemoved, Payload: `{}`},
			want: "note removed",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := SummarizeEvent(tc.row)
			if got != tc.want {
				t.Fatalf("SummarizeEvent(%q) =\n  got  %q\n  want %q", tc.row.EventType, got, tc.want)
			}
		})
	}
}
