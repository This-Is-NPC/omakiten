package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestCommentServiceAdd(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store)
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	service := NewCommentService(store)

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

	taskService := NewTaskServiceFromStore(store)
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	service := NewCommentService(store)
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
