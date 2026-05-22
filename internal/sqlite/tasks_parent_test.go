package sqlite

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

func setupParentFixture(t *testing.T) (context.Context, *storeFixture, domain.ProjectContext) {
	t.Helper()
	ctx, store, project := setupLifecycle(t)
	return ctx, store, project
}

func TestListDirectChildrenReturnsOnlyDirectChildren(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, err := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(root) = %v", err)
	}
	childA, err := store.CreateTask(ctx, project.ID, "child-a", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(child-a) = %v", err)
	}
	childB, err := store.CreateTask(ctx, project.ID, "child-b", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(child-b) = %v", err)
	}
	grand, err := store.CreateTask(ctx, project.ID, "grandchild", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(grand) = %v", err)
	}

	if err := store.SetTaskParent(ctx, project.ID, childA.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child-a, root) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, childB.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child-b, root) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, grand.ID, &childA.ID); err != nil {
		t.Fatalf("SetTaskParent(grand, child-a) = %v", err)
	}

	children, err := store.ListDirectChildren(ctx, project.ID, root.ID, store.snap())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("direct children = %d, want 2 (grandchild must not surface)", len(children))
	}
	for _, c := range children {
		if c.ParentID == nil || *c.ParentID != root.ID {
			t.Fatalf("child %d ParentID = %v, want %d", c.ID, c.ParentID, root.ID)
		}
		if c.BucketKey != "backlog" {
			t.Fatalf("child %d BucketKey = %q, want backlog (resolver must run)", c.ID, c.BucketKey)
		}
	}
}

func TestFirstChildNotInBucketSurfacesOpenChild(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, err := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(root) = %v", err)
	}
	childA, err := store.CreateTask(ctx, project.ID, "child-a", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(child-a) = %v", err)
	}
	childB, err := store.CreateTask(ctx, project.ID, "child-b", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(child-b) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, childA.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child-a) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, childB.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child-b) = %v", err)
	}

	doneBucket, ok := store.snap().BucketByKey("done")
	if !ok {
		t.Fatal("snap missing done bucket")
	}

	// All children open (in backlog) → must return childA (lowest id).
	got, found, err := store.FirstChildNotInBucket(ctx, project.ID, root.ID, doneBucket.ID, store.snap())
	if err != nil {
		t.Fatalf("FirstChildNotInBucket(open) = %v", err)
	}
	if !found {
		t.Fatal("FirstChildNotInBucket returned found=false, want true")
	}
	if got.ID != childA.ID {
		t.Fatalf("first open child id = %d, want %d", got.ID, childA.ID)
	}

	// Move both children to done — the guard read must report empty.
	walkToDone(t, ctx, store, project.ID, childA.ID)
	walkToDone(t, ctx, store, project.ID, childB.ID)

	_, found, err = store.FirstChildNotInBucket(ctx, project.ID, root.ID, doneBucket.ID, store.snap())
	if err != nil {
		t.Fatalf("FirstChildNotInBucket(all done) = %v", err)
	}
	if found {
		t.Fatal("FirstChildNotInBucket returned found=true after all children moved to done")
	}
}

func TestCountDescendantsWalksFullSubtree(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, err := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(root) = %v", err)
	}
	if n, err := store.CountDescendants(ctx, project.ID, root.ID); err != nil {
		t.Fatalf("CountDescendants(empty) = %v", err)
	} else if n != 0 {
		t.Fatalf("CountDescendants(empty) = %d, want 0", n)
	}

	c1, _ := store.CreateTask(ctx, project.ID, "c1", "", domain.Priority(2), "backlog", store.snap())
	c2, _ := store.CreateTask(ctx, project.ID, "c2", "", domain.Priority(2), "backlog", store.snap())
	g1, _ := store.CreateTask(ctx, project.ID, "g1", "", domain.Priority(2), "backlog", store.snap())
	gg1, _ := store.CreateTask(ctx, project.ID, "gg1", "", domain.Priority(2), "backlog", store.snap())

	for taskID, parentID := range map[int64]int64{
		c1.ID:  root.ID,
		c2.ID:  root.ID,
		g1.ID:  c1.ID,
		gg1.ID: g1.ID,
	} {
		pid := parentID
		if err := store.SetTaskParent(ctx, project.ID, taskID, &pid); err != nil {
			t.Fatalf("SetTaskParent(%d→%d) = %v", taskID, parentID, err)
		}
	}

	got, err := store.CountDescendants(ctx, project.ID, root.ID)
	if err != nil {
		t.Fatalf("CountDescendants = %v", err)
	}
	if got != 4 {
		t.Fatalf("CountDescendants = %d, want 4 (c1,c2,g1,gg1)", got)
	}
}

func TestIsDescendantOfDetectsCycles(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, _ := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	child, _ := store.CreateTask(ctx, project.ID, "child", "", domain.Priority(2), "backlog", store.snap())
	grand, _ := store.CreateTask(ctx, project.ID, "grand", "", domain.Priority(2), "backlog", store.snap())
	sibling, _ := store.CreateTask(ctx, project.ID, "sibling", "", domain.Priority(2), "backlog", store.snap())

	if err := store.SetTaskParent(ctx, project.ID, child.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, grand.ID, &child.ID); err != nil {
		t.Fatalf("SetTaskParent(grand) = %v", err)
	}

	// grand IS a descendant of root → cycle if we set root.parent = grand.
	got, err := store.IsDescendantOf(ctx, project.ID, grand.ID, root.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf(grand of root) = %v", err)
	}
	if !got {
		t.Fatal("IsDescendantOf(grand, root) = false, want true (grand sits under root)")
	}

	// sibling is independent.
	got, err = store.IsDescendantOf(ctx, project.ID, sibling.ID, root.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf(sibling) = %v", err)
	}
	if got {
		t.Fatal("IsDescendantOf(sibling, root) = true, want false (sibling has no parent)")
	}

	// Self-loop is a degenerate cycle.
	got, err = store.IsDescendantOf(ctx, project.ID, root.ID, root.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf(self) = %v", err)
	}
	if !got {
		t.Fatal("IsDescendantOf(self) = false, want true (a node descends from itself)")
	}
}

func TestSetTaskParentRejectsSelfParent(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	task, _ := store.CreateTask(ctx, project.ID, "solo", "", domain.Priority(2), "backlog", store.snap())
	err := store.SetTaskParent(ctx, project.ID, task.ID, &task.ID)
	if err == nil {
		t.Fatal("SetTaskParent(self) succeeded, want validation error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("SetTaskParent(self) error = %v, want validation", err)
	}
}

func TestSetTaskParentClearsToRoot(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, _ := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	child, _ := store.CreateTask(ctx, project.ID, "child", "", domain.Priority(2), "backlog", store.snap())

	if err := store.SetTaskParent(ctx, project.ID, child.ID, &root.ID); err != nil {
		t.Fatalf("SetTaskParent(child→root) = %v", err)
	}
	if err := store.SetTaskParent(ctx, project.ID, child.ID, nil); err != nil {
		t.Fatalf("SetTaskParent(child→nil) = %v", err)
	}

	got, err := store.ListDirectChildren(ctx, project.ID, root.ID, store.snap())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("root still has %d children after clearing", len(got))
	}
}

func TestCountDirectChildrenIgnoresGrandchildren(t *testing.T) {
	t.Parallel()
	ctx, store, project := setupParentFixture(t)

	root, _ := store.CreateTask(ctx, project.ID, "root", "", domain.Priority(2), "backlog", store.snap())
	c1, _ := store.CreateTask(ctx, project.ID, "c1", "", domain.Priority(2), "backlog", store.snap())
	c2, _ := store.CreateTask(ctx, project.ID, "c2", "", domain.Priority(2), "backlog", store.snap())
	g, _ := store.CreateTask(ctx, project.ID, "g", "", domain.Priority(2), "backlog", store.snap())

	for taskID, parentID := range map[int64]int64{
		c1.ID: root.ID,
		c2.ID: root.ID,
		g.ID:  c1.ID,
	} {
		pid := parentID
		_ = store.SetTaskParent(ctx, project.ID, taskID, &pid)
	}

	got, err := store.CountDirectChildren(ctx, project.ID, root.ID)
	if err != nil {
		t.Fatalf("CountDirectChildren = %v", err)
	}
	if got != 2 {
		t.Fatalf("CountDirectChildren = %d, want 2 (grandchild excluded)", got)
	}
}

// walkToDone moves the task through the workflow until it reaches the
// final bucket. The lifecycle fixture's workflow is backlog→dev→done
// (see testfixtures/lifecycle_policy.yaml).
func walkToDone(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
	t.Helper()
	for _, key := range []string{"dev", "done"} {
		if _, err := store.MoveTask(ctx, projectID, taskID, key, store.snap()); err != nil {
			t.Fatalf("walkToDone(%s) = %v", key, err)
		}
	}
}
