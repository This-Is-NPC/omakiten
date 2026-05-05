package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestRecordErrorWithTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")

	tags := []domain.Tag{{Name: "sqlite", Label: "Sqlite"}, {Name: "fk", Label: "Fk"}}
	record, err := store.RecordError(ctx, project.ID, "FK violation", "during migration", tags)
	if err != nil {
		t.Fatalf("RecordError() error = %v", err)
	}
	if record.ID == 0 || record.Description != "FK violation" || record.Context != "during migration" {
		t.Fatalf("RecordError() = %+v", record)
	}
	if record.ProjectID != project.ID {
		t.Fatalf("RecordError() ProjectID = %d, want %d", record.ProjectID, project.ID)
	}
	if len(record.Tags) != 2 {
		t.Fatalf("RecordError() Tags len = %d, want 2", len(record.Tags))
	}
}

func TestRecordErrorWithoutProject(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	record, err := store.RecordError(ctx, 0, "no-project boom", "", nil)
	if err != nil {
		t.Fatalf("RecordError() error = %v", err)
	}
	if record.ProjectID != 0 {
		t.Fatalf("RecordError() ProjectID = %d, want 0", record.ProjectID)
	}
}

func TestSearchErrorsByTagIntersection(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	projectA, _ := store.UpsertProject(ctx, "A", "a", "/work/a")
	projectB, _ := store.UpsertProject(ctx, "B", "b", "/work/b")

	// Error in project A tagged sqlite + fk
	errA, _ := store.RecordError(ctx, projectA.ID, "FK violation in A", "", []domain.Tag{{Name: "sqlite", Label: "Sqlite"}, {Name: "fk", Label: "Fk"}})
	// Error in project B tagged sqlite + index
	errB, _ := store.RecordError(ctx, projectB.ID, "missing index in B", "", []domain.Tag{{Name: "sqlite", Label: "Sqlite"}, {Name: "index", Label: "Index"}})
	// Unrelated error tagged go
	errGo, _ := store.RecordError(ctx, projectA.ID, "go panic", "", []domain.Tag{{Name: "go", Label: "Go"}})

	results, err := store.SearchErrors(ctx, "", []string{"sqlite"})
	if err != nil {
		t.Fatalf("SearchErrors() error = %v", err)
	}

	ids := map[int64]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if !ids[errA.ID] || !ids[errB.ID] {
		t.Fatalf("SearchErrors(sqlite) missing cross-project errors: got %v", ids)
	}
	if ids[errGo.ID] {
		t.Fatalf("SearchErrors(sqlite) returned unrelated error %d", errGo.ID)
	}
}

func TestSearchErrorsByQueryText(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	hit, _ := store.RecordError(ctx, project.ID, "FOREIGN KEY constraint failed", "details", nil)
	miss, _ := store.RecordError(ctx, project.ID, "unrelated error", "", nil)

	results, err := store.SearchErrors(ctx, "FOREIGN KEY", nil)
	if err != nil {
		t.Fatalf("SearchErrors() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != hit.ID {
		t.Fatalf("SearchErrors() = %+v, want only %d", results, hit.ID)
	}

	// case-insensitive substring should match the lower-case query against the
	// stored text via LIKE — sqlite default LIKE is case-insensitive for ASCII.
	results, _ = store.SearchErrors(ctx, "foreign", nil)
	if len(results) != 1 {
		t.Fatalf("SearchErrors(case) = %+v", results)
	}
	_ = miss
}

func TestAddSolutionAndRanking(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	rec, _ := store.RecordError(ctx, project.ID, "boom", "", []domain.Tag{{Name: "boom", Label: "Boom"}})

	// failed older solution
	oldFail, _ := store.AddSolution(ctx, rec.ID, "old failed", "", nil)
	if _, err := store.ConfirmSolution(ctx, oldFail.ID, false); err != nil {
		t.Fatalf("ConfirmSolution(false) error = %v", err)
	}

	// succeeded newer solution
	winner, _ := store.AddSolution(ctx, rec.ID, "winner", "do X", nil)
	winnerConfirmed, err := store.ConfirmSolution(ctx, winner.ID, true)
	if err != nil {
		t.Fatalf("ConfirmSolution(true) error = %v", err)
	}
	if winnerConfirmed.Success == nil || !*winnerConfirmed.Success {
		t.Fatalf("ConfirmSolution(true) Success = %v", winnerConfirmed.Success)
	}

	// untried solution (success=NULL)
	untried, _ := store.AddSolution(ctx, rec.ID, "untried hypothesis", "", nil)

	results, err := store.SearchErrors(ctx, "", []string{"boom"})
	if err != nil {
		t.Fatalf("SearchErrors() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchErrors() len = %d, want 1", len(results))
	}
	sols := results[0].Solutions
	if len(sols) != 3 {
		t.Fatalf("Solutions len = %d, want 3", len(sols))
	}
	if sols[0].ID != winner.ID {
		t.Fatalf("Solutions[0] = %d, want winner %d (success=true must rank first)", sols[0].ID, winner.ID)
	}
	// position 1 is the untried (NULL); position 2 is the failed.
	if sols[1].ID != untried.ID {
		t.Fatalf("Solutions[1] = %d, want untried %d (NULL outranks success=false)", sols[1].ID, untried.ID)
	}
	if sols[2].ID != oldFail.ID {
		t.Fatalf("Solutions[2] = %d, want oldFail %d", sols[2].ID, oldFail.ID)
	}
	if sols[2].Success == nil || *sols[2].Success {
		t.Fatalf("Solutions[2].Success = %v, want pointer to false", sols[2].Success)
	}
}

func TestConfirmSolutionUnknownID(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	_, err := store.ConfirmSolution(ctx, 9999, true)
	if err == nil {
		t.Fatal("ConfirmSolution(unknown) error = nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok || coded.Code != domain.ErrSolutionNotFound {
		t.Fatalf("ConfirmSolution(unknown) error = %v, want solution_not_found", err)
	}
}

func TestAddSolutionUnknownError(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	_, err := store.AddSolution(ctx, 9999, "x", "", nil)
	if err == nil {
		t.Fatal("AddSolution(unknown error) error = nil")
	}
	coded, ok := err.(*domain.CodedError)
	if !ok || coded.Code != domain.ErrErrorNotFound {
		t.Fatalf("AddSolution(unknown) error = %v, want error_not_found", err)
	}
}

func TestErrorTagsLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	rec, _ := store.RecordError(ctx, 0, "x", "", nil)
	tag, _ := store.FindOrCreateTag(ctx, "sqlite", "Sqlite")

	if err := store.AddErrorTag(ctx, rec.ID, tag.ID); err != nil {
		t.Fatalf("AddErrorTag() error = %v", err)
	}
	// idempotent
	if err := store.AddErrorTag(ctx, rec.ID, tag.ID); err != nil {
		t.Fatalf("AddErrorTag() idempotent error = %v", err)
	}

	tags, _ := store.ListErrorTags(ctx, rec.ID)
	if len(tags) != 1 || tags[0].Name != "sqlite" {
		t.Fatalf("ListErrorTags() = %+v", tags)
	}

	// usage count includes error_tags
	all, _ := store.ListAllTags(ctx)
	var got int
	for _, t := range all {
		if t.Name == "sqlite" {
			got = t.UsageCount
		}
	}
	if got != 1 {
		t.Fatalf("ListAllTags(sqlite).UsageCount = %d, want 1", got)
	}

	if err := store.RemoveErrorTag(ctx, rec.ID, tag.ID); err != nil {
		t.Fatalf("RemoveErrorTag() error = %v", err)
	}
	tags, _ = store.ListErrorTags(ctx, rec.ID)
	if len(tags) != 0 {
		t.Fatalf("ListErrorTags() after remove len = %d, want 0", len(tags))
	}
}

func TestMergeTagsReassignsErrorTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	rec, _ := store.RecordError(ctx, 0, "x", "", []domain.Tag{{Name: "golang-alias", Label: "Golang alias"}})
	canonical, _ := store.FindOrCreateTag(ctx, "go", "Go")

	var aliasID int64
	all, _ := store.ListAllTags(ctx)
	for _, t := range all {
		if t.Name == "golang-alias" {
			aliasID = t.ID
		}
	}

	if _, err := store.MergeTags(ctx, aliasID, canonical.ID); err != nil {
		t.Fatalf("MergeTags() error = %v", err)
	}

	tags, _ := store.ListErrorTags(ctx, rec.ID)
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Fatalf("ListErrorTags() after merge = %+v, want [go]", tags)
	}
}

func TestDeleteOrphanTagsRespectsErrorTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	// referenced via error_tags only
	rec, _ := store.RecordError(ctx, 0, "x", "", []domain.Tag{{Name: "stillused", Label: "Stillused"}})
	_ = rec

	// orphan
	_, _ = store.FindOrCreateTag(ctx, "loose", "Loose")

	n, err := store.DeleteOrphanTags(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanTags() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteOrphanTags() = %d, want 1", n)
	}
	all, _ := store.ListAllTags(ctx)
	for _, t := range all {
		if t.Name == "stillused" {
			return
		}
	}
	t.Fatal("DeleteOrphanTags() removed tag still referenced via error_tags")
}
