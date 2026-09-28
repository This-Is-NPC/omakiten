package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

// TestSubtasksEndToEndSmoke walks the full DoD smoke flow for task #190:
//
//  1. Create a parent task in dev.
//  2. Attach three sub-tasks (c1, c2, c3) — landing in the root kit's
//     first bucket (backlog), not the parent's bucket.
//  3. Attach a grandchild under c1.
//  4. Attempt dev→review on the parent — the subtasks_complete guard
//     fires and names the first open child found.
//  5. Walk every descendant through backlog → dev → review → done so
//     each level's subtasks_complete guard clears in order (grandchild →
//     c1, then c1/c2/c3 → parent).
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

	projectCtx := project.Context()
	children := addSubtasks(t, ctx, service, projectCtx, parent.ID)
	grandchild := addSubtask(t, ctx, service, projectCtx, children[0].ID, "gc1")
	assertSubtasksGuard(t, ctx, service, projectCtx, parent.ID, children)
	walkTasksToDone(t, ctx, service, projectCtx, grandchild, children)
	closeParent(t, ctx, service, projectCtx, parent.ID)
	assertRootDone(t, ctx, service, projectCtx, parent.ID)
}

func addSubtasks(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) []domain.Task {
	children := make([]domain.Task, 0, 3)
	for _, title := range []string{"c1", "c2", "c3"} {
		children = append(children, addSubtask(t, ctx, service, project, parentID, title))
	}
	return children
}

func addSubtask(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64, title string) domain.Task {
	child, err := service.AddSub(ctx, project, parentID, title, "", "", "")
	if err != nil {
		t.Fatalf("AddSub(%s) = %v", title, err)
	}
	if child.BucketKey != "backlog" {
		t.Fatalf("AddSub(%s) bucket = %q, want backlog (root kit first bucket)", title, child.BucketKey)
	}
	return child
}

func assertSubtasksGuard(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64, children []domain.Task) {
	_, err := service.Move(ctx, project, parentID, "review")
	if err == nil {
		t.Fatal("Move(parent dev→review) error = nil, want guard violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation || !strings.Contains(coded.Message, "subtasks_complete") {
		t.Fatalf("Move(parent) error = %v, want subtasks_complete guard violation", err)
	}
	for _, child := range children {
		if strings.Contains(coded.Message, child.Title) {
			return
		}
	}
	t.Fatalf("guard message %q must name an open direct child (c1/c2/c3)", coded.Message)
}

func walkTasksToDone(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, grandchild domain.Task, children []domain.Task) {
	walkTaskToDone(t, ctx, service, project, grandchild.ID, grandchild.Title)
	for _, child := range children {
		walkTaskToDone(t, ctx, service, project, child.ID, child.Title)
	}
}

func walkTaskToDone(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, taskID int64, label string) {
	for _, bucket := range []string{"dev", "review", "done"} {
		if _, err := service.Move(ctx, project, taskID, bucket); err != nil {
			t.Fatalf("Move(%s -> %s) = %v", label, bucket, err)
		}
	}
}

func closeParent(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) {
	if _, err := service.Move(ctx, project, parentID, "review"); err != nil {
		t.Fatalf("Move(parent dev→review) after children done = %v", err)
	}
	if _, err := service.Move(ctx, project, parentID, "done"); err != nil {
		t.Fatalf("Move(parent review→done) = %v", err)
	}
}

func assertRootDone(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) {
	roots, err := service.List(ctx, project, domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	if len(roots) != 1 || roots[0].ID != parentID || roots[0].BucketKey != "done" {
		t.Fatalf("roots = %+v, want one root in done", roots)
	}
}

func TestSubtasksCompleteUsesSubtaskKitFinalBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle, registry := testfixtures.LoadBundle(t, "subtasks_smoke.yaml")
	bundle.SubtaskBundle = subtaskRuntimeBundle("izakaya", []config.Bucket{
		{ID: 2, Key: "dev", Name: "Development", Position: 1},
		{ID: 3, Key: "done", Name: "Done", Position: 2},
	}, []config.Transition{{From: 2, To: 3}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, registry, store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(child) = %v", err)
	}
	if child.BucketKey != "dev" {
		t.Fatalf("child.BucketKey = %q, want sub-kit first bucket dev", child.BucketKey)
	}
	if _, err := service.Move(ctx, project.Context(), child.ID, "done"); err != nil {
		t.Fatalf("Move(child dev->done in sub-kit) = %v", err)
	}
	if _, err := service.Move(ctx, project.Context(), parent.ID, "review"); err != nil {
		t.Fatalf("Move(parent dev->review) after child reached sub-kit done = %v", err)
	}
}
