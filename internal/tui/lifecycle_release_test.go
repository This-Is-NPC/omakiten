package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestCloseTaskScreenReleasesActivityCardsCache pins the W7 #226
// lifecycle hook: a task that opened then closed must not leave its
// rendered activity card slice in memory. The cache is keyed on
// (taskID, cursor, width, events) so the stale entry would survive
// the rest of the process without an explicit drop.
func TestCloseTaskScreenReleasesActivityCardsCache(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.taskID = 42
	model.activityForTask = 42
	model.activity = []domain.Event{
		{ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "hi", AuthorType: "human"},
	}
	model.activityCursor = -1

	_ = model.cachedActivityRowsForRender(model.activity)
	if !model.activityCardsCache.valid {
		t.Fatalf("cache should be populated before close")
	}

	model.closeTaskScreen("")

	if model.activityCardsCache.valid {
		t.Fatalf("closeTaskScreen left activityCardsCache populated; .valid=true")
	}
	if model.activity != nil {
		t.Fatalf("closeTaskScreen left m.activity populated")
	}
	if model.taskID != 0 {
		t.Fatalf("closeTaskScreen left m.taskID = %d, want 0", model.taskID)
	}
}
