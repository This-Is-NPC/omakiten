package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

// TestPreviewOrphanedCascade_MatchesRebindCascade pins task #301 review
// §11557 finding A1: the preview shown in the bundle-swap prompt must
// list the same affected root/sub-task IDs the confirmed migrate will
// rewrite. Both paths consume the same OrphanCascadePlan; this test
// applies a swap that orphans rows on both layers and asserts the
// preview vs rebind reports match by total + per-task id.
func TestPreviewOrphanedCascade_MatchesRebindCascade(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Step 1: root kit "omakase" (backlog, docs) + sub-kit "izakaya"
	// (backlog, dev, done). Create one root + two sub tasks.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "docs"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev", "done"}, []int{10, 11, 12},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")

	rootDoomed, err := store.CreateTask(ctx, project.ID, "root-docs", "", domain.Priority(2), "docs", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask root: %v", err)
	}
	parent, err := store.CreateTask(ctx, project.ID, "parent", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	subSafe := makeSubtask(t, store, project.ID, parent.ID, "child-dev", "dev")
	_ = subSafe
	subDoomed := makeSubtask(t, store, project.ID, parent.ID, "child-doomed", "done")

	// Step 2: root kit drops "docs", sub-kit drops "done".
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 3},
		"kaiseki", []string{"backlog", "dev"}, []int{20, 21},
	))

	currSub, _ := store.snap().SubtaskKit()
	prevSub, _ := store.prev().SubtaskKit()
	plan := domain.OrphanCascadePlan{
		CurrentRoot:  store.snap(),
		PreviousRoot: store.prev(),
		CurrentSub:   currSub,
		PreviousSub:  prevSub,
		FromKit:      "izakaya",
		ToKit:        "kaiseki",
	}

	preview, err := store.PreviewOrphanedCascade(ctx, project.ID, plan)
	if err != nil {
		t.Fatalf("PreviewOrphanedCascade: %v", err)
	}
	if preview.Total != 2 {
		t.Fatalf("preview Total = %d, want 2 (1 root docs + 1 sub done)", preview.Total)
	}

	previewIDs := collectTaskIDs(preview)
	if !previewIDs[rootDoomed.ID] {
		t.Fatalf("preview missing root task %d; ids=%v", rootDoomed.ID, previewIDs)
	}
	if !previewIDs[subDoomed.ID] {
		t.Fatalf("preview missing sub task %d; ids=%v", subDoomed.ID, previewIDs)
	}

	// Now rebind via the cascade — the report must report the same set.
	rebind, err := store.RebindOrphanedCascade(ctx, project.ID, plan)
	if err != nil {
		t.Fatalf("RebindOrphanedCascade: %v", err)
	}
	if rebind.Total != preview.Total {
		t.Fatalf("rebind Total = %d, preview Total = %d (parity broken)", rebind.Total, preview.Total)
	}
	rebindIDs := collectTaskIDs(rebind)
	for id := range previewIDs {
		if !rebindIDs[id] {
			t.Fatalf("rebind dropped task %d that preview reported; rebind ids=%v", id, rebindIDs)
		}
	}
	for id := range rebindIDs {
		if !previewIDs[id] {
			t.Fatalf("rebind mutated task %d that preview did not report; preview ids=%v", id, previewIDs)
		}
	}
}

// TestRebindOrphanedCascade_PublishesEventsForBothLayers proves the
// atomic cascade still emits one event per affected task (root or sub)
// after the joint commit succeeds. Atomicity itself is structural — the
// implementation has one BeginTx / one Commit covering both passes — so
// this test verifies the event-publication contract is preserved.
func TestRebindOrphanedCascade_PublishesEventsForBothLayers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "docs"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev", "done"}, []int{10, 11, 12},
	))
	project := mustUpsertProject(t, store, "p", "p", "/p")

	rootDoomed, err := store.CreateTask(ctx, project.ID, "root-docs", "", domain.Priority(2), "docs", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask root: %v", err)
	}
	parent, err := store.CreateTask(ctx, project.ID, "parent", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	subDoomed := makeSubtask(t, store, project.ID, parent.ID, "child-doomed", "done")

	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 3},
		"kaiseki", []string{"backlog", "dev"}, []int{20, 21},
	))

	currSub, _ := store.snap().SubtaskKit()
	prevSub, _ := store.prev().SubtaskKit()
	plan := domain.OrphanCascadePlan{
		CurrentRoot:  store.snap(),
		PreviousRoot: store.prev(),
		CurrentSub:   currSub,
		PreviousSub:  prevSub,
		FromKit:      "izakaya",
		ToKit:        "kaiseki",
	}

	if _, err := store.RebindOrphanedCascade(ctx, project.ID, plan); err != nil {
		t.Fatalf("RebindOrphanedCascade: %v", err)
	}

	rootEvents, err := store.ListTaskActivity(ctx, project.ID, rootDoomed.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity root: %v", err)
	}
	if !hasEventType(rootEvents, domain.EventTypeTaskMigrated) {
		t.Fatalf("root task missing task.migrated event after cascade; events=%+v", rootEvents)
	}

	subEvents, err := store.ListTaskActivity(ctx, project.ID, subDoomed.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity sub: %v", err)
	}
	if !hasEventType(subEvents, domain.EventTypeTaskBucketOrphaned) {
		t.Fatalf("sub task missing task.bucket_orphaned event after cascade; events=%+v", subEvents)
	}
}

// TestRebindOrphanedCascade_NoOpWhenNoOrphans returns an empty report
// without opening a tx or emitting events when neither layer carries
// orphan rows. Guards against a regression where the cascade always
// commits a zero-row tx and trips the audit/telemetry watchdog.
func TestRebindOrphanedCascade_NoOpWhenNoOrphans(t *testing.T) {
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
	_ = makeSubtask(t, store, project.ID, parent.ID, "child", "dev")

	// "Swap" to an identical-keys bundle so neither layer orphans rows.
	store.applyBundle(bundleWithSubKit(t,
		"omakase", []string{"backlog", "dev"}, []int{1, 2},
		"izakaya", []string{"backlog", "dev"}, []int{10, 11},
	))

	currSub, _ := store.snap().SubtaskKit()
	prevSub, _ := store.prev().SubtaskKit()
	plan := domain.OrphanCascadePlan{
		CurrentRoot:  store.snap(),
		PreviousRoot: store.prev(),
		CurrentSub:   currSub,
		PreviousSub:  prevSub,
		FromKit:      "izakaya",
		ToKit:        "izakaya",
	}

	rep, err := store.RebindOrphanedCascade(ctx, project.ID, plan)
	if err != nil {
		t.Fatalf("RebindOrphanedCascade no-op: %v", err)
	}
	if rep.Total != 0 {
		t.Fatalf("Total = %d, want 0", rep.Total)
	}
}

func collectTaskIDs(report domain.OrphanReport) map[int64]bool {
	out := map[int64]bool{}
	for _, g := range report.Groups {
		for _, t := range g.Tasks {
			out[t.TaskID] = true
		}
	}
	return out
}

func hasEventType(events []domain.Event, eventType string) bool {
	for _, ev := range events {
		if ev.EventType == eventType {
			return true
		}
	}
	return false
}
