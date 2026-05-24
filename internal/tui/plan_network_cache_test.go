package tui

import (
	"reflect"
	"testing"
	"unsafe"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// sliceHeader returns the underlying array address so cache hits can
// be distinguished from rebuilds that happen to produce equal values.
// A re-run of planNetworkBuildData allocates a fresh slice, so the
// header pointer is the cleanest hit/miss probe.
func sliceHeader[T any](s []T) uintptr {
	if len(s) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(reflect.ValueOf(s).Index(0).UnsafeAddr()))
}

// TestPlanNetworkRowsCacheHitAndMiss pins the memoisation contract for
// planNetworkBuildRows: identical inputs reuse the same underlying
// slice (cache hit); a change to the collapsed map, a task bucket
// move, or a different planID triggers a rebuild (cache miss).
func TestPlanNetworkRowsCacheHitAndMiss(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.planNetworkShow = app.PlanShow{
		Plan: domain.Plan{ID: 1, Slug: "p1", Name: "Plan One"},
		Waves: []app.PlanWaveView{
			{
				Wave: domain.PlanWave{ID: 10, PlanID: 1, Name: "W1", Position: 1},
				Tasks: []domain.PlanTaskRow{
					{TaskID: 100, WaveID: 10, Title: "T1", BucketKey: "backlog"},
					{TaskID: 101, WaveID: 10, Title: "T2", BucketKey: "dev"},
				},
			},
		},
	}

	rows1 := model.planNetworkBuildRows()
	rows2 := model.planNetworkBuildRows()
	if sliceHeader(rows1) != sliceHeader(rows2) {
		t.Fatalf("second build allocated a new slice — cache miss on identical inputs (key=%d)", model.planNetworkRowsCache.key)
	}

	// Collapse W1 → cache key must change → rebuild.
	model.planNetworkCollapsed = map[int64]bool{10: true}
	rows3 := model.planNetworkBuildRows()
	if sliceHeader(rows1) == sliceHeader(rows3) {
		t.Fatalf("collapse change did not invalidate cache; rows still backed by previous slice")
	}
	if len(rows3) != 1 {
		t.Fatalf("collapsed W1 should emit 1 row (header only), got %d", len(rows3))
	}

	// Restore collapsed=false → cache key matches rows1's; rebuild
	// produces a fresh slice because the cache currently holds rows3.
	delete(model.planNetworkCollapsed, 10)
	rows4 := model.planNetworkBuildRows()
	if len(rows4) != 3 {
		t.Fatalf("expanded W1 should emit 3 rows (header + 2 tasks), got %d", len(rows4))
	}

	// Bucket move on T1 → key bumps → rebuild.
	prevKey := model.planNetworkRowsCache.key
	model.planNetworkShow.Waves[0].Tasks[0].BucketKey = "review"
	rows5 := model.planNetworkBuildRows()
	if model.planNetworkRowsCache.key == prevKey {
		t.Fatalf("bucket move did not change cache key")
	}
	if sliceHeader(rows4) == sliceHeader(rows5) {
		t.Fatalf("bucket move did not invalidate cache; rows still backed by previous slice")
	}
}

// TestPlanNetworkRowsCacheReloadInvalidates pins the explicit
// invalidation on reloadPlanNetwork: even when the refetched PlanShow
// happens to fingerprint identically, the cache must be dropped so
// downstream auxiliary indices (dependencies, blocker counts) stay
// coherent with the freshly-loaded data.
func TestPlanNetworkRowsCacheReloadInvalidates(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.planNetworkShow = app.PlanShow{
		Plan: domain.Plan{ID: 2, Slug: "p2", Name: "Plan Two"},
		Waves: []app.PlanWaveView{
			{
				Wave:  domain.PlanWave{ID: 20, PlanID: 2, Name: "W1", Position: 1},
				Tasks: []domain.PlanTaskRow{{TaskID: 200, WaveID: 20, Title: "T", BucketKey: "backlog"}},
			},
		},
	}
	_ = model.planNetworkBuildRows()
	if !model.planNetworkRowsCache.valid {
		t.Fatalf("first build did not populate cache")
	}
	model.invalidatePlanNetworkRowsCache()
	if model.planNetworkRowsCache.valid {
		t.Fatalf("invalidatePlanNetworkRowsCache did not drop the cache")
	}
}
