package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
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

// TestCommentServiceScopeAwareGuards proves the scope-keyed comment edit
// policy from omakiten #389: a task comment is blocked in a bucket where
// comment.edit resolves to false, while a project comment is allowed because
// defaults.comment.project.edit=true — resolved task-lessly with no
// GetTaskByID call. Backward-compat (flat config = task scope) is covered by
// the resolver unit tests; here the fixture uses the explicit per-scope shape.
func TestCommentServiceScopeAwareGuards(t *testing.T) {
	ctx := context.Background()

	store := snapstore.Open(t, t.TempDir()+"/scopes.db")
	defer func() { _ = store.Close() }()
	bundle, _ := testfixtures.LoadBundle(t, "policy_comment_scopes.yaml")
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}

	workflow := NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	service := NewCommentServiceWithWorkflow(store, workflow, store.Snapshot())

	// task comment in the backlog bucket (permissions.comment.edit=false).
	taskSvc := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	task, err := taskSvc.Add(ctx, project.Context(), "T", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(task) = %v", err)
	}
	taskComment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: task.ID, Body: "task note",
	})
	if err != nil {
		t.Fatalf("AddScoped(task) = %v", err)
	}

	// task comment edit must be blocked by the bucket policy.
	if _, err := service.Edit(ctx, project.Context(), taskComment.ID, "edited", nil); err == nil {
		t.Fatal("Edit(task comment) = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// project comment edit must be allowed (defaults.comment.project.edit=true).
	projComment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeProject, Body: "project note",
	})
	if err != nil {
		t.Fatalf("AddScoped(project) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), projComment.ID, "edited project", nil); err != nil {
		t.Fatalf("Edit(project comment) = %v, want allowed", err)
	}

	// universal comment edit must be blocked (defaults.comment.universal.edit=false).
	uniComment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "universal note",
	})
	if err != nil {
		t.Fatalf("AddScoped(universal) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), uniComment.ID, "edited universal", nil); err == nil {
		t.Fatal("Edit(universal comment) = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// Bucket-free chain: a task in the "dev" bucket (which declares NO
	// permissions) must still be denied a comment edit purely by
	// defaults.comment.task.edit=false. This exercises #389's designed chain
	// itself, not a bucket override. Fails before the ResolveCommentPermission
	// fix, where defaults.comment.task was never consulted.
	devTask, err := taskSvc.Add(ctx, project.Context(), "Dev", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(dev task) = %v", err)
	}
	if _, err := taskSvc.Move(ctx, project.Context(), devTask.ID, "dev"); err != nil {
		t.Fatalf("Move(dev task -> dev) = %v", err)
	}
	devComment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeTask, TaskID: devTask.ID, Body: "dev note",
	})
	if err != nil {
		t.Fatalf("AddScoped(dev task) = %v", err)
	}
	if _, err := service.Edit(ctx, project.Context(), devComment.ID, "edited dev", nil); err == nil {
		t.Fatal("Edit(dev-bucket task comment) = nil error, want guard violation via defaults.comment.task")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}

	// Delete-scope coverage (was previously untested at the service layer):
	// task delete denied by defaults.comment.task.delete=false (dev bucket has
	// no override), project delete denied by defaults.comment.project.delete=false.
	if _, err := service.Remove(ctx, project.Context(), devComment.ID); err == nil {
		t.Fatal("Remove(task comment) = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}
	if _, err := service.Remove(ctx, project.Context(), projComment.ID); err == nil {
		t.Fatal("Remove(project comment) = nil error, want guard violation")
	} else {
		assertCodedError(t, err, domain.ErrGuardViolation)
	}
}

// TestUniversalCommentViolationIsProjectLess proves the universal-scope guard
// violation row is stored project-less (project_id IS NULL → 0 via COALESCE),
// matching how universal comments themselves are stored. Before the fix the
// emit stamped the acting project id onto a project_id-NULL entity.
func TestUniversalCommentViolationIsProjectLess(t *testing.T) {
	ctx := context.Background()

	store := snapstore.Open(t, t.TempDir()+"/uni.db")
	defer func() { _ = store.Close() }()
	bundle, _ := testfixtures.LoadBundle(t, "policy_comment_scopes.yaml")
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}

	workflow := NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	service := NewCommentServiceWithWorkflow(store, workflow, store.Snapshot())

	uni, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "universal note",
	})
	if err != nil {
		t.Fatalf("AddScoped(universal) = %v", err)
	}
	// defaults.comment.universal.edit=false denies the edit and emits a violation.
	if _, err := service.Edit(ctx, project.Context(), uni.ID, "edited", nil); err == nil {
		t.Fatal("Edit(universal) = nil error, want guard violation")
	}

	rows, err := store.ListEvents(ctx, domain.EventFilter{Categories: []domain.EventCategory{domain.EventCategoryGuard}})
	if err != nil {
		t.Fatalf("ListEvents(guard) = %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.EventType == domain.EventTypeGuardViolated && r.EntityType == domain.EventEntityUniversal {
			found = true
			if r.ProjectID != 0 {
				t.Fatalf("universal guard.violated project_id = %d, want 0 (project-less)", r.ProjectID)
			}
		}
	}
	if !found {
		t.Fatalf("no universal guard.violated row emitted; rows = %+v", rows)
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
