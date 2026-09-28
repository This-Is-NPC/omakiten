package plan

import (
	"sort"

	"omakiten/internal/domain"
)

// IntraWaveBlockers is the blocker map for dependency edges whose source AND
// destination tasks live in the same wave. The rail tree consumes it.
func IntraWaveBlockers(deps []domain.TaskDependency, waves []domain.PlanWaveView) map[int64][]int64 {
	return waveScopedBlockers(deps, waves, true)
}

// CrossWaveBlockers is the blocker map for dependency edges that CROSS wave
// boundaries. A reader surfaces these away from the rail tree — as an inline
// annotation, as a lane, or both.
func CrossWaveBlockers(deps []domain.TaskDependency, waves []domain.PlanWaveView) map[int64][]int64 {
	return waveScopedBlockers(deps, waves, false)
}

// waveScopedBlockers shares the wave-membership walk between the intra- and
// cross-wave indices. sameWave=true keeps edges within a wave; sameWave=false
// keeps the ones that cross. Blocker lists sort ascending so two reads of the
// same plan produce the same index. Edges touching a task no wave carries are
// dropped: an edge with one end outside the plan is neither intra nor cross.
func waveScopedBlockers(deps []domain.TaskDependency, waves []domain.PlanWaveView, sameWave bool) map[int64][]int64 {
	if len(deps) == 0 || len(waves) == 0 {
		return nil
	}
	taskToWave := make(map[int64]int64, len(deps))
	for _, wv := range waves {
		for _, t := range wv.Tasks {
			taskToWave[t.TaskID] = wv.Wave.ID
		}
	}
	blockers := map[int64][]int64{}
	for _, d := range deps {
		srcWave, sok := taskToWave[d.DependsOnTaskID]
		dstWave, dok := taskToWave[d.TaskID]
		if !sok || !dok {
			continue
		}
		if (srcWave == dstWave) != sameWave {
			continue
		}
		blockers[d.TaskID] = append(blockers[d.TaskID], d.DependsOnTaskID)
	}
	for k := range blockers {
		sort.Slice(blockers[k], func(i, j int) bool { return blockers[k][i] < blockers[k][j] })
	}
	return blockers
}

// ExcludeID drops `exclude` from `ids`. Returns the slice unchanged when
// `exclude` is zero or not in the slice — the caller that already draws one
// edge as structure uses it to keep that edge out of the textual list.
func ExcludeID(ids []int64, exclude int64) []int64 {
	if exclude == 0 || len(ids) == 0 {
		return ids
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}
