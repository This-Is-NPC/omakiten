package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestCommentServiceAdd(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	service := NewCommentService(store, store.Snapshot())

	_, err = service.Add(ctx, project.Context(), 0, "body", "human", nil)
	if err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Add(ctx, project.Context(), task.ID, "", "human", nil)
	if err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	comment, err := service.Add(ctx, project.Context(), task.ID, " Note ", "", nil)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if comment.Body != "Note" {
		t.Fatalf("Add().Body = %q, want %q", comment.Body, "Note")
	}
	if comment.AuthorType != "human" {
		t.Fatalf("Add().AuthorType = %q, want %q", comment.AuthorType, "human")
	}

	_, err = service.Add(ctx, project.Context(), task.ID, "body", "invalid", nil)
	if err == nil {
		t.Fatal("Add() invalid authorType error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	agentComment, err := service.Add(ctx, project.Context(), task.ID, "agent note", "agent", nil)
	if err != nil {
		t.Fatalf("Add() agent error = %v", err)
	}
	if agentComment.AuthorType != "agent" {
		t.Fatalf("Add().AuthorType = %q, want %q", agentComment.AuthorType, "agent")
	}
}

func TestCommentServiceList(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	service := NewCommentService(store, store.Snapshot())
	if _, err := service.Add(ctx, project.Context(), task.ID, "A", "human", nil); err != nil {
		t.Fatalf("Add(A) error = %v", err)
	}

	_, err = service.List(ctx, project.Context(), -1)
	if err == nil {
		t.Fatal("List() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	comments, err := service.List(ctx, project.Context(), task.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("List() len = %d, want 1", len(comments))
	}
}

func TestCommentServiceAddScoped(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	service := NewCommentService(store, store.Snapshot())

	// task scope still requires a positive task id.
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeTask, Body: "x"}); err == nil {
		t.Fatal("AddScoped(task, no id) = nil error, want validation")
	}

	// project scope: no task id needed, note fields carried.
	projC, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: " recap ", Kind: "recap", Title: "Week", Pinned: true,
	})
	if err != nil {
		t.Fatalf("AddScoped(project) error = %v", err)
	}
	if projC.Scope != domain.CommentScopeProject || projC.Body != "recap" || projC.Kind != "recap" || !projC.Pinned {
		t.Fatalf("AddScoped(project) = %+v", projC)
	}

	// universal scope.
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeUniversal, Body: "global"}); err != nil {
		t.Fatalf("AddScoped(universal) error = %v", err)
	}

	// task scope with a real task and a tag.
	taskC, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "note", AuthorType: "agent",
		Tags: []domain.Tag{{Name: "resume", Label: "resume"}},
	})
	if err != nil {
		t.Fatalf("AddScoped(task) error = %v", err)
	}
	if len(taskC.Tags) != 1 || taskC.Tags[0].Name != "resume" {
		t.Fatalf("AddScoped(task) tags = %+v", taskC.Tags)
	}

	// empty body rejected.
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeProject, Body: "  "}); err == nil {
		t.Fatal("AddScoped(empty body) = nil error, want validation")
	}
	// bad author rejected.
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeProject, Body: "x", AuthorType: "robot"}); err == nil {
		t.Fatal("AddScoped(bad author) = nil error, want validation")
	}
}

func TestCommentServiceQuery(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewCommentService(store, store.Snapshot())
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeProject, Body: "a", Kind: "recap", Pinned: true}); err != nil {
		t.Fatalf("AddScoped(a) error = %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{Scope: domain.CommentScopeUniversal, Body: "b", Kind: "handoff"}); err != nil {
		t.Fatalf("AddScoped(b) error = %v", err)
	}

	pinned, err := service.Query(ctx, project.Context(), domain.CommentFilter{PinnedOnly: true})
	if err != nil {
		t.Fatalf("Query(pinned) error = %v", err)
	}
	if len(pinned) != 1 || pinned[0].Body != "a" {
		t.Fatalf("Query(pinned) = %+v", pinned)
	}

	handoff, err := service.Query(ctx, project.Context(), domain.CommentFilter{Kind: "handoff"})
	if err != nil {
		t.Fatalf("Query(handoff) error = %v", err)
	}
	if len(handoff) != 1 || handoff[0].Body != "b" {
		t.Fatalf("Query(handoff) = %+v", handoff)
	}
}
