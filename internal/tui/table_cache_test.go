package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
)

// TestRebuildBoardCachesPopulatesBucketsAndTableView pins the
// refresh-time cache populator: after rebuildBoardCaches runs,
// tasksByBucket and applyTableView short-circuit to the stored
// slices instead of re-filtering m.tasks on every call.
func TestRebuildBoardCachesPopulatesBucketsAndTableView(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.tasks = []domain.Task{
		{ID: 1, Title: "A", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "B", BucketKey: "dev", Priority: domain.Priority(3)},
	}
	model.rebuildBoardCaches()
	if model.cachedTasksByBucket == nil {
		t.Fatalf("cachedTasksByBucket is nil after rebuild")
	}
	if model.cachedTableView == nil {
		t.Fatalf("cachedTableView is nil after rebuild")
	}

	first := model.tasksByBucket()
	second := model.tasksByBucket()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("tasksByBucket returned different maps across calls")
	}
	if &first == &second { // map identity is meaningless; check the value reference instead
		t.Fatalf("unexpected reference comparison; reflect.DeepEqual is the contract here")
	}

	table1 := model.applyTableView()
	table2 := model.applyTableView()
	if len(table1) != 2 || len(table2) != 2 {
		t.Fatalf("applyTableView lengths = (%d, %d), want (2, 2)", len(table1), len(table2))
	}
	if sliceHeader(table1) != sliceHeader(table2) {
		t.Fatalf("applyTableView returned different backing arrays; cache did not short-circuit")
	}
}

// TestStyleWidthFromCacheHits memoises lipgloss.Style.Width(N) lookups
// across multiple card renders: repeat calls for the same width return
// the cached entry, and a fresh width populates a new entry.
func TestStyleWidthFromCacheHits(t *testing.T) {
	cache := map[int]lipgloss.Style{}
	base := lipgloss.NewStyle().Padding(0, 1)

	w26a := styleWidthFromCache(cache, base, 26)
	w26b := styleWidthFromCache(cache, base, 26)
	if !reflect.DeepEqual(w26a, w26b) {
		t.Fatalf("repeat lookup for width 26 returned different style")
	}
	if _, ok := cache[26]; !ok {
		t.Fatalf("width 26 entry missing from cache")
	}
	_ = styleWidthFromCache(cache, base, 32)
	if _, ok := cache[32]; !ok {
		t.Fatalf("new width 32 not cached")
	}
	if len(cache) != 2 {
		t.Fatalf("cache size = %d, want 2", len(cache))
	}
}

// TestStyleWidthFromCacheNilCacheStillRenders proves the nil-map
// safety: value-receiver render paths that get an uninitialised cache
// still get a valid Style back (degraded to the raw base.Width call).
func TestStyleWidthFromCacheNilCacheStillRenders(t *testing.T) {
	base := lipgloss.NewStyle().Padding(0, 1)
	got := styleWidthFromCache(nil, base, 18)
	want := base.Width(18)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nil-cache fallback did not return base.Width(18)")
	}
}
