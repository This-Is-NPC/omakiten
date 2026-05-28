package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"

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
	if diff := cmp.Diff(first, second); diff != "" {
		t.Fatalf("tasksByBucket returned different maps across calls (-first +second):\n%s", diff)
	}
	if &first == &second { // map identity is meaningless; check the value reference instead
		t.Fatalf("unexpected reference comparison; deep equality is the contract here")
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
// across multiple card renders: repeat calls for the same (kind, width)
// pair return the cached entry, and a fresh width populates a new entry
// under the same kind without disturbing other kinds.
func TestStyleWidthFromCacheHits(t *testing.T) {
	cache := map[styleKind]map[int]lipgloss.Style{}
	base := lipgloss.NewStyle().Padding(0, 1)

	w26a := styleWidthFromCache(cache, styleKindCard, base, 26)
	w26b := styleWidthFromCache(cache, styleKindCard, base, 26)
	if !reflect.DeepEqual(w26a, w26b) {
		t.Fatalf("repeat lookup for width 26 returned different style")
	}
	if _, ok := cache[styleKindCard][26]; !ok {
		t.Fatalf("width 26 entry missing from cache")
	}
	_ = styleWidthFromCache(cache, styleKindCard, base, 32)
	if _, ok := cache[styleKindCard][32]; !ok {
		t.Fatalf("new width 32 not cached")
	}
	if len(cache[styleKindCard]) != 2 {
		t.Fatalf("inner cache size = %d, want 2", len(cache[styleKindCard]))
	}
}

// TestStyleWidthFromCacheNilCacheStillRenders proves the nil-map
// safety: value-receiver render paths that get an uninitialised cache
// still get a valid Style back (degraded to the raw base.Width call).
func TestStyleWidthFromCacheNilCacheStillRenders(t *testing.T) {
	base := lipgloss.NewStyle().Padding(0, 1)
	got := styleWidthFromCache(nil, styleKindInput, base, 18)
	want := base.Width(18)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nil-cache fallback did not return base.Width(18)")
	}
}

// TestStyleWidthFromCacheKindRouting pins the kind-routing contract:
// each styleKind owns an independent inner map, so two kinds at the
// same width never collide. Without per-kind partitioning, the second
// caller would read the first caller's cached entry and render with
// the wrong base style — the regression W8-1 explicitly guards.
func TestStyleWidthFromCacheKindRouting(t *testing.T) {
	cache := map[styleKind]map[int]lipgloss.Style{}
	cardBase := lipgloss.NewStyle().Padding(0, 1).Bold(false)
	selectedBase := lipgloss.NewStyle().Padding(0, 1).Bold(true)

	cardW := styleWidthFromCache(cache, styleKindCard, cardBase, 24)
	selectedW := styleWidthFromCache(cache, styleKindCardSelected, selectedBase, 24)
	if reflect.DeepEqual(cardW, selectedW) {
		t.Fatalf("kind routing collapsed: card and cardSelected at width 24 returned same style")
	}
	if _, ok := cache[styleKindCard][24]; !ok {
		t.Fatalf("card inner cache missing entry for width 24")
	}
	if _, ok := cache[styleKindCardSelected][24]; !ok {
		t.Fatalf("cardSelected inner cache missing entry for width 24")
	}
	if len(cache) != 2 {
		t.Fatalf("outer cache size = %d, want 2 (one per kind)", len(cache))
	}

	// Re-hit each kind at the same width: the cached entries return
	// without growth.
	_ = styleWidthFromCache(cache, styleKindCard, cardBase, 24)
	_ = styleWidthFromCache(cache, styleKindCardSelected, selectedBase, 24)
	if len(cache[styleKindCard]) != 1 || len(cache[styleKindCardSelected]) != 1 {
		t.Fatalf("repeat lookup grew the inner cache; want 1/1 got %d/%d", len(cache[styleKindCard]), len(cache[styleKindCardSelected]))
	}
}
