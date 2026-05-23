package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

// TestSubtasksEndToEndSmoke walks the full DoD smoke flow for task #190:
//
//  1. Create a parent task in dev.
//  2. Attach three sub-tasks (c1, c2, c3) — inheriting parent bucket.
//  3. Attach a grandchild under c1.
//  4. Attempt dev→review on the parent — the subtasks_complete guard
//     fires and names the first open child found.
//  5. Walk every descendant through dev → review → done so each level's
//     subtasks_complete guard clears in order (grandchild → c1, then
//     c1/c2/c3 → parent).
//  6. Move the parent dev→review → done — both transitions succeed once
//     the subtree is fully closed.
//
// Service-layer integration is intentional: it covers the same code
// paths the TUI exercises on `space`-direct-done and the manual smoke
// described in the task DoD, without standing up a Bubbletea harness.
func TestSubtasksEndToEndSmoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle, _ := testfixtures.LoadBundle(t, "subtasks_smoke.yaml")
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}

	addChild := func(parentID int64, title string) domain.Task {
		t.Helper()
		child, err := service.AddSub(ctx, project.Context(), parentID, title, "", "", "")
		if err != nil {
			t.Fatalf("AddSub(%s) = %v", title, err)
		}
		if child.BucketKey != "dev" {
			t.Fatalf("AddSub(%s) bucket = %q, want dev (inherited)", title, child.BucketKey)
		}
		return child
	}

	c1 := addChild(parent.ID, "c1")
	c2 := addChild(parent.ID, "c2")
	c3 := addChild(parent.ID, "c3")
	gc1 := addChild(c1.ID, "gc1")

	// Guard fires before any child reaches done. The message must name
	// one of the open direct children — order is FK-iteration order so
	// any of c1/c2/c3 is acceptable, but never the grandchild (guard
	// only walks direct children).
	_, err = service.Move(ctx, project.Context(), parent.ID, "review")
	if err == nil {
		t.Fatal("Move(parent dev→review) error = nil, want guard violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("Move(parent) error code = %v, want ErrGuardViolation (%v)", err, domain.ErrGuardViolation)
	}
	if !strings.Contains(coded.Message, "subtasks_complete") {
		t.Fatalf("guard message %q missing rule name", coded.Message)
	}
	named := false
	for _, child := range []domain.Task{c1, c2, c3} {
		if strings.Contains(coded.Message, child.Title) {
			named = true
			break
		}
	}
	if !named {
		t.Fatalf("guard message %q must name an open direct child (c1/c2/c3)", coded.Message)
	}

	walkToDone := func(taskID int64, label string) {
		t.Helper()
		if _, err := service.Move(ctx, project.Context(), taskID, "review"); err != nil {
			t.Fatalf("Move(%s dev→review) = %v", label, err)
		}
		if _, err := service.Move(ctx, project.Context(), taskID, "done"); err != nil {
			t.Fatalf("Move(%s review→done) = %v", label, err)
		}
	}

	// Grandchild has no children — guard passes immediately at each level.
	walkToDone(gc1.ID, "gc1")
	// c1 now has gc1 in done — its own dev→review guard passes.
	walkToDone(c1.ID, "c1")
	walkToDone(c2.ID, "c2")
	walkToDone(c3.ID, "c3")

	// All direct children of parent are in done — parent promotion succeeds.
	if _, err := service.Move(ctx, project.Context(), parent.ID, "review"); err != nil {
		t.Fatalf("Move(parent dev→review) after children done = %v", err)
	}
	if _, err := service.Move(ctx, project.Context(), parent.ID, "done"); err != nil {
		t.Fatalf("Move(parent review→done) = %v", err)
	}

	// Sanity: the parent really did land in done.
	roots, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	if len(roots) != 1 || roots[0].ID != parent.ID || roots[0].BucketKey != "done" {
		t.Fatalf("roots = %+v, want one root in done", roots)
	}
}
