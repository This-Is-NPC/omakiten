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
// TestListCommentsByID proves comments.list with comment_id returns exactly the
// named row, and that a universal note (project_id NULL) is reachable by id even
// though the routine scoped list would project-scope it out. Fails before the
// comment_id filter existed.
func TestListCommentsByID(t *testing.T) {
	fixture := newAgentFixture(t)

	taskC, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		TaskID: fixture.taskA1.ID, Body: "task note", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddComment(task) error = %v", err)
	}
	uniC, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeUniversal, Body: "global note", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddComment(universal) error = %v", err)
	}

	got, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{CommentID: taskC.Comment.ID})
	if err != nil {
		t.Fatalf("ListComments(comment_id task) error = %v", err)
	}
	if len(got.Comments) != 1 || got.Comments[0].ID != taskC.Comment.ID || got.Comments[0].Body != "task note" {
		t.Fatalf("ListComments(comment_id=%d) = %+v, want exactly the task comment", taskC.Comment.ID, got.Comments)
	}

	gotUni, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{CommentID: uniC.Comment.ID})
	if err != nil {
		t.Fatalf("ListComments(comment_id universal) error = %v", err)
	}
	if len(gotUni.Comments) != 1 || gotUni.Comments[0].ID != uniC.Comment.ID {
		t.Fatalf("ListComments(comment_id=%d universal) = %+v, want exactly the universal note", uniC.Comment.ID, gotUni.Comments)
	}
}

func TestEditCommentScopedFields(t *testing.T) {
	fixture := newAgentFixture(t)

	created, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "before", AuthorType: "agent", Kind: "draft",
	})
	if err != nil {
		t.Fatalf("AddComment(project) error = %v", err)
	}

	pinned := true
	title := "Final"
	kind := "recap"
	edited, err := fixture.service.EditComment(fixture.ctx, EditCommentInput{
		CommentID: created.Comment.ID, Body: "after", Title: &title, Kind: &kind, Pinned: &pinned,
	})
	if err != nil {
		t.Fatalf("EditComment() error = %v", err)
	}
	if edited.Comment.Body != "after" || edited.Comment.Title != "Final" || edited.Comment.Kind != "recap" || !edited.Comment.Pinned {
		t.Fatalf("EditComment() = %+v, want body/title/kind/pinned applied", edited.Comment)
	}
}

// TestEditCommentPreservesNoteFieldsOnBodyOnlyEdit pins the tri-state contract
// at the agent boundary: a body-only edit (Title/Kind/Pinned omitted → nil)
// must NOT wipe a pinned=true, titled, kinded note. An explicit pinned=false
// then unpins. Fails on the pre-fix code where the DTO carried plain
// string/bool zero values that the store wrote unconditionally.
func TestEditCommentPreservesNoteFieldsOnBodyOnlyEdit(t *testing.T) {
	fixture := newAgentFixture(t)

	pinned := true
	title := "Heading"
	kind := "handoff"
	created, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{
		Scope: domain.CommentScopeProject, Body: "before", AuthorType: "agent",
		Title: title, Kind: kind, Pinned: pinned,
	})
	if err != nil {
		t.Fatalf("AddComment(project) error = %v", err)
	}

	// Body-only edit: Title/Kind/Pinned omitted.
	edited, err := fixture.service.EditComment(fixture.ctx, EditCommentInput{
		CommentID: created.Comment.ID, Body: "after",
	})
	if err != nil {
		t.Fatalf("EditComment(body only) error = %v", err)
	}
	if edited.Comment.Body != "after" {
		t.Fatalf("EditComment body = %q, want %q", edited.Comment.Body, "after")
	}
	if !edited.Comment.Pinned || edited.Comment.Title != "Heading" || edited.Comment.Kind != "handoff" {
		t.Fatalf("EditComment(body only) wiped note fields = %+v", edited.Comment)
	}

	// Explicit pinned=false unpins (and leaves title/kind alone).
	unpin := false
	edited2, err := fixture.service.EditComment(fixture.ctx, EditCommentInput{
		CommentID: created.Comment.ID, Body: "after2", Pinned: &unpin,
	})
	if err != nil {
		t.Fatalf("EditComment(unpin) error = %v", err)
	}
	if edited2.Comment.Pinned {
		t.Fatalf("EditComment(pinned=false) did not unpin = %+v", edited2.Comment)
	}
	if edited2.Comment.Title != "Heading" || edited2.Comment.Kind != "handoff" {
		t.Fatalf("EditComment(unpin) wiped title/kind = %+v", edited2.Comment)
	}
}
