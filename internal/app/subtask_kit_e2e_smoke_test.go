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

// TestSubtaskKitEndToEndSmoke walks the full DoD scenario for the
// sub-task kit cascade (#281): a root kit `omakase` (backlog/dev/
// review/done) plus a sub-kit `izakaya` (backlog/dev/done, no
// `review`). The smoke confirms:
//
//  1. Parent lands in the root kit's bucket.
//  2. AddSub lands children in the sub-kit's first bucket (NOT the
//     root's first bucket).
//  3. Children walk izakaya backlog → dev → done without a `review`
//     step (the bucket does not exist in the sub-kit).
//  4. The `subtasks_complete` guard on the parent's omakase dev →
//     review fires while any child is open and reads the SUB-KIT's
//     final bucket (`done`) to decide success.
//  5. Once children are done, the parent walks dev → review → done
//     through the root kit.
//
// Service-layer integration matches `TestSubtasksEndToEndSmoke` (the
// #190 baseline) so the DoD smoke runs without standing up a TUI.
func TestSubtaskKitEndToEndSmoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bundle, registry := testfixtures.LoadBundle(t, "subtasks_smoke.yaml")
	bundle.Kit = config.Kit{ID: 1, Key: "omakase", Name: "omakase"}
	bundle.SubtaskBundle = subtaskRuntimeBundle("izakaya", []config.Bucket{
		{ID: 10, Key: "backlog", Name: "Backlog", Position: 1},
		{ID: 11, Key: "dev", Name: "Development", Position: 2},
		{ID: 12, Key: "done", Name: "Done", Position: 3},
	}, []config.Transition{
		{From: 10, To: 11},
		{From: 11, To: 12},
	})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, registry, store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	if parent.BucketKey != "dev" {
		t.Fatalf("parent.BucketKey = %q, want dev (root kit)", parent.BucketKey)
	}

	addChild := func(title string) domain.Task {
		t.Helper()
		child, err := service.AddSub(ctx, project.Context(), parent.ID, title, "", "", "")
		if err != nil {
			t.Fatalf("AddSub(%s) = %v", title, err)
		}
		if child.BucketKey != "backlog" {
			t.Fatalf("AddSub(%s) bucket = %q, want sub-kit first bucket backlog", title, child.BucketKey)
		}
		return child
	}

	c1 := addChild("c1")
	c2 := addChild("c2")
	c3 := addChild("c3")

	// Parent dev → review must fail while any sub-task is still open,
	// and the guard message must name a directly open child.
	_, err = service.Move(ctx, project.Context(), parent.ID, "review")
	if err == nil {
		t.Fatal("Move(parent dev→review) before children done = nil, want guard violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("guard error code = %v, want ErrGuardViolation", err)
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
		t.Fatalf("guard message %q must name an open direct child", coded.Message)
	}

	walkSubKit := func(child domain.Task) {
		t.Helper()
		if _, err := service.Move(ctx, project.Context(), child.ID, "dev"); err != nil {
			t.Fatalf("Move(%s backlog→dev) = %v", child.Title, err)
		}
		if _, err := service.Move(ctx, project.Context(), child.ID, "done"); err != nil {
			t.Fatalf("Move(%s dev→done) = %v", child.Title, err)
		}
		// `review` is intentionally missing from the sub-kit. Asserting
		// the transition is rejected pins the cascade contract: sub-task
		// moves resolve through the sub-kit's workflow, not the root's.
		if _, err := service.Move(ctx, project.Context(), child.ID, "review"); err == nil {
			t.Fatalf("Move(%s done→review) succeeded; sub-kit has no review bucket", child.Title)
		}
	}
	walkSubKit(c1)
	walkSubKit(c2)
	walkSubKit(c3)

	// All children in sub-kit `done` — parent dev → review must pass
	// because the guard reads the child's resolved kit final bucket
	// (sub-kit `done`), not the root's final bucket (`done` here too
	// but for a different reason — the spec's success path).
	if _, err := service.Move(ctx, project.Context(), parent.ID, "review"); err != nil {
		t.Fatalf("Move(parent dev→review) after children done = %v", err)
	}
	if _, err := service.Move(ctx, project.Context(), parent.ID, "done"); err != nil {
		t.Fatalf("Move(parent review→done) = %v", err)
	}

	// Sanity: parent landed in done; children are not surfaced in the
	// roots-only view (sub-tasks stay hidden from the board).
	roots, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	if len(roots) != 1 || roots[0].ID != parent.ID || roots[0].BucketKey != "done" {
		t.Fatalf("roots = %+v, want one root in done", roots)
	}

	// Sub-task event metadata must carry the sub-kit identity so hooks
	// dispatched against depth ≥ 1 resolve through izakaya. Reads back
	// the activity log for one child's move and confirms the resolved
	// kit string. Pinned in #284 but rechecked here as a closeout gate.
	events, err := store.ListTaskActivity(ctx, project.ID, c1.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity(c1) = %v", err)
	}
	var sawSubKitMetadata bool
	for _, ev := range events {
		if ev.EventType != domain.EventTypeTaskMoved {
			continue
		}
		if strings.Contains(ev.Payload, `"resolved_kit":"izakaya"`) {
			sawSubKitMetadata = true
			break
		}
	}
	if !sawSubKitMetadata {
		t.Fatalf("sub-task move events missing resolved_kit=izakaya; events=%+v", events)
	}
}
