package sqlite

import (
	"context"
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

	hits, err := store.Search(ctx, "tls", 0, nil, 200)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	seen := map[domain.SearchEntityType]bool{}
	for _, h := range hits {
		seen[h.EntityType] = true
	}
	for _, expected := range []domain.SearchEntityType{
		domain.SearchEntityTask,
		domain.SearchEntityComment,
		domain.SearchEntityError,
		domain.SearchEntitySolution,
	} {
		if !seen[expected] {
			t.Fatalf("Search(tls) missing entity_type=%s; hits=%+v", expected, hits)
		}
	}
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

	hits, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
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

	hits, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
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

	hits, err := store.Search(ctx, "tls", projectA.ID, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
	if err != nil {
		t.Fatalf("Search projectA: %v", err)
	}
	for _, h := range hits {
		if h.ProjectID != projectA.ID {
			t.Fatalf("Search(projectA) returned project=%d hit %+v", h.ProjectID, h)
		}
	}

	all, err := store.Search(ctx, "tls", 0, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
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

	hits, err := store.Search(ctx, "marker", 0, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
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

	stale, err := store.Search(ctx, "original", 0, []domain.SearchEntityType{domain.SearchEntityTask}, 200)
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
	_, err := store.Search(ctx, `"unterminated`, 0, nil, 200)
	if err == nil {
		t.Fatal("Search(invalid) error = nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok || coded.Code != domain.ErrValidation {
		t.Fatalf("Search(invalid) error = %v, want validation_error", err)
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

	hits, err := store.Search(ctx, "renew", 0, []domain.SearchEntityType{domain.SearchEntitySolution}, 200)
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

func stringPtr(s string) *string { return &s }
