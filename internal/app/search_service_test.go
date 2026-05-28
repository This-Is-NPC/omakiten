package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

func TestSearchServiceRejectsEmptyQuery(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewSearchService(store, store)
	_, err := svc.Search(ctx, project.Context(), "   ", nil)
	if err == nil {
		t.Fatal("Search(empty) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestSearchServiceRejectsUnknownEntityType(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewSearchService(store, store)
	_, err := svc.Search(ctx, project.Context(), "tls", []string{"banana"})
	if err == nil {
		t.Fatal("Search(banana) error = nil")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestSearchServiceEmitsErrorsResearchedForErrorScope(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "search", "claude-opus-4-7", "sess-1")
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	errSvc := NewErrorService(store, store.Snapshot())
	if _, err := errSvc.Record(ctx, project.Context(), "tls boom", "", nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	svc := NewSearchService(store, store)
	if _, err := svc.Search(ctx, project.Context(), "tls", []string{"error"}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	ev := assertLatestEvent(t, store, domain.EventTypeErrorsResearched, "search", 0, "claude-opus-4-7", "sess-1")
	if !strings.Contains(ev.Payload, `"unified":true`) {
		t.Fatalf("errors.researched payload missing unified marker: %s", ev.Payload)
	}
}

func TestSearchServiceSkipsErrorsResearchedWhenScopeExcludesError(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "search", "claude-opus-4-7", "sess-2")
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewSearchService(store, store)
	if _, err := svc.Search(ctx, project.Context(), "tls", []string{"task"}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Fresh store + task-only scope ⇒ zero errors.researched events should
	// exist. Asserting absence of any row (rather than just absence of
	// the unified marker) catches regressions where an old-shape payload
	// would slip through.
	recent, err := store.ListRecentEvents(ctx, domain.EventTypeErrorsResearched, 50)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(recent) != 0 {
		t.Fatalf("expected zero errors.researched events for task-only scope, got %d: %+v", len(recent), recent)
	}
}

// Cross-project search (project.ID == 0) must still emit errors.researched
// when the scope covers errors, and the payload must carry the unified
// marker. Coverage gap: TestSearchServiceEmitsErrorsResearchedForErrorScope
// only exercises the per-project path.
func TestSearchServiceEmitsErrorsResearchedForCrossProjectScope(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "search", "claude-opus-4-7", "sess-3")
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	errSvc := NewErrorService(store, store.Snapshot())
	if _, err := errSvc.Record(ctx, project.Context(), "tls cross", "", nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	svc := NewSearchService(store, store)
	if _, err := svc.Search(ctx, domain.ProjectContext{}, "tls", nil); err != nil {
		t.Fatalf("Search: %v", err)
	}

	// project.ID = 0 propagates to RecordEntityEvent; assertLatestEvent
	// expects entityID==0 (the search action targets no specific entity).
	ev := assertLatestEvent(t, store, domain.EventTypeErrorsResearched, "search", 0, "claude-opus-4-7", "sess-3")
	if !strings.Contains(ev.Payload, `"unified":true`) {
		t.Fatalf("cross-project errors.researched payload missing unified marker: %s", ev.Payload)
	}
}
