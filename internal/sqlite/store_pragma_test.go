package sqlite

import (
	"context"
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
