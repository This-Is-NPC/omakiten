package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestDependencyServiceAdd(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())
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
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())
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

func TestDependencyServiceSyncBlockers(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())
	main, _ := taskService.Add(ctx, project.Context(), "main", "", "", "backlog")
	a, _ := taskService.Add(ctx, project.Context(), "a", "", "", "backlog")
	b, _ := taskService.Add(ctx, project.Context(), "b", "", "", "backlog")
	c, _ := taskService.Add(ctx, project.Context(), "c", "", "", "backlog")

	service := NewDependencyService(store)

	// Initial sync — adds a and b.
	if err := service.SyncBlockers(ctx, project.Context(), main.ID, []int64{a.ID, b.ID}); err != nil {
		t.Fatalf("SyncBlockers(initial) error = %v", err)
	}
	got, _ := service.List(ctx, project.Context(), main.ID)
	if len(got) != 2 {
		t.Fatalf("len(deps) after initial = %d, want 2", len(got))
	}

	// Reshape to {b, c} — removes a, adds c, keeps b.
	if err := service.SyncBlockers(ctx, project.Context(), main.ID, []int64{b.ID, c.ID}); err != nil {
		t.Fatalf("SyncBlockers(reshape) error = %v", err)
	}
	got, _ = service.List(ctx, project.Context(), main.ID)
	if len(got) != 2 {
		t.Fatalf("len(deps) after reshape = %d, want 2", len(got))
	}
	have := map[int64]bool{}
	for _, d := range got {
		have[d.DependsOnTaskID] = true
	}
	if !have[b.ID] || !have[c.ID] || have[a.ID] {
		t.Fatalf("deps after reshape = %+v, want b+c only", got)
	}

	// Sync to empty — drops everything.
	if err := service.SyncBlockers(ctx, project.Context(), main.ID, nil); err != nil {
		t.Fatalf("SyncBlockers(empty) error = %v", err)
	}
	got, _ = service.List(ctx, project.Context(), main.ID)
	if len(got) != 0 {
		t.Fatalf("len(deps) after empty = %d, want 0", len(got))
	}

	// Validates task id.
	if err := service.SyncBlockers(ctx, project.Context(), 0, nil); err == nil {
		t.Fatal("SyncBlockers(taskID=0) error = nil, want validation error")
	}
}

func TestDependencyServiceList(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry())
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
