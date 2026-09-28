package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCurrentSchemaEventsOrderByIndexes asserts the current indexes let the
// planner satisfy the Logs read-path ORDER BY without a temp
// b-tree. The superseded event index served the filter but left
// `USE TEMP B-TREE FOR ORDER BY` on the project-only and multi-`event_type IN`
// paths (entity_type sat between event_type and created_at, and id was absent),
// so the chosen falsifier here is: the target queries use a current index AND the
// plan no longer contains a temp b-tree. The index contract is pure schema, so the
// meaningful regression guard is that the planner keeps choosing these indexes
// with index-ordered output — a plan that reverts to a temp b-tree means the
// reshape stopped pulling its weight.
func TestCurrentSchemaEventsOrderByIndexes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	// Open initializes the authoritative baseline on a fresh DB.
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedRealtimeReadPathRows(t, store)

	const tempBTree = "USE TEMP B-TREE FOR ORDER BY"

	t.Run("project-only Logs read uses idx_events_project_created with no temp b-tree", func(t *testing.T) {
		assertEventsPlan(t, store, tempBTree, "idx_events_project_created",
			"SELECT id FROM events WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 200", true, 1)
	})

	t.Run("multi event_type IN Logs read uses idx_events_project_created with no temp b-tree", func(t *testing.T) {
		assertEventsPlan(t, store, tempBTree, "idx_events_project_created",
			"SELECT id FROM events WHERE project_id = ? AND event_type IN ('comment','operation') "+
				"AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 200",
			true, 1, "2000-01-01 00:00:00")
	})

	t.Run("single event_type Logs read uses idx_events_project_type_created with no temp b-tree", func(t *testing.T) {
		assertEventsPlan(t, store, tempBTree, "idx_events_project_type_created",
			"SELECT id FROM events WHERE project_id = ? AND event_type IN ('comment') "+
				"AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 200",
			true, 1, "2000-01-01 00:00:00")
	})

	t.Run("category aggregate stays project-scoped on an index (no full table scan)", func(t *testing.T) {
		// GROUP BY event_type (list_events.go EventCategoryCounts). This must
		// stay project-scoped via an index and not regress to a SCAN over the
		// whole table once idx_events_project_type is dropped.
		plan := explainQueryPlan(t, store,
			"SELECT event_type, COUNT(*) FROM events WHERE project_id = ? AND created_at >= ? GROUP BY event_type",
			1, "2000-01-01 00:00:00")
		if !strings.Contains(plan, "idx_events_project_type_created") {
			t.Fatalf("category aggregate plan does not use idx_events_project_type_created:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("category aggregate plan regressed to a full table scan of events:\n%s", plan)
		}
	})

	t.Run("superseded idx_events_project_type is absent", func(t *testing.T) {
		var n int
		if err := store.db.QueryRow(
			"SELECT COUNT(1) FROM sqlite_master WHERE type='index' AND name='idx_events_project_type'",
		).Scan(&n); err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		if n != 0 {
			t.Fatalf("idx_events_project_type should be absent, found %d", n)
		}
	})
}

func assertEventsPlan(t *testing.T, store *Store, tempBTree, index, query string, noTemp bool, args ...any) {
	t.Helper()
	plan := explainQueryPlan(t, store, query, args...)
	if !strings.Contains(plan, index) {
		t.Fatalf("events plan does not use %s:\n%s", index, plan)
	}
	if noTemp && strings.Contains(plan, tempBTree) {
		t.Fatalf("events plan still spills to a temp b-tree:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN events") {
		t.Fatalf("events plan still full-scans events:\n%s", plan)
	}
}
