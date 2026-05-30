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

	title, kind, pinned := "Heading", "recap", true
	edited, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{
		Body: strPtr("after"), Title: &title, Kind: &kind, Pinned: &pinned,
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

// TestEditCommentTriStatePreservesUnsetFields proves the store keeps the loaded
// row's title/kind/pinned when the patch pointer is nil, and overwrites only on
// an explicit non-nil pointer. Fails on the pre-fix EditComment which wrote all
// three columns unconditionally from zero-value fields.
func TestEditCommentTriStatePreservesUnsetFields(t *testing.T) {
	ctx, store, project, _ := commentScopeFixture(t)
	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "before", Title: "X", Kind: "handoff", Pinned: true, AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}

	// Body-only edit: Title/Kind/Pinned nil → must preserve.
	edited, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{Body: strPtr("after")})
	if err != nil {
		t.Fatalf("EditComment(body only): %v", err)
	}
	if edited.Body != "after" || edited.Title != "X" || edited.Kind != "handoff" || !edited.Pinned {
		t.Fatalf("EditComment(body only) = %+v, want title/kind/pinned preserved", edited)
	}
	reloaded, err := store.CommentByID(ctx, project.ID, c.ID)
	if err != nil {
		t.Fatalf("CommentByID: %v", err)
	}
	if reloaded.Title != "X" || reloaded.Kind != "handoff" || !reloaded.Pinned {
		t.Fatalf("reloaded after body-only edit = %+v, want preserved", reloaded)
	}

	// Explicit pinned=false unpins; title/kind still preserved.
	unpin := false
	edited2, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{Body: strPtr("after2"), Pinned: &unpin})
	if err != nil {
		t.Fatalf("EditComment(unpin): %v", err)
	}
	if edited2.Pinned {
		t.Fatalf("EditComment(pinned=false) did not unpin = %+v", edited2)
	}
	if edited2.Title != "X" || edited2.Kind != "handoff" {
		t.Fatalf("EditComment(unpin) wiped title/kind = %+v", edited2)
	}
}

// TestEditCommentTriStateBodyPreservesOnNil proves the store keeps the loaded
// row's body when edit.Body is nil (a metadata-only edit) and overwrites it
// only on an explicit non-nil pointer. Fails on the pre-tri-state EditComment
// which wrote the body column unconditionally from a plain string field.
func TestEditCommentTriStateBodyPreservesOnNil(t *testing.T) {
	ctx, store, project, _ := commentScopeFixture(t)
	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "keep me", Pinned: false, AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}

	// Metadata-only edit: Body nil → body must be preserved, pinned flips.
	pin := true
	edited, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{Pinned: &pin})
	if err != nil {
		t.Fatalf("EditComment(body nil): %v", err)
	}
	if edited.Body != "keep me" || !edited.Pinned {
		t.Fatalf("EditComment(body nil) = %+v, want body preserved + pinned", edited)
	}
	reloaded, err := store.CommentByID(ctx, project.ID, c.ID)
	if err != nil {
		t.Fatalf("CommentByID: %v", err)
	}
	if reloaded.Body != "keep me" || !reloaded.Pinned {
		t.Fatalf("reloaded after metadata-only edit = %+v, want body preserved", reloaded)
	}

	// Explicit non-nil body overwrites.
	edited2, _, err := store.EditComment(ctx, project.ID, c.ID, domain.CommentEdit{Body: strPtr("rewritten")})
	if err != nil {
		t.Fatalf("EditComment(body set): %v", err)
	}
	if edited2.Body != "rewritten" {
		t.Fatalf("EditComment(body set) = %+v, want body overwritten", edited2)
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

// TestQueryCommentsByID proves the comment_id filter returns exactly the named
// row and nothing else. Fails before the CommentFilter.CommentID addition,
// which had no get-by-id predicate.
func TestQueryCommentsByID(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)

	a, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID, Body: "a", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(a): %v", err)
	}
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID, Body: "b", AuthorType: "human",
	}); err != nil {
		t.Fatalf("AddScopedComment(b): %v", err)
	}

	got, err := store.QueryComments(ctx, domain.CommentFilter{CommentID: a.ID})
	if err != nil {
		t.Fatalf("QueryComments(comment_id): %v", err)
	}
	if len(got) != 1 || got[0].ID != a.ID || got[0].Body != "a" {
		t.Fatalf("QueryComments(comment_id=%d) = %+v, want exactly comment a", a.ID, got)
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

// TestQueryCommentsTimeWindowRFC3339SameDay pins the datetime() normalization:
// created_at is stored "YYYY-MM-DD HH:MM:SS" (space), but an RFC3339 bound on
// the SAME calendar day uses a 'T' separator. A lexicographic string compare
// sorts 'T' (0x54) above the space (0x20), so `created_at >= bound` wrongly
// excludes the row — an empty window. datetime() on both sides fixes it.
func TestQueryCommentsTimeWindowRFC3339SameDay(t *testing.T) {
	ctx, store, project, task := commentScopeFixture(t)

	c, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID,
		Body: "today", AuthorType: "agent",
	})
	if err != nil {
		t.Fatalf("AddScopedComment: %v", err)
	}

	// Read the row's stored date and build a same-day RFC3339 floor at 00:00:00Z.
	stored, err := store.CommentByID(ctx, project.ID, c.ID)
	if err != nil {
		t.Fatalf("CommentByID: %v", err)
	}
	day := stored.CreatedAt // "YYYY-MM-DD HH:MM:SS"
	if len(day) < 10 {
		t.Fatalf("unexpected created_at %q", stored.CreatedAt)
	}
	floor := day[:10] + "T00:00:00Z" // same day, midnight, RFC3339 with 'T'

	got, err := store.QueryComments(ctx, domain.CommentFilter{CreatedAfter: floor})
	if err != nil {
		t.Fatalf("QueryComments(same-day RFC3339 floor): %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("QueryComments(CreatedAfter=%q) = %+v, want the seeded comment", floor, got)
	}
}
