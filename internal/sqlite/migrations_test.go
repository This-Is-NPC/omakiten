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

	// Sanity: the Phase 1 columns from 002_entities.sql must exist on the
	// reopened DB.
	rows, err := store2.db.QueryContext(ctx, "PRAGMA table_info(skills)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(skills) error = %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"description", "body", "source_path"} {
		if !cols[want] {
			t.Fatalf("skills table missing column %q after reopen", want)
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
