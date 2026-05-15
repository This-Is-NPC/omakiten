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

// TestBundleCacheResolveEmptyPathRebuildOnMtimeChange pins the bug
// fix for Resolve(ctx, id, "") with mtime drift on a cached entry.
// Before the fix, Resolve routed the original empty configPath into
// rebuild and bailed with "bundle cache: configPath is required";
// after, Resolve falls back to the cached SourcePath so the rebuild
// succeeds.
func TestBundleCacheResolveEmptyPathRebuildOnMtimeChange(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	first, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve seed: %v", err)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(rt.configPath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second, err := cache.Resolve(ctx, rt.defaultProjectID, "")
	if err != nil {
		t.Fatalf("Resolve with empty configPath after mtime change: %v", err)
	}
	if first == second {
		t.Fatal("Resolve did not rebuild against cached SourcePath")
	}
	if second.SourcePath != rt.configPath {
		t.Fatalf("rebuild SourcePath = %q, want %q", second.SourcePath, rt.configPath)
	}
}

// TestBundleCachePerProjectNotificationActionIsolation pins the Phase
// 3d invariant: two ProjectRuntime entries (different project ids)
// hold distinct ActionRegistry and NotificationShowAction instances.
// The engine.projectID filter alone is not enough; if both engines
// dispatched against the same NotificationShowAction, a project A
// notification slug would resolve against B's catalog. Distinct
// actions with distinct snapshot pointers prove isolation at the
// action layer.
func TestBundleCachePerProjectNotificationActionIsolation(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	cache := rt.Cache()
	prA, err := cache.Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	// Resolve a second project entry against the same source so we
	// exercise the registry/snapshot isolation independent of bundle
	// content. Two distinct entries on the same yaml is the minimum
	// shape the cache supports today.
	prB, err := cache.Resolve(ctx, rt.defaultProjectID+1, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve B: %v", err)
	}
	if prA == prB {
		t.Fatal("Resolve returned the same entry for different ids")
	}
	if prA.NotificationAction == nil || prB.NotificationAction == nil {
		t.Fatal("ProjectRuntime missing NotificationAction")
	}
	if prA.NotificationAction == prB.NotificationAction {
		t.Fatal("per-project NotificationAction pointer aliased — actions would cross-fire on shared sender")
	}
	if prA.ActionRegistry == prB.ActionRegistry {
		t.Fatal("per-project ActionRegistry pointer aliased — Register on A would leak into B")
	}
	if prA.HooksEngine == prB.HooksEngine {
		t.Fatal("per-project HooksEngine pointer aliased")
	}
}

// TestBundleCacheReloadPreservesProjectSelector pins the Phase 3a
// invariant the original ship missed: a mtime-driven Reload must
// carry the boot-resolved ProjectSelector into the freshly built
// agent.Service. Without it, calls without explicit project args
// fall to a zero selector after the first rebuild.
func TestBundleCacheReloadPreservesProjectSelector(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	bootSelector := rt.Service().Selector()
	if bootSelector.CWD == "" {
		t.Fatal("test fixture sanity: boot selector CWD empty")
	}

	if _, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := rt.Service().Selector()
	if got != bootSelector {
		t.Fatalf("Reload lost selector: got %+v want %+v", got, bootSelector)
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
