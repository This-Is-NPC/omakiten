package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

// TestDataVersionAdvancesOnSecondConnectionCommit proves the pinned
// change-probe connection observes a write committed through a DIFFERENT
// connection of the same process's pool. data_version is per-connection:
// the counter on the pinned probe connection only advances when some other
// connection commits since the probe last read it. This is the contract the
// realtime-tick gate depends on — a pooled write (CreateTask runs on a pool
// connection, not the pinned probe) must move the watermark.
func TestDataVersionAdvancesOnSecondConnectionCommit(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	store.applyBundle(sqliteTestBundle(t))
	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}

	// Pin the probe connection and capture the baseline.
	before, err := store.DataVersion(ctx)
	if err != nil {
		t.Fatalf("DataVersion(before) = %v", err)
	}

	// An unchanged DB must read back the SAME watermark — this is the idle
	// case the tick gate skips on.
	idle, err := store.DataVersion(ctx)
	if err != nil {
		t.Fatalf("DataVersion(idle) = %v", err)
	}
	if idle != before {
		t.Fatalf("DataVersion advanced with no write: before=%d idle=%d", before, idle)
	}

	// A write through the pool (a different connection than the pinned probe)
	// must advance the watermark.
	if _, err := store.CreateTask(ctx, project.ID, "task-a", "", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	after, err := store.DataVersion(ctx)
	if err != nil {
		t.Fatalf("DataVersion(after) = %v", err)
	}
	if after == before {
		t.Fatalf("DataVersion did not advance after a pooled write: still %d", after)
	}
}

// TestDataVersionObservesCrossProcessWALCommit is the WAL frame-visibility
// boundary check (council/Hughes). A SECOND, fully independent *Store opened
// on the same .db file stands in for another OS process: it commits a write
// into the shared WAL, and the first store's pinned probe connection must see
// the watermark advance. Without the pin + a real second sql.DB this would
// silently pass on shared in-memory state; opening a separate Store on the
// same path exercises the genuine cross-connection WAL path.
func TestDataVersionObservesCrossProcessWALCommit(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/omakiten.db"

	reader := openStoreFixture(t, path)
	reader.applyBundle(sqliteTestBundle(t))
	project, err := reader.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}

	before, err := reader.DataVersion(ctx)
	if err != nil {
		t.Fatalf("DataVersion(before) = %v", err)
	}

	// Independent Store on the same file — the WAL is shared on disk, so this
	// is the cross-process boundary the watermark must cross.
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(writer) = %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := writer.CreateTask(ctx, project.ID, "cross-task", "", domain.Priority(2), "backlog", nil, reader.snap()); err != nil {
		t.Fatalf("CreateTask(writer) = %v", err)
	}

	after, err := reader.DataVersion(ctx)
	if err != nil {
		t.Fatalf("DataVersion(after) = %v", err)
	}
	if after == before {
		t.Fatalf("DataVersion did not advance after a cross-connection WAL commit: still %d", after)
	}
}

// TestDataVersionAfterCloseIsSafe guards the Close path: releasing the pinned
// probe connection must not panic, and Close must remain idempotent-safe with
// the underlying pool close.
func TestDataVersionAfterCloseIsSafe(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if _, err := store.DataVersion(ctx); err != nil {
		t.Fatalf("DataVersion() = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}
