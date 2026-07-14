package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestSearchIndexCoversCoreEntities runs against a store that has no
// pre-existing data; the assertion is that migration 022's CREATE
// triggers fire from the very first INSERT so the index is populated
// without any explicit backfill step at runtime.
func TestSearchIndexCoversCoreEntities(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	task, err := store.CreateTask(ctx, project.ID, "deploy pipeline", "tls handshake fix", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if _, err := store.AddComment(ctx, project.ID, task.ID, "tls failing on staging", "agent", nil); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	rec, err := store.RecordError(ctx, project.ID, "tls certificate expired", "during deploy", nil)
	if err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	if _, err := store.AddSolution(ctx, rec.ID, "rotate the tls cert", "openssl rekey", nil); err != nil {
		t.Fatalf("AddSolution: %v", err)
	}
	if _, err := store.CreatePlan(ctx, project.ID, "tls-plan", "TLS plan", "tls rollout"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('tls orphan', 'note', 999, ?)`,
		project.ID,
	); err != nil {
		t.Fatalf("insert retired note row: %v", err)
	}

	hits, err := store.Search(ctx, "tls", 0, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	seen := map[domain.SearchEntityType]bool{}
	for _, h := range hits {
		seen[h.EntityType] = true
		if h.EntityType == domain.SearchEntityType("note") {
			t.Fatalf("Search(tls) returned unsupported retired note row: %+v", h)
		}
	}
	for _, expected := range []domain.SearchEntityType{
		domain.SearchEntityTask,
		domain.SearchEntityComment,
		domain.SearchEntityError,
		domain.SearchEntitySolution,
		domain.SearchEntityPlan,
	} {
		if !seen[expected] {
			t.Fatalf("Search(tls) missing entity_type=%s; hits=%+v", expected, hits)
		}
	}
}

func TestSearchReturnsScopedNoteLikeContentAsComments(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	projectComment, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "project handoff marker", Title: "Project note", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(project): %v", err)
	}
	universalComment, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeUniversal,
		Body:  "universal handoff marker", Title: "Universal note", AuthorType: "human",
	})
	if err != nil {
		t.Fatalf("AddScopedComment(universal): %v", err)
	}

	assertCommentHit := func(query string, projectID, wantID int64) {
		t.Helper()
		hits, err := store.Search(ctx, query, projectID, nil)
		if err != nil {
			t.Fatalf("Search(%s): %v", query, err)
		}
		for _, hit := range hits {
			if hit.ID == wantID && hit.EntityType == domain.SearchEntityComment {
				return
			}
		}
		t.Fatalf("Search(%s) missing comment %d; hits=%+v", query, wantID, hits)
	}

	assertCommentHit("project", project.ID, projectComment.ID)
	assertCommentHit("universal", 0, universalComment.ID)
}

func TestSearchFiltersByEntityTypes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	task, err := store.CreateTask(ctx, project.ID, "tls task", "tls body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "tls comment", "agent", nil); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	hits, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("Search(tls, task) empty")
	}
	for _, h := range hits {
		if h.EntityType != domain.SearchEntityTask {
			t.Fatalf("Search(task) unexpected entity_type=%s", h.EntityType)
		}
	}
}

func TestSearchExcludesArchivedTasks(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	task, err := store.CreateTask(ctx, project.ID, "tls active", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if _, _, err := store.SetTaskState(ctx, project.ID, task.ID, domain.TaskStateArchived, "", store.snap()); err != nil {
		t.Fatalf("SetTaskState archived: %v", err)
	}

	hits, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.ID == task.ID {
			t.Fatalf("Search returned archived task %d: %+v", task.ID, h)
		}
	}
}

func TestSearchProjectFilter(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	projectA := mustUpsertProject(t, store, "A", "a", "/work/a")
	projectB := mustUpsertProject(t, store, "B", "b", "/work/b")

	if _, err := store.CreateTask(ctx, projectA.ID, "tls in A", "body A", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if _, err := store.CreateTask(ctx, projectB.ID, "tls in B", "body B", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}

	hits, err := store.Search(ctx, "tls", projectA.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search projectA: %v", err)
	}
	for _, h := range hits {
		if h.ProjectID != projectA.ID {
			t.Fatalf("Search(projectA) returned project=%d hit %+v", h.ProjectID, h)
		}
	}

	all, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search cross-project: %v", err)
	}
	projects := map[int64]bool{}
	for _, h := range all {
		projects[h.ProjectID] = true
	}
	if !projects[projectA.ID] || !projects[projectB.ID] {
		t.Fatalf("Search cross-project missing one project: %v", projects)
	}
}

func TestSearchUpdateTriggerRefreshesContent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	task, err := store.CreateTask(ctx, project.ID, "original title", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if _, err := store.UpdateTask(ctx, project.ID, task.ID, domain.TaskUpdate{Title: stringPtr("updated marker")}, store.snap()); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	hits, err := store.Search(ctx, "marker", 0, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Search(marker) did not find updated task; hits=%+v", hits)
	}

	stale, err := store.Search(ctx, "original", 0, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search original: %v", err)
	}
	for _, h := range stale {
		if h.ID == task.ID {
			t.Fatalf("stale 'original' hit survives update: %+v", h)
		}
	}
}

func TestSearchInvalidQuery(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	// FTS5 rejects unbalanced quotes — surfaces as a coded validation_error.
	_, err := store.Search(ctx, `"unterminated`, 0, nil)
	if err == nil {
		t.Fatal("Search(invalid) error = nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok || coded.Code != domain.ErrValidation {
		t.Fatalf("Search(invalid) error = %v, want validation_error", err)
	}
}

func TestSearchRejectsAmplificationAtSQLiteBoundary(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	query := strings.Repeat("term OR ", domain.SearchQueryMaxTokens/2+1) + "term"
	_, err := store.Search(context.Background(), query, 0, nil)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation || coded.Message != "search query exceeds limits" {
		t.Fatalf("Store.Search error = %v, want stable shared-cap validation", err)
	}
}

func TestSearchSolutionProjectIDDerived(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")

	rec, err := store.RecordError(ctx, project.ID, "tls boom", "", nil)
	if err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	if _, err := store.AddSolution(ctx, rec.ID, "rotate", "tls renew", nil); err != nil {
		t.Fatalf("AddSolution: %v", err)
	}

	hits, err := store.Search(ctx, "renew", 0, []domain.SearchEntityType{domain.SearchEntitySolution})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("Search(renew, solution) empty")
	}
	if hits[0].ProjectID != project.ID {
		t.Fatalf("solution hit project_id = %d, want %d (derived from errors.project_id via trigger)", hits[0].ProjectID, project.ID)
	}
}

func TestSearchCapsResultsAtTwoHundredRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	if _, err := store.db.ExecContext(ctx, `
WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < 201)
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state)
SELECT ?, 1, 'shared cap marker ' || n, '', 2, 'active' FROM seq`, project.ID); err != nil {
		t.Fatalf("insert search rows: %v", err)
	}
	hits, err := store.Search(ctx, "shared", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 200 {
		t.Fatalf("Search returned %d rows, want 200", len(hits))
	}
}

func stringPtr(s string) *string { return &s }
