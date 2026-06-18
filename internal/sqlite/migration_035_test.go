package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigration035EventsOrderByIndexes asserts the indexes added in migration
// 035 let the planner satisfy the Logs read-path ORDER BY without a temp
// b-tree. Migration 034's idx_events_project_type served the filter but left
// `USE TEMP B-TREE FOR ORDER BY` on the project-only and multi-`event_type IN`
// paths (entity_type sat between event_type and created_at, and id was absent),
// so the chosen falsifier here is: the target queries use a 035 index AND the
// plan no longer contains a temp b-tree. The migration is pure schema, so the
// meaningful regression guard is that the planner keeps choosing these indexes
// with index-ordered output — a plan that reverts to a temp b-tree means the
// reshape stopped pulling its weight.
func TestMigration035EventsOrderByIndexes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")

	// Open applies the full migration chain on a fresh DB — this is also the
	// "applies clean on a fresh DB" assertion from the DoD.
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedRealtimeReadPathRows(t, store)

	const tempBTree = "USE TEMP B-TREE FOR ORDER BY"

	t.Run("project-only Logs read uses idx_events_project_created with no temp b-tree", func(t *testing.T) {
		plan := explainQueryPlan(t, store,
			"SELECT id FROM events WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 200", 1)
		if !strings.Contains(plan, "idx_events_project_created") {
			t.Fatalf("project-only events plan does not use idx_events_project_created:\n%s", plan)
		}
		if strings.Contains(plan, tempBTree) {
			t.Fatalf("project-only events plan still spills to a temp b-tree:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("project-only events plan still full-scans events:\n%s", plan)
		}
	})

	t.Run("multi event_type IN Logs read uses idx_events_project_created with no temp b-tree", func(t *testing.T) {
		plan := explainQueryPlan(t, store,
			"SELECT id FROM events WHERE project_id = ? AND event_type IN ('comment','operation') "+
				"AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 200",
			1, "2000-01-01 00:00:00")
		if !strings.Contains(plan, "idx_events_project_created") {
			t.Fatalf("multi-IN events plan does not use idx_events_project_created:\n%s", plan)
		}
		if strings.Contains(plan, tempBTree) {
			t.Fatalf("multi-IN events plan still spills to a temp b-tree:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("multi-IN events plan still full-scans events:\n%s", plan)
		}
	})

	t.Run("single event_type Logs read uses idx_events_project_type_created with no temp b-tree", func(t *testing.T) {
		plan := explainQueryPlan(t, store,
			"SELECT id FROM events WHERE project_id = ? AND event_type IN ('comment') "+
				"AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 200",
			1, "2000-01-01 00:00:00")
		if !strings.Contains(plan, "idx_events_project_type_created") {
			t.Fatalf("single-type events plan does not use idx_events_project_type_created:\n%s", plan)
		}
		if strings.Contains(plan, tempBTree) {
			t.Fatalf("single-type events plan still spills to a temp b-tree:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN events") {
			t.Fatalf("single-type events plan still full-scans events:\n%s", plan)
		}
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

	t.Run("migration 034 idx_events_project_type is dropped", func(t *testing.T) {
		var n int
		if err := store.db.QueryRow(
			"SELECT COUNT(1) FROM sqlite_master WHERE type='index' AND name='idx_events_project_type'",
		).Scan(&n); err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		if n != 0 {
			t.Fatalf("idx_events_project_type should be dropped by migration 035, found %d", n)
		}
	})
}
