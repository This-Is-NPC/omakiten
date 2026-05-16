package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

func TestErrorServiceEmitsAttributedDomainEvents(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "errors_record", "claude-opus-4-7", "sess-42")
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())
	service.SetSolutionsDefaults(SolutionsDefaults{TopLimitDefault: 10, TopLimitMax: 100})

	rec, err := service.Record(ctx, project.Context(), "FK violation", "during migration", []string{"sqlite"})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	assertLatestEvent(t, store, domain.EventTypeErrorRecorded, "error", rec.ID, "claude-opus-4-7", "sess-42")

	searchSvc := NewSearchService(store, store)
	if _, err := searchSvc.Search(ctx, project.Context(), "FK", []string{"error"}); err != nil {
		t.Fatalf("SearchService.Search() error = %v", err)
	}
	searchEv := assertLatestEvent(t, store, domain.EventTypeErrorSearched, "error", 0, "claude-opus-4-7", "sess-42")
	if !strings.Contains(searchEv.Payload, `"result_count":1`) {
		t.Fatalf("error.searched payload missing result_count: %s", searchEv.Payload)
	}

	sol, err := service.AddSolution(ctx, project.Context(), rec.ID, "drop fk", "alter table", nil)
	if err != nil {
		t.Fatalf("AddSolution() error = %v", err)
	}
	assertLatestEvent(t, store, domain.EventTypeSolutionAdded, "solution", sol.ID, "claude-opus-4-7", "sess-42")

	confirmed, err := service.ConfirmSolution(ctx, project.Context(), sol.ID, true)
	if err != nil {
		t.Fatalf("ConfirmSolution(true) error = %v", err)
	}
	assertLatestEvent(t, store, domain.EventTypeSolutionLiked, "solution", confirmed.ID, "claude-opus-4-7", "sess-42")

	failedSol, err := service.AddSolution(ctx, project.Context(), rec.ID, "another", "", nil)
	if err != nil {
		t.Fatalf("AddSolution() error = %v", err)
	}
	if _, err := service.ConfirmSolution(ctx, project.Context(), failedSol.ID, false); err != nil {
		t.Fatalf("ConfirmSolution(false) error = %v", err)
	}
	assertLatestEvent(t, store, domain.EventTypeSolutionFailed, "solution", failedSol.ID, "claude-opus-4-7", "sess-42")

	if _, err := service.ListTopSolutions(ctx, project.Context(), 5); err != nil {
		t.Fatalf("ListTopSolutions() error = %v", err)
	}
	assertLatestEvent(t, store, domain.EventTypeSolutionViewedTop, "solution", 0, "claude-opus-4-7", "sess-42")
}

func assertLatestEvent(t *testing.T, store eventReader, eventType, wantEntityType string, wantEntityID int64, wantModel, wantSession string) domain.Event {
	t.Helper()
	events, err := store.ListRecentEvents(context.Background(), eventType, 1)
	if err != nil {
		t.Fatalf("ListRecentEvents(%s) error = %v", eventType, err)
	}
	if len(events) == 0 {
		t.Fatalf("ListRecentEvents(%s) returned no events", eventType)
	}
	ev := events[0]
	if ev.EntityType != wantEntityType {
		t.Fatalf("%s entity_type = %q, want %q", eventType, ev.EntityType, wantEntityType)
	}
	if ev.EntityID != wantEntityID {
		t.Fatalf("%s entity_id = %d, want %d", eventType, ev.EntityID, wantEntityID)
	}
	if ev.AgentModel != wantModel {
		t.Fatalf("%s agent_model = %q, want %q", eventType, ev.AgentModel, wantModel)
	}
	if ev.AgentSessionID != wantSession {
		t.Fatalf("%s agent_session_id = %q, want %q", eventType, ev.AgentSessionID, wantSession)
	}
	return ev
}

type eventReader interface {
	ListRecentEvents(ctx context.Context, eventType string, limit int) ([]domain.Event, error)
}

func TestErrorServiceRecordValidatesDescription(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())

	_, err := service.Record(ctx, project.Context(), "  ", "", nil)
	if err == nil {
		t.Fatal("Record(empty description) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestErrorServiceRecordNormalizesTags(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	// Rotate the store's snapshot to one that carries the kit's tag-synonym
	// table before constructing the service so NormalizeTagName resolves the
	// "Go" / "golang" aliases to the canonical "go" without any setter call.
	bundle := appTestBundle(t, 1000)
	bundle.Config.TagSynonyms = kitSynonyms()
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	service := NewErrorService(store, store.Snapshot())

	rec, err := service.Record(ctx, project.Context(), "boom", "ctx", []string{"Go", "golang", "  GOLANG"})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if len(rec.Tags) != 1 {
		t.Fatalf("Record() Tags = %+v, want deduped to single canonical 'go'", rec.Tags)
	}
	if rec.Tags[0].Name != "go" {
		t.Fatalf("Record() Tag = %q, want canonical 'go'", rec.Tags[0].Name)
	}
}

func TestErrorServiceAddSolutionValidates(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())

	_, err := service.AddSolution(ctx, project.Context(), 0, "fix", "", nil)
	if err == nil {
		t.Fatal("AddSolution(error_id=0) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)

	rec, _ := service.Record(ctx, project.Context(), "boom", "", nil)
	_, err = service.AddSolution(ctx, project.Context(), rec.ID, "  ", "", nil)
	if err == nil {
		t.Fatal("AddSolution(empty description) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestErrorServiceConfirmSolutionRanks(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())
	service.SetSolutionsDefaults(SolutionsDefaults{TopLimitDefault: 10, TopLimitMax: 100})
	rec, _ := service.Record(ctx, project.Context(), "boom", "", []string{"boom"})

	loser, _ := service.AddSolution(ctx, project.Context(), rec.ID, "loser", "", nil)
	winner, _ := service.AddSolution(ctx, project.Context(), rec.ID, "winner", "", nil)

	if _, err := service.ConfirmSolution(ctx, project.Context(), loser.ID, false); err != nil {
		t.Fatalf("ConfirmSolution(loser) error = %v", err)
	}
	if _, err := service.ConfirmSolution(ctx, project.Context(), winner.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(winner) error = %v", err)
	}

	top, err := service.ListTopSolutions(ctx, project.Context(), 10)
	if err != nil {
		t.Fatalf("ListTopSolutions() error = %v", err)
	}
	if len(top) == 0 || top[0].ID != winner.ID {
		t.Fatalf("ListTopSolutions()[0] = %+v, want winner (%d) first", top, winner.ID)
	}
}

func TestErrorServiceListTopSolutionsRanksByLikes(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())
	service.SetSolutionsDefaults(SolutionsDefaults{TopLimitDefault: 10, TopLimitMax: 100})
	rec, _ := service.Record(ctx, project.Context(), "boom", "", nil)

	popular, _ := service.AddSolution(ctx, project.Context(), rec.ID, "popular", "", nil)
	if _, err := service.ConfirmSolution(ctx, project.Context(), popular.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(popular) error = %v", err)
	}
	if _, err := service.ConfirmSolution(ctx, project.Context(), popular.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(popular) error = %v", err)
	}
	other, _ := service.AddSolution(ctx, project.Context(), rec.ID, "other", "", nil)
	if _, err := service.ConfirmSolution(ctx, project.Context(), other.ID, true); err != nil {
		t.Fatalf("ConfirmSolution(other) error = %v", err)
	}

	top, err := service.ListTopSolutions(ctx, project.Context(), 0)
	if err != nil {
		t.Fatalf("ListTopSolutions() error = %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("ListTopSolutions() len = %d, want 2", len(top))
	}
	if top[0].ID != popular.ID || top[0].Likes != 2 {
		t.Fatalf("ListTopSolutions()[0] = %+v, want popular likes=2", top[0])
	}
}

func TestErrorServiceListTopSolutionsClampsLimit(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewErrorService(store, store.Snapshot())
	service.SetSolutionsDefaults(SolutionsDefaults{TopLimitDefault: 10, TopLimitMax: 100})
	rec, _ := service.Record(ctx, project.Context(), "boom", "", nil)
	for i := 0; i < 3; i++ {
		sol, _ := service.AddSolution(ctx, project.Context(), rec.ID, "fix", "", nil)
		if _, err := service.ConfirmSolution(ctx, project.Context(), sol.ID, true); err != nil {
			t.Fatalf("ConfirmSolution() error = %v", err)
		}
	}

	// limit > 100 must be clamped silently to 100; here we only have 3 rows so
	// the visible signal is that the call succeeds and returns all of them.
	top, err := service.ListTopSolutions(ctx, project.Context(), 9999)
	if err != nil {
		t.Fatalf("ListTopSolutions(9999) error = %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("ListTopSolutions(9999) len = %d, want 3", len(top))
	}
}

func TestErrorServiceTagEntityIntegration(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	tagService := NewTagService(store, store.Snapshot())
	errService := NewErrorService(store, store.Snapshot())

	rec, _ := errService.Record(ctx, project.Context(), "boom", "", nil)

	// add via TagService entity_type=error
	tag, err := tagService.Add(ctx, project.Context(), TagEntityError, rec.ID, "sqlite")
	if err != nil {
		t.Fatalf("TagService.Add(error) error = %v", err)
	}
	if tag.Name != "sqlite" {
		t.Fatalf("TagService.Add(error) tag = %q", tag.Name)
	}

	tags, err := tagService.List(ctx, project.Context(), TagEntityError, rec.ID)
	if err != nil {
		t.Fatalf("TagService.List(error) error = %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "sqlite" {
		t.Fatalf("TagService.List(error) = %+v", tags)
	}

	if err := tagService.Remove(ctx, project.Context(), TagEntityError, rec.ID, tag.ID); err != nil {
		t.Fatalf("TagService.Remove(error) error = %v", err)
	}
	tags, _ = tagService.List(ctx, project.Context(), TagEntityError, rec.ID)
	if len(tags) != 0 {
		t.Fatalf("TagService.List(error) after remove len = %d, want 0", len(tags))
	}
}

func TestErrorServiceTagEntityRequiresEntityID(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	tagService := NewTagService(store, store.Snapshot())
	_, err := tagService.Add(ctx, project.Context(), TagEntityError, 0, "x")
	if err == nil {
		t.Fatal("TagService.Add(error, 0) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}
