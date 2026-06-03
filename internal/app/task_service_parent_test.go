package app

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestTaskServiceAddSubAttachesParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatalf("child.ParentID = %v, want %d", child.ParentID, parent.ID)
	}
	if !child.IsSubTask() {
		t.Fatal("IsSubTask = false after AddSub")
	}
}

func TestTaskServiceAddSubRejectsMissingParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	_, err := service.AddSub(ctx, project.Context(), 9999, "Orphan", "", "", "backlog")
	if err == nil {
		t.Fatal("AddSub(missing parent) error = nil, want task_not_found")
	}
	assertCodedError(t, err, domain.ErrTaskNotFound)
}

func TestTaskServiceEditReparent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	a, _ := service.Add(ctx, project.Context(), "A", "", "", "backlog")
	b, _ := service.Add(ctx, project.Context(), "B", "", "", "backlog")

	if _, err := service.Edit(ctx, project.Context(), b.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &a.ID}); err != nil {
		t.Fatalf("Edit(reparent) = %v", err)
	}

	children, err := store.ListDirectChildren(ctx, project.ID, a.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(children) != 1 || children[0].ID != b.ID {
		t.Fatalf("A's children after reparent = %+v, want [B]", children)
	}
}

func TestTaskServiceEditRejectsCycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	root, _ := service.Add(ctx, project.Context(), "Root", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), root.ID, "Child", "", "", "backlog")

	_, err := service.Edit(ctx, project.Context(), root.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &child.ID})
	if err == nil {
		t.Fatal("Edit(cycle) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceEditClearsParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")

	if _, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: nil}); err != nil {
		t.Fatalf("Edit(clear parent) = %v", err)
	}

	children, err := store.ListDirectChildren(ctx, project.ID, parent.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("parent still has %d children after clear", len(children))
	}
}

func TestTaskServiceAddSubRejectsArchivedParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if _, _, err := service.Archive(ctx, project.Context(), parent.ID); err != nil {
		t.Fatalf("Archive(parent) = %v", err)
	}

	_, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err == nil {
		t.Fatal("AddSub(archived parent) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceAddSubLandsInRootKitFirstBucket_WhenNoCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// A new sub-task is new work and must land at the start of the
	// resolved workflow — the root kit's first bucket when no sub-kit
	// is configured. The pre-#281 "workflow herdado do pai" invariant
	// is gone: a fresh sub-task in done while the parent sits in dev
	// never made sense.
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(default bucket) = %v", err)
	}
	if child.BucketKey != "backlog" {
		t.Fatalf("child.BucketKey = %q, want backlog (root kit first bucket)", child.BucketKey)
	}
}

func TestTaskServiceAddSubAcceptsExplicitNonParentBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// A caller that names an explicit bucket continues to work — the
	// cross-bucket validation of the pre-#281 invariant is removed
	// along with the parent-bucket inheritance. (The default fixture
	// only ships backlog + dev; landing in backlog explicitly proves
	// the caller picks the lane, not the parent's current bucket.)
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err != nil {
		t.Fatalf("AddSub(explicit backlog) = %v", err)
	}
	if child.BucketKey != "backlog" {
		t.Fatalf("child.BucketKey = %q, want backlog (explicit caller choice)", child.BucketKey)
	}
	if parent.BucketKey != "dev" {
		t.Fatalf("parent.BucketKey = %q (sanity); child lane must differ from parent", parent.BucketKey)
	}
}

func TestTaskServiceAddSubUsesSubtaskKitFirstBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, nil)
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}
	if child.BucketKey != "todo" {
		t.Fatalf("child.BucketKey = %q, want sub-kit first bucket todo", child.BucketKey)
	}
}

func TestTaskServiceMoveSubtaskUsesSubtaskKitTransitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle, registry := testfixtures.LoadBundle(t, "subtasks_smoke.yaml")
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 2, Key: "dev", Name: "Development", Position: 1},
		{ID: 3, Key: "review", Name: "Review", Position: 2},
		{ID: 4, Key: "done", Name: "Done", Position: 3},
	}, []config.Transition{{From: 2, To: 4}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, registry, store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}

	_, err = service.Move(ctx, project.Context(), child.ID, "review")
	if err == nil {
		t.Fatal("Move(child dev->review) error = nil, want sub-kit transition rejection")
	}
	assertCodedError(t, err, domain.ErrWorkflowInvalidTransition)
}

func TestTaskServiceEventsCarryDepthMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, []config.Transition{{From: 10, To: 20}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(child) = %v", err)
	}
	if _, err := service.Move(ctx, project.Context(), child.ID, "done"); err != nil {
		t.Fatalf("Move(child todo->done) = %v", err)
	}
	renamed := "Child renamed"
	if _, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{Title: &renamed}); err != nil {
		t.Fatalf("Edit(child title) = %v", err)
	}

	createdEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(created) = %v", err)
	}
	rootPayload := eventPayloadForTask(t, createdEvents, parent.ID)
	assertSubjectMetadata(t, rootPayload, parent.ID, nil, 0, "default")
	childPayload := eventPayloadForTask(t, createdEvents, child.ID)
	assertSubjectMetadata(t, childPayload, child.ID, &parent.ID, 1, "sub")

	movedEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskMoved, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(moved) = %v", err)
	}
	movePayload := eventPayloadForTask(t, movedEvents, child.ID)
	assertSubjectMetadata(t, movePayload, child.ID, &parent.ID, 1, "sub")

	editedEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskEdited, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(edited) = %v", err)
	}
	editPayload := eventPayloadForTask(t, editedEvents, child.ID)
	assertSubjectMetadata(t, editPayload, child.ID, &parent.ID, 1, "sub")
}

func TestTaskDepth_RootInsertsAtZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	root, err := service.Add(ctx, project.Context(), "Root", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(root) = %v", err)
	}
	if root.Depth != 0 {
		t.Fatalf("root.Depth = %d, want 0", root.Depth)
	}
}

func TestTaskDepth_SubInsertAtParentDepthPlusOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(child) = %v", err)
	}
	if child.Depth != parent.Depth+1 {
		t.Fatalf("child.Depth = %d, want %d (parent %d + 1)", child.Depth, parent.Depth+1, parent.Depth)
	}
}

func TestTaskDepth_GrandchildAtTwo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	grandchild, err := service.AddSub(ctx, project.Context(), child.ID, "Grandchild", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(grandchild) = %v", err)
	}
	if grandchild.Depth != 2 {
		t.Fatalf("grandchild.Depth = %d, want 2 (root=0, child=1, grandchild=2)", grandchild.Depth)
	}
}

func TestTaskDepth_ReparentRecomputesSubtree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	// Build two roots: A (depth 0) and B (depth 0). Attach child under A
	// (depth 1) and grandchild under that child (depth 2). Then reparent
	// the child under B — its new depth should still be 1, and the
	// grandchild's depth should recompute to 2 (B.depth + 1 + 1).
	a, _ := service.Add(ctx, project.Context(), "A", "", "", "backlog")
	b, _ := service.Add(ctx, project.Context(), "B", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), a.ID, "child", "", "", "")
	grandchild, _ := service.AddSub(ctx, project.Context(), child.ID, "grandchild", "", "", "")
	if child.Depth != 1 || grandchild.Depth != 2 {
		t.Fatalf("pre-reparent depths off: child=%d grandchild=%d", child.Depth, grandchild.Depth)
	}

	if _, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &b.ID}); err != nil {
		t.Fatalf("Edit(reparent child under B) = %v", err)
	}

	// Re-read the subtree. Child's depth = B.depth + 1; grandchild's depth
	// = child.depth + 1. The reparent must propagate through the subtree.
	rows, err := store.ListDirectChildren(ctx, project.ID, b.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren(B) = %v", err)
	}
	if len(rows) != 1 || rows[0].ID != child.ID {
		t.Fatalf("B's children after reparent = %+v, want [child]", rows)
	}
	if rows[0].Depth != b.Depth+1 {
		t.Fatalf("child.Depth after reparent = %d, want %d (B.depth + 1)", rows[0].Depth, b.Depth+1)
	}
	gcRows, err := store.ListDirectChildren(ctx, project.ID, child.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren(child) = %v", err)
	}
	if len(gcRows) != 1 || gcRows[0].ID != grandchild.ID {
		t.Fatalf("child's children after reparent = %+v, want [grandchild]", gcRows)
	}
	if gcRows[0].Depth != rows[0].Depth+1 {
		t.Fatalf("grandchild.Depth after reparent = %d, want %d (child.depth + 1)", gcRows[0].Depth, rows[0].Depth+1)
	}
}

func TestSubjectDepth_GrandchildEmitsTwo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	grandchild, err := service.AddSub(ctx, project.Context(), child.ID, "Grandchild", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(grandchild) = %v", err)
	}

	events, err := store.ListRecentEvents(ctx, domain.EventTypeTaskCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(created) = %v", err)
	}
	payload := eventPayloadForTask(t, events, grandchild.ID)
	if got := int(payload["subject_depth"].(float64)); got != 2 {
		t.Fatalf("subject_depth for grandchild created = %d, want 2 (payload %+v)", got, payload)
	}
}

func TestTaskServiceEditRejectsReparentUnderArchived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	candidate, _ := service.Add(ctx, project.Context(), "candidate parent", "", "", "backlog")
	target, _ := service.Add(ctx, project.Context(), "target", "", "", "backlog")
	if _, _, err := service.Archive(ctx, project.Context(), candidate.ID); err != nil {
		t.Fatalf("Archive(candidate) = %v", err)
	}

	_, err := service.Edit(ctx, project.Context(), target.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &candidate.ID})
	if err == nil {
		t.Fatal("Edit(reparent under archived) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceListParentMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	root, _ := service.Add(ctx, project.Context(), "root", "", "", "backlog")
	sibling, _ := service.Add(ctx, project.Context(), "sibling", "", "", "backlog")
	c1, _ := service.AddSub(ctx, project.Context(), root.ID, "c1", "", "", "backlog")
	_, _ = service.AddSub(ctx, project.Context(), root.ID, "c2", "", "", "backlog")

	roots, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	rootIDs := map[int64]bool{}
	for _, r := range roots {
		rootIDs[r.ID] = true
	}
	if !rootIDs[root.ID] || !rootIDs[sibling.ID] {
		t.Fatalf("roots filter missed expected ids: %+v", rootIDs)
	}
	if rootIDs[c1.ID] {
		t.Fatal("roots filter leaked a sub-task")
	}

	children, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentChildren, ParentValue: root.ID})
	if err != nil {
		t.Fatalf("List(children) = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children of root = %d, want 2", len(children))
	}
	for _, c := range children {
		if c.ParentID == nil || *c.ParentID != root.ID {
			t.Fatalf("child %d ParentID = %v", c.ID, c.ParentID)
		}
	}
}

// TestTaskServiceEditReparentRebindsRootToSubFirstBucket pins task #301
// review §11557 finding A2: when a root task is reparented under another
// root, the new resolved kit is the sub-kit. The root task's bucket
// ("dev") does not exist in the sub-kit; the implementation must
// force-rebind it to the sub-kit's first bucket (mirroring AddSub
// policy from commit c9fa08e). The returned row also has to reflect
// the new bucket key and depth — fresh row guarantee (#301 finding B6).
func TestTaskServiceEditReparentRebindsRootToSubFirstBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, nil)
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	orphanCandidate, err := service.Add(ctx, project.Context(), "Orphan", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(orphan) = %v", err)
	}

	got, err := service.Edit(ctx, project.Context(), orphanCandidate.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &parent.ID})
	if err != nil {
		t.Fatalf("Edit(reparent) = %v", err)
	}
	if got.BucketKey != "todo" {
		t.Fatalf("got.BucketKey = %q, want todo (sub-kit first bucket — root bucket dev not present in sub kit)", got.BucketKey)
	}
	if got.Depth != 1 {
		t.Fatalf("got.Depth = %d, want 1", got.Depth)
	}
	if got.ParentID == nil || *got.ParentID != parent.ID {
		t.Fatalf("got.ParentID = %v, want %d", got.ParentID, parent.ID)
	}
}

// TestTaskServiceEditReparentRebindsSubToRootFirstBucket pins task #301
// review §11557 finding A2 in the opposite direction: clearing the
// parent on a sub-task that holds a sub-kit-only bucket key forces a
// rebind to the root kit's first bucket. The row never gets stuck
// pointing at a bucket the new resolved kit does not know.
func TestTaskServiceEditReparentRebindsSubToRootFirstBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, []config.Transition{{From: 10, To: 20}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "done")
	if err != nil {
		t.Fatalf("AddSub(child) = %v", err)
	}
	if child.BucketKey != "done" {
		t.Fatalf("setup precondition: child bucket = %q, want done", child.BucketKey)
	}

	got, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: nil})
	if err != nil {
		t.Fatalf("Edit(clear parent) = %v", err)
	}
	if got.ParentID != nil {
		t.Fatalf("got.ParentID = %v, want nil (re-rooted)", got.ParentID)
	}
	if got.Depth != 0 {
		t.Fatalf("got.Depth = %d, want 0 (root)", got.Depth)
	}
	if got.BucketKey != "backlog" {
		t.Fatalf("got.BucketKey = %q, want backlog (root kit first bucket — sub-kit bucket done not in root)", got.BucketKey)
	}
}

// TestTaskServiceEditCombinedFieldEditAndReparentReturnsFreshRow pins
// task #301 review §11557 finding B6: combining field edits with a
// parent change must return the post-write snapshot. Previously the
// service patched `task.ParentID` in memory without re-reading, so
// Depth and BucketKey could lag if the parent change crossed kits or
// triggered a subtree-depth recompute.
func TestTaskServiceEditCombinedFieldEditAndReparentReturnsFreshRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, nil)
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	orphanCandidate, _ := service.Add(ctx, project.Context(), "Orphan", "", "", "dev")
	newTitle := "Renamed"

	got, err := service.Edit(ctx, project.Context(), orphanCandidate.ID, domain.TaskUpdate{
		Title:        &newTitle,
		ChangeParent: true,
		NewParentID:  &parent.ID,
	})
	if err != nil {
		t.Fatalf("Edit(title + reparent) = %v", err)
	}
	if got.Title != newTitle {
		t.Fatalf("got.Title = %q, want %q (field edit lost)", got.Title, newTitle)
	}
	if got.Depth != 1 {
		t.Fatalf("got.Depth = %d, want 1 (reparent recompute lost)", got.Depth)
	}
	if got.ParentID == nil || *got.ParentID != parent.ID {
		t.Fatalf("got.ParentID = %v, want %d", got.ParentID, parent.ID)
	}
	if got.BucketKey != "todo" {
		t.Fatalf("got.BucketKey = %q, want todo (cross-kit first-bucket rebind lost)", got.BucketKey)
	}
}

func subtaskRuntimeBundle(key string, buckets []config.Bucket, transitions []config.Transition) *config.Bundle {
	return &config.Bundle{
		Kit:    config.Kit{ID: 900, Key: key, Name: key},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: key}},
		Workflows: []config.Workflow{{
			ID:          900,
			Key:         key,
			Name:        key,
			Buckets:     buckets,
			Transitions: transitions,
		}},
	}
}

func eventPayloadForTask(t *testing.T, events []domain.Event, taskID int64) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.EntityID != taskID {
			continue
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			t.Fatalf("event payload for task %d is not JSON: %v", taskID, err)
		}
		return payload
	}
	t.Fatalf("event for task %d not found in %+v", taskID, events)
	return nil
}

func assertSubjectMetadata(t *testing.T, payload map[string]any, taskID int64, parentID *int64, depth int, kit string) {
	t.Helper()
	if got := int64(payload["subject_task_id"].(float64)); got != taskID {
		t.Fatalf("subject_task_id = %d, want %d (payload %+v)", got, taskID, payload)
	}
	if parentID == nil {
		if payload["subject_parent_id"] != nil {
			t.Fatalf("subject_parent_id = %v, want nil (payload %+v)", payload["subject_parent_id"], payload)
		}
	} else if got := int64(payload["subject_parent_id"].(float64)); got != *parentID {
		t.Fatalf("subject_parent_id = %d, want %d (payload %+v)", got, *parentID, payload)
	}
	if got := int(payload["subject_depth"].(float64)); got != depth {
		t.Fatalf("subject_depth = %d, want %d (payload %+v)", got, depth, payload)
	}
	if got := payload["resolved_kit"]; got != kit {
		t.Fatalf("resolved_kit = %v, want %q (payload %+v)", got, kit, payload)
	}
}
