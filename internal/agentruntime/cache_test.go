package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestBundleCacheHitReturnsSamePointer asserts the Phase 3a invariant:
// repeated Resolve calls on the same project id without filesystem
// changes return the identical *ProjectRuntime. A different pointer
// would mean either a torn cache (concurrent rebuild) or a missing
// mtime short-circuit — both regressions the cache exists to prevent.
func TestBundleCacheHitReturnsSamePointer(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	if cache == nil {
		t.Fatal("Runtime.Cache() = nil")
	}
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	second, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if first != second {
		t.Fatalf("cache miss on second Resolve: first=%p second=%p", first, second)
	}
	if cache.Size() != 1 {
		t.Fatalf("Phase 3a invariant: cache size = %d, want 1", cache.Size())
	}
}

// TestBundleCacheMtimeChangeTriggersRebuild bumps the source bundle's
// mtime and confirms Resolve returns a fresh runtime. The previous
// engine must also be Stop()ed so the new one can subscribe to the
// shared bus without two listeners stealing each other's events.
func TestBundleCacheMtimeChangeTriggersRebuild(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}

	// Advance the source mtime to one second in the future. Filesystem
	// resolution is second-grained on most filesystems, so anything
	// smaller risks the stat returning the same Mtime and the test
	// silently passing for the wrong reason.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if first == second {
		t.Fatalf("Resolve did not rebuild on mtime change: same pointer %p", first)
	}
	if cache.Size() != 1 {
		t.Fatalf("cache size after rebuild = %d, want 1", cache.Size())
	}
}

// TestBundleCacheReloadForcesRebuild bypasses the mtime check and
// confirms Reload always returns a fresh runtime. Used by the TUI
// hot-reload path where the user picked a different config without
// touching the file system.
func TestBundleCacheReloadForcesRebuild(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := cache.Reload(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if first == second {
		t.Fatalf("Reload returned cached pointer instead of rebuilding")
	}
}

// TestBundleCacheConcurrentResolveSafe runs many goroutines through
// Resolve to surface any race condition the RWMutex would miss. The
// test does not assert pointer identity (a rebuild could interleave
// with another reader); only that no goroutine errors and the final
// size still satisfies the Phase 3a invariant.
func TestBundleCacheConcurrentResolveSafe(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	const readers = 32
	const iters = 50

	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if _, err := rt.Cache().Resolve(ctx, rt.defaultProjectID, rt.configPath); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Resolve error: %v", err)
		}
	}
	if rt.Cache().Size() != 1 {
		t.Fatalf("cache size after concurrent reads = %d, want 1", rt.Cache().Size())
	}
}

func openTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	rt, err := Open(ctx, Options{
		DBPath:     filepath.Join(tmp, "omakiten.db"),
		ConfigPath: filepath.Join(tmp, "config", "omakase.yaml"),
		CWD:        tmp,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return rt
}
