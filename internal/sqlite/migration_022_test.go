package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigration022SearchIndexCreates(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(context.Background(), filepath.Join(dir, "okt.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	var name string
	if err := store.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'search_index'`,
	).Scan(&name); err != nil {
		t.Fatalf("search_index not present: %v", err)
	}

	var trigCount int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'search_index_%'`,
	).Scan(&trigCount); err != nil {
		t.Fatalf("count triggers: %v", err)
	}
	// 21 sync triggers (7 tables × INSERT/UPDATE/DELETE) plus the
	// defensive `search_index_comments_au_demote` cleanup trigger that
	// drops stale rows when an event's `event_type` is mutated away
	// from 'comment'. Plans were added in migration 024; notes in 031.
	if trigCount != 22 {
		t.Fatalf("trigger count = %d, want 22", trigCount)
	}
}
