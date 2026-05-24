package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestOpenWithOptionsAppliesCacheSizeAndMmapPragmas pins the W7 #225
// pragma wiring: PRAGMA cache_size returns the negative kilobyte form
// the Store passed in, and PRAGMA mmap_size returns the requested byte
// count. Both must surface on the first connection the pool hands out
// (PRAGMAs are per-connection in SQLite).
func TestOpenWithOptionsAppliesCacheSizeAndMmapPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.db")
	store, err := OpenWithOptions(ctx, path, Options{
		BusyTimeoutMs: 5000,
		CacheSizeKB:   2048,
		MmapSizeBytes: 268435456, // 256 MiB
	})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	defer func() { _ = store.Close() }()

	var cacheSize int64
	if err := store.db.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize); err != nil {
		t.Fatalf("query cache_size: %v", err)
	}
	if cacheSize != -2048 {
		t.Fatalf("PRAGMA cache_size = %d, want -2048 (negative kilobyte form)", cacheSize)
	}

	var mmapSize int64
	if err := store.db.QueryRowContext(ctx, "PRAGMA mmap_size").Scan(&mmapSize); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmapSize != 268435456 {
		t.Fatalf("PRAGMA mmap_size = %d, want 268435456", mmapSize)
	}
}

// TestOpenWithOptionsFallsBackToKitCacheSize asserts the zero-value
// fallback: a test that does not thread the bundle's CacheSizeKB
// still gets a sensible page cache from the embedded kit YAML (today
// 1024 KiB), so test paths don't open at SQLite's 2 MiB default.
func TestOpenWithOptionsFallsBackToKitCacheSize(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.db")
	store, err := OpenWithOptions(ctx, path, Options{BusyTimeoutMs: 5000})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	defer func() { _ = store.Close() }()

	var cacheSize int64
	if err := store.db.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize); err != nil {
		t.Fatalf("query cache_size: %v", err)
	}
	if cacheSize >= 0 {
		t.Fatalf("PRAGMA cache_size = %d, want negative (KiB form); got the SQLite default", cacheSize)
	}
}

// TestApplyPragmasIssuesAllThreeKnobs pins the W8-3 helper contract:
// applyPragmas issues PRAGMA busy_timeout / cache_size / mmap_size with
// the same value semantics OpenWithOptions used to issue inline, and
// each PRAGMA persists on the connection so a subsequent QueryRow
// reads back the same value.
func TestApplyPragmasIssuesAllThreeKnobs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pragmaset.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	// Single-connection pool so the PRAGMAs we issue and the values we
	// scan back hit the same underlying connection — modernc.org/sqlite
	// PRAGMAs are sticky per-connection, not per-handle.
	db.SetMaxOpenConns(1)

	if err := applyPragmas(ctx, db, pragmaSet{
		BusyTimeoutMs: 7000,
		CacheSizeKB:   4096,
		MmapSizeBytes: 134217728, // 128 MiB
	}); err != nil {
		t.Fatalf("applyPragmas: %v", err)
	}

	var busy int64
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busy != 7000 {
		t.Fatalf("PRAGMA busy_timeout = %d, want 7000", busy)
	}

	var cache int64
	if err := db.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cache); err != nil {
		t.Fatalf("query cache_size: %v", err)
	}
	if cache != -4096 {
		t.Fatalf("PRAGMA cache_size = %d, want -4096 (negative KiB form)", cache)
	}

	var mmap int64
	if err := db.QueryRowContext(ctx, "PRAGMA mmap_size").Scan(&mmap); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmap != 134217728 {
		t.Fatalf("PRAGMA mmap_size = %d, want 134217728", mmap)
	}
}

// TestApplyPragmasSkipsZeroValues pins the optional-knob contract:
// BusyTimeoutMs <= 0 and CacheSizeKB <= 0 skip the issue so ApplyConfig
// can hand a partially-filled pragmaSet without overwriting whatever
// Open already established on the connection. MmapSizeBytes == 0 is a
// valid value (mmap disabled) so it MUST still issue.
func TestApplyPragmasSkipsZeroValues(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pragmaskip.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	// Seed: establish a busy_timeout via Open's inline shape.
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 3000"); err != nil {
		t.Fatalf("seed busy_timeout: %v", err)
	}

	// Apply with all zeros for the cache+busy knobs (skip) but a valid
	// mmap_size = 0 (issue). Helper must not zero the seeded busy_timeout.
	if err := applyPragmas(ctx, db, pragmaSet{
		BusyTimeoutMs: 0,
		CacheSizeKB:   0,
		MmapSizeBytes: 0,
	}); err != nil {
		t.Fatalf("applyPragmas: %v", err)
	}

	var busy int64
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busy != 3000 {
		t.Fatalf("PRAGMA busy_timeout = %d, want 3000 (skip preserved seed)", busy)
	}

	var mmap int64
	if err := db.QueryRowContext(ctx, "PRAGMA mmap_size").Scan(&mmap); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmap != 0 {
		t.Fatalf("PRAGMA mmap_size = %d, want 0 (mmap disabled)", mmap)
	}
}

// TestApplyPragmasSkipsNegativeMmap pins the MmapSizeBytes < 0 branch:
// the helper treats negative as "skip" so a caller passing -1 does not
// flip mmap to a nonsense value via SQLite's type coercion. Without
// this guard, fmt.Sprintf("%d", -1) would produce "PRAGMA mmap_size =
// -1" which SQLite accepts and stores literally.
func TestApplyPragmasSkipsNegativeMmap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pragmaneg.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	// Seed mmap to a known positive value.
	if _, err := db.ExecContext(ctx, "PRAGMA mmap_size = 65536"); err != nil {
		t.Fatalf("seed mmap_size: %v", err)
	}

	if err := applyPragmas(ctx, db, pragmaSet{MmapSizeBytes: -1}); err != nil {
		t.Fatalf("applyPragmas: %v", err)
	}

	var mmap int64
	if err := db.QueryRowContext(ctx, "PRAGMA mmap_size").Scan(&mmap); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmap != 65536 {
		t.Fatalf("PRAGMA mmap_size = %d, want 65536 (negative skipped)", mmap)
	}
}
