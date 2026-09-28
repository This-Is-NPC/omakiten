package plannetwork

import (
	"testing"

	"omakiten/internal/domain"
	networkprojection "omakiten/internal/plannetwork"
)

// TestPlanNetworkFullBuildMemoEqualsFresh is the equals-fresh oracle for
// the memoized full plan-network build (the critical-path DFS + cross-
// blocker index + next-claimable peek). The memoized planNetworkFullBuild
// must equal a fresh projection across a dependency
// mutation: warm the memo, prove the hit reuses it, then mutate the
// dependency set (which the DFS reads) and prove the rebuild matches the
// fresh truth.
func TestPlanNetworkFullBuildMemoEqualsFresh(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.planNetworkShow = domain.PlanShow{
		Plan: domain.Plan{ID: 1, Slug: "p1", Name: "Plan One"},
		Waves: []domain.PlanWaveView{
			{
				Wave: domain.PlanWave{ID: 10, PlanID: 1, Name: "W1", Position: 1},
				Tasks: []domain.PlanTaskRow{
					{TaskID: 100, WaveID: 10, Title: "T1", BucketKey: "backlog"},
					{TaskID: 101, WaveID: 10, Title: "T2", BucketKey: "dev"},
					{TaskID: 102, WaveID: 10, Title: "T3", BucketKey: "dev"},
				},
			},
		},
		Dependencies: []domain.TaskDependency{
			{TaskID: 101, DependsOnTaskID: 100},
			{TaskID: 102, DependsOnTaskID: 101},
		},
	}

	build1 := model.planNetworkFullBuild()
	if !model.planNetworkBuildCache.valid {
		t.Fatalf("first full build did not warm the cache")
	}
	warmKey := model.planNetworkBuildCache.key

	// Cache hit on identical inputs.
	_ = model.planNetworkFullBuild()
	if model.planNetworkBuildCache.key != warmKey {
		t.Fatalf("identical inputs rebuilt the full-build cache")
	}

	// The warm build must equal a fresh full build.
	fresh1 := networkprojection.Build(model.planNetworkInput())
	assertCriticalPathEqual(t, "warm vs fresh", build1, fresh1)

	// Mutate the dependency edge set — an input ONLY the full build's
	// critical-path DFS reads (the row-only key omits it). Drop the
	// 101→102 chain so the critical path shortens.
	model.planNetworkShow.Dependencies = []domain.TaskDependency{
		{TaskID: 101, DependsOnTaskID: 100},
	}
	build2 := model.planNetworkFullBuild()
	if model.planNetworkBuildCache.key == warmKey {
		t.Fatalf("dependency mutation did not change the full-build key")
	}
	fresh2 := networkprojection.Build(model.planNetworkInput())
	assertCriticalPathEqual(t, "post-dep-mutation memo vs fresh", build2, fresh2)
}

// assertCriticalPathEqual compares the IsCritical flag + cross-blocker
// set + next-claimable id of two builds row-by-row. These are exactly the
// fields the memoized full build adds over the row-only projection, so
// equality here is the meaningful oracle.
func assertCriticalPathEqual(t *testing.T, when string, got, want networkprojection.Projection) {
	t.Helper()
	if got.NextClaimableID != want.NextClaimableID {
		t.Fatalf("%s: NextClaimableID memo=%d fresh=%d", when, got.NextClaimableID, want.NextClaimableID)
	}
	if len(got.Rows) != len(want.Rows) {
		t.Fatalf("%s: row count memo=%d fresh=%d", when, len(got.Rows), len(want.Rows))
	}
	for i := range got.Rows {
		if got.Rows[i].IsCritical != want.Rows[i].IsCritical {
			t.Fatalf("%s: row %d IsCritical memo=%v fresh=%v (task #%d)",
				when, i, got.Rows[i].IsCritical, want.Rows[i].IsCritical, got.Rows[i].Task.TaskID)
		}
		if got.Rows[i].BlockerCount != want.Rows[i].BlockerCount {
			t.Fatalf("%s: row %d BlockerCount memo=%d fresh=%d",
				when, i, got.Rows[i].BlockerCount, want.Rows[i].BlockerCount)
		}
	}
}
