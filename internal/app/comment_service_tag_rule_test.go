package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

// TestCommentServiceTagRuleGuards proves the tag-conditional comment guards
// from omakiten #405 end-to-end against a real sqlite store + the
// policy_comment_tag_rules.yaml fixture, covering all four acceptance criteria:
//
//  1. require_tags on project create  — denied without the tag, allowed with it.
//  2. deny_tags on task-scope edit    — denied editing a comment carrying the
//     denied tag (predicate reads the STORED comment tags).
//  3. require_any_tag on universal create — denied for an untagged comment,
//     allowed once any tag is attached.
//  4. bucket + rule combine           — deny_tags on a done-bucket edit blocks
//     editing a locked-tagged comment on a task in that bucket.
func TestCommentServiceTagRuleGuards(t *testing.T) {
	ctx := context.Background()

	store := snapstore.Open(t, t.TempDir()+"/tagrules.db")
	defer func() { _ = store.Close() }()
	bundle, _ := testfixtures.LoadBundle(t, "policy_comment_tag_rules.yaml")
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

	tag := func(name string) domain.Tag { return domain.Tag{Name: name, Label: name} }

	// ---- Acceptance 1: require_tags on project create ----
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: "missing x",
	}); err == nil {
		t.Fatal("project create without tag x = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: "has x", Tags: []domain.Tag{tag("x")},
	}); err != nil {
		t.Fatalf("project create with tag x: want allow, got %v", err)
	}

	// ---- Acceptance 3: require_any_tag on universal create ----
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "untagged",
	}); err == nil {
		t.Fatal("universal create untagged = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}
	if _, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "tagged", Tags: []domain.Tag{tag("anything")},
	}); err != nil {
		t.Fatalf("universal create with a tag: want allow, got %v", err)
	}

	// ---- Acceptance 2: deny_tags on task-scope edit, stored tags as source ----
	task, err := taskSvc.Add(ctx, project.Context(), "T", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(task) = %v", err)
	}
	// A comment WITHOUT the denied tag edits fine.
	plain, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "plain",
	})
	if err != nil {
		t.Fatalf("AddScoped(plain task comment) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), plain.ID, "plain edited", nil); err != nil {
		t.Fatalf("edit comment without denied tag: want allow, got %v", err)
	}
	// A comment carrying the denied tag y cannot be edited (stored tags trip
	// deny_tags even though the edit itself supplies no tags).
	tagged, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "tagged y", Tags: []domain.Tag{tag("y")},
	})
	if err != nil {
		t.Fatalf("AddScoped(y-tagged comment) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), tagged.ID, "blocked", []string{"y"}); err == nil {
		t.Fatal("edit y-tagged comment = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// ---- Acceptance 4: bucket + rule combine (done bucket deny_tags:[locked]) ----
	doneTask, err := taskSvc.Add(ctx, project.Context(), "D", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(done task) = %v", err)
	}
	locked, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: doneTask.ID, Body: "locked note", Tags: []domain.Tag{tag("locked")},
	})
	if err != nil {
		t.Fatalf("AddScoped(locked comment) = %v", err)
	}
	if _, err := taskSvc.Move(ctx, project.Context(), doneTask.ID, "done"); err != nil {
		t.Fatalf("Move(done task -> done) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), locked.ID, "blocked", []string{"locked"}); err == nil {
		t.Fatal("edit locked comment in done bucket = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}
}
