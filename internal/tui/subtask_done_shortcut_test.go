package tui

import (
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestFinalBucketSnapshotForTask_RoutesSubtaskToSubKit pins task #301
// review §11557 finding A3: the "send focused sub-task to done"
// shortcut (`space` on the sub-tasks pane) must resolve the final
// bucket via the FOCUSED CHILD's resolved kit, not the root kit. The
// regression before this fix sent a sub-task to whatever bucket key
// the root kit named "final" — quietly bypassing the sub-kit's
// terminal bucket.
func TestFinalBucketSnapshotForTask_RoutesSubtaskToSubKit(t *testing.T) {
	rootBundle := config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "shipped", Name: "Shipped", Position: 2}, // root final bucket key = "shipped"
			},
		}},
		SubtaskBundle: &config.Bundle{
			Kit:    config.Kit{Key: "sub"},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "sub",
				Name: "Sub",
				Buckets: []config.Bucket{
					{ID: 10, Key: "todo", Name: "Todo", Position: 1},
					{ID: 20, Key: "closed", Name: "Closed", Position: 2}, // sub final bucket key = "closed"
				},
			}},
		},
	}
	snap := config.BuildSnapshot(rootBundle)

	parentID := int64(42)
	child := domain.Task{ID: 7, ParentID: &parentID, BucketKey: "todo"}
	childSnap := finalBucketSnapshotForTask(snap, child)
	if childSnap == nil {
		t.Fatal("finalBucketSnapshotForTask(sub-task) = nil")
	}
	if got := childSnap.Workflow().FinalBucketKey(); got != "closed" {
		t.Fatalf("sub-task final bucket = %q, want closed (sub-kit's terminal bucket); root kit's would be shipped", got)
	}

	root := domain.Task{ID: 5, BucketKey: "backlog"}
	rootSnap := finalBucketSnapshotForTask(snap, root)
	if rootSnap == nil {
		t.Fatal("finalBucketSnapshotForTask(root task) = nil")
	}
	if got := rootSnap.Workflow().FinalBucketKey(); got != "shipped" {
		t.Fatalf("root task final bucket = %q, want shipped (root kit terminal)", got)
	}
}

// TestFinalBucketSnapshotForTask_FallsBackToRootWhenNoSubKit guards
// pre-cascade behaviour: projects without subtask_kit always resolve
// the final bucket against the root snapshot, regardless of whether
// the task is a sub-task or not.
func TestFinalBucketSnapshotForTask_FallsBackToRootWhenNoSubKit(t *testing.T) {
	rootBundle := config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "done", Name: "Done", Position: 2},
			},
		}},
	}
	snap := config.BuildSnapshot(rootBundle)

	parentID := int64(99)
	child := domain.Task{ID: 10, ParentID: &parentID, BucketKey: "backlog"}
	got := finalBucketSnapshotForTask(snap, child)
	if got == nil {
		t.Fatal("finalBucketSnapshotForTask returned nil (expected root snapshot fallback)")
	}
	if got.Workflow().FinalBucketKey() != "done" {
		t.Fatalf("final bucket = %q, want done (root fallback)", got.Workflow().FinalBucketKey())
	}
}
