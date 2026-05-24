package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestBoardLaneCursorAlwaysVisibleAfterMigration pins the W11-B-2
// contract: the board's focused-lane cardlist.Model keeps the
// selected card visible across j/k navigation. The cardlist
// component owns the scroll field internally; the previous
// per-bucket m.boardScroll map has been replaced with
// m.boardLists[bucket]. After any keystroke, the cursor is
// guaranteed to sit inside the rendered slice — by construction,
// not by hand-rolled sync routines.
func TestBoardLaneCursorAlwaysVisibleAfterMigration(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 120
	model.height = 30

	// Seed a single-bucket workflow with a packed lane so the inner
	// viewport reservation kicks in.
	const taskCount = 25
	tasks := make([]domain.Task, taskCount)
	for i := range tasks {
		tasks[i] = domain.Task{
			ID:        int64(700 + i),
			Title:     "Card",
			BucketKey: "backlog",
			Priority:  domain.Priority(2),
		}
	}
	model.tasks = tasks
	model.rebuildBoardCaches()
	model.colIdx = 0
	model.cardIdx = 0
	model.syncFocusedColumnScroll()

	// Walk the cursor through every card. After every step, the
	// focused-lane cardlist's Scroll() must stay in card-index
	// range [0, taskCount-1] (the unit-mismatch the W11 refactor
	// exists to prevent), and the cursor must never sit above the
	// scroll offset.
	for step := 0; step < taskCount; step++ {
		if step > 0 {
			model.cardIdx++
			model.syncFocusedColumnScroll()
		}
		bucket, ok := model.focusedBucketKey()
		if !ok {
			t.Fatalf("step %d: no focused bucket", step)
		}
		list, exists := model.boardLists[bucket]
		if !exists {
			t.Fatalf("step %d: boardLists missing entry for focused bucket %q", step, bucket)
		}
		scroll := list.Scroll()
		cursor := list.Cursor()
		if scroll < 0 || scroll >= taskCount {
			t.Fatalf("step %d: boardLists[%q].Scroll=%d out of card-index range [0,%d)", step, bucket, scroll, taskCount)
		}
		if cursor < scroll {
			t.Fatalf("step %d: cursor=%d above scroll=%d (cursor scrolled off the top)", step, cursor, scroll)
		}
	}
}

// TestBoardLaneEmptyBucketDropsListEntry pins the housekeeping
// invariant from syncFocusedColumnScroll: a lane that drains to
// zero tasks must drop its boardLists entry so the map does not
// accumulate ghost entries as the user moves cards between lanes.
func TestBoardLaneEmptyBucketDropsListEntry(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 120
	model.height = 30

	tasks := []domain.Task{
		{ID: 800, Title: "A", BucketKey: "backlog", Priority: domain.Priority(2)},
	}
	model.tasks = tasks
	model.rebuildBoardCaches()
	model.colIdx = 0
	model.cardIdx = 0
	model.syncFocusedColumnScroll()
	if _, ok := model.boardLists["backlog"]; !ok {
		t.Fatalf("setup: expected boardLists[backlog] after sync")
	}

	model.tasks = nil
	model.rebuildBoardCaches()
	model.cardIdx = 0
	model.syncFocusedColumnScroll()
	if _, ok := model.boardLists["backlog"]; ok {
		t.Fatalf("emptied lane left stale boardLists entry")
	}
}
