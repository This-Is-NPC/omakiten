// Package snapstore wraps a *sqlite.Store with per-fixture snapshot
// machinery. Phase 2-bis moved ownership of the per-project Snapshot
// out of the SQL adapter into agentruntime.ProjectRuntime; production
// callers reach it through the runtime. Tests that drive the Store
// directly use SnapStore so they can still rotate / inspect the
// snapshot pair without dragging that state back onto the adapter.
//
// The package is intentionally small and lives outside the parent
// testfixtures package so the sqlite test suite (which imports
// testfixtures for LoadBundle / CanonicalRegistry) can import this
// without creating a cycle.
package snapstore

import (
	"context"
	"sync"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/sqlite"
)

// Store bundles a *sqlite.Store with the per-project Snapshot
// machinery. The *sqlite.Store embed exposes every domain method
// directly (CreateTask, MoveTask, ListTasks, …). Snapshot /
// PreviousSnapshot return the per-fixture snapshots; ImportBundle
// rotates them by rebuilding from the supplied bundle. The helper
// holds its own rotation lock locally so concurrent ImportBundle /
// Snapshot calls stay race-free.
type Store struct {
	*sqlite.Store

	mu       sync.RWMutex
	snap     *config.Snapshot
	previous *config.Snapshot
}

// New wraps the supplied *sqlite.Store, seeding an empty snapshot.
// Tests typically call ImportBundle immediately after to install the
// canonical kit/workflow shape.
func New(store *sqlite.Store) *Store {
	return &Store{
		Store: store,
		snap:  config.BuildSnapshot(config.Bundle{}),
	}
}

// Open is sugar that calls sqlite.Open on the supplied path, wires
// t.Cleanup to close the underlying *sqlite.Store, and returns a
// fresh SnapStore wrapping the result.
func Open(t testing.TB, path string) *Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store)
}

// Snapshot returns the active per-bundle snapshot. Always non-nil:
// the constructor seeds an empty snapshot so callers that read before
// the first ImportBundle still observe a sensible (empty) view.
func (s *Store) Snapshot() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// PreviousSnapshot returns the snapshot installed before the most
// recent ImportBundle, or nil when the fixture has only seen one
// bundle.
func (s *Store) PreviousSnapshot() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.previous
}

// ImportBundle rotates the snapshot pair. The signature mirrors the
// historical *sqlite.Store.ImportBundle so existing test call sites
// migrate by switching the receiver type, not the call shape.
func (s *Store) ImportBundle(_ context.Context, bundle config.Bundle, _ string, _ string) error {
	next := config.BuildSnapshot(bundle)
	s.mu.Lock()
	if s.snap != nil {
		s.previous = s.snap
	}
	s.snap = next
	s.mu.Unlock()
	return nil
}
