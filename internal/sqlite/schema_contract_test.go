package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestCurrentSchemaFreshOpenAndReopen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "omakiten.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("fresh Open: %v", err)
	}
	first, err := schemaFingerprint(ctx, store.db)
	if err != nil {
		t.Fatalf("fresh schema fingerprint: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("fresh Close: %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("current reopen: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	second, err := schemaFingerprint(ctx, store.db)
	if err != nil {
		t.Fatalf("reopen schema fingerprint: %v", err)
	}
	if second != first {
		t.Fatalf("schema changed on reopen:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestOpenRejectsNonCurrentDatabasesWithoutMutation(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*testing.T, context.Context, string){
		"legacy migration history": func(t *testing.T, ctx context.Context, path string) {
			execRawSchemaChange(t, ctx, path, "CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
		},
		"missing baseline marker": func(t *testing.T, ctx context.Context, path string) {
			execRawSchemaChange(t, ctx, path, "PRAGMA user_version = 0")
		},
		"extra history": func(t *testing.T, ctx context.Context, path string) {
			execRawSchemaChange(t, ctx, path, "CREATE TABLE migration_history (version TEXT PRIMARY KEY)")
		},
		"structurally incompatible": func(t *testing.T, ctx context.Context, path string) {
			execRawSchemaChange(t, ctx, path, "DROP INDEX idx_events_project_created")
		},
		"foreign database": func(t *testing.T, ctx context.Context, path string) {
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("foreign sql.Open: %v", err)
			}
			defer func() { _ = db.Close() }()
			if _, err := db.ExecContext(ctx, "CREATE TABLE foreign_data (id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
				t.Fatalf("foreign seed: %v", err)
			}
		},
	}

	for name, prepare := range cases {
		name, prepare := name, prepare
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNonCurrentDatabaseRejected(t, name, prepare)
		})
	}
}

func assertNonCurrentDatabaseRejected(t *testing.T, name string, prepare func(*testing.T, context.Context, string)) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "omakiten.db")
	if name != "foreign database" {
		seedCurrentDatabase(t, ctx, path)
	}
	prepare(t, ctx, path)
	before := schemaFileSnapshot(t, path)
	_, err := Open(ctx, path)
	if err == nil {
		t.Fatal("Open succeeded for a non-current database")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation ||
		coded.Message != "database is not a current compatible Omakiten database" {
		t.Fatalf("Open error = %q, want stable validation error", err)
	}
	if !strings.Contains(coded.Details["reason"].(string), "new database path") {
		t.Fatalf("Open error details = %#v, want actionable reason", coded.Details)
	}
	if after := schemaFileSnapshot(t, path); after != before {
		t.Fatalf("rejected database mutated:\nbefore=%s\nafter=%s", before, after)
	}
}

func seedCurrentDatabase(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("seed Close: %v", err)
	}
}

func execRawSchemaChange(t *testing.T, ctx context.Context, path, statement string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, statement); err != nil {
		t.Fatalf("raw schema change %q: %v", statement, err)
	}
}

func schemaFileSnapshot(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("snapshot sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	fingerprint, err := schemaFingerprint(context.Background(), db)
	if err != nil {
		t.Fatalf("snapshot fingerprint: %v", err)
	}
	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("snapshot journal mode: %v", err)
	}
	return fingerprint + "\njournal=" + journal
}
