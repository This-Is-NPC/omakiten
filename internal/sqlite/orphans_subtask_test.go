package sqlite

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// bundleWithSubKit composes a root bundle (rootKeys/rootIDs) with a
// sub-kit bundle (subKeys/subIDs) under a distinct kit identity. The
// resulting Bundle carries SubtaskBundle so config.BuildSnapshot loads
// both kits — Snapshot.SubtaskKit() returns the sub-kit snapshot,
// Snapshot.For(task) routes to it for sub-tasks.
func bundleWithSubKit(t *testing.T, rootKit string, rootKeys []string, rootIDs []int, subKit string, subKeys []string, subIDs []int) config.Bundle {
	t.Helper()
	root := bundleWithKeys(t, rootKit, rootKeys, rootIDs)
	root.Kit.Key = rootKit
	root.Kit.Name = rootKit
	sub := bundleWithKeys(t, subKit, subKeys, subIDs)
	sub.Kit.Key = subKit
	sub.Kit.Name = subKit
	root.SubtaskBundle = &sub
	return root
}

// makeSubtask creates a task whose bucket is resolved against the
// sub-kit snapshot, then reparents it so the row has
// tasks.parent_id != NULL. Returns the reparented sub-task row.
func makeSubtask(t *testing.T, store *storeFixture, projectID, parentID int64, title, bucketKey string) domain.Task {
	t.Helper()
	ctx := context.Background()
	sub, ok := store.snap().SubtaskKit()
	if !ok {
		t.Fatalf("makeSubtask: snapshot missing sub-kit (test misconfiguration)")
	}
	task, err := store.CreateTask(ctx, projectID, title, "", domain.Priority(2), bucketKey, nil, sub)
	if err != nil {
		t.Fatalf("CreateTask(%s): %v", title, err)
	}
	if err := store.SetTaskParent(ctx, projectID, task.ID, &parentID); err != nil {
		t.Fatalf("SetTaskParent(%d → %d): %v", task.ID, parentID, err)
	}
	task.ParentID = &parentID
	return task
}

func TestRebindOrphanedSubtasks_EmitsBucketOrphanedWithLockedPayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Step 1: root kit "omakase" (backlog, dev) + sub-kit "izakaya"
	// (backlog, dev, done). Parent on root, two sub-tasks on sub-kit.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev", "done"}, []int{10, 11, 12},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")

	parent, err := store.CreateTask(ctx, project.ID, "parent", "", domain.Priority(2), "dev", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	makeSubtask(t, store, project.ID, parent.ID, "child-dev", "dev")
	missingChild := makeSubtask(t, store, project.ID, parent.ID, "child-doomed", "done")

	// Step 2: sub-kit "kaiseki" replaces "izakaya": drops "done",
	// keeps "dev"; "done" becomes orphan.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"kaiseki", []string{"backlog", "dev"}, []int{20, 21},
	))

	currSub, ok := store.snap().SubtaskKit()
	if !ok {
		t.Fatalf("current snapshot missing sub-kit")
	}
	prevSub, ok := store.prev().SubtaskKit()
	if !ok {
		t.Fatalf("previous snapshot missing sub-kit")
	}

	report, err := store.RebindOrphanedSubtasks(ctx, project.ID, currSub, prevSub, "izakaya", "kaiseki")
	if err != nil {
		t.Fatalf("RebindOrphanedSubtasks: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, want 1", report.Total)
	}

	events, err := store.ListTaskActivity(ctx, project.ID, missingChild.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity: %v", err)
	}
	var payload string
	for _, ev := range events {
		if ev.EventType == domain.EventTypeTaskBucketOrphaned {
			payload = ev.Payload
			break
		}
	}
	if payload == "" {
		t.Fatalf("task.bucket_orphaned event not emitted; events=%+v", events)
	}
	requiredSubstrings := []string{
		`"task_id":`,
		`"parent_id":`,
		`"depth":1`,
		`"old_bucket":"done"`,
		`"from_kit":"izakaya"`,
		`"to_kit":"kaiseki"`,
		`"resolved_kit":"kaiseki"`,
		`"reason":"bucket_missing_in_resolved_kit"`,
	}
	for _, want := range requiredSubstrings {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q; got %q", want, payload)
		}
	}
}

func TestRebindOrphanedSubtasks_PreservedKeyNotOrphan(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")
	parent, err := store.CreateTask(ctx, project.ID, "parent", "", domain.Priority(2), "dev", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	makeSubtask(t, store, project.ID, parent.ID, "child", "dev")

	// Sub-kit swap: same keys, different ids → no orphan.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"kaiseki", []string{"backlog", "dev"}, []int{99, 100},
	))

	currSub, _ := store.snap().SubtaskKit()
	prevSub, _ := store.prev().SubtaskKit()
	report, err := store.RebindOrphanedSubtasks(ctx, project.ID, currSub, prevSub, "izakaya", "kaiseki")
	if err != nil {
		t.Fatalf("RebindOrphanedSubtasks: %v", err)
	}
	if report.Total != 0 {
		t.Fatalf("Total = %d, want 0 (key survived swap)", report.Total)
	}
}

func TestRebindOrphanedSubtasks_IgnoresRootTasks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "docs"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")

	// Root task in a bucket the new root kit will drop. Must NOT be
	// picked up by the sub-task path.
	if _, err := store.CreateTask(ctx, project.ID, "root-doc", "", domain.Priority(2), "docs", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask root: %v", err)
	}

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))

	currSub, _ := store.snap().SubtaskKit()
	prevSub, _ := store.prev().SubtaskKit()
	report, err := store.RebindOrphanedSubtasks(ctx, project.ID, currSub, prevSub, "izakaya", "izakaya")
	if err != nil {
		t.Fatalf("RebindOrphanedSubtasks: %v", err)
	}
	if report.Total != 0 {
		t.Fatalf("Total = %d, want 0 (root tasks ignored by sub-task path)", report.Total)
	}
}

func TestRebindOrphanedRootTasks_IgnoresSubtasks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "docs"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")

	parent, err := store.CreateTask(ctx, project.ID, "parent", "", domain.Priority(2), "docs", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	// Sub-task whose bucket key matches a root-kit key but lives under sub-kit.
	makeSubtask(t, store, project.ID, parent.ID, "child", "dev")

	// Root kit swap drops "docs"; sub-kit unchanged.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))

	report, err := store.RebindOrphanedRootTasks(ctx, project.ID, store.snap(), store.prev())
	if err != nil {
		t.Fatalf("RebindOrphanedRootTasks: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, want 1 (only root parent orphaned)", report.Total)
	}
	// And the emitted event must be task.migrated (not task.bucket_orphaned)
	// — root-kit migration retains legacy semantics.
	events, err := store.ListTaskActivity(ctx, project.ID, parent.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity: %v", err)
	}
	var seenMigrated bool
	for _, ev := range events {
		if ev.EventType == domain.EventTypeTaskBucketOrphaned {
			t.Fatalf("root task emitted task.bucket_orphaned; want task.migrated; payload=%q", ev.Payload)
		}
		if ev.EventType == domain.EventTypeTaskMigrated {
			seenMigrated = true
		}
	}
	if !seenMigrated {
		t.Fatalf("root task missing task.migrated event; events=%+v", events)
	}
}
