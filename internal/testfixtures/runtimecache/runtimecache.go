// Package runtimecache provides a minimal *agentruntime.BundleCache for
// TUI / CLI tests that drive code through the cache accessor without
// going through the full BundleCache.Resolve build path.
//
// Spec note: Phase 2-bis dropped the Repositories.Snapshot test-only
// escape hatch the TUI used to plug a per-project *config.Snapshot
// without a real cache. Every TUI test now wires a real BundleCache via
// Install so the runtime-side accessor (r.Cache.Get(r.ProjectID)) is
// the single source of truth in production AND in tests.
package runtimecache

import (
	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
)

// Install returns a wired *agentruntime.BundleCache with one pre-built
// entry whose Snapshot is snap. projectID must match the
// Repositories.ProjectID the test sets — TUI tests that leave
// ProjectID at its zero value should pass 0 here so the cache lookup
// hits the installed entry.
//
// The cache is constructed with nil store/bus/cs because callers that
// only exercise Snapshot reads never trigger rebuild. Tests that
// additionally exercise the reload path must construct a real
// BundleCache directly (see agentruntime tests for the pattern).
func Install(projectID int64, snap *config.Snapshot) *agentruntime.BundleCache {
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(projectID, &agentruntime.ProjectRuntime{Snapshot: snap})
	return cache
}

// InstallWithPrevious extends Install with the previous Snapshot pointer
// so orphan-flow tests can exercise both snapshot reads through the
// same runtime accessor.
func InstallWithPrevious(projectID int64, current, previous *config.Snapshot) *agentruntime.BundleCache {
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(projectID, &agentruntime.ProjectRuntime{Snapshot: current, PreviousSnapshot: previous})
	return cache
}

// RefreshFromEditor re-installs the cache entry with a snapshot rebuilt
// from the editor's current view. Tests that mutate config via app
// services without going through the TUI edit→rotateSnapshotAfterEdit
// loop call this to mirror production's BundleCache.Reload effect.
func RefreshFromEditor(cache *agentruntime.BundleCache, projectID int64, editor *app.BundleEditor) error {
	bundle, err := editor.Load()
	if err != nil {
		return err
	}
	cache.Install(projectID, &agentruntime.ProjectRuntime{Snapshot: config.BuildSnapshot(bundle)})
	return nil
}
