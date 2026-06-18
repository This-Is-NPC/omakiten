package tui

import (
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// TestBoardBadgeCountsMemoEqualsFresh is the equals-fresh oracle for the
// precomputed board badge-count maps: after rebuildBoardCaches, the O(1)
// map lookups (boardDependencyCount / boardCommentCount /
// boardSubtaskCount) must return exactly what the original O(n) live
// scans (dependencyCount / commentCount / subtaskCount) return, BEFORE
// and AFTER a state mutation. The maps are the cached truth; the scans
// are the fresh truth — they must agree.
func TestBoardBadgeCountsMemoEqualsFresh(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.tasks = []domain.Task{
		{ID: 1, Title: "Parent", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "Child A", BucketKey: "dev", Priority: domain.Priority(2), ParentID: ptrInt64(1)},
		{ID: 3, Title: "Child B", BucketKey: "dev", Priority: domain.Priority(2), ParentID: ptrInt64(1)},
		{ID: 4, Title: "Lonely", BucketKey: "review", Priority: domain.Priority(2)},
	}
	model.dependencies = []domain.TaskDependency{
		{TaskID: 2, DependsOnTaskID: 1},
		{TaskID: 4, DependsOnTaskID: 1},
		{TaskID: 4, DependsOnTaskID: 2},
	}
	model.comments = []domain.Comment{
		{ID: 10, TaskID: 1, Body: "c1"},
		{ID: 11, TaskID: 1, Body: "c2"},
		{ID: 12, TaskID: 4, Body: "c3"},
	}
	model.rebuildBoardCaches()

	assertCountsMatch := func(t *testing.T, when string) {
		t.Helper()
		for _, task := range model.tasks {
			if got, want := model.boardDependencyCount(task.ID), model.dependencyCount(task.ID); got != want {
				t.Fatalf("%s: depCount(#%d) memo=%d fresh=%d", when, task.ID, got, want)
			}
			if got, want := model.boardCommentCount(task.ID), model.commentCount(task.ID); got != want {
				t.Fatalf("%s: commentCount(#%d) memo=%d fresh=%d", when, task.ID, got, want)
			}
			if got, want := model.boardSubtaskCount(task.ID), model.subtaskCount(task.ID); got != want {
				t.Fatalf("%s: subtaskCount(#%d) memo=%d fresh=%d", when, task.ID, got, want)
			}
		}
	}
	assertCountsMatch(t, "before mutation")

	// Mutate every slice the maps cover, then rebuild (the self-write
	// seam) and re-assert the maps still equal the fresh scans.
	model.dependencies = append(model.dependencies, domain.TaskDependency{TaskID: 3, DependsOnTaskID: 1})
	model.comments = append(model.comments, domain.Comment{ID: 13, TaskID: 2, Body: "c4"})
	model.tasks = append(model.tasks, domain.Task{ID: 5, Title: "Child C", BucketKey: "dev", Priority: domain.Priority(2), ParentID: ptrInt64(1)})
	model.rebuildBoardCaches()
	assertCountsMatch(t, "after mutation")

	// Sanity: the parent now has 3 children, and the map says so.
	if got := model.boardSubtaskCount(1); got != 3 {
		t.Fatalf("after mutation: parent #1 child count = %d, want 3", got)
	}
}

// TestBoardStringMemoEqualsFresh is the equals-fresh oracle for the
// full board-string memoization: the memoized renderBoard() output must
// be byte-identical to a fresh renderBoardUncached() across a state
// mutation. First render warms the memo; the second (same inputs) must be
// served from the cache yet still equal the uncached truth; after a
// mutation + rebuild, the memo invalidates and the new memoized string
// must again equal the fresh render.
func TestBoardStringMemoEqualsFresh(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 120
	model.height = 30
	model.tasks = []domain.Task{
		{ID: 1, Title: "Alpha", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "Beta", BucketKey: "dev", Priority: domain.Priority(2)},
	}
	model.dependencies = []domain.TaskDependency{{TaskID: 2, DependsOnTaskID: 1}}
	model.rebuildBoardCaches()
	model.colIdx = 0
	model.cardIdx = 0

	first := model.renderBoard()
	if !model.boardStringCache.valid {
		t.Fatalf("first renderBoard did not warm the memo")
	}
	warmKey := model.boardStringCache.key

	// Same inputs → served from memo, and the memo equals the fresh
	// uncached render.
	second := model.renderBoard()
	if model.boardStringCache.key != warmKey {
		t.Fatalf("identical inputs rebuilt the memo (key moved)")
	}
	if second != first {
		t.Fatalf("memoized render differs from prior memoized render")
	}
	if fresh := model.renderBoardUncached(); second != fresh {
		t.Fatalf("memoized board string != fresh uncached render")
	}

	// Mutate + rebuild (self-write seam) → epoch bumps → memo
	// invalidates → new memoized string equals the fresh render. The new
	// card lands in "dev", a lane the test workflow actually renders, so
	// the board string genuinely changes.
	model.tasks = append(model.tasks, domain.Task{ID: 3, Title: "Gamma", BucketKey: "dev", Priority: domain.Priority(2)})
	model.rebuildBoardCaches()
	mutated := model.renderBoard()
	if mutated == first {
		t.Fatalf("memo served stale board string after mutation")
	}
	if fresh := model.renderBoardUncached(); mutated != fresh {
		t.Fatalf("post-mutation memoized string != fresh uncached render")
	}
}

// TestBoardStringMemoStaleAfterSelfWrite is the critical council
// requirement: the memo key folds the LOCAL mutation epoch, not the
// data_version watermark. A same-connection self-write does NOT move the
// watermark, so a watermark-keyed memo would serve a stale board after a
// local edit. This test moves a task between buckets the way an inline
// m.refresh() self-write would (mutate the slice, rebuildBoardCaches) and
// proves the memoized render reflects the edit immediately.
func TestBoardStringMemoStaleAfterSelfWrite(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.width = 120
	model.height = 30
	model.tasks = []domain.Task{
		{ID: 1, Title: "Movable", BucketKey: "backlog", Priority: domain.Priority(2)},
	}
	model.rebuildBoardCaches()
	model.colIdx = 0
	model.cardIdx = 0

	before := model.renderBoard()
	epochBefore := model.boardMutationEpoch

	// Self-write: the task moves backlog → dev. The data_version
	// watermark would NOT move for this same-connection write; only the
	// local mutation epoch (bumped inside rebuildBoardCaches) does.
	model.tasks[0].BucketKey = "dev"
	model.rebuildBoardCaches()
	if model.boardMutationEpoch == epochBefore {
		t.Fatalf("rebuildBoardCaches did not bump the local mutation epoch")
	}

	after := model.renderBoard()
	if after == before {
		t.Fatalf("board memo served a stale render after a self-write " +
			"(epoch key failed to invalidate — would regress if keyed on data_version)")
	}
	if fresh := model.renderBoardUncached(); after != fresh {
		t.Fatalf("post-self-write memoized string != fresh uncached render")
	}
}

// TestPlanNetworkFullBuildMemoEqualsFresh is the equals-fresh oracle for
// the memoized full plan-network build (the critical-path DFS + cross-
// blocker index + next-claimable peek). The memoized planNetworkFullBuild
// must equal a fresh planNetworkBuildData(full opts) across a dependency
// mutation: warm the memo, prove the hit reuses it, then mutate the
// dependency set (which the DFS reads) and prove the rebuild matches the
// fresh truth.
func TestPlanNetworkFullBuildMemoEqualsFresh(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.planNetworkShow = app.PlanShow{
		Plan: domain.Plan{ID: 1, Slug: "p1", Name: "Plan One"},
		Waves: []app.PlanWaveView{
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
	fresh1 := model.planNetworkBuildData(planNetworkBuildOpts{})
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
	fresh2 := model.planNetworkBuildData(planNetworkBuildOpts{})
	assertCriticalPathEqual(t, "post-dep-mutation memo vs fresh", build2, fresh2)
}

// assertCriticalPathEqual compares the IsCritical flag + cross-blocker
// set + next-claimable id of two builds row-by-row. These are exactly the
// fields the memoized full build adds over the row-only projection, so
// equality here is the meaningful oracle.
func assertCriticalPathEqual(t *testing.T, when string, got, want planNetworkBuild) {
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
