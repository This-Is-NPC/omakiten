package plan

import (
	"testing"

	"omakiten/internal/domain"
)

func twoWaves() []domain.PlanWaveView {
	return []domain.PlanWaveView{
		{Wave: domain.PlanWave{ID: 100}, Tasks: []domain.PlanTaskRow{{TaskID: 10}, {TaskID: 11}}},
		{Wave: domain.PlanWave{ID: 200}, Tasks: []domain.PlanTaskRow{{TaskID: 20}}},
	}
}

// TestCrossWaveBlockersFiltersIntraEdges confirms the helper returns only edges
// whose source and destination tasks live in different waves. Intra-wave edges
// are excluded because the rail tree already surfaces them.
func TestCrossWaveBlockersFiltersIntraEdges(t *testing.T) {
	deps := []domain.TaskDependency{
		{TaskID: 11, DependsOnTaskID: 10}, // intra W1 — excluded
		{TaskID: 20, DependsOnTaskID: 10}, // cross W1→W2 — kept
	}
	blockers := CrossWaveBlockers(deps, twoWaves())
	if len(blockers[20]) != 1 || blockers[20][0] != 10 {
		t.Fatalf("blockers[20] = %v, want [10]", blockers[20])
	}
	if _, ok := blockers[11]; ok {
		t.Fatalf("intra-wave edge leaked into cross-wave blockers: %+v", blockers)
	}
}

// TestIntraWaveBlockersKeepsOnlySameWaveEdges is the mirror scope: the same
// edge set read the other way round, so the two indices are proven to partition
// the edges rather than both answering the same question.
func TestIntraWaveBlockersKeepsOnlySameWaveEdges(t *testing.T) {
	deps := []domain.TaskDependency{
		{TaskID: 11, DependsOnTaskID: 10}, // intra W1 — kept
		{TaskID: 20, DependsOnTaskID: 10}, // cross W1→W2 — excluded
		{TaskID: 20, DependsOnTaskID: 99}, // one end outside the plan — dropped
	}
	blockers := IntraWaveBlockers(deps, twoWaves())
	if len(blockers[11]) != 1 || blockers[11][0] != 10 {
		t.Fatalf("blockers[11] = %v, want [10]", blockers[11])
	}
	if _, ok := blockers[20]; ok {
		t.Fatalf("cross-wave or unplaced edge leaked into intra-wave blockers: %+v", blockers)
	}
}

// TestExcludeIDStripsRailParent proves the helper drops the rail-parent id from
// the inline-annotation list so the same blocker doesn't surface twice (rail
// glyph + "← #N").
func TestExcludeIDStripsRailParent(t *testing.T) {
	got := ExcludeID([]int64{1, 2, 3}, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("excludeID = %v, want [1 3]", got)
	}
	if untouched := ExcludeID([]int64{1, 2, 3}, 0); len(untouched) != 3 {
		t.Fatalf("excludeID with zero parent should return slice unchanged, got %v", untouched)
	}
}

// TestPercentFloorsAndGuardsZero pins the wave-completion arithmetic, including
// the total that would otherwise divide by zero.
func TestPercentFloorsAndGuardsZero(t *testing.T) {
	cases := map[string]struct {
		done, total, want int
	}{
		"none done":   {0, 4, 0},
		"floors down": {1, 3, 33},
		"all done":    {4, 4, 100},
		"no tasks":    {0, 0, 0},
		"negative":    {1, -1, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Percent(tc.done, tc.total); got != tc.want {
				t.Fatalf("Percent(%d, %d) = %d, want %d", tc.done, tc.total, got, tc.want)
			}
		})
	}
}
