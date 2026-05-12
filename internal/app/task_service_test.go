package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestTaskServiceAdd(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())

	_, err := service.Add(ctx, project.Context(), "", "", "", "")
	if err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	task, err := service.Add(ctx, project.Context(), " Title ", " Description ", "", " backlog ")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if task.Title != "Title" {
		t.Fatalf("Add().Title = %q, want %q", task.Title, "Title")
	}
	if task.Description != "Description" {
		t.Fatalf("Add().Description = %q, want %q", task.Description, "Description")
	}
	if task.BucketKey != "backlog" {
		t.Fatalf("Add().BucketKey = %q, want %q", task.BucketKey, "backlog")
	}
	if task.Priority != domain.Priority(2) {
		t.Fatalf("Add().Priority = %q, want %q", task.Priority, domain.Priority(2))
	}

	highTask, err := service.Add(ctx, project.Context(), "High", "", "high", "backlog")
	if err != nil {
		t.Fatalf("Add(high priority) error = %v", err)
	}
	if highTask.Priority != domain.Priority(3) {
		t.Fatalf("Add(high).Priority = %q, want %q", highTask.Priority, domain.Priority(3))
	}

	_, err = service.Add(ctx, project.Context(), "Invalid", "", "urgent", "backlog")
	if err == nil {
		t.Fatal("Add(invalid priority) error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceList(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())
	if _, err := service.Add(ctx, project.Context(), "A", "", "", "backlog"); err != nil {
		t.Fatalf("Add(A) error = %v", err)
	}
	if _, err := service.Add(ctx, project.Context(), "B", "", "", "dev"); err != nil {
		t.Fatalf("Add(B) error = %v", err)
	}

	all, err := service.List(ctx, project.Context(), domain.TaskFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List() len = %d, want 2", len(all))
	}

	filtered, err := service.List(ctx, project.Context(), domain.TaskFilter{BucketKey: "backlog"})
	if err != nil {
		t.Fatalf("List(backlog) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].Title != "A" {
		t.Fatalf("List(backlog) = %#v, want 1 task A", filtered)
	}
}

func TestTaskServiceMove(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())

	_, err := service.Move(ctx, project.Context(), 0, "dev")
	if err == nil {
		t.Fatal("Move() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Move(ctx, project.Context(), 1, "")
	if err == nil {
		t.Fatal("Move() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	task, err := service.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	moved, err := service.Move(ctx, project.Context(), task.ID, "dev")
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("Move().BucketKey = %q, want %q", moved.BucketKey, "dev")
	}
}

func TestTaskServiceEdit(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())

	_, err := service.Edit(ctx, project.Context(), 0, domain.TaskUpdate{})
	if err == nil {
		t.Fatal("Edit() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Edit(ctx, project.Context(), 1, domain.TaskUpdate{})
	if err == nil {
		t.Fatal("Edit() no changes error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	task, err := service.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	title := ""
	_, err = service.Edit(ctx, project.Context(), task.ID, domain.TaskUpdate{Title: &title})
	if err == nil {
		t.Fatal("Edit() empty title error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	// Priority id 999 is not in the registered table → must reject.
	invalidPriority := domain.Priority(999)
	_, err = service.Edit(ctx, project.Context(), task.ID, domain.TaskUpdate{Priority: &invalidPriority})
	if err == nil {
		t.Fatal("Edit() invalid priority error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	// Move + update combined
	newTitle := "Updated"
	newDesc := "Desc"
	normalPriority := domain.Priority(2)
	edited, err := service.Edit(ctx, project.Context(), task.ID, domain.TaskUpdate{
		Title:       &newTitle,
		Description: &newDesc,
		Priority:    &normalPriority,
		BucketKey:   "dev",
	})
	if err != nil {
		t.Fatalf("Edit() combined error = %v", err)
	}
	if edited.Title != "Updated" {
		t.Fatalf("Edit().Title = %q, want %q", edited.Title, "Updated")
	}
	if edited.Description != "Desc" {
		t.Fatalf("Edit().Description = %q, want %q", edited.Description, "Desc")
	}
	if edited.Priority != domain.Priority(2) {
		t.Fatalf("Edit().Priority = %q, want %q", edited.Priority, domain.Priority(2))
	}
	if edited.BucketKey != "dev" {
		t.Fatalf("Edit().BucketKey = %q, want %q", edited.BucketKey, "dev")
	}
}

func assertCodedError(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T %v, want CodedError", err, err)
	}
	if coded.Code != code {
		t.Fatalf("CodedError.Code = %q, want %q", coded.Code, code)
	}
}
