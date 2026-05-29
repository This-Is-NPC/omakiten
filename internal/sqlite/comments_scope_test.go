package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

// commentScopeFixture spins up a store with one project and one task so the
// scope tests can exercise all three comment scopes against real rows.
func commentScopeFixture(t *testing.T) (context.Context, *storeFixture, domain.Project, domain.Task) {
	t.Helper()
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	store.applyBundle(sqliteTestBundle(t))

	project, err := store.UpsertProject(ctx, "Project", "p", "/work/p")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return ctx, store, project, task
}

func TestAddScopedCommentScopes(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)

	taskC, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID,
		Body: "task body", Title: "T", Kind: "handoff", Pinned: true, AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(task): %v", err)
	}
	if taskC.Scope != domain.CommentScopeTask || taskC.TaskID != task.ID || taskC.ProjectID != project.ID {
		t.Fatalf("task comment scope/ids = %+v", taskC)
	}
	if taskC.Title != "T" || taskC.Kind != "handoff" || !taskC.Pinned {
		t.Fatalf("task comment note fields = %+v", taskC)
	}

	projC, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "project body", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(project): %v", err)
	}
	if projC.Scope != domain.CommentScopeProject || projC.ProjectID != project.ID || projC.TaskID != 0 {
		t.Fatalf("project comment scope/ids = %+v", projC)
	}

	uniC, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeUniversal,
		Body:  "universal body", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(universal): %v", err)
	}
	if uniC.Scope != domain.CommentScopeUniversal || uniC.ProjectID != 0 || uniC.TaskID != 0 {
		t.Fatalf("universal comment scope/ids = %+v", uniC)
	}

	// Round-trip each scope through CommentByID.
	for _, want := range []domain.Comment{taskC, projC, uniC} {
		got, err := store.CommentByID(ctx, project.ID, want.ID)
		if err != nil {
			t.Fatalf("CommentByID(%d): %v", want.ID, err)
		}
		if got.Scope != want.Scope || got.Body != want.Body {
			t.Fatalf("CommentByID(%d) = %+v, want scope %q body %q", want.ID, got, want.Scope, want.Body)
		}
	}

	// Project comment without a project id is rejected.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{Scope: domain.CommentScopeProject, Body: "x"}); err == nil {
		t.Fatal("AddScopedComment(project, no project id) = nil error, want validation")
	}
	// Unknown scope is rejected.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{Scope: "bogus", Body: "x"}); err == nil {
		t.Fatal("AddScopedComment(bogus) = nil error, want validation")
	}
}

func TestAddCommentDelegatesTaskScope(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)
	c, err := store.AddComment(ctx, project.ID, task.ID, "legacy", "human", nil)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if c.Scope != domain.CommentScopeTask || c.TaskID != task.ID {
		t.Fatalf("AddComment scope = %+v, want task scope", c)
	}
	// ListComments still surfaces only task-scoped comments.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{Scope: domain.CommentScopeProject, ProjectID: project.ID, Body: "proj", AuthorType: "human"}); err != nil {
		t.Fatalf("AddScopedComment(project): %v", err)
	}
	got, err := store.ListComments(ctx, project.ID, 0)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 1 || got[0].Body != "legacy" {
		t.Fatalf("ListComments = %+v, want only the task comment", got)
	}
}

func TestEditCommentWidePatch(t *testing.T) {
	ctx, store, project, _ := commentScopeFixture(t)
	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID, Body: "before", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}
	if c.UpdatedAt != "" {
		t.Fatalf("new comment UpdatedAt = %q, want empty", c.UpdatedAt)
	}

	edited, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{
		Body: "after", Title: "Heading", Kind: "recap", Pinned: true,
		Tags: []domain.Tag{{Name: "resume", Label: "resume"}},
	})
	if err != nil {
		t.Fatalf("EditComment: %v", err)
	}
	if edited.Body != "after" || edited.Title != "Heading" || edited.Kind != "recap" || !edited.Pinned {
		t.Fatalf("EditComment result = %+v", edited)
	}

	reloaded, err := store.CommentByID(ctx, project.ID, c.ID)
	if err != nil {
		t.Fatalf("CommentByID: %v", err)
	}
	if reloaded.UpdatedAt == "" {
		t.Fatal("EditComment did not stamp updated_at")
	}
	if reloaded.Title != "Heading" || reloaded.Kind != "recap" || !reloaded.Pinned {
		t.Fatalf("reloaded note fields = %+v", reloaded)
	}

	// CommentByID does not eager-load tags; QueryComments does. Verify the
	// edited tag set persisted through the tag-loading read path.
	queried, err := store.QueryComments(ctx, domain.CommentFilter{Tag: "resume"})
	if err != nil {
		t.Fatalf("QueryComments(tag): %v", err)
	}
	if len(queried) != 1 || queried[0].ID != c.ID {
		t.Fatalf("QueryComments(tag) = %+v, want edited comment", queried)
	}
	if len(queried[0].Tags) != 1 || queried[0].Tags[0].Name != "resume" {
		t.Fatalf("reloaded tags = %+v", queried[0].Tags)
	}
}

func TestDeleteCommentNonTaskScope(t *testing.T) {
	ctx, store, project, _ := commentScopeFixture(t)
	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeUniversal, Body: "gone soon", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}
	if _, err := store.DeleteComment(ctx, project.ID, c.ID); err != nil {
		t.Fatalf("DeleteComment(universal): %v", err)
	}
	if _, err := store.CommentByID(ctx, project.ID, c.ID); err == nil {
		t.Fatal("CommentByID after delete = nil error, want not found")
	}
}

func TestQueryComments(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)

	// task comment, pinned, kind=handoff, tagged resume, body mentions tls.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID,
		Body: "tls handoff note", Title: "deploy", Kind: "handoff", Pinned: true, AuthorType: "agent",
		Tags: []domain.Tag{{Name: "resume", Label: "resume"}},
	}); err != nil {
		t.Fatalf("seed task comment: %v", err)
	}
	// project comment, kind=recap, not pinned.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "weekly recap", Kind: "recap", AuthorType: "human",
	}); err != nil {
		t.Fatalf("seed project comment: %v", err)
	}
	// universal comment, kind=handoff, pinned.
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeUniversal,
		Body:  "global handoff", Kind: "handoff", Pinned: true, AuthorType: "human",
	}); err != nil {
		t.Fatalf("seed universal comment: %v", err)
	}

	cases := []struct {
		name   string
		filter domain.CommentFilter
		want   int
	}{
		{"by scope task", domain.CommentFilter{Scope: domain.CommentScopeTask}, 1},
		{"by scope project", domain.CommentFilter{Scope: domain.CommentScopeProject}, 1},
		{"by scope universal", domain.CommentFilter{Scope: domain.CommentScopeUniversal}, 1},
		{"by kind handoff", domain.CommentFilter{Kind: "handoff"}, 2},
		{"by kind recap", domain.CommentFilter{Kind: "recap"}, 1},
		{"by tag resume", domain.CommentFilter{Tag: "resume"}, 1},
		{"pinned only", domain.CommentFilter{PinnedOnly: true}, 2},
		{"fts body", domain.CommentFilter{Search: "tls"}, 1},
		{"fts title", domain.CommentFilter{Search: "deploy"}, 1},
		{"single project", domain.CommentFilter{ProjectID: project.ID}, 2}, // task + project; universal has NULL project_id
		{"cross project all", domain.CommentFilter{}, 3},
		{"kind + pinned combined", domain.CommentFilter{Kind: "handoff", PinnedOnly: true}, 2},
		{"scope + pinned combined", domain.CommentFilter{Scope: domain.CommentScopeProject, PinnedOnly: true}, 0},
		{"task id narrow", domain.CommentFilter{TaskID: task.ID}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.QueryComments(ctx, tc.filter)
			if err != nil {
				t.Fatalf("QueryComments(%+v): %v", tc.filter, err)
			}
			if len(got) != tc.want {
				t.Fatalf("QueryComments(%+v) len = %d, want %d (%+v)", tc.filter, len(got), tc.want, got)
			}
		})
	}
}

func TestQueryCommentsTimeWindow(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)

	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID,
		Body: "windowed", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}

	// Window that includes the row (created_at is stamped by SQLite at insert).
	got, err := store.QueryComments(ctx, domain.CommentFilter{
		CreatedAfter:  "1970-01-01T00:00:00Z",
		CreatedBefore: "2999-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("QueryComments(window in): %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("QueryComments(window in) = %+v, want the seeded comment", got)
	}

	// Window entirely before the row.
	got, err = store.QueryComments(ctx, domain.CommentFilter{
		CreatedBefore: "1971-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("QueryComments(window out): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("QueryComments(window out) = %+v, want none", got)
	}
}
