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

	children := addSubKitChildren(t, ctx, service, project.Context(), parent.ID)
	assertSubtaskKitGuard(t, ctx, service, project.Context(), parent.ID, children)
	walkSubKitChildren(t, ctx, service, project.Context(), children)
	closeSubtaskKitParent(t, ctx, service, project.Context(), parent.ID)
	assertSubtaskKitRoots(t, ctx, service, project.Context(), parent.ID)
	assertSubtaskKitMetadata(t, ctx, store, project.ID, children[0].ID)
}

func addSubKitChildren(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) []domain.Task {
	children := make([]domain.Task, 0, 3)
	for _, title := range []string{"c1", "c2", "c3"} {
		child, err := service.AddSub(ctx, project, parentID, title, "", "", "")
		if err != nil {
			t.Fatalf("AddSub(%s) = %v", title, err)
		}
		if child.BucketKey != "backlog" {
			t.Fatalf("AddSub(%s) bucket = %q, want sub-kit first bucket backlog", title, child.BucketKey)
		}
		children = append(children, child)
	}
	return children
}

func assertSubtaskKitGuard(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64, children []domain.Task) {
	_, err := service.Move(ctx, project, parentID, "review")
	if err == nil {
		t.Fatal("Move(parent dev→review) before children done = nil, want guard violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation || !strings.Contains(coded.Message, "subtasks_complete") {
		t.Fatalf("guard error = %v, want subtasks_complete guard violation", err)
	}
	for _, child := range children {
		if strings.Contains(coded.Message, child.Title) {
			return
		}
	}
	t.Fatalf("guard message %q must name an open direct child", coded.Message)
}

func walkSubKitChildren(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, children []domain.Task) {
	for _, child := range children {
		if _, err := service.Move(ctx, project, child.ID, "dev"); err != nil {
			t.Fatalf("Move(%s backlog→dev) = %v", child.Title, err)
		}
		if _, err := service.Move(ctx, project, child.ID, "done"); err != nil {
			t.Fatalf("Move(%s dev→done) = %v", child.Title, err)
		}
		if _, err := service.Move(ctx, project, child.ID, "review"); err == nil {
			t.Fatalf("Move(%s done→review) succeeded; sub-kit has no review bucket", child.Title)
		}
	}
}

func closeSubtaskKitParent(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) {
	if _, err := service.Move(ctx, project, parentID, "review"); err != nil {
		t.Fatalf("Move(parent dev→review) after children done = %v", err)
	}
	if _, err := service.Move(ctx, project, parentID, "done"); err != nil {
		t.Fatalf("Move(parent review→done) = %v", err)
	}
}

func assertSubtaskKitRoots(t *testing.T, ctx context.Context, service *TaskService, project domain.ProjectContext, parentID int64) {
	roots, err := service.List(ctx, project, domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	if len(roots) != 1 || roots[0].ID != parentID || roots[0].BucketKey != "done" {
		t.Fatalf("roots = %+v, want one root in done", roots)
	}
}

func assertSubtaskKitMetadata(t *testing.T, ctx context.Context, store interface {
	ListTaskActivity(context.Context, int64, int64, string) ([]domain.Event, error)
}, projectID, childID int64) {
	events, err := store.ListTaskActivity(ctx, projectID, childID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity(c1) = %v", err)
	}
	for _, event := range events {
		if event.EventType == domain.EventTypeTaskMoved && strings.Contains(event.Payload, `"resolved_kit":"izakaya"`) {
			return
		}
	}
	t.Fatalf("sub-task move events missing resolved_kit=izakaya; events=%+v", events)
}
