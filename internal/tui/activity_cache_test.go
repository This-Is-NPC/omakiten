package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestActivityCardsCacheHitAndMiss pins the memoisation contract:
// identical inputs share the same underlying card slice (cache hit);
// cursor moves and new events both flip the fingerprint and force a
// rebuild with a fresh allocation.
func TestActivityCardsCacheHitAndMiss(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.taskID = 42
	model.activityForTask = 42
	model.activity = []domain.Event{
		{ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "first", AuthorType: "human"},
		{ID: 2, EntityID: 42, EventType: domain.EventTypeTaskCreated, Body: "{}"},
	}
	model.activityCursor = -1

	// Need to drive through *Model to write the cache; the renderTaskCommentsCell
	// path uses the value-receiver activityRowsForRender which reads the cache.
	cards1 := model.cachedActivityRowsForRender(model.activity)
	cards2 := model.cachedActivityRowsForRender(model.activity)
	if sliceHeader(cards1) != sliceHeader(cards2) {
		t.Fatalf("identical inputs allocated a new slice — cache miss (key=%d)", model.activityCardsCache.key)
	}

	prevKey := model.activityCardsCache.key
	model.activityCursor = 0 // focus first card
	cards3 := model.cachedActivityRowsForRender(model.activity)
	if model.activityCardsCache.key == prevKey {
		t.Fatalf("cursor move did not bump cache key")
	}
	if sliceHeader(cards1) == sliceHeader(cards3) {
		t.Fatalf("cursor move did not invalidate cache; backing slice reused")
	}

	prevKey = model.activityCardsCache.key
	model.activity = append(model.activity, domain.Event{ID: 3, EntityID: 42, EventType: domain.EventTypeComment, Body: "third"})
	cards4 := model.cachedActivityRowsForRender(model.activity)
	if model.activityCardsCache.key == prevKey {
		t.Fatalf("new event did not bump cache key")
	}
	if len(cards4) != 3 {
		t.Fatalf("expected 3 cards after appending event, got %d", len(cards4))
	}
}

// TestActivityCardsCacheValueReceiverSeesCacheFromHandlers proves the
// value-receiver activityRowsForRender (used by the render pass)
// reads from the cache the *Model handlers warmed — the Bubbletea
// value-copy semantics carry the cache forward into View().
func TestActivityCardsCacheValueReceiverSeesCacheFromHandlers(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.taskID = 7
	model.activityForTask = 7
	model.activity = []domain.Event{{ID: 11, EntityID: 7, EventType: domain.EventTypeComment, Body: "hi", AuthorType: "human"}}
	model.activityCursor = -1

	warmed := model.cachedActivityRowsForRender(model.activity)
	rendered := model.activityRowsForRender(model.activity)
	if sliceHeader(warmed) != sliceHeader(rendered) {
		t.Fatalf("value-receiver render path did not hit the warmed cache; backing slice differs")
	}
}
