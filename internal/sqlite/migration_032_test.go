package sqlite

import (
	"context"
	"strings"
	"testing"
)

// TestMigration032AddsEventCommentColumns asserts the four note-like columns
// the comment log gained land on a fresh `events` table.
func TestMigration032AddsEventCommentColumns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	want := map[string]bool{
		"kind":       false,
		"title":      false,
		"pinned":     false,
		"updated_at": false,
	}
	rows, err := store.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('events')`)
	if err != nil {
		t.Fatalf("pragma_table_info(events): %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for col, seen := range want {
		if !seen {
			t.Fatalf("events missing column %q after migration 032", col)
		}
	}
}

// TestMigration032DropsNotesEntity asserts the unreleased notes entity is gone:
// its tables, FTS table, and its search_index sync triggers.
func TestMigration032DropsNotesEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	for _, tbl := range []string{"notes", "notes_tags", "notes_fts"} {
		var n int
		if err := store.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM sqlite_master WHERE type IN ('table','view') AND name = ?`, tbl,
		).Scan(&n); err != nil {
			t.Fatalf("lookup table %q: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("table %q still present after migration 032", tbl)
		}
	}

	for _, trg := range []string{
		"search_index_notes_ai", "search_index_notes_au", "search_index_notes_ad",
		"notes_fts_ai", "notes_fts_au", "notes_fts_ad",
	} {
		var n int
		if err := store.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trg,
		).Scan(&n); err != nil {
			t.Fatalf("lookup trigger %q: %v", trg, err)
		}
		if n != 0 {
			t.Fatalf("trigger %q still present after migration 032", trg)
		}
	}
}

// TestMigration032CommentScopeAndSearch exercises the recast scope model and
// the title-aware comment search_index triggers: a project-scoped comment
// (entity_type='project', entity_id=projectID) and a universal comment
// (entity_type='universal', entity_id NULL, project_id NULL) both insert
// cleanly into the FK-less events table and land in search_index with the
// title indexed.
func TestMigration032CommentScopeAndSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)

	const projectID = 7

	// Project-scoped comment. Title carries a word that is NOT in the body so
	// a hit on it proves the trigger indexes title.
	res, err := store.db.ExecContext(ctx,
		`INSERT INTO events(entity_type, entity_id, project_id, event_type, body, title)
		 VALUES ('project', ?, ?, 'comment', 'project body alpha', 'projheading zulu')`,
		projectID, projectID,
	)
	if err != nil {
		t.Fatalf("insert project comment: %v", err)
	}
	projComment, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("project comment id: %v", err)
	}

	// Universal comment: no parent entity, no project. FK-less events must
	// accept entity_id NULL and project_id NULL.
	res, err = store.db.ExecContext(ctx,
		`INSERT INTO events(entity_type, entity_id, project_id, event_type, body, title)
		 VALUES ('universal', NULL, NULL, 'comment', 'universal body bravo', 'univheading yankee')`,
	)
	if err != nil {
		t.Fatalf("insert universal comment: %v", err)
	}
	univComment, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("universal comment id: %v", err)
	}

	// search_index rows: title word and project_id must be indexed.
	assertIndexed := func(entityID int64, titleWord string, wantProjectID int) {
		var content string
		var pid int
		if err := store.db.QueryRowContext(ctx,
			`SELECT content, project_id FROM search_index
			 WHERE entity_type = 'comment' AND entity_id = ?`, entityID,
		).Scan(&content, &pid); err != nil {
			t.Fatalf("search_index row for comment %d: %v", entityID, err)
		}
		if !strings.Contains(content, titleWord) {
			t.Fatalf("comment %d indexed content = %q, want title word %q present", entityID, content, titleWord)
		}
		if pid != wantProjectID {
			t.Fatalf("comment %d indexed project_id = %d, want %d", entityID, pid, wantProjectID)
		}
	}

	// project comment: project_id indexed verbatim.
	assertIndexed(projComment, "projheading", projectID)
	// universal comment: COALESCE(project_id, 0) -> 0 for the NULL project.
	assertIndexed(univComment, "univheading", 0)

	// FTS MATCH on a title-only word must surface the universal comment, proving
	// the title is searchable through the porter index, not just stored.
	var hitID int64
	if err := store.db.QueryRowContext(ctx,
		`SELECT entity_id FROM search_index
		 WHERE entity_type = 'comment' AND search_index MATCH 'yankee'`,
	).Scan(&hitID); err != nil {
		t.Fatalf("MATCH 'yankee': %v", err)
	}
	if hitID != univComment {
		t.Fatalf("MATCH 'yankee' returned entity_id %d, want %d", hitID, univComment)
	}
}
