package agent

import (
	"testing"

	"omakiten/internal/domain"
)

// TestAddCommentScopeValidation covers the scope/task_id matrix the agent
// surface enforces before reaching AddScoped.
func TestAddCommentScopeValidation(t *testing.T) {
	fixture := newAgentFixture(t)

	// task scope without task_id is rejected.
	_, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{Scope: domain.CommentScopeTask, Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)

	// project scope must not carry task_id.
	_, err = fixture.service.AddComment(fixture.ctx, AddCommentInput{Scope: domain.CommentScopeProject, TaskID: fixture.taskA1.ID, Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)

	// universal scope must not carry task_id.
	_, err = fixture.service.AddComment(fixture.ctx, AddCommentInput{Scope: domain.CommentScopeUniversal, TaskID: fixture.taskA1.ID, Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)

	// unknown scope is rejected.
	_, err = fixture.service.AddComment(fixture.ctx, AddCommentInput{Scope: "bogus", Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)
}

// TestAddCommentEachScope creates a comment at task, project, and universal
// scope and verifies the returned summary carries scope + note-like fields.
func TestAddCommentEachScope(t *testing.T) {
	fixture := newAgentFixture(t)

	taskC, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		TaskID: fixture.taskA1.ID, Body: "task note", AuthorType: "agent",
		Kind: "handoff", Title: "T", Pinned: true,
	})
	if err != nil {
		t.Fatalf("AddComment(task) error = %v", err)
	}
	if taskC.Comment.Scope != domain.CommentScopeTask {
		t.Fatalf("task scope = %q, want task", taskC.Comment.Scope)
	}
	if taskC.Comment.Kind != "handoff" || taskC.Comment.Title != "T" || !taskC.Comment.Pinned {
		t.Fatalf("task note-like fields = %+v, want kind/title/pinned set", taskC.Comment)
	}

	projC, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "project note", AuthorType: "agent", Kind: "recap",
	})
	if err != nil {
		t.Fatalf("AddComment(project) error = %v", err)
	}
	if projC.Comment.Scope != domain.CommentScopeProject {
		t.Fatalf("project scope = %q, want project", projC.Comment.Scope)
	}

	uniC, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeUniversal, Body: "global note", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddComment(universal) error = %v", err)
	}
	if uniC.Comment.Scope != domain.CommentScopeUniversal {
		t.Fatalf("universal scope = %q, want universal", uniC.Comment.Scope)
	}
}

// TestListCommentsFilters exercises the filterable handoff-log path: scope,
// kind, tag, pinned-only, and FTS query.
func TestListCommentsFilters(t *testing.T) {
	fixture := newAgentFixture(t)

	if _, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "deploy plan alpha", AuthorType: "agent",
		Kind: "recap", Pinned: true, Tags: []string{"deploy"},
	}); err != nil {
		t.Fatalf("seed project comment: %v", err)
	}
	if _, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "standup beta", AuthorType: "agent", Kind: "standup",
	}); err != nil {
		t.Fatalf("seed project comment 2: %v", err)
	}

	// scope=project returns both project rows (not the task-scoped seed).
	byScope, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{Scope: domain.CommentScopeProject})
	if err != nil {
		t.Fatalf("ListComments(scope) error = %v", err)
	}
	if len(byScope.Comments) != 2 {
		t.Fatalf("ListComments(scope=project) = %d rows, want 2", len(byScope.Comments))
	}

	// kind filter narrows to recap.
	byKind, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{Kind: "recap"})
	if err != nil {
		t.Fatalf("ListComments(kind) error = %v", err)
	}
	if len(byKind.Comments) != 1 || byKind.Comments[0].Kind != "recap" {
		t.Fatalf("ListComments(kind=recap) = %+v, want one recap", byKind.Comments)
	}

	// pinned-only returns the single pinned row.
	byPinned, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{Pinned: true})
	if err != nil {
		t.Fatalf("ListComments(pinned) error = %v", err)
	}
	if len(byPinned.Comments) != 1 || !byPinned.Comments[0].Pinned {
		t.Fatalf("ListComments(pinned) = %+v, want one pinned", byPinned.Comments)
	}

	// tag filter narrows to the deploy-tagged row.
	byTag, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{Tag: "deploy"})
	if err != nil {
		t.Fatalf("ListComments(tag) error = %v", err)
	}
	if len(byTag.Comments) != 1 {
		t.Fatalf("ListComments(tag=deploy) = %+v, want one row", byTag.Comments)
	}

	// FTS query matches the body token.
	byQuery, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{Query: "alpha"})
	if err != nil {
		t.Fatalf("ListComments(query) error = %v", err)
	}
	if len(byQuery.Comments) != 1 {
		t.Fatalf("ListComments(query=alpha) = %+v, want one row", byQuery.Comments)
	}
}

// TestListCommentsDefaultUnchanged confirms the task-only listing still returns
// the seeded task comment via the original List path.
func TestListCommentsDefaultUnchanged(t *testing.T) {
	fixture := newAgentFixture(t)

	resp, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ListComments(default) error = %v", err)
	}
	if len(resp.Comments) != 1 || resp.Comments[0].Body != "A comment" {
		t.Fatalf("ListComments(default) = %+v, want the seeded A comment", resp.Comments)
	}
}

// TestEditCommentScopedFields edits a comment's title/kind/pinned alongside its
// body and verifies the patch round-trips.
func TestEditCommentScopedFields(t *testing.T) {
	fixture := newAgentFixture(t)

	created, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "before", AuthorType: "agent", Kind: "draft",
	})
	if err != nil {
		t.Fatalf("AddComment(project) error = %v", err)
	}

	pinned := true
	edited, err := fixture.service.EditComment(fixture.ctx, EditCommentInput{
		CommentID: created.Comment.ID, Body: "after", Title: "Final", Kind: "recap", Pinned: &pinned,
	})
	if err != nil {
		t.Fatalf("EditComment() error = %v", err)
	}
	if edited.Comment.Body != "after" || edited.Comment.Title != "Final" || edited.Comment.Kind != "recap" || !edited.Comment.Pinned {
		t.Fatalf("EditComment() = %+v, want body/title/kind/pinned applied", edited.Comment)
	}
}
