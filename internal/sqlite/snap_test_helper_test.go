package sqlite

import (
	"context"
	"sync"
	"testing"

	"omakiten/internal/config"
)

// snapStore wraps the package-local *Store with per-fixture Snapshot
// machinery. Phase 2-bis moved the snapshot pair out of the SQL
// adapter; this test-only helper restores the rotate/inspect surface
// the sqlite tests depend on without leaking config state into the
// production *Store. Lives in a `_test.go` file so the helper does
// not contribute to the production binary or to gate-6's
// "bundles.go absent" check.
type snapStore struct {
	*Store

	mu       sync.RWMutex
	snap     *config.Snapshot
	previous *config.Snapshot
}

func newSnapStore(t testing.TB, store *Store) *snapStore {
	t.Helper()
	return &snapStore{Store: store, snap: config.BuildSnapshot(config.Bundle{})}
}

// openSnapStore opens a fresh *Store at path and wraps it in a
// snapStore. t.Cleanup wires the close.
func openSnapStore(t testing.TB, path string) *snapStore {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newSnapStore(t, store)
}

func (s *snapStore) Snapshot() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

func (s *snapStore) PreviousSnapshot() *config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.previous
}

func (s *snapStore) ImportBundle(_ context.Context, bundle config.Bundle, _ string, _ string) error {
	next := config.BuildSnapshot(bundle)
	s.mu.Lock()
	if s.snap != nil {
		s.previous = s.snap
	}
	s.snap = next
	s.mu.Unlock()
	return nil
}
