package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestTaskServiceAddSubAttachesParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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

func TestTaskServiceAddSubInheritsParentBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// Empty bucketKey on AddSub must inherit the parent's current
	// bucket — the "workflow herdado do pai" invariant. Without this,
	// a sub-task could be created directly in done while the parent
	// sits in dev.
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(inherit) = %v", err)
	}
	if child.BucketKey != parent.BucketKey {
		t.Fatalf("child.BucketKey = %q, want %q (parent bucket)", child.BucketKey, parent.BucketKey)
	}
}

func TestTaskServiceAddSubRejectsExplicitCrossBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// An explicit bucketKey that differs from the parent must be
	// rejected: the workflow position is inherited, so a sub-task
	// cannot land in done while its parent still sits in dev.
	_, err = service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "done")
	if err == nil {
		t.Fatal("AddSub(cross-bucket) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)

	// Sanity: matching the parent's bucket explicitly is still allowed.
	if _, err := service.AddSub(ctx, project.Context(), parent.ID, "Child2", "", "", "dev"); err != nil {
		t.Fatalf("AddSub(matching bucket) = %v", err)
	}
}

func TestTaskServiceEditRejectsReparentUnderArchived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
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
