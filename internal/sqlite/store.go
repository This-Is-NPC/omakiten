package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"omakiten/migrations"
)

// Store wraps the SQLite connection pool with the domain-specific methods used
// by the rest of the app. The methods themselves live in topic-focused files
// (tasks.go, comments.go, bundles.go, ...) so this file stays small and
// focused on lifecycle: opening, closing, and bringing the schema up to date.
type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite is single-writer regardless of pool size, so a tiny pool with a
	// single live connection avoids "database is locked" surprises when both
	// the TUI and the MCP server share one Store. Idle conn caps at 2 so the
	// reader pool can warm up without holding extra fds open indefinitely.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	store := &Store{db: db}
	// PRAGMAs run per-connection in SQLite, so they have to fire on every conn
	// the pool hands out — not just once at Open. journal_mode=WAL is the
	// outlier (it persists to the database header), but setting it here is
	// still required so the FIRST connection is the one that flips it.
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL", // WAL-safe; full-fsync is overkill for a local CLI.
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := store.applyMigrations(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) applyMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		return err
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE version = ?", name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}


// placeholders builds an "?,?,?"-shaped string for IN clauses. Lives at the
// package root because tasks.go, comments.go and personas.go all need it for
// parameterised IN-list queries — keeping it here avoids three copies.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
