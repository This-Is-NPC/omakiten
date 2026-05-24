package tui

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

// TestSubtasksFormHeightCacheHitAndMiss pins the W2 #246 fix: the
// stacked-layout sub-tasks viewport math must read the form box
// height from a per-Model cache instead of rendering the form box on
// every keystroke. Identical inputs share the same cached entry; a
// task switch or width change bumps the fingerprint and invalidates.
func TestSubtasksFormHeightCacheHitAndMiss(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	if len(model.tasks) == 0 {
		t.Fatalf("seed produced no tasks")
	}
	task := model.tasks[0]
	model.taskID = task.ID
	model.taskScreen = taskScreenView
	model.width = 80
	model.height = 24

	layout := model.computeTaskViewLayout(model.availableWidth(), true)

	// Prime via *Model: first call computes + caches; second hits.
	h1 := model.primeTaskDetailsBoxHeight(task, layout)
	if !model.taskDetailsBoxHeightCache.valid {
		t.Fatalf("cache not valid after prime")
	}
	keyAfterFirst := model.taskDetailsBoxHeightCache.key
	h2 := model.primeTaskDetailsBoxHeight(task, layout)
	if h1 != h2 {
		t.Fatalf("identical inputs produced different heights: %d vs %d", h1, h2)
	}
	if model.taskDetailsBoxHeightCache.key != keyAfterFirst {
		t.Fatalf("identical inputs bumped cache key — fingerprint not stable")
	}

	// Width change invalidates: same task, different formValueWidth.
	model.width = 120
	layout2 := model.computeTaskViewLayout(model.availableWidth(), true)
	if layout.formValueWidth == layout2.formValueWidth {
		t.Skip("width change did not alter layout.formValueWidth; cannot exercise invalidation here")
	}
	model.primeTaskDetailsBoxHeight(task, layout2)
	if model.taskDetailsBoxHeightCache.key == keyAfterFirst {
		t.Fatalf("width change did not bump cache key")
	}
}

// TestSubtasksFormHeightValueReceiverSeesWarmCache mirrors the
// activity cache contract: the value-receiver render path
// (subtasksViewportRows, taskFocusedSectionOffset) reads the cache
// the *Model handler primed. Bubbletea's value-copy semantics carry
// the cache forward into View() within the same Update→View tick.
func TestSubtasksFormHeightValueReceiverSeesWarmCache(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	if len(model.tasks) == 0 {
		t.Fatalf("seed produced no tasks")
	}
	task := model.tasks[0]
	model.taskID = task.ID
	model.taskScreen = taskScreenView
	model.width = 80
	model.height = 24

	layout := model.computeTaskViewLayout(model.availableWidth(), true)
	warm := model.primeTaskDetailsBoxHeight(task, layout)
	via := model.cachedTaskDetailsBoxHeight(task, layout)
	if warm != via {
		t.Fatalf("value-receiver read = %d, want %d (cache hit)", via, warm)
	}
}

// TestViewChangeRefreshRegistryShrinksAfterFold pins the W2 #3
// finding: applyRefreshAfterViewChange must deregister the cmd
// pointer from viewChangeRefreshRegistry so a long-running TUI
// session does not leak one entry per nav.
func TestViewChangeRefreshRegistryShrinksAfterFold(t *testing.T) {
	clearViewChangeRefreshRegistry()
	model := buildRefreshHotPathModel(t)
	model.top = topTasks
	model.sub = subBoard

	// Drive enough nav cycles to exercise the registry on every refresh.
	const navCount = 8
	for i := 0; i < navCount; i++ {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		got := updated.(Model)
		if cmd == nil {
			t.Fatalf("Update(/) nav %d returned nil cmd", i)
		}
		msg := cmd()
		folded, _ := got.Update(msg)
		model = folded.(Model)
	}

	after := countViewChangeRefreshRegistry()
	if after > 1 {
		t.Fatalf("viewChangeRefreshRegistry leaked %d entries after %d nav cycles (want <= 1)", after, navCount)
	}
}

// TestActivityRowsForRenderKeyDetectsTagSwap pins the W2 #5 finding:
// the activity card cache fingerprint must include each event's tag
// set so a same-length tag swap (e.g. remove tag 3, add tag 5) flips
// the key and invalidates the cached card slice. Without it, an
// agent that retags a comment leaves the activity panel rendering
// stale tag chips until the cursor moves.
func TestActivityRowsForRenderKeyDetectsTagSwap(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.taskID = 42
	model.activityForTask = 42
	model.activityCursor = -1

	withTagsA := []domain.Event{{
		ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "x",
		AuthorType: "human",
		Tags:       []domain.Tag{{ID: 3, Name: "alpha"}, {ID: 7, Name: "gamma"}},
	}}
	withTagsB := []domain.Event{{
		ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "x",
		AuthorType: "human",
		Tags:       []domain.Tag{{ID: 5, Name: "beta"}, {ID: 7, Name: "gamma"}},
	}}

	keyA := model.activityRowsForRenderKey(withTagsA)
	keyB := model.activityRowsForRenderKey(withTagsB)
	if keyA == keyB {
		t.Fatalf("tag swap did not bump key: A=%d B=%d", keyA, keyB)
	}

	// Reorder-only must NOT bump the key (sort by tag.ID is stable).
	withTagsAReordered := []domain.Event{{
		ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "x",
		AuthorType: "human",
		Tags:       []domain.Tag{{ID: 7, Name: "gamma"}, {ID: 3, Name: "alpha"}},
	}}
	keyAReordered := model.activityRowsForRenderKey(withTagsAReordered)
	if keyAReordered != keyA {
		t.Fatalf("tag reorder bumped key: A=%d reorder=%d (fingerprint is order-sensitive — sort tags by id)", keyA, keyAReordered)
	}

	// Untagged baseline differs from the tagged form.
	noTags := []domain.Event{{
		ID: 1, EntityID: 42, EventType: domain.EventTypeComment, Body: "x", AuthorType: "human",
	}}
	if model.activityRowsForRenderKey(noTags) == keyA {
		t.Fatalf("adding tags did not bump key")
	}
}

// clearViewChangeRefreshRegistry resets the package-level sync.Map so
// tests run in isolation regardless of prior test ordering. Production
// code never clears the registry — this is test-only.
func clearViewChangeRefreshRegistry() {
	viewChangeRefreshRegistry.Range(func(k, _ any) bool {
		viewChangeRefreshRegistry.Delete(k)
		return true
	})
}

// countViewChangeRefreshRegistry returns the live entry count.
func countViewChangeRefreshRegistry() int {
	var n atomic.Int64
	viewChangeRefreshRegistry.Range(func(_, _ any) bool {
		n.Add(1)
		return true
	})
	return int(n.Load())
}
