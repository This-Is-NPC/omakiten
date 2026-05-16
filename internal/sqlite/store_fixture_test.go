package sqlite

import (
	"context"
	"sync"
	"testing"

	"omakiten/internal/config"
)

// storeFixture pairs a sqlite-backed *Store with the per-bundle
// Snapshot pair that Round-2 moved out of the production adapter.
// Tests build a Bundle, call applyBundle to rotate the fixture's
// snap/prev pair, then read fixture.snap()/fixture.prev() when
// invoking Store methods that take a domain.BucketResolver. The
// helper deliberately exposes snap/prev under fixture-local names
// (snap / prev / applyBundle) so it cannot be confused with the
// production *Store API that Round-2 retired.
type storeFixture struct {
	*Store

	mu       sync.RWMutex
	current  *config.Snapshot
	previous *config.Snapshot
}

func newStoreFixture(t testing.TB, store *Store) *storeFixture {
	t.Helper()
	return &storeFixture{Store: store, current: config.BuildSnapshot(config.Bundle{})}
}

// openStoreFixture opens a fresh *Store at path and wraps it in a
// storeFixture. t.Cleanup wires the close.
func openStoreFixture(t testing.TB, path string) *storeFixture {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newStoreFixture(t, store)
}

func (s *storeFixture) snap() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *storeFixture) prev() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.previous
}

// applyBundle rotates the fixture's snapshot pair: the prior current
// becomes previous and a fresh snapshot is built from the supplied
// bundle. Returns no error — the helper writes nothing to SQLite.
// Callers that need to emit the bundle.imported audit event should
// invoke s.Store.EmitBundleImported separately.
func (s *storeFixture) applyBundle(bundle config.Bundle) {
	next := config.BuildSnapshot(bundle)
	s.mu.Lock()
	if s.current != nil {
		s.previous = s.current
	}
	s.current = next
	s.mu.Unlock()
}
