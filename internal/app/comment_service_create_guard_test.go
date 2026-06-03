package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

// TestCommentServiceCreateGuard proves the comment.create guard from
// omakiten #404, mirroring TestCommentServiceScopeAwareGuards: a real sqlite
// store + the policy_comment_create_scopes.yaml fixture, exercising the workflow-bound
// CommentService (the production composition for comments.add / okt comment add).
//
// Fixture create policy:
//   - bucket backlog: comment.create=true   → task create allowed
//   - bucket dev:     comment.create=false  → task create blocked (bucket-granular)
//   - defaults.comment.project.create=false → project create blocked
//   - defaults.comment.universal.create=true → universal create allowed
func TestCommentServiceCreateGuard(t *testing.T) {
	ctx := context.Background()

	store := snapstore.Open(t, t.TempDir()+"/create.db")
	defer func() { _ = store.Close() }()
	bundle, _ := testfixtures.LoadBundle(t, "policy_comment_create_scopes.yaml")
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}

	workflow := NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	service := NewCommentServiceWithWorkflow(store, workflow, store.Snapshot())
	taskSvc := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	// ---- task scope: bucket-granular (the headline example) ----
	// backlog bucket permits comment.create.
	openTask, err := taskSvc.Add(ctx, project.Context(), "Open", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(open task) = %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: openTask.ID, Body: "ok",
	}); err != nil {
		t.Fatalf("create in permissive bucket: want allow, got %v", err)
	}

	// dev bucket forbids comment.create.
	lockedTask, err := taskSvc.Add(ctx, project.Context(), "Locked", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(locked task) = %v", err)
	}
	if _, err := taskSvc.Move(ctx, project.Context(), lockedTask.ID, "dev"); err != nil {
		t.Fatalf("Move(locked task -> dev) = %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: lockedTask.ID, Body: "blocked",
	}); err == nil {
		t.Fatal("create in locked bucket = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// ---- project scope: denied by defaults.comment.project.create=false ----
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: "blocked",
	}); err == nil {
		t.Fatal("project create = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// ---- universal scope: allowed by defaults.comment.universal.create=true ----
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "global",
	}); err != nil {
		t.Fatalf("universal create: want allow, got %v", err)
	}
}

// TestCommentServiceCreateGuardBackCompat proves that with no comment.create
// rule declared at any layer, create stays allowed at every scope — no
// behaviour change for existing configs. The non-workflow CommentService
// (read-only composition) likewise never gates create.
func TestCommentServiceCreateGuardBackCompat(t *testing.T) {
	ctx := context.Background()

	store := snapstore.Open(t, t.TempDir()+"/create_backcompat.db")
	defer func() { _ = store.Close() }()
	// appTestBundle ships a canonical workflow with no comment.create policy.
	store2, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store2.Close() }()
	_ = store

	workflow := NewWorkflowServiceFromStore(store2, testfixtures.CanonicalRegistry(), store2.Snapshot())
	service := NewCommentServiceWithWorkflow(store2, workflow, store2.Snapshot())
	taskSvc := NewTaskServiceFromStore(store2, testfixtures.CanonicalRegistry(), store2.Snapshot())

	task, err := taskSvc.Add(ctx, project.Context(), "T", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(task) = %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "task note",
	}); err != nil {
		t.Fatalf("no rule => task create must be allowed, got %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: "project note",
	}); err != nil {
		t.Fatalf("no rule => project create must be allowed, got %v", err)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "universal note",
	}); err != nil {
		t.Fatalf("no rule => universal create must be allowed, got %v", err)
	}

	// Non-workflow service skips the guard entirely (read-only composition).
	plain := NewCommentService(store2, store2.Snapshot())
	if _, err := plain.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "x",
	}); err != nil {
		t.Fatalf("plain service must skip create guard, got %v", err)
	}
}
