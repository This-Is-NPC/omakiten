// Package runtimecache wires runtime fixtures for delivery tests.
package runtimecache

import (
	"context"
	"fmt"

	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/operation"
)

type snapshotRepo interface {
	operation.Repository
	Snapshot() *config.Snapshot
}

// Install returns a wired *agentruntime.BundleCache with one pre-built
// entry whose Snapshot is snap. projectID must match the
// Repositories.ProjectID the test sets — TUI tests that leave
// ProjectID at its zero value should pass 0 here so the cache lookup
// hits the installed entry.
func Install(projectID int64, snap *config.Snapshot) *FixtureCache {
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(projectID, &agentruntime.ProjectRuntime{Snapshot: snap})
	return &FixtureCache{BundleCache: cache}
}

// InstallWithStore wires Snapshot + operation.Service so TUI tests that
// route mutations through the facade have a Service on the cache entry.
func InstallWithStore(projectID int64, store snapshotRepo) *FixtureCache {
	return InstallWithStoreSnap(projectID, store, store.Snapshot())
}

// InstallWithStoreSnap is InstallWithStore with an explicit snapshot.
func InstallWithStoreSnap(projectID int64, store operation.Repository, snap *config.Snapshot) *FixtureCache {
	svc := operation.NewService(store, contract.ProjectSelector{})
	svc.SetSnapshot(snap)
	cache := agentruntime.NewBundleCache(nil, nil, nil)
	cache.Install(projectID, &agentruntime.ProjectRuntime{Snapshot: snap, Service: svc})
	return &FixtureCache{BundleCache: cache}
}

// bundleLoader is the Load surface RefreshFromEditor needs. *app.BundleEditor
// and the TUI BundleEditor port both satisfy it.
type bundleLoader interface {
	Load() (config.Bundle, error)
}

// RefreshFromEditor re-installs the cache entry with a snapshot rebuilt
// from the editor's current view. Tests that mutate config via app
// services without going through the TUI edit→rotateSnapshotAfterEdit
// loop call this to mirror production's BundleCache.Reload effect.
// The existing operation.Service (if any) is preserved and rotated onto
// the new snapshot so facade-backed TUI tests keep working.
func RefreshFromEditor(cache any, projectID int64, editor bundleLoader) error {
	bundle, err := editor.Load()
	if err != nil {
		return err
	}
	snap := config.BuildSnapshot(bundle)
	runtime := &agentruntime.ProjectRuntime{Snapshot: snap}
	if existing := underlying(cache).Get(projectID); existing != nil {
		runtime.Service = existing.Service
		runtime.Editor = existing.Editor
		runtime.PreviousSnapshot = existing.PreviousSnapshot
		if runtime.Service != nil {
			runtime.Service.SetSnapshot(snap)
		}
	}
	return underlying(cache).Install(projectID, runtime)
}

// SetEntityRepos wires authoring ports onto the facade so TUI tests can
// call AddSkill/AddLaw/AddPersona without naming app types (D1 / 3.2).
func SetEntityRepos(svc *operation.Service, editor *app.BundleEditor, files app.EntityFileWriter, slugger app.Slugifier) {
	if svc == nil {
		return
	}
	svc.SetEntityRepos(editor, files, slugger)
}

// FixtureCache reloads an in-memory editor without a production SQLite connection.
type FixtureCache struct{ *agentruntime.BundleCache }

func (c *FixtureCache) ReloadView(ctx context.Context, id int64, path string) (*contract.RuntimeView, error) {
	entry := c.Get(id)
	if entry == nil || entry.Editor == nil {
		return nil, fmt.Errorf("fixture editor is not installed")
	}
	if err := RefreshFromEditor(c, id, entry.Editor); err != nil {
		return nil, err
	}
	return c.View(id), nil
}
func underlying(cache any) *agentruntime.BundleCache {
	switch c := cache.(type) {
	case *FixtureCache:
		return c.BundleCache
	case *agentruntime.BundleCache:
		return c
	default:
		panic("unexpected fixture cache")
	}
}

// InstallRuntime replaces one fixture entry through its concrete runtime owner.
func InstallRuntime(cache any, id int64, entry *agentruntime.ProjectRuntime) error {
	return underlying(cache).Install(id, entry)
}

// Service returns the concrete service for fixture setup only.
func Service(cache any, id int64) *operation.Service { return underlying(cache).Get(id).Service }
