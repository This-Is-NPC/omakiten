package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration031NotesSchema asserts the notes table, its FTS sibling,
// and the search_index sync triggers all land after Open() runs every
// migration. The trigger count is fragile by design — if any future
// migration adds or drops a `notes_fts_*` / `search_index_notes_*`
// trigger this test fires so the author updates the expectation here
// at the same time.
func TestMigration031NotesSchema(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(context.Background(), filepath.Join(dir, "okt.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, name := range []string{"notes", "notes_tags", "notes_fts"} {
		var found string
		if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, name).Scan(&found); err != nil {
			t.Fatalf("expected %q in schema: %v", name, err)
		}
	}

	// 3 notes_fts_* triggers (ai/au/ad) + 3 search_index_notes_* triggers.
	var trigCount int
	if err := store.db.QueryRow(`
SELECT COUNT(*) FROM sqlite_master
WHERE type = 'trigger' AND (name LIKE 'notes_fts_%' OR name LIKE 'search_index_notes_%')`).Scan(&trigCount); err != nil {
		t.Fatalf("count notes triggers: %v", err)
	}
	// 6 = 3 notes_fts sync triggers + 3 search_index_notes triggers
	if trigCount != 6 {
		t.Fatalf("notes triggers = %d, want 6", trigCount)
	}

	// Partial index on (project_id, pinned) WHERE pinned = 1.
	var partialSQL string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'idx_notes_pinned'`).Scan(&partialSQL); err != nil {
		t.Fatalf("idx_notes_pinned missing: %v", err)
	}
	if partialSQL == "" {
		t.Fatalf("idx_notes_pinned has empty sql")
	}
}
