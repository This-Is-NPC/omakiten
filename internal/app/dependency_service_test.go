package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestDependencyServiceAdd(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskService(store)
	taskA, _ := taskService.Add(ctx, project.Context(), "A", "", "", "backlog")
	taskB, _ := taskService.Add(ctx, project.Context(), "B", "", "", "backlog")
	taskC, _ := taskService.Add(ctx, project.Context(), "C", "", "", "backlog")
	taskD, _ := taskService.Add(ctx, project.Context(), "D", "", "", "backlog")

	service := NewDependencyService(store)

	_, err := service.Add(ctx, project.Context(), 0, taskA.ID)
	if err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	_, err = service.Add(ctx, project.Context(), taskA.ID, taskA.ID)
	if err == nil {
		t.Fatal("Add() self-dependency error = nil")
	}
	assertCodedError(t, err, domain.ErrDependencyInvalid)

	// Happy path: A depends on B
	dep, err := service.Add(ctx, project.Context(), taskA.ID, taskB.ID)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if dep.TaskID != taskA.ID || dep.DependsOnTaskID != taskB.ID {
		t.Fatalf("Add() = %#v, want A depends on B", dep)
	}

	// C depends on B (no cycle)
	if _, err := service.Add(ctx, project.Context(), taskC.ID, taskB.ID); err != nil {
		t.Fatalf("Add(C->B) error = %v", err)
	}

	// D depends on C (still no cycle)
	if _, err := service.Add(ctx, project.Context(), taskD.ID, taskC.ID); err != nil {
		t.Fatalf("Add(D->C) error = %v", err)
	}

	// Cycle: B depends on D, completing B -> D -> C -> B... wait, D->C exists, not C->D.
	// Let's create a real cycle: B -> A (A already exists, but B->A + A->B is a 2-node cycle)
	// Better: create C -> A and then try A -> C which creates A -> C -> B -> ... no, A->B and C->B.
	// Let's make A -> D which is fine, then try D -> A creating a cycle D -> A -> D
	if _, err := service.Add(ctx, project.Context(), taskA.ID, taskD.ID); err != nil {
		t.Fatalf("Add(A->D) error = %v", err)
	}
	_, err = service.Add(ctx, project.Context(), taskD.ID, taskA.ID)
	if err == nil {
		t.Fatal("Add() cycle error = nil")
	}
	assertCodedError(t, err, domain.ErrDependencyInvalid)
}

func TestDependencyServiceRemove(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskService(store)
	taskA, _ := taskService.Add(ctx, project.Context(), "A", "", "", "backlog")
	taskB, _ := taskService.Add(ctx, project.Context(), "B", "", "", "backlog")

	service := NewDependencyService(store)
	if _, err := service.Add(ctx, project.Context(), taskB.ID, taskA.ID); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	err := service.Remove(ctx, project.Context(), 0, taskA.ID)
	if err == nil {
		t.Fatal("Remove() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	if err := service.Remove(ctx, project.Context(), taskB.ID, taskA.ID); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestDependencyServiceList(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskService(store)
	taskA, _ := taskService.Add(ctx, project.Context(), "A", "", "", "backlog")
	taskB, _ := taskService.Add(ctx, project.Context(), "B", "", "", "backlog")

	service := NewDependencyService(store)
	if _, err := service.Add(ctx, project.Context(), taskB.ID, taskA.ID); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, err := service.List(ctx, project.Context(), -1)
	if err == nil {
		t.Fatal("List() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	all, err := service.List(ctx, project.Context(), 0)
	if err != nil {
		t.Fatalf("List(0) error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List(0) len = %d, want 1", len(all))
	}

	filtered, err := service.List(ctx, project.Context(), taskB.ID)
	if err != nil {
		t.Fatalf("List(B) error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("List(B) len = %d, want 1", len(filtered))
	}
}
