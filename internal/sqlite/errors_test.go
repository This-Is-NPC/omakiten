package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

func TestRecordErrorPersistsAgentAttribution(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "errors_record", "claude-opus-4-7", "sess-xyz")
	store := openTestStore(t)

	record, err := store.RecordError(ctx, 0, "boom", "", nil)
	if err != nil {
		t.Fatalf("RecordError() error = %v", err)
	}
	if record.Source != "mcp" || record.Entrypoint != "errors_record" {
		t.Fatalf("source/entrypoint = %q/%q, want mcp/errors_record", record.Source, record.Entrypoint)
	}
	if record.AgentModel != "claude-opus-4-7" {
		t.Fatalf("agent_model = %q, want claude-opus-4-7", record.AgentModel)
	}
	if record.AgentSessionID != "sess-xyz" {
		t.Fatalf("agent_session_id = %q, want sess-xyz", record.AgentSessionID)
	}
}

func TestAddSolutionPersistsAgentAttribution(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "solutions_add", "claude-sonnet-4-6", "")
	store := openTestStore(t)

	parent, _ := store.RecordError(ctx, 0, "parent", "", nil)
	solution, err := store.AddSolution(ctx, parent.ID, "fix", "steps", nil)
	if err != nil {
		t.Fatalf("AddSolution() error = %v", err)
	}
	if solution.Source != "mcp" || solution.Entrypoint != "solutions_add" {
		t.Fatalf("source/entrypoint = %q/%q", solution.Source, solution.Entrypoint)
	}
	if solution.AgentModel != "claude-sonnet-4-6" {
		t.Fatalf("agent_model = %q, want claude-sonnet-4-6", solution.AgentModel)
	}
	if solution.AgentSessionID != "" {
		t.Fatalf("agent_session_id = %q, want empty (NULL session)", solution.AgentSessionID)
	}
}

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

func TestAddSolutionRecordsSuccessState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	rec, _ := store.RecordError(ctx, project.ID, "boom", "", []domain.Tag{{Name: "boom", Label: "Boom"}})

	winner, _ := store.AddSolution(ctx, rec.ID, "winner", "do X", nil)
	winnerConfirmed, err := store.ConfirmSolution(ctx, winner.ID, true)
	if err != nil {
		t.Fatalf("ConfirmSolution(true) error = %v", err)
	}
	if winnerConfirmed.Success == nil || !*winnerConfirmed.Success {
		t.Fatalf("ConfirmSolution(true) Success = %v", winnerConfirmed.Success)
	}
	if winnerConfirmed.Likes != 1 {
		t.Fatalf("ConfirmSolution(true) Likes = %d, want 1", winnerConfirmed.Likes)
	}

	loser, _ := store.AddSolution(ctx, rec.ID, "loser", "do Y", nil)
	loserConfirmed, err := store.ConfirmSolution(ctx, loser.ID, false)
	if err != nil {
		t.Fatalf("ConfirmSolution(false) error = %v", err)
	}
	if loserConfirmed.Success == nil || *loserConfirmed.Success {
		t.Fatalf("ConfirmSolution(false) Success = %v, want pointer to false", loserConfirmed.Success)
	}
	if loserConfirmed.Likes != 0 {
		t.Fatalf("ConfirmSolution(false) Likes = %d, want 0", loserConfirmed.Likes)
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

func TestConfirmSolutionIncrementsLikesOnSuccess(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	rec, _ := store.RecordError(ctx, 0, "boom", "", nil)
	sol, _ := store.AddSolution(ctx, rec.ID, "fix", "", nil)
	if sol.Likes != 0 {
		t.Fatalf("AddSolution() Likes = %d, want 0", sol.Likes)
	}

	confirmed, err := store.ConfirmSolution(ctx, sol.ID, true)
	if err != nil {
		t.Fatalf("ConfirmSolution(true) error = %v", err)
	}
	if confirmed.Likes != 1 {
		t.Fatalf("ConfirmSolution(true) Likes = %d, want 1", confirmed.Likes)
	}

	// Re-confirming success should keep accumulating likes.
	confirmed, err = store.ConfirmSolution(ctx, sol.ID, true)
	if err != nil {
		t.Fatalf("ConfirmSolution(true) again error = %v", err)
	}
	if confirmed.Likes != 2 {
		t.Fatalf("ConfirmSolution(true) re-confirm Likes = %d, want 2", confirmed.Likes)
	}

	// success=false must not decrement or otherwise touch the counter.
	confirmed, err = store.ConfirmSolution(ctx, sol.ID, false)
	if err != nil {
		t.Fatalf("ConfirmSolution(false) error = %v", err)
	}
	if confirmed.Likes != 2 {
		t.Fatalf("ConfirmSolution(false) Likes = %d, want 2 (unchanged)", confirmed.Likes)
	}
}

func TestListTopSolutionsCrossProject(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	projectA, _ := store.UpsertProject(ctx, "A", "a", "/work/a")
	projectB, _ := store.UpsertProject(ctx, "B", "b", "/work/b")

	errA, _ := store.RecordError(ctx, projectA.ID, "issue A", "", nil)
	errB, _ := store.RecordError(ctx, projectB.ID, "issue B", "", nil)

	// solution in A confirmed twice
	solA, _ := store.AddSolution(ctx, errA.ID, "fix A", "", nil)
	if _, err := store.ConfirmSolution(ctx, solA.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(A) error = %v", err)
	}
	if _, err := store.ConfirmSolution(ctx, solA.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(A) error = %v", err)
	}
	// solution in B confirmed once
	solB, _ := store.AddSolution(ctx, errB.ID, "fix B", "", nil)
	if _, err := store.ConfirmSolution(ctx, solB.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(B) error = %v", err)
	}
	// untouched solution stays at zero likes
	zero, _ := store.AddSolution(ctx, errA.ID, "untried", "", nil)

	top, err := store.ListTopSolutions(ctx, 5)
	if err != nil {
		t.Fatalf("ListTopSolutions() error = %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("ListTopSolutions() len = %d, want 3", len(top))
	}
	if top[0].ID != solA.ID || top[0].Likes != 2 {
		t.Fatalf("ListTopSolutions()[0] = %+v, want solA likes=2", top[0])
	}
	if top[0].ProjectSlug != "a" {
		t.Fatalf("ListTopSolutions()[0].ProjectSlug = %q, want a (cross-project metadata)", top[0].ProjectSlug)
	}
	if top[1].ID != solB.ID || top[1].Likes != 1 {
		t.Fatalf("ListTopSolutions()[1] = %+v, want solB likes=1", top[1])
	}
	if top[1].ProjectSlug != "b" {
		t.Fatalf("ListTopSolutions()[1].ProjectSlug = %q, want b", top[1].ProjectSlug)
	}
	if top[2].ID != zero.ID || top[2].Likes != 0 {
		t.Fatalf("ListTopSolutions()[2] = %+v, want untried likes=0", top[2])
	}
}

func TestListTopSolutionsRespectsLimit(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	rec, _ := store.RecordError(ctx, 0, "boom", "", nil)
	for i := 0; i < 5; i++ {
		sol, _ := store.AddSolution(ctx, rec.ID, "candidate", "", nil)
		if _, err := store.ConfirmSolution(ctx, sol.ID, true); err != nil {
			t.Fatalf("ConfirmSolution() error = %v", err)
		}
	}

	top, err := store.ListTopSolutions(ctx, 2)
	if err != nil {
		t.Fatalf("ListTopSolutions(2) error = %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("ListTopSolutions(2) len = %d, want 2", len(top))
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
