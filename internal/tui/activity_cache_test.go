package tui

import (
	"strings"
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

// TestActivityCardsCacheInvalidatesSameLengthBodyEdit pins the task-596 fix:
// editing a comment body in place to DIFFERENT text of the SAME character
// length must bump the fingerprint so the activity feed renders the new body
// instead of reusing the stale cached card. The prior len(ev.Body) proxy let
// "first" → "fifth" collide.
func TestActivityCardsCacheInvalidatesSameLengthBodyEdit(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.taskID = 42
	model.activityForTask = 42
	model.activity = []domain.Event{
		{ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "first", AuthorType: "human"},
	}
	model.activityCursor = -1

	prevKey := model.activityRowsForRenderKey(model.activity)
	cardsBefore := model.cachedActivityRowsForRender(model.activity)
	if !strings.Contains(strings.Join(cardsBefore, "\n"), "first") {
		t.Fatalf("expected original body rendered; got:\n%s", strings.Join(cardsBefore, "\n"))
	}

	// Edit the body to a different string of the same length (5 runes).
	model.activity[0].Body = "fifth"
	if len(model.activity[0].Body) != 5 {
		t.Fatalf("test invariant broken: edited body must keep length 5")
	}

	newKey := model.activityRowsForRenderKey(model.activity)
	if newKey == prevKey {
		t.Fatalf("same-length body edit did not bump cache key — stale card would be reused")
	}

	cardsAfter := model.cachedActivityRowsForRender(model.activity)
	joined := strings.Join(cardsAfter, "\n")
	if !strings.Contains(joined, "fifth") {
		t.Fatalf("activity card did not render edited body; got:\n%s", joined)
	}
	if strings.Contains(joined, "first") {
		t.Fatalf("activity card still shows stale body after same-length edit:\n%s", joined)
	}
}

// TestActivityCacheKeySameLengthBodyAcrossScopes proves the shared activity
// card renderer (task + project/universal feeds funnel through
// activityRowsForRenderKey) invalidates on a same-length body change for a
// project-scoped event, not just task-scoped — covers acceptance criterion 2.
func TestActivityCacheKeySameLengthBodyAcrossScopes(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.activityCursor = -1

	base := []domain.Event{{
		ID: 7, EntityType: domain.EventEntityProject, EntityID: 3,
		EventType: domain.EventTypeComment, Body: "alpha", AuthorType: "agent",
	}}
	edited := []domain.Event{{
		ID: 7, EntityType: domain.EventEntityProject, EntityID: 3,
		EventType: domain.EventTypeComment, Body: "omega", AuthorType: "agent",
	}}

	if model.activityRowsForRenderKey(base) == model.activityRowsForRenderKey(edited) {
		t.Fatal("project-scoped same-length body edit hashed identical — feed would show stale text")
	}
}
