package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigrationsAreIdempotent guards against the regression risk that opening
// a DB twice would re-apply 002_entities.sql and crash on a duplicate column
// error. The schema_migrations guard inside applyMigrations should make this a
// no-op on the second open.
func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() #1 error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() #1 error = %v", err)
	}

	store2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() #2 error = %v (migrations not idempotent)", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	// Sanity: every config table dropped by migration 020 must stay
	// dropped on a fresh open. Idempotent runs of the migration set
	// must converge on the same schema; a regression that re-creates
	// any of these would let stale rows reappear after a config edit.
	for _, dropped := range []string{
		"skills", "personas", "persona_skills", "laws",
		"workflows", "workflow_buckets", "workflow_transitions",
		"settings", "config_bundles",
	} {
		var count int
		if err := store2.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, dropped).Scan(&count); err != nil {
			t.Fatalf("sqlite_master lookup for %q error = %v", dropped, err)
		}
		if count != 0 {
			t.Fatalf("dropped config table %q reappeared after migrations re-run", dropped)
		}
	}
}

func TestMigrationsRecordSchemaVersions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	rows, err := store.db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("Query schema_migrations error = %v", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		versions = append(versions, v)
	}

	if len(versions) == 0 {
		t.Fatal("schema_migrations is empty")
	}
	// Verify migrations are recorded in lexical order
	for i := 1; i < len(versions); i++ {
		if versions[i] < versions[i-1] {
			t.Fatalf("schema_migrations not in lexical order: %v", versions)
		}
	}
}
