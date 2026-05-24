package sqlutil_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"testing"

	_ "modernc.org/sqlite"

	"omakiten/internal/sqlite/sqlutil"
)

// openMemDB opens a fresh shared-cache in-memory DB and turns on FK
// enforcement so SQLITE_CONSTRAINT_FOREIGNKEY can fire. Each test gets
// its own DB to keep schemas isolated.
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}
	return db
}

func TestMapSQLiteError_Unique(t *testing.T) {
	ctx := context.Background()
	db := openMemDB(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE tasks (id INTEGER PRIMARY KEY, slug TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks(slug) VALUES ('alpha')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	_, err := db.ExecContext(ctx, `INSERT INTO tasks(slug) VALUES ('alpha')`)
	if err == nil {
		t.Fatalf("expected UNIQUE violation, got nil")
	}

	mapped := sqlutil.MapSQLiteError(err)
	var ce *sqlutil.ConstraintError
	if !errors.As(mapped, &ce) {
		t.Fatalf("MapSQLiteError did not surface *ConstraintError: got %T (%v)", mapped, mapped)
	}
	if ce.Violation != sqlutil.ViolationUnique {
		t.Fatalf("Violation = %v, want ViolationUnique", ce.Violation)
	}
	if ce.Table != "tasks" {
		t.Fatalf("Table = %q, want tasks", ce.Table)
	}
	if ce.Field != "slug" {
		t.Fatalf("Field = %q, want slug", ce.Field)
	}
	if !errors.Is(mapped, err) {
		t.Fatalf("mapped error must wrap the original via Unwrap; errors.Is failed")
	}
}

func TestMapSQLiteError_ForeignKey(t *testing.T) {
	ctx := context.Background()
	db := openMemDB(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE parents (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create parents: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents(id))`); err != nil {
		t.Fatalf("create children: %v", err)
	}

	_, err := db.ExecContext(ctx, `INSERT INTO children(parent_id) VALUES (999)`)
	if err == nil {
		t.Fatalf("expected FK violation, got nil")
	}

	mapped := sqlutil.MapSQLiteError(err)
	var ce *sqlutil.ConstraintError
	if !errors.As(mapped, &ce) {
		t.Fatalf("MapSQLiteError did not surface *ConstraintError: got %T (%v)", mapped, mapped)
	}
	if ce.Violation != sqlutil.ViolationForeignKey {
		t.Fatalf("Violation = %v, want ViolationForeignKey", ce.Violation)
	}
}

func TestMapSQLiteError_Check(t *testing.T) {
	ctx := context.Background()
	db := openMemDB(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, qty INTEGER CHECK (qty > 0))`); err != nil {
		t.Fatalf("create items: %v", err)
	}

	_, err := db.ExecContext(ctx, `INSERT INTO items(qty) VALUES (-1)`)
	if err == nil {
		t.Fatalf("expected CHECK violation, got nil")
	}

	mapped := sqlutil.MapSQLiteError(err)
	var ce *sqlutil.ConstraintError
	if !errors.As(mapped, &ce) {
		t.Fatalf("MapSQLiteError did not surface *ConstraintError: got %T (%v)", mapped, mapped)
	}
	if ce.Violation != sqlutil.ViolationCheck {
		t.Fatalf("Violation = %v, want ViolationCheck", ce.Violation)
	}
}

func TestMapSQLiteError_NotNull(t *testing.T) {
	ctx := context.Background()
	db := openMemDB(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE rows_ (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`); err != nil {
		t.Fatalf("create rows_: %v", err)
	}

	_, err := db.ExecContext(ctx, `INSERT INTO rows_(label) VALUES (NULL)`)
	if err == nil {
		t.Fatalf("expected NOT NULL violation, got nil")
	}

	mapped := sqlutil.MapSQLiteError(err)
	var ce *sqlutil.ConstraintError
	if !errors.As(mapped, &ce) {
		t.Fatalf("MapSQLiteError did not surface *ConstraintError: got %T (%v)", mapped, mapped)
	}
	if ce.Violation != sqlutil.ViolationNotNull {
		t.Fatalf("Violation = %v, want ViolationNotNull", ce.Violation)
	}
	if ce.Table != "rows_" {
		t.Fatalf("Table = %q, want rows_", ce.Table)
	}
	if ce.Field != "label" {
		t.Fatalf("Field = %q, want label", ce.Field)
	}
}

func TestMapSQLiteError_NonConstraintPassthrough(t *testing.T) {
	cases := []struct {
		name string
		in   error
	}{
		{"sql.ErrNoRows", sql.ErrNoRows},
		{"io.EOF", io.EOF},
		{"plain", errors.New("boom")},
		{"nil", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sqlutil.MapSQLiteError(tc.in)
			if got != tc.in {
				t.Fatalf("MapSQLiteError(%v) = %v, want passthrough", tc.in, got)
			}
		})
	}
}
